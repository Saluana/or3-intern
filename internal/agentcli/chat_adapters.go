package agentcli

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	runtimeEventContentDelta         = "content.delta"
	runtimeEventItemStarted          = "item.started"
	runtimeEventItemUpdated          = "item.updated"
	runtimeEventItemCompleted        = "item.completed"
	runtimeEventRequestOpened        = "request.opened"
	runtimeEventRequestResolved      = "request.resolved"
	runtimeEventTurnPlanUpdated      = "turn.plan.updated"
	runtimeEventTurnProposedDelta    = "turn.proposed.delta"
	runtimeEventTurnDiffUpdated      = "turn.diff.updated"
	runtimeEventTurnCompleted        = "turn.completed"
	runtimeEventRuntimeWarning       = "runtime.warning"
	runtimeEventRuntimeError         = "runtime.error"
	runtimeStreamAssistantText       = "assistant_text"
	runtimeStreamReasoningText       = "reasoning_text"
	runtimeStreamReasoningSummary    = "reasoning_summary_text"
	runtimeStreamPlanText            = "plan_text"
	runtimeStreamCommandOutput       = "command_output"
	runtimeStreamFileChangeOutput    = "file_change_output"
	runtimeStreamUnknown             = "unknown"
	runtimeItemAssistantMessage      = "assistant_message"
	runtimeItemReasoning             = "reasoning"
	runtimeItemPlan                  = "plan"
	runtimeItemCommandExecution      = "command_execution"
	runtimeItemFileChange            = "file_change"
	runtimeItemMCPToolCall           = "mcp_tool_call"
	runtimeItemWebSearch             = "web_search"
	runtimeItemCollabAgentToolCall   = "collab_agent_tool_call"
	runtimeItemDynamicToolCall       = "dynamic_tool_call"
	runtimeItemUnknown               = "unknown"
	runtimeRequestCommandApproval    = "command_execution_approval"
	runtimeRequestFileReadApproval   = "file_read_approval"
	runtimeRequestFileChangeApproval = "file_change_approval"
	runtimeRequestUnknown            = "unknown"
)

func (a *OpenCodeAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	args := []string{"run", "--format", "json"}
	if req.NativeSessionRef != "" {
		args = append(args, "--session", req.NativeSessionRef)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	mode := RunnerMode(req.Mode)
	if mode == RunnerModeSandboxAuto {
		args = append(args, "--dangerously-skip-permissions")
	}
	task := strings.TrimSpace(req.UserMessage)
	if req.ContinuationMode != ContinuationNative || task == "" {
		task = strings.TrimSpace(req.ReplayPrompt)
		if task == "" {
			task = strings.TrimSpace(req.UserMessage)
		}
	}
	args = append(args, task)
	return CommandSpec{
		RunnerID:    a.ID(),
		Binary:      a.spec.Binary,
		Args:        args,
		Cwd:         req.Cwd,
		OutputMode:  OutputJSON,
		ArgvPreview: append([]string{}, args...),
	}, nil
}

func (a *CodexAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	if req.ContinuationMode != ContinuationNative || strings.TrimSpace(req.NativeSessionRef) == "" {
		return a.BuildCommand(chatCommandRunRequest(a.ID(), req))
	}
	return a.buildNativeResumeChatCommand(req)
}

func (a *ClaudeAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	if req.ContinuationMode != ContinuationNative || strings.TrimSpace(req.NativeSessionRef) == "" {
		return a.BuildCommand(chatCommandRunRequest(a.ID(), req))
	}
	cmd, err := a.BuildCommand(chatCommandRunRequest(a.ID(), req))
	if err != nil {
		return CommandSpec{}, err
	}
	args := make([]string, 0, len(cmd.Args)+2)
	if len(cmd.Args) > 0 && cmd.Args[0] == "--bare" {
		args = append(args, "--bare", "--resume", strings.TrimSpace(req.NativeSessionRef))
		args = append(args, cmd.Args[1:]...)
	} else {
		args = append(args, "--resume", strings.TrimSpace(req.NativeSessionRef))
		args = append(args, cmd.Args...)
	}
	cmd.Args = args
	cmd.ArgvPreview = append([]string{}, args...)
	return cmd, nil
}

func (a *GeminiAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	if req.ContinuationMode != ContinuationNative {
		return a.buildReplayChatCommand(req)
	}
	return a.buildNativeChatCommand(req)
}

func (a *GeminiAdapter) buildReplayChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	task := chatCommandRunRequest(a.ID(), req).Task
	args := []string{"--prompt", task, "--output-format", "stream-json"}
	mode := RunnerMode(req.Mode)
	switch mode {
	case RunnerModeReview:
		args = append(args, "--approval-mode", "default")
	case RunnerModeSafeEdit, "":
		args = append(args, "--approval-mode", "auto_edit")
	case RunnerModeSandboxAuto:
		args = append(args, "--approval-mode", "yolo")
	default:
		return CommandSpec{}, fmt.Errorf("unsupported gemini mode %q", req.Mode)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	return CommandSpec{RunnerID: a.ID(), Binary: a.spec.Binary, Args: args, Cwd: req.Cwd, OutputMode: OutputJSONL, ArgvPreview: append([]string{}, args...)}, nil
}

func (a *CodexAdapter) buildNativeResumeChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	base := chatCommandRunRequest(a.ID(), req)
	mode := RunnerMode(req.Mode)
	args := make([]string, 0, 18)
	if mode != RunnerModeSandboxAuto {
		args = append(args, "--ask-for-approval", "never")
	}
	if req.Cwd != "" {
		args = append(args, "--cd", req.Cwd)
	}
	if permission, ok := runnerPermissionFromMeta(req.Meta); ok && permission.Access == runnerPermissionAccessWrite {
		args = append(args, "--add-dir", permission.TargetPath)
	}
	switch mode {
	case RunnerModeReview:
		args = append(args, "--sandbox", "read-only")
	case RunnerModeSafeEdit, "":
		args = append(args, "--sandbox", "workspace-write")
	case RunnerModeSandboxAuto:
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	default:
		return CommandSpec{}, fmt.Errorf("unsupported codex mode %q", req.Mode)
	}
	args = append(args, "exec", "resume", "--json", "--skip-git-repo-check")
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	args = append(args, strings.TrimSpace(req.NativeSessionRef), base.Task)
	return CommandSpec{RunnerID: a.ID(), Binary: a.spec.Binary, Args: args, Cwd: req.Cwd, OutputMode: OutputJSONL, ArgvPreview: append([]string{}, args...)}, nil
}

