package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

func (r *Runtime) executeConversation(ctx context.Context, eventType bus.EventType, sessionKey string, messages []providers.ChatMessage, reg *tools.Registry, channel string, replyTo string, replyMeta map[string]any) (string, bool, error) {
	messages = append([]providers.ChatMessage(nil), messages...)
	if reg == nil {
		reg = tools.NewRegistry()
	}
	observer := conversationObserverFromContext(ctx)
	scopeKey := sessionKey
	if r.DB != nil && strings.TrimSpace(sessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, sessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	messageQuotas := &quotaCounters{}
	maxLoops := r.MaxToolLoops
	if maxLoops <= 0 {
		maxLoops = 6
	}
	maxLoops = ToolBudgetOverridesFromContext(ctx).EffectiveMaxToolLoops(maxLoops)
	loopLimit := maxLoops
	validationFailures := map[string]int{}
	for loop := 0; ; loop++ {
		if loop >= loopLimit {
			if err := r.handleToolLoopLimitExceeded(ctx, sessionKey, loopLimit); err != nil {
				return "", false, err
			}
			loopLimit += maxLoops
		}
		turnTools := r.exposedToolsForTurn(ctx, reg, messages, channel)
		modelCfg := r.modelConfigForEvent(eventType)
		if modelCfg.Provider == nil {
			return "", false, fmt.Errorf("provider not configured")
		}
		profile := modelCfg.Provider.ProviderProfile(modelCfg.Model)
		toolDefs, sanitizerReports := toProviderToolDefs(turnTools, profile)
		for _, report := range sanitizerReports {
			log.Printf("provider tool schema sanitized: profile=%s %s", profile.Name, report.String())
		}
		req := providers.ChatCompletionRequest{
			Model:       modelCfg.Model,
			Messages:    messages,
			Tools:       toolDefs,
			Temperature: modelCfg.Temperature,
		}

		var resp providers.ChatCompletionResponse
		var err error
		var sw channels.StreamWriter
		var swOnce sync.Once
		streamer := r.streamerForContext(ctx)
		if streamer != nil {
			resp, err = modelCfg.Provider.ChatStream(ctx, req, func(text string) {
				if observer != nil {
					observer.OnTextDelta(ctx, text)
				}
				swOnce.Do(func() {
					streamMeta := channels.CloneMeta(replyMeta)
					if streamMeta == nil {
						streamMeta = map[string]any{}
					}
					streamMeta["channel"] = channel
					w, beginErr := streamer.BeginStream(ctx, replyTo, streamMeta)
					if beginErr == nil {
						sw = w
					}
				})
				if sw != nil {
					_ = sw.WriteDelta(ctx, text)
				}
			})
		} else {
			resp, err = modelCfg.Provider.Chat(ctx, req)
		}
		if err != nil {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			if observer != nil {
				observer.OnError(ctx, err)
			}
			return "", false, err
		}
		if len(resp.Choices) == 0 {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			err = fmt.Errorf("no choices")
			if observer != nil {
				observer.OnError(ctx, err)
			}
			return "", false, err
		}
		msg := resp.Choices[0].Message
		toolCallSource := ToolCallSourceProvider
		if len(msg.ToolCalls) == 0 {
			if raw, ok := msg.Content.(string); ok {
				if calls := parseToolMarkupCalls(raw, fmt.Sprintf("markup_%d", loop+1)); len(calls) > 0 {
					msg.ToolCalls = calls
					msg.Content = sanitizeToolTurnContent(raw)
					toolCallSource = ToolCallSourceMarkup
				}
			}
		}
		normalizedCalls := normalizeProviderToolCalls(msg.ToolCalls, toolCallSource, fmt.Sprintf("tool_%d", loop+1))
		if len(normalizedCalls) > 0 {
			availableCalls := availableNormalizedToolCalls(normalizedCalls, turnTools)
			if len(availableCalls) == 0 {
				if sw != nil {
					_ = sw.Abort(ctx)
				}
				for _, tc := range normalizedCalls {
					var parsedParams map[string]any
					_ = json.Unmarshal([]byte(tc.ArgumentsJSON), &parsedParams)
					toolOut := formatToolExecutionError(tc.Name, parsedParams, "", fmt.Errorf("tool not available in this turn"))
					emitToolCallStarted(ctx, observer, tc)
					emitToolCallFinished(ctx, observer, tc, toolOut, "", fmt.Errorf("tool not available in this turn"))
				}
				messages = append(messages, providers.ChatMessage{
					Role:    "system",
					Content: unavailableNormalizedToolCallPrompt(normalizedCalls, turnTools),
				})
				continue
			}
			normalizedCalls = availableCalls
			msg.ToolCalls = normalizedToProviderToolCalls(normalizedCalls)
		}
		if len(normalizedCalls) == 0 {
			finalText := strings.TrimSpace(contentToString(msg.Content))
			if sw != nil {
				_ = sw.Close(ctx, finalText)
				if observer != nil {
					observer.OnCompletion(ctx, finalText, true)
				}
				return finalText, true, nil
			}
			if observer != nil {
				observer.OnCompletion(ctx, finalText, false)
			}
			return finalText, false, nil
		}

		if sw != nil {
			_ = sw.Abort(ctx)
		}

		toolTurnContent := msg.Content
		if raw, ok := msg.Content.(string); ok {
			toolTurnContent = sanitizeToolTurnContent(raw)
		}

		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: toolTurnContent, ToolCalls: msg.ToolCalls})
		if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", sanitizeToolTurnContent(contentToString(msg.Content)), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("append assistant(tool_calls) failed: %v", err)
		}

		for _, tc := range normalizedCalls {
			emitToolCallStarted(ctx, observer, tc)
			toolCtx := tools.ContextWithSession(ctx, scopeKey)
			toolCtx = tools.ContextWithDelivery(toolCtx, channel, replyTo)
			toolCtx = tools.ContextWithDeliveryMeta(toolCtx, replyMeta)
			toolCtx = r.contextWithTrustedToolAccess(toolCtx, bus.Event{Type: eventType, SessionKey: sessionKey, Channel: channel})
			toolCtx = context.WithValue(toolCtx, messageQuotaCountersContextKey{}, messageQuotas)
			toolCtx = tools.ContextWithToolGuard(toolCtx, r.guardToolExecution)
			tool := turnTools.Get(tc.Name)
			validation := ToolArgumentValidator{}.ValidateAndCoerce(tool, tc.ArgumentsJSON)
			if len(validation.Errors) > 0 {
				out := formatToolValidationError(tc, validation)
				err := fmt.Errorf("tool argument validation failed")
				emitToolCallFinished(ctx, observer, tc, out, "", err)
				payload := map[string]any{
					"tool":         tc.Name,
					"tool_call_id": tc.ID,
					"args":         json.RawMessage([]byte(tc.ArgumentsJSON)),
					"public_code":  "tool_argument_validation_failed",
				}
				if _, appendErr := r.DB.AppendMessage(ctx, sessionKey, "tool", out, payload); appendErr != nil {
					log.Printf("append validation tool message failed: %v", appendErr)
				}
				messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: out})
				for _, validationErr := range validation.Errors {
					key := tc.Name + ":" + validationErr.Path + ":" + validationErr.Code
					validationFailures[key]++
					if validationFailures[key] >= 2 {
						return "", false, fmt.Errorf("tool argument validation failed repeatedly for %s at %s", tc.Name, validationErr.Path)
					}
				}
				continue
			}
			tc.ArgumentsJSON = validation.ArgumentsJSON
			out, err := turnTools.ExecuteParams(toolCtx, tc.Name, validation.Params)
			if err != nil {
				var parsedParams map[string]any
				_ = json.Unmarshal([]byte(tc.ArgumentsJSON), &parsedParams)
				out = formatToolExecutionError(tc.Name, parsedParams, out, err)
			}

			payload := map[string]any{
				"tool":         tc.Name,
				"tool_call_id": tc.ID,
				"args":         json.RawMessage([]byte(tc.ArgumentsJSON)),
			}
			if strings.HasPrefix(strings.TrimSpace(sessionKey), "doctor-app-") {
				if toolResult, ok := tools.DecodeToolResult(out); ok {
					payload["doctor_tool_result"] = toolResult
					payload["source"] = "doctor_tool"
				}
			}
			sendOut, preview, artifactID := r.boundTextResult(ctx, sessionKey, out)
			if artifactID != "" {
				payload["artifact_id"] = artifactID
				payload["preview"] = preview
			}
			emitToolCallFinished(ctx, observer, tc, out, artifactID, err)
			if _, err := r.DB.AppendMessage(ctx, sessionKey, "tool", sendOut, payload); err != nil {
				log.Printf("append tool message failed: %v", err)
			}
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: sendOut})
			var approvalErr *tools.ApprovalRequiredError
			if errors.As(err, &approvalErr) {
				if tools.RequestSourceFromContext(ctx) == tools.RequestSourceService {
					return "", false, err
				}
				finalText, streamed := r.narrateApprovalRequired(ctx, messages)
				return finalText, streamed, err
			}
			if finalText := terminalToolResultText(tc.Name, out); finalText != "" {
				if observer != nil {
					observer.OnCompletion(ctx, finalText, false)
				}
				return finalText, false, nil
			}
		}
	}
}

