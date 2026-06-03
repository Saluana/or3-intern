// Package doctorbrain runs bounded provider+tool turns for Doctor internal admin brain
// without the legacy built-in runtime.
package doctorbrain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
	"log"
	"strings"

	"or3-intern/internal/actionresult"
	"or3-intern/internal/db"
	"or3-intern/internal/doctoradmin"
	"or3-intern/internal/providers"
	"or3-intern/internal/serviceerrors"
	"or3-intern/internal/streaming"
	"or3-intern/internal/tools"
)

const defaultMaxToolLoops = 48

// Config holds dependencies for a doctor admin brain turn.
type Config struct {
	DB          *db.DB
	Provider    *providers.Client
	Model       string
	Temperature float64
	Admin       *doctoradmin.Registry
	Allowed     []string
	MaxToolLoops int
	MaxToolBytes int
}

// TurnInput describes one doctor admin brain user turn.
type TurnInput struct {
	SessionKey   string
	SystemPrompt string
	UserMessage  string
}

// ExecuteTurn runs a doctor admin brain turn: provider chat with doctoradmin actions only.
func ExecuteTurn(ctx context.Context, cfg Config, input TurnInput, observer streaming.ConversationObserver) error {
	if cfg.DB == nil || cfg.Provider == nil || cfg.Admin == nil {
		return fmt.Errorf("doctor brain dependencies unavailable")
	}
	sessionKey := strings.TrimSpace(input.SessionKey)
	if sessionKey == "" {
		return fmt.Errorf("session key required")
	}
	maxLoops := cfg.MaxToolLoops
	if maxLoops <= 0 {
		maxLoops = defaultMaxToolLoops
	}
	toolDefs := cfg.Admin.ProviderToolDefs(cfg.Allowed)
	if len(toolDefs) == 0 {
		return fmt.Errorf("no doctor admin actions available")
	}

	messages, err := loadHistory(ctx, cfg.DB, sessionKey, input.SystemPrompt)
	if err != nil {
		return err
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: strings.TrimSpace(input.UserMessage)})
	}

	for loop := 0; loop < maxLoops; loop++ {
		resp, err := cfg.Provider.Chat(ctx, providers.ChatCompletionRequest{
			Model:       strings.TrimSpace(cfg.Model),
			Messages:    messages,
			Tools:       toolDefs,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			if observer != nil {
				observer.OnError(ctx, err)
			}
			return err
		}
		if len(resp.Choices) == 0 {
			err = fmt.Errorf("no choices from provider")
			if observer != nil {
				observer.OnError(ctx, err)
			}
			return err
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText := strings.TrimSpace(contentString(msg.Content))
			if _, err := cfg.DB.AppendMessage(ctx, sessionKey, "assistant", finalText, nil); err != nil {
				log.Printf("doctorbrain: append assistant failed: %v", err)
			}
			if observer != nil {
				observer.OnCompletion(ctx, finalText, false)
			}
			return nil
		}

		messages = append(messages, providers.ChatMessage{
			Role:      "assistant",
			Content:   contentString(msg.Content),
			ToolCalls: msg.ToolCalls,
		})
		if _, err := cfg.DB.AppendMessage(ctx, sessionKey, "assistant", contentString(msg.Content), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("doctorbrain: append assistant tool_calls failed: %v", err)
		}

		for _, tc := range msg.ToolCalls {
			toolName := strings.TrimSpace(tc.Function.Name)
			argsJSON := strings.TrimSpace(tc.Function.Arguments)
			if argsJSON == "" {
				argsJSON = "{}"
			}
			toolCallID := strings.TrimSpace(tc.ID)
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("doctor_tool_%d", loop+1)
			}
			emitToolStarted(ctx, observer, toolName, argsJSON, toolCallID)

			var params map[string]any
			_ = json.Unmarshal([]byte(argsJSON), &params)
			out, execErr := cfg.Admin.Execute(ctx, toolName, params)
			if execErr != nil {
				out = encodeFailure(toolName, params, out, execErr)
			}
			out = boundOutput(out, cfg.MaxToolBytes)

			emitToolFinished(ctx, observer, toolName, argsJSON, toolCallID, out, execErr)
			payload := map[string]any{
				"tool":         toolName,
				"tool_call_id": toolCallID,
				"args":         json.RawMessage([]byte(argsJSON)),
				"source":       "doctor_tool",
			}
			if toolResult, ok := actionresult.Decode(out); ok {
				payload["doctor_tool_result"] = toolResult
			}
			if _, err := cfg.DB.AppendMessage(ctx, sessionKey, "tool", out, payload); err != nil {
				log.Printf("doctorbrain: append tool failed: %v", err)
			}
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: toolCallID, Content: out})

			var approvalErr *tools.ApprovalRequiredError
			if errors.As(execErr, &approvalErr) {
				return execErr
			}
		}
	}
	err = fmt.Errorf("doctor admin brain exceeded tool loop limit")
	if observer != nil {
		observer.OnError(ctx, err)
	}
	return err
}