func (a *GeminiAdapter) buildNativeChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	task := chatCommandRunRequest(a.ID(), req).Task
	args := make([]string, 0, 12)
	if ref := strings.TrimSpace(req.NativeSessionRef); ref != "" {
		args = append(args, "--resume", ref)
	}
	args = append(args, "--prompt", task, "--output-format", "stream-json")
	mode := RunnerMode(req.Mode)
	switch mode {
	case RunnerModeReview:
		args = append(args, "--approval-mode", "default")
	case RunnerModeSafeEdit, "":
		args = append(args, "--approval-mode", "auto_edit")
	case RunnerModeSandboxAuto:
		args = append(args, "--approval-mode", "yolo")
	default:
		return CommandSpec{}, fmt.Errorf("unsupported gemini mode %q", req.Mode)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	return CommandSpec{RunnerID: a.ID(), Binary: a.spec.Binary, Args: args, Cwd: req.Cwd, OutputMode: OutputJSONL, ArgvPreview: append([]string{}, args...)}, nil
}

func (a *OpenCodeAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if raw.Type == "structured" {
		if events := normalizeOpenCodeStructuredChatEvent(raw); len(events) > 0 {
			return events
		}
		if isSuppressedOpenCodeStructuredEvent(raw.Payload) {
			return nil
		}
		return []RunnerChatEvent{{Type: "runner_output", Seq: raw.Seq, Stream: raw.Stream, Payload: rawEventPayload(raw)}}
	}
	if raw.Type == "output" && raw.Stream == "stdout" {
		trimmed := strings.TrimSpace(raw.Chunk)
		payloads, _ := decodeStructuredPayloads(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || len(payloads) > 0 {
			return []RunnerChatEvent{{
				Type:    "runner_output",
				Seq:     raw.Seq,
				Stream:  raw.Stream,
				Payload: rawEventPayload(raw),
			}}
		}
	}
	return normalizeGenericChatEvent(raw)
}

func (a *CodexAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if raw.Type == "structured" {
		if events := normalizeCodexStructuredChatEvent(raw); len(events) > 0 {
			return events
		}
		if isSuppressedCodexStructuredEvent(raw.Payload) {
			return nil
		}
		return []RunnerChatEvent{{Type: "runner_output", Seq: raw.Seq, Stream: raw.Stream, Payload: rawEventPayload(raw)}}
	}
	if raw.Type == "output" && raw.Stream == "stdout" {
		trimmed := strings.TrimSpace(raw.Chunk)
		payloads, _ := decodeStructuredPayloads(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || len(payloads) > 0 {
			return []RunnerChatEvent{{
				Type:    "runner_output",
				Seq:     raw.Seq,
				Stream:  raw.Stream,
				Payload: rawEventPayload(raw),
			}}
		}
	}
	return normalizeGenericChatEvent(raw)
}

func (a *ClaudeAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if raw.Type == "structured" {
		if events := normalizeClaudeStructuredChatEvent(raw); len(events) > 0 {
			return events
		}
		return []RunnerChatEvent{{Type: "runner_output", Seq: raw.Seq, Stream: raw.Stream, Payload: rawEventPayload(raw)}}
	}
	if raw.Type == "output" && raw.Stream == "stdout" {
		trimmed := strings.TrimSpace(raw.Chunk)
		payloads, _ := decodeStructuredPayloads(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || len(payloads) > 0 {
			return []RunnerChatEvent{{Type: "runner_output", Seq: raw.Seq, Stream: raw.Stream, Payload: rawEventPayload(raw)}}
		}
	}
	return normalizeGenericChatEvent(raw)
}

func (a *GeminiAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if raw.Type == "structured" {
		if events := normalizeGeminiStructuredChatEvent(raw); len(events) > 0 {
			return events
		}
		if isSuppressedGeminiStructuredEvent(raw.Payload) {
			return nil
		}
		return []RunnerChatEvent{{Type: "runner_output", Seq: raw.Seq, Stream: raw.Stream, Payload: rawEventPayload(raw)}}
	}
	if raw.Type == "output" && raw.Stream == "stdout" {
		trimmed := strings.TrimSpace(raw.Chunk)
		payloads, _ := decodeStructuredPayloads(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || len(payloads) > 0 {
			return []RunnerChatEvent{{Type: "runner_output", Seq: raw.Seq, Stream: raw.Stream, Payload: rawEventPayload(raw)}}
		}
	}
	return normalizeGenericChatEvent(raw)
}

func (a *OpenCodeAdapter) ExtractNativeSessionRef(event AgentRunEvent) (string, bool) {
	payload := strings.TrimSpace(extractOpenCodeSessionRefPayload(event))
	if payload == "" {
		return "", false
	}
	return payload, true
}

func (a *CodexAdapter) ExtractNativeSessionRef(event AgentRunEvent) (string, bool) {
	ref := extractProviderSessionRef(event, []string{"thread_id", "threadId"}, func(obj map[string]any) bool {
		return stringField(obj, "type") == "thread.started" || stringField(obj, "method") == "thread/started"
	})
	return ref, ref != ""
}

func (a *ClaudeAdapter) ExtractNativeSessionRef(event AgentRunEvent) (string, bool) {
	ref := extractProviderSessionRef(event, []string{"session_id", "sessionId"}, func(obj map[string]any) bool {
		return stringField(obj, "type") == "system" && stringField(obj, "subtype") == "init"
	})
	return ref, ref != ""
}

func (a *GeminiAdapter) ExtractNativeSessionRef(event AgentRunEvent) (string, bool) {
	ref := extractProviderSessionRef(event, []string{"session_id", "sessionId"}, func(obj map[string]any) bool {
		return stringField(obj, "type") == "init"
	})
	return ref, ref != ""
}

func chatCommandRunRequest(id RunnerID, req RunnerChatCommandRequest) AgentRunRequest {
	task := strings.TrimSpace(req.UserMessage)
	if req.ContinuationMode != ContinuationNative {
		task = strings.TrimSpace(req.ReplayPrompt)
	}
	if task == "" {
		task = strings.TrimSpace(req.UserMessage)
	}
	return AgentRunRequest{
		ParentSessionKey: req.SessionID,
		RunnerID:         string(id),
		Task:             task,
		TimeoutSeconds:   req.TimeoutSeconds,
		Cwd:              req.Cwd,
		Model:            req.Model,
		Mode:             req.Mode,
		Isolation:        req.Isolation,
		MaxTurns:         req.MaxTurns,
		Meta:             req.Meta,
	}
}

func normalizeGenericChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if raw.Type == "" {
		return nil
	}
	payload := rawEventPayload(raw)
	eventType := raw.Type
	text := raw.Chunk
	if raw.Type == "output" {
		eventType = "runner_output"
		if raw.Stream == "stdout" {
			eventType = "text_delta"
		}
	}
	if raw.Type == "completion" && raw.Status != "" {
		eventType = "completion"
	}
	return []RunnerChatEvent{{
		Type:    eventType,
		Seq:     raw.Seq,
		Stream:  raw.Stream,
		Text:    text,
		Payload: payload,
	}}
}

func normalizeOpenCodeStructuredChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if len(raw.Payload) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw.Payload, &obj); err != nil {
		return nil
	}
	switch stringField(obj, "type") {
	case "text":
		text, ok := openCodeTextPart(obj["part"])
		if !ok || text == "" {
			return nil
		}
		return []RunnerChatEvent{{
			Type:    "text_delta",
			Seq:     raw.Seq,
			Text:    text,
			Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload),
		}}
	case "assistant", "assistant_message":
		text := extractOpenCodeAssistantText(obj)
		if text == "" {
			return nil
		}
		return []RunnerChatEvent{{
			Type:    "text_delta",
			Seq:     raw.Seq,
			Text:    text,
			Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload),
		}}
	case "message.part.delta":
		text := extractString(firstNonNil(obj["delta"], obj["text"], obj["content"]))
		if text == "" {
			text = extractString(obj["part"])
		}
		if text == "" {
			return nil
		}
		return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: text, Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload)}}
	case "message.part.updated":
		return normalizeOpenCodePartUpdated(raw, obj)
	case "tool_use":
		return normalizeOpenCodeToolUse(raw, obj)
	case "permission.asked", "permission.replied", "question.asked", "question.replied", "session.error", "session.status":
		return normalizeOpenCodeRuntimeEvent(raw, obj)
	default:
		return nil
	}
}