func (r *Runtime) streamerForContext(ctx context.Context) channels.StreamingChannel {
	if streamer := streamingChannelFromContext(ctx); streamer != nil {
		return streamer
	}
	return r.Streamer
}

func (r *Runtime) narrateApprovalRequired(ctx context.Context, messages []providers.ChatMessage) (string, bool) {
	if r == nil {
		return "", false
	}
	modelCfg := r.CurrentModelConfig()
	if modelCfg.Provider == nil {
		return "", false
	}
	prompt := append(append([]providers.ChatMessage{}, messages...), providers.ChatMessage{
		Role:    "system",
		Content: "The last tool result indicates that approval is required before work can continue. Briefly explain to the user what needs approval and why. Do not call any tools. Keep the reply short and concrete.",
	})
	resp, err := modelCfg.Provider.Chat(ctx, providers.ChatCompletionRequest{
		Model:       modelCfg.Model,
		Messages:    prompt,
		Temperature: modelCfg.Temperature,
	})
	if err != nil || len(resp.Choices) == 0 {
		return "", false
	}
	finalText := strings.TrimSpace(contentToString(resp.Choices[0].Message.Content))
	if finalText == "" {
		return "", false
	}
	if observer := conversationObserverFromContext(ctx); observer != nil {
		observer.OnCompletion(ctx, finalText, false)
	}
	return finalText, false
}