func loadHistory(ctx context.Context, database *db.DB, sessionKey, systemPrompt string) ([]providers.ChatMessage, error) {
	messages := []providers.ChatMessage{{Role: "system", Content: strings.TrimSpace(systemPrompt)}}
	rows, err := database.GetLastMessages(ctx, sessionKey, 80)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		role := strings.TrimSpace(row.Role)
		if role == "" || role == "system" {
			continue
		}
		msg := providers.ChatMessage{Role: role, Content: row.Content}
		if strings.TrimSpace(row.PayloadJSON) != "" {
			var payload map[string]any
			if json.Unmarshal([]byte(row.PayloadJSON), &payload) == nil {
				if raw, ok := payload["tool_calls"]; ok {
					if encoded, err := json.Marshal(raw); err == nil {
						var calls []providers.ToolCall
						if json.Unmarshal(encoded, &calls) == nil {
							msg.ToolCalls = calls
						}
					}
				}
				if rawID, ok := payload["tool_call_id"]; ok {
					msg.ToolCallID = strings.TrimSpace(fmt.Sprint(rawID))
				}
			}
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func contentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func boundOutput(text string, maxBytes int) string {
	text = strings.TrimSpace(text)
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	trunc := text[:maxBytes]
	for len(trunc) > 0 && !utf8.ValidString(trunc) {
		trunc = trunc[:len(trunc)-1]
	}
	return trunc + "..."
}

func encodeFailure(toolName string, params map[string]any, out string, err error) string {
	if strings.TrimSpace(out) != "" {
		return out
	}
	payload := map[string]any{
		"ok":      false,
		"tool":    toolName,
		"error":   err.Error(),
		"summary": err.Error(),
	}
	if code := serviceerrors.PublicErrorCode(err); code != "" {
		payload["public_code"] = code
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func emitToolStarted(ctx context.Context, observer streaming.ConversationObserver, name, args, id string) {
	if observer == nil {
		return
	}
	observer.OnToolCall(ctx, name, args)
	if lifecycle, ok := observer.(streaming.ToolLifecycleObserver); ok {
		lifecycle.OnToolLifecycle(ctx, streaming.ToolLifecycleEvent{Name: name, Status: "running", ToolCallID: id, Arguments: args})
	}
}

func emitToolFinished(ctx context.Context, observer streaming.ConversationObserver, name, args, id, out string, err error) {
	if observer == nil {
		return
	}
	observer.OnToolResult(ctx, name, out, err)
	status := "completed"
	publicCode := ""
	if err != nil {
		status = "failed"
		publicCode = serviceerrors.PublicErrorCode(err)
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			status = "approval_required"
		}
	}
	if lifecycle, ok := observer.(streaming.ToolLifecycleObserver); ok {
		lifecycle.OnToolLifecycle(ctx, streaming.ToolLifecycleEvent{
			Name:       name,
			Status:     status,
			ToolCallID: id,
			Arguments:  args,
			Result:     out,
			PublicCode: publicCode,
		})
	}
}