func isSuppressedOpenCodeStructuredEvent(raw json.RawMessage) bool {
	obj, ok := rawObject(raw)
	if !ok {
		return false
	}
	switch stringField(obj, "type") {
	case "step_start", "step_finish", "step.completed", "step.started", "session.updated":
		return true
	default:
		return false
	}
}

func normalizeCodexStructuredChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if len(raw.Payload) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw.Payload, &obj); err != nil {
		return nil
	}
	providerType := stringField(obj, "type")
	method := firstNonEmptyStr(providerType, stringField(obj, "method"))
	switch providerType {
	case "item.completed":
		text := extractCodexAgentMessageText(obj)
		item := mapField(obj, "item")
		itemType := codexItemType(item)
		if itemType == runtimeItemPlan {
			plan := extractString(firstNonNil(item["text"], item["content"]))
			if plan == "" {
				return nil
			}
			return []RunnerChatEvent{{Type: runtimeEventTurnProposedDelta, Seq: raw.Seq, Text: plan, Payload: runtimePayload(runtimeEventTurnProposedDelta, map[string]any{"delta": plan}, raw.Payload)}}
		}
		if text != "" {
			return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: text, Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload)}}
		}
		return normalizeCodexLifecycleEvent(raw, obj, runtimeEventItemCompleted)
	case "item.started":
		return normalizeCodexLifecycleEvent(raw, obj, runtimeEventItemStarted)
	case "item.updated":
		return normalizeCodexLifecycleEvent(raw, obj, runtimeEventItemUpdated)
	case "turn.completed":
		return []RunnerChatEvent{{Type: runtimeEventTurnCompleted, Seq: raw.Seq, Payload: runtimePayload(runtimeEventTurnCompleted, map[string]any{"state": codexTurnState(obj)}, raw.Payload)}}
	case "turn.plan.updated":
		return []RunnerChatEvent{{Type: runtimeEventTurnPlanUpdated, Seq: raw.Seq, Payload: runtimePayload(runtimeEventTurnPlanUpdated, map[string]any{"plan": firstNonNil(obj["plan"], obj["steps"]), "explanation": extractString(obj["explanation"])}, raw.Payload)}}
	case "turn.diff.updated":
		diff := extractString(firstNonNil(obj["diff"], obj["unified_diff"], obj["unifiedDiff"]))
		return []RunnerChatEvent{{Type: runtimeEventTurnDiffUpdated, Seq: raw.Seq, Text: diff, Payload: runtimePayload(runtimeEventTurnDiffUpdated, map[string]any{"unified_diff": diff}, raw.Payload)}}
	case "error":
		msg := extractString(firstNonNil(obj["message"], obj["error"]))
		return []RunnerChatEvent{{Type: runtimeEventRuntimeError, Seq: raw.Seq, Text: msg, Payload: runtimePayload(runtimeEventRuntimeError, map[string]any{"message": msg}, raw.Payload)}}
	default:
		if event := normalizeCodexMethodEvent(raw, obj, method); len(event) > 0 {
			return event
		}
		return nil
	}
}

