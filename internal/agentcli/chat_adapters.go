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
	return normalizeGenericChatEvent(raw)
}

func (a *CodexAdapter) NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent {
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