func (r *Runtime) handleToolLoopLimitExceeded(ctx context.Context, sessionKey string, currentLimit int) error {
	if r.effectiveToolLoopLimitAction() == config.QuotaExceededActionFail {
		return toolLoopLimitExceededError(sessionKey, currentLimit, "hard limit reached")
	}
	if r.ApprovalBroker == nil {
		return toolLoopLimitExceededError(sessionKey, currentLimit, "approval is configured, but the approval broker is unavailable")
	}
	identity := tools.RequesterIdentityFromContext(ctx)
	decision, err := r.ApprovalBroker.EvaluateToolQuota(ctx, approval.ToolQuotaEvaluation{
		Scope:         "message",
		LimitName:     "tool_loops",
		ToolName:      "tool loop continuation",
		Current:       currentLimit,
		Limit:         currentLimit,
		AgentID:       firstNonEmptyString(identity.Actor, "runtime"),
		SessionID:     sessionKey,
		ApprovalToken: tools.ApprovalTokenFromContext(ctx),
	}, config.ApprovalModeAsk)
	if err != nil {
		return err
	}
	if decision.Allowed {
		if r.Audit != nil {
			_ = r.Audit.Record(ctx, "tool_loop.override", sessionKey, "approval", map[string]any{
				"limit":        currentLimit,
				"subject_hash": decision.SubjectHash,
			})
		}
		return nil
	}
	if decision.RequiresApproval {
		return &tools.ApprovalRequiredError{ToolName: "tool loop continuation", RequestID: decision.RequestID}
	}
	return toolLoopLimitExceededError(sessionKey, currentLimit, decision.Reason)
}

func toolLoopLimitExceededError(sessionKey string, currentLimit int, reason string) error {
	resolvedSession := strings.TrimSpace(sessionKey)
	if resolvedSession == "" {
		resolvedSession = "unknown"
	}
	message := fmt.Sprintf("max tool loops exceeded for session %s after %d rounds", resolvedSession, currentLimit)
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		message += fmt.Sprintf(" (%s)", trimmed)
	}
	return errors.New(message)
}

func (r *Runtime) effectiveToolLoopLimitAction() config.QuotaExceededAction {
	if strings.EqualFold(string(r.MaxToolLoopsExceededAction), string(config.QuotaExceededActionFail)) {
		return config.QuotaExceededActionFail
	}
	return config.QuotaExceededActionAsk
}

func terminalToolResultText(toolName string, out string) string {
	if strings.TrimSpace(toolName) != "read_skill" || strings.TrimSpace(out) == "" {
		return ""
	}
	var result struct {
		OK      *bool  `json:"ok"`
		Kind    string `json:"kind"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return ""
	}
	if result.OK == nil || *result.OK || result.Kind != "skill_read" {
		return ""
	}
	summary := strings.TrimSpace(result.Summary)
	if summary == "" || !strings.Contains(strings.ToLower(summary), "unavailable") {
		return ""
	}
	return summary + ". I can't complete that with the tools currently available in this turn."
}

func toProviderToolDefs(reg *tools.Registry, profile providers.ProviderProfile) ([]providers.ToolDef, []providers.SchemaSanitizationReport) {
	if reg == nil {
		return nil, nil
	}
	raw := reg.Definitions()
	out := make([]providers.ToolDef, 0, len(raw))
	for _, d := range raw {
		fn, _ := d["function"].(map[string]any)
		td := providers.ToolDef{
			Type: "function",
			Function: providers.ToolFunc{
				Name:        fmt.Sprint(fn["name"]),
				Description: fmt.Sprint(fn["description"]),
				Parameters:  fn["parameters"],
			},
		}
		out = append(out, td)
	}
	return providers.SanitizeToolDefs(out, profile)
}