func isSuppressedCodexStructuredEvent(raw json.RawMessage) bool {
	obj, ok := rawObject(raw)
	if !ok {
		return false
	}
	switch stringField(obj, "type") {
	case "thread.started", "turn.started":
		return true
	default:
		return false
	}
}

func normalizeClaudeStructuredChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	obj, ok := rawObject(raw.Payload)
	if !ok {
		return nil
	}
	if stringField(obj, "type") == "assistant" || stringField(obj, "type") == "assistant_message" {
		text := extractString(firstNonNil(obj["message"], obj["content"], obj["text"]))
		if text != "" {
			return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: text, Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload)}}
		}
	}
	if stringField(obj, "type") == "content_block_delta" || stringField(obj, "type") == "delta" {
		text := extractString(firstNonNil(obj["text"], obj["delta"], mapField(obj, "delta")["text"]))
		if text != "" {
			return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: text, Payload: runtimeContentDeltaPayload(runtimeStreamKindFromClaude(obj), text, raw.Payload)}}
		}
	}
	if stringField(obj, "type") == "tool_use" || stringField(obj, "type") == "tool_result" {
		lifecycle := runtimeEventItemStarted
		status := "inProgress"
		if stringField(obj, "type") == "tool_result" {
			lifecycle = runtimeEventItemCompleted
			status = "completed"
		}
		name := extractString(firstNonNil(obj["name"], obj["tool_name"]))
		payload := toolLifecyclePayload(classifyToolName(name), status, name, extractString(firstNonNil(obj["input"], obj["content"], obj["result"])), obj)
		return []RunnerChatEvent{{Type: lifecycle, Seq: raw.Seq, Payload: runtimePayload(lifecycle, payload, raw.Payload)}}
	}
	if stringField(obj, "type") == "result" {
		status := "completed"
		if strings.TrimSpace(extractString(obj["error"])) != "" {
			status = "failed"
		}
		return []RunnerChatEvent{{Type: runtimeEventTurnCompleted, Seq: raw.Seq, Payload: runtimePayload(runtimeEventTurnCompleted, map[string]any{"state": status}, raw.Payload)}}
	}
	return nil
}

func normalizeGeminiStructuredChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	obj, ok := rawObject(raw.Payload)
	if !ok {
		return nil
	}
	eventType := strings.ToLower(stringField(obj, "type"))
	if eventType == "init" {
		return nil
	}
	if eventType == "result" {
		if !strings.EqualFold(stringField(obj, "status"), "error") {
			return nil
		}
		msg := extractString(firstNonNil(mapField(obj, "error")["message"], obj["message"], obj["error"]))
		return []RunnerChatEvent{{Type: runtimeEventRuntimeError, Seq: raw.Seq, Text: msg, Payload: runtimePayload(runtimeEventRuntimeError, map[string]any{"message": msg}, raw.Payload)}}
	}
	if eventType == "message" && strings.EqualFold(stringField(obj, "role"), "user") {
		return nil
	}
	text := extractGeminiAssistantText(obj)
	if eventType == "content" || eventType == "assistant" || eventType == "message" || (eventType == "" && text != "") {
		if text != "" {
			return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: text, Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload)}}
		}
	}
	if strings.Contains(strings.ToLower(eventType), "tool") {
		status := "inProgress"
		lifecycle := runtimeEventItemStarted
		if strings.Contains(strings.ToLower(eventType), "completed") || strings.Contains(strings.ToLower(eventType), "result") {
			status = "completed"
			lifecycle = runtimeEventItemCompleted
		}
		name := geminiToolName(obj)
		data := geminiToolData(obj, name)
		detail := extractString(firstNonNil(obj["arguments"], obj["parameters"], obj["input"], obj["output"], mapField(obj, "error")["message"]))
		return []RunnerChatEvent{{Type: lifecycle, Seq: raw.Seq, Payload: runtimePayload(lifecycle, toolLifecyclePayload(classifyToolName(name), status, name, detail, data), raw.Payload)}}
	}
	if eventType == "error" {
		msg := extractString(firstNonNil(obj["message"], obj["error"]))
		return []RunnerChatEvent{{Type: runtimeEventRuntimeError, Seq: raw.Seq, Text: msg, Payload: runtimePayload(runtimeEventRuntimeError, map[string]any{"message": msg}, raw.Payload)}}
	}
	return nil
}

func isSuppressedGeminiStructuredEvent(raw json.RawMessage) bool {
	obj, ok := rawObject(raw)
	if !ok {
		return false
	}
	eventType := strings.ToLower(stringField(obj, "type"))
	if eventType == "init" {
		return true
	}
	if eventType == "message" && strings.EqualFold(stringField(obj, "role"), "user") {
		return true
	}
	if eventType == "result" && !strings.EqualFold(stringField(obj, "status"), "error") {
		return true
	}
	return false
}

