package agentcli

import (
	"encoding/json"
	"strings"
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
	return a.BuildCommand(chatCommandRunRequest(a.ID(), req))
}

func (a *ClaudeAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	return a.BuildCommand(chatCommandRunRequest(a.ID(), req))
}

func (a *GeminiAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	return a.BuildCommand(chatCommandRunRequest(a.ID(), req))
}

func (a *OpenCodeAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	if raw.Type == "structured" {
		return normalizeOpenCodeStructuredChatEvent(raw)
	}
	if raw.Type == "output" && raw.Stream == "stdout" {
		trimmed := strings.TrimSpace(raw.Chunk)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || len(decodeStructuredPayloads(trimmed)) > 0 {
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
		return normalizeCodexStructuredChatEvent(raw)
	}
	if raw.Type == "output" && raw.Stream == "stdout" {
		trimmed := strings.TrimSpace(raw.Chunk)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || len(decodeStructuredPayloads(trimmed)) > 0 {
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
	return normalizeGenericChatEvent(raw)
}

func (a *GeminiAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
	return normalizeGenericChatEvent(raw)
}

func (a *OpenCodeAdapter) ExtractNativeSessionRef(event AgentRunEvent) (string, bool) {
	payload := strings.TrimSpace(extractOpenCodeSessionRefPayload(event))
	if payload == "" {
		return "", false
	}
	return payload, true
}

func chatCommandRunRequest(id RunnerID, req RunnerChatCommandRequest) AgentRunRequest {
	task := strings.TrimSpace(req.ReplayPrompt)
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
			Payload: raw.Payload,
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
			Payload: raw.Payload,
		}}
	default:
		return nil
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
	switch stringField(obj, "type") {
	case "item.completed":
		text := extractCodexAgentMessageText(obj)
		if text == "" {
			return nil
		}
		return []RunnerChatEvent{{
			Type:    "text_delta",
			Seq:     raw.Seq,
			Text:    text,
			Payload: raw.Payload,
		}}
	default:
		return nil
	}
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
	return payload
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