func geminiToolName(obj map[string]any) string {
	name := extractString(firstNonNil(obj["name"], obj["tool_name"], obj["toolName"]))
	if name != "" {
		return name
	}
	toolID := strings.TrimSpace(extractString(firstNonNil(obj["tool_id"], obj["toolId"], obj["id"])))
	if toolID == "" {
		return ""
	}
	parts := strings.Split(toolID, "_")
	if len(parts) >= 3 && isNumericString(parts[len(parts)-1]) && isNumericString(parts[len(parts)-2]) {
		return strings.Join(parts[:len(parts)-2], "_")
	}
	return toolID
}

func geminiToolData(obj map[string]any, name string) map[string]any {
	data := make(map[string]any, len(obj)+4)
	for key, value := range obj {
		data[key] = value
	}
	toolID := strings.TrimSpace(extractString(firstNonNil(obj["tool_id"], obj["toolId"], obj["id"])))
	if toolID != "" {
		data["id"] = toolID
		data["callID"] = toolID
	}
	if strings.TrimSpace(name) != "" {
		data["name"] = name
		data["tool"] = name
	}
	if parameters := mapField(obj, "parameters"); len(parameters) > 0 {
		data["input"] = parameters
	}
	if output := extractString(obj["output"]); output != "" {
		data["result"] = output
	}
	return data
}

func isNumericString(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func extractGeminiAssistantText(obj map[string]any) string {
	return extractGeminiAssistantValue(firstNonNil(
		obj["response"],
		obj["result"],
		obj["text"],
		obj["content"],
		obj["delta"],
		obj["message"],
	), 0)
}

func extractGeminiAssistantValue(value any, depth int) string {
	if depth > 4 {
		return ""
	}
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return ""
		}
		if nested := extractGeminiAssistantFromSerialized(trimmed, depth+1); nested != "" {
			return nested
		}
		return trimmed
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractGeminiAssistantValue(item, depth+1); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	case map[string]any:
		for _, key := range []string{"response", "result", "text", "content", "delta", "message"} {
			if text := extractGeminiAssistantValue(v[key], depth+1); text != "" {
				return text
			}
		}
	}
	return ""
}

func extractGeminiAssistantFromSerialized(text string, depth int) string {
	if depth > 4 {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	candidates := []string{trimmed}
	if strings.HasPrefix(trimmed, "\"") && strings.Contains(trimmed, "\":") && !strings.HasPrefix(trimmed, "\"{") {
		candidates = append(candidates, "{"+trimmed+"}")
	}
	for _, candidate := range candidates {
		var parsed any
		if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
			continue
		}
		if nested := extractGeminiAssistantValue(parsed, depth+1); nested != "" && nested != trimmed {
			return nested
		}
	}
	return ""
}

func openCodeTextPart(value any) (string, bool) {
	part, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	if partType := stringField(part, "type"); partType != "" && partType != "text" {
		return "", false
	}
	text, ok := part["text"].(string)
	return text, ok
}

func extractOpenCodeAssistantText(obj map[string]any) string {
	for _, key := range []string{"message", "content", "text"} {
		if text, ok := obj[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func extractCodexAgentMessageText(obj map[string]any) string {
	item, ok := obj["item"].(map[string]any)
	if !ok || stringField(item, "type") != "agent_message" {
		return ""
	}
	if text := extractString(item["text"]); text != "" {
		return text
	}
	return extractString(item["content"])
}

func rawEventPayload(raw AgentRunEvent) json.RawMessage {
	payload := raw.Payload
	if len(payload) == 0 {
		payload, _ = json.Marshal(map[string]any{
			"type":        raw.Type,
			"stream":      raw.Stream,
			"chunk":       raw.Chunk,
			"status":      raw.Status,
			"message":     raw.Message,
			"duration_ms": raw.DurationMS,
		})
	}
	return sanitizedRawPayload(payload)
}

func extractOpenCodeSessionRefPayload(event AgentRunEvent) string {
	if len(event.Payload) > 0 {
		if ref := extractOpenCodeSessionRefJSON(event.Payload); ref != "" {
			return ref
		}
	}
	chunk := strings.TrimSpace(event.Chunk)
	if strings.HasPrefix(chunk, "{") {
		if ref := extractOpenCodeSessionRefJSON([]byte(chunk)); ref != "" {
			return ref
		}
	}
	return ""
}

func extractOpenCodeSessionRefJSON(raw []byte) string {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(findSessionRef(payload))
}

func extractProviderSessionRef(event AgentRunEvent, keys []string, match func(map[string]any) bool) string {
	for _, raw := range eventJSONPayloads(event) {
		obj, ok := rawObject(raw)
		if !ok || !match(obj) {
			continue
		}
		for _, key := range keys {
			if value := strings.TrimSpace(stringField(obj, key)); value != "" {
				return value
			}
		}
		for _, nestedKey := range []string{"thread", "session", "data", "payload"} {
			nested := mapField(obj, nestedKey)
			for _, key := range keys {
				if value := strings.TrimSpace(stringField(nested, key)); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func eventJSONPayloads(event AgentRunEvent) []json.RawMessage {
	out := make([]json.RawMessage, 0, 2)
	if len(event.Payload) > 0 {
		out = append(out, event.Payload)
	}
	chunk := strings.TrimSpace(event.Chunk)
	if strings.HasPrefix(chunk, "{") {
		out = append(out, json.RawMessage(chunk))
	}
	return out
}

func rawObject(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, false
	}
	return sanitizeRawValue(obj).(map[string]any), true
}

func mapField(obj map[string]any, key string) map[string]any {
	if obj == nil {
		return nil
	}
	value, _ := obj[key].(map[string]any)
	return value
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
		}
		return value
	}
	return nil
}

func runtimePayload(eventType string, fields map[string]any, raw json.RawMessage) json.RawMessage {
	payload := map[string]any{"type": eventType}
	for key, value := range fields {
		if value == nil {
			continue
		}
		value = sanitizeRawValue(value)
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		payload[key] = value
	}
	if len(raw) > 0 {
		payload["raw"] = sanitizedRawObject(raw)
	}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func runtimeContentDeltaPayload(streamKind, delta string, raw json.RawMessage) json.RawMessage {
	return runtimePayload(runtimeEventContentDelta, map[string]any{"stream_kind": streamKind, "delta": truncateDiagnostic(delta)}, raw)
}

func toolLifecyclePayload(itemType, status, title, detail string, data any) map[string]any {
	return map[string]any{
		"item_type": itemType,
		"status":    status,
		"title":     title,
		"detail":    detail,
		"data":      data,
	}
}

func normalizeOpenCodePartUpdated(raw AgentRunEvent, obj map[string]any) []RunnerChatEvent {
	part := mapField(obj, "part")
	if len(part) == 0 {
		return nil
	}
	partType := strings.ToLower(stringField(part, "type"))
	if partType == "text" {
		text := extractString(part["text"])
		if text == "" {
			return nil
		}
		return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: text, Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, text, raw.Payload)}}
	}
	if partType != "tool" {
		return nil
	}
	toolName := firstNonEmptyStr(stringField(part, "tool"), stringField(part, "name"))
	state := mapField(part, "state")
	status := runtimeItemStatusFromProvider(firstNonEmptyStr(stringField(state, "status"), stringField(part, "status")))
	lifecycle := lifecycleFromStatus(status)
	detail := extractString(firstNonNil(state["title"], state["output"], state["error"], part["detail"]))
	payload := toolLifecyclePayload(classifyToolName(toolName), status, firstNonEmptyStr(extractString(state["title"]), toolName, "Tool"), detail, part)
	return []RunnerChatEvent{{Type: lifecycle, Seq: raw.Seq, Payload: runtimePayload(lifecycle, payload, raw.Payload)}}
}

func normalizeOpenCodeToolUse(raw AgentRunEvent, obj map[string]any) []RunnerChatEvent {
	part := mapField(obj, "part")
	if len(part) == 0 {
		return nil
	}
	toolName := firstNonEmptyStr(stringField(part, "tool"), stringField(part, "name"), "Tool")
	state := mapField(part, "state")
	status := runtimeItemStatusFromProvider(stringField(state, "status"))
	lifecycle := lifecycleFromStatus(status)
	detail := extractString(firstNonNil(state["output"], state["error"], state["input"], part["detail"]))
	payload := toolLifecyclePayload(classifyToolName(toolName), status, toolName, detail, part)
	return []RunnerChatEvent{{Type: lifecycle, Seq: raw.Seq, Payload: runtimePayload(lifecycle, payload, raw.Payload)}}
}

func normalizeOpenCodeRuntimeEvent(raw AgentRunEvent, obj map[string]any) []RunnerChatEvent {
	switch stringField(obj, "type") {
	case "permission.asked":
		return []RunnerChatEvent{{Type: runtimeEventRequestOpened, Seq: raw.Seq, Payload: runtimePayload(runtimeEventRequestOpened, map[string]any{"request_type": runtimeRequestUnknown, "detail": extractString(firstNonNil(obj["permission"], obj["question"], obj["message"])), "args": obj}, raw.Payload)}}
	case "permission.replied":
		return []RunnerChatEvent{{Type: runtimeEventRequestResolved, Seq: raw.Seq, Payload: runtimePayload(runtimeEventRequestResolved, map[string]any{"request_type": runtimeRequestUnknown, "decision": extractString(firstNonNil(obj["decision"], obj["answer"])), "resolution": obj}, raw.Payload)}}
	case "question.asked":
		return []RunnerChatEvent{{Type: "user-input.requested", Seq: raw.Seq, Payload: runtimePayload("user-input.requested", map[string]any{"questions": firstNonNil(obj["questions"], obj["question"]), "detail": extractString(obj["question"])}, raw.Payload)}}
	case "question.replied":
		return []RunnerChatEvent{{Type: "user-input.resolved", Seq: raw.Seq, Payload: runtimePayload("user-input.resolved", map[string]any{"answers": firstNonNil(obj["answers"], obj["answer"])}, raw.Payload)}}
	case "session.error":
		message := extractString(firstNonNil(obj["message"], obj["error"]))
		return []RunnerChatEvent{{Type: runtimeEventRuntimeError, Seq: raw.Seq, Text: message, Payload: runtimePayload(runtimeEventRuntimeError, map[string]any{"message": message}, raw.Payload)}}
	case "session.status":
		if strings.EqualFold(extractString(firstNonNil(obj["status"], obj["state"])), "idle") {
			return []RunnerChatEvent{{Type: runtimeEventTurnCompleted, Seq: raw.Seq, Payload: runtimePayload(runtimeEventTurnCompleted, map[string]any{"state": "completed"}, raw.Payload)}}
		}
	}
	return nil
}

func normalizeCodexLifecycleEvent(raw AgentRunEvent, obj map[string]any, lifecycle string) []RunnerChatEvent {
	item := mapField(obj, "item")
	if len(item) == 0 {
		item = mapField(mapField(obj, "payload"), "item")
	}
	if len(item) == 0 {
		return nil
	}
	itemType := codexItemType(item)
	if itemType == runtimeItemUnknown && lifecycle != runtimeEventItemUpdated {
		return nil
	}
	status := "inProgress"
	if lifecycle == runtimeEventItemCompleted {
		status = "completed"
	}
	title := firstNonEmptyStr(extractString(item["title"]), itemTitle(itemType))
	detail := extractString(firstNonNil(item["text"], item["content"], item["command"], item["path"], item["name"]))
	payload := toolLifecyclePayload(itemType, status, title, detail, item)
	return []RunnerChatEvent{{Type: lifecycle, Seq: raw.Seq, Text: detail, Payload: runtimePayload(lifecycle, payload, raw.Payload)}}
}

func normalizeCodexMethodEvent(raw AgentRunEvent, obj map[string]any, method string) []RunnerChatEvent {
	method = strings.TrimSpace(method)
	params := mapField(obj, "params")
	delta := extractDeltaString(firstNonNil(obj["delta"], obj["text_delta"], obj["textDelta"], params["delta"], params["text"]))
	switch method {
	case "item/agentMessage/delta":
		if delta == "" {
			return nil
		}
		return []RunnerChatEvent{{Type: "text_delta", Seq: raw.Seq, Text: delta, Payload: runtimeContentDeltaPayload(runtimeStreamAssistantText, delta, raw.Payload)}}
	case "item/reasoning/textDelta":
		if delta == "" {
			return nil
		}
		return []RunnerChatEvent{{Type: "reasoning_delta", Seq: raw.Seq, Text: delta, Payload: runtimeContentDeltaPayload(runtimeStreamReasoningText, delta, raw.Payload)}}
	case "item/reasoning/summaryTextDelta":
		if delta == "" {
			return nil
		}
		return []RunnerChatEvent{{Type: "reasoning_delta", Seq: raw.Seq, Text: delta, Payload: runtimeContentDeltaPayload(runtimeStreamReasoningSummary, delta, raw.Payload)}}
	case "item/plan/delta":
		if delta == "" {
			return nil
		}
		return []RunnerChatEvent{{Type: runtimeEventTurnProposedDelta, Seq: raw.Seq, Text: delta, Payload: runtimePayload(runtimeEventTurnProposedDelta, map[string]any{"delta": delta}, raw.Payload)}}
	case "item/commandExecution/outputDelta", "command/exec/outputDelta", "process/outputDelta":
		if delta == "" {
			return nil
		}
		delta = truncateDiagnostic(delta)
		return []RunnerChatEvent{{Type: runtimeEventContentDelta, Seq: raw.Seq, Text: delta, Payload: runtimeContentDeltaPayload(runtimeStreamCommandOutput, delta, raw.Payload)}}
	case "item/fileChange/outputDelta":
		if delta == "" {
			return nil
		}
		delta = truncateDiagnostic(delta)
		return []RunnerChatEvent{{Type: runtimeEventContentDelta, Seq: raw.Seq, Text: delta, Payload: runtimeContentDeltaPayload(runtimeStreamFileChangeOutput, delta, raw.Payload)}}
	case "item/commandExecution/requestApproval", "execCommandApproval":
		return []RunnerChatEvent{{Type: runtimeEventRequestOpened, Seq: raw.Seq, Payload: runtimePayload(runtimeEventRequestOpened, map[string]any{"request_type": runtimeRequestCommandApproval, "detail": extractString(firstNonNil(params["command"], params["reason"], obj["message"])), "args": firstNonNil(params, obj)}, raw.Payload)}}
	case "item/fileChange/requestApproval", "applyPatchApproval":
		return []RunnerChatEvent{{Type: runtimeEventRequestOpened, Seq: raw.Seq, Payload: runtimePayload(runtimeEventRequestOpened, map[string]any{"request_type": runtimeRequestFileChangeApproval, "detail": extractString(firstNonNil(params["reason"], params["path"], obj["message"])), "args": firstNonNil(params, obj)}, raw.Payload)}}
	case "serverRequest/resolved", "item/requestApproval/decision":
		return []RunnerChatEvent{{Type: runtimeEventRequestResolved, Seq: raw.Seq, Payload: runtimePayload(runtimeEventRequestResolved, map[string]any{"request_type": runtimeRequestUnknown, "decision": extractString(firstNonNil(params["decision"], obj["decision"])), "resolution": firstNonNil(params, obj)}, raw.Payload)}}
	case "turn/diff/updated":
		diff := extractString(firstNonNil(params["diff"], obj["diff"]))
		return []RunnerChatEvent{{Type: runtimeEventTurnDiffUpdated, Seq: raw.Seq, Text: diff, Payload: runtimePayload(runtimeEventTurnDiffUpdated, map[string]any{"unified_diff": diff}, raw.Payload)}}
	case "turn/plan/updated":
		return []RunnerChatEvent{{Type: runtimeEventTurnPlanUpdated, Seq: raw.Seq, Payload: runtimePayload(runtimeEventTurnPlanUpdated, map[string]any{"plan": firstNonNil(params["plan"], obj["plan"]), "explanation": extractString(firstNonNil(params["explanation"], obj["explanation"]))}, raw.Payload)}}
	case "error", "session/error":
		msg := extractString(firstNonNil(params["message"], params["error"], obj["message"], obj["error"]))
		return []RunnerChatEvent{{Type: runtimeEventRuntimeError, Seq: raw.Seq, Text: msg, Payload: runtimePayload(runtimeEventRuntimeError, map[string]any{"message": msg}, raw.Payload)}}
	}
	return nil
}

func extractDeltaString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractDeltaString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		for _, key := range []string{"delta", "text", "content"} {
			if text := extractDeltaString(v[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func codexItemType(item map[string]any) string {
	switch strings.ToLower(strings.ReplaceAll(firstNonEmptyStr(stringField(item, "type"), stringField(item, "kind")), "_", "")) {
	case "agentmessage", "assistantmessage", "message":
		return runtimeItemAssistantMessage
	case "reasoning":
		return runtimeItemReasoning
	case "plan":
		return runtimeItemPlan
	case "commandexecution", "command", "exec", "shell":
		return runtimeItemCommandExecution
	case "filechange", "patch", "edit", "write":
		return runtimeItemFileChange
	case "mcptoolcall", "mcp":
		return runtimeItemMCPToolCall
	case "websearch", "search":
		return runtimeItemWebSearch
	case "task", "agent", "subagent":
		return runtimeItemCollabAgentToolCall
	case "tool", "dynamictoolcall":
		return runtimeItemDynamicToolCall
	default:
		return runtimeItemUnknown
	}
}

func codexTurnState(obj map[string]any) string {
	status := strings.ToLower(extractString(firstNonNil(obj["status"], mapField(obj, "turn")["status"])))
	switch status {
	case "failed", "error":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	case "interrupted", "aborted":
		return "interrupted"
	default:
		return "completed"
	}
}

func runtimeStreamKindFromClaude(obj map[string]any) string {
	kind := strings.ToLower(firstNonEmptyStr(stringField(obj, "stream_kind"), stringField(obj, "streamKind"), stringField(obj, "kind")))
	if strings.Contains(kind, "reasoning") || strings.Contains(kind, "thinking") {
		return runtimeStreamReasoningText
	}
	return runtimeStreamAssistantText
}

const maxRawDiagnosticString = 4096

func sanitizedRawObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{"truncated": len(raw) > maxRawDiagnosticString, "text": truncateDiagnostic(string(raw))}
	}
	return sanitizeRawValue(value)
}

func sanitizedRawPayload(raw json.RawMessage) json.RawMessage {
	encoded, err := json.Marshal(sanitizedRawObject(raw))
	if err != nil {
		return json.RawMessage(`{"truncated":true}`)
	}
	return encoded
}

func sanitizeRawValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if isSensitiveRawKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = sanitizeRawValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, child := range v {
			out = append(out, sanitizeRawValue(child))
		}
		return out
	case string:
		return truncateDiagnostic(v)
	default:
		return value
	}
}

func isSensitiveRawKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range []string{"authorization", "password", "secret", "token", "api_key", "apikey"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func truncateDiagnostic(value string) string {
	if len(value) <= maxRawDiagnosticString {
		return value
	}
	return value[:maxRawDiagnosticString] + "…[truncated]"
}

func classifyToolName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "bash"), strings.Contains(lower, "shell"), strings.Contains(lower, "command"), strings.Contains(lower, "exec"):
		return runtimeItemCommandExecution
	case strings.Contains(lower, "edit"), strings.Contains(lower, "write"), strings.Contains(lower, "file"), strings.Contains(lower, "patch"):
		return runtimeItemFileChange
	case strings.Contains(lower, "mcp"):
		return runtimeItemMCPToolCall
	case strings.Contains(lower, "search"), strings.Contains(lower, "web"):
		return runtimeItemWebSearch
	case strings.Contains(lower, "task"), strings.Contains(lower, "agent"), strings.Contains(lower, "subagent"):
		return runtimeItemCollabAgentToolCall
	case lower == "":
		return runtimeItemUnknown
	default:
		return runtimeItemDynamicToolCall
	}
}

func runtimeItemStatusFromProvider(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "done", "success", "succeeded":
		return "completed"
	case "failed", "error", "errored":
		return "failed"
	case "declined", "denied", "rejected":
		return "declined"
	default:
		return "inProgress"
	}
}

func lifecycleFromStatus(status string) string {
	switch status {
	case "completed", "failed", "declined":
		return runtimeEventItemCompleted
	default:
		return runtimeEventItemUpdated
	}
}

func itemTitle(itemType string) string {
	switch itemType {
	case runtimeItemAssistantMessage:
		return "Assistant message"
	case runtimeItemReasoning:
		return "Reasoning"
	case runtimeItemPlan:
		return "Plan"
	case runtimeItemCommandExecution:
		return "Command run"
	case runtimeItemFileChange:
		return "File change"
	case runtimeItemMCPToolCall:
		return "MCP tool call"
	case runtimeItemWebSearch:
		return "Web search"
	case runtimeItemCollabAgentToolCall:
		return "Subagent task"
	case runtimeItemDynamicToolCall:
		return "Tool call"
	default:
		return "Tool"
	}
}

func findSessionRef(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"sessionID", "sessionId", "session_id", "id"} {
			if text, ok := v[key].(string); ok && strings.TrimSpace(text) != "" {
				if key != "id" || looksSessionID(text) {
					return text
				}
			}
		}
		for _, key := range []string{"session", "info", "data"} {
			if nested := findSessionRef(v[key]); nested != "" {
				return nested
			}
		}
	case []any:
		for _, item := range v {
			if nested := findSessionRef(item); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func looksSessionID(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "session_") || strings.HasPrefix(trimmed, "ses_") || strings.HasPrefix(trimmed, "sess_") || len(trimmed) >= 8
}

var _ RunnerChatAdapter = (*OpenCodeAdapter)(nil)
var _ RunnerChatAdapter = (*CodexAdapter)(nil)
var _ RunnerChatAdapter = (*ClaudeAdapter)(nil)
var _ RunnerChatAdapter = (*GeminiAdapter)(nil)
var _ NativeRunnerChatAdapter = (*OpenCodeAdapter)(nil)
var _ NativeRunnerChatAdapter = (*CodexAdapter)(nil)
var _ NativeRunnerChatAdapter = (*ClaudeAdapter)(nil)
var _ NativeRunnerChatAdapter = (*GeminiAdapter)(nil)
