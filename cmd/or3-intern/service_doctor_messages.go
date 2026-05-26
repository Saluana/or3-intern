package main

import (
	"context"
	"encoding/json"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

const doctorAppSessionPrefix = "doctor-app-"

func isDoctorAppSessionKey(sessionKey string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionKey), doctorAppSessionPrefix)
}

func containsDoctorAdminBrainEnvelope(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	return strings.Contains(content, "Current doctor summary:") &&
		strings.Contains(content, "User message:")
}

func scrubDoctorUserMessageContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return content
	}
	if idx := strings.LastIndex(content, "\n\nUser message:\n"); idx >= 0 {
		after := strings.TrimSpace(content[idx+len("\n\nUser message:\n"):])
		if after != "" {
			return after
		}
	}
	if idx := strings.LastIndex(content, "User message:\n"); idx >= 0 {
		after := strings.TrimSpace(content[idx+len("User message:\n"):])
		if after != "" {
			return after
		}
	}
	return content
}

func doctorMessagePayload(source string, seq int64, extra map[string]any) map[string]any {
	payload := map[string]any{
		"source":     source,
		"doctor_seq": seq,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func (s *serviceServer) nextDoctorMessageSequence(ctx context.Context, sessionKey string) int64 {
	store := s.doctorDB()
	if store == nil {
		return 0
	}
	messages, err := store.GetLastMessages(ctx, sessionKey, 1)
	if err != nil || len(messages) == 0 {
		return 1
	}
	var maxSeq int64
	for _, message := range messages {
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(message.PayloadJSON)), &payload); err != nil {
			continue
		}
		switch v := payload["doctor_seq"].(type) {
		case float64:
			if int64(v) > maxSeq {
				maxSeq = int64(v)
			}
		case int64:
			if v > maxSeq {
				maxSeq = v
			}
		}
		if message.ID > maxSeq {
			maxSeq = message.ID
		}
	}
	return maxSeq + 1
}

func doctorToolResultFromMessage(m db.ChatMessage) (tools.ToolResult, bool) {
	if strings.TrimSpace(m.Role) != "tool" {
		return tools.ToolResult{}, false
	}
	var payload map[string]any
	if raw := strings.TrimSpace(m.PayloadJSON); raw != "" && raw != "{}" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	if payload != nil {
		if embedded, ok := payload["doctor_tool_result"].(map[string]any); ok {
			b, err := json.Marshal(embedded)
			if err == nil {
				var result tools.ToolResult
				if json.Unmarshal(b, &result) == nil && strings.TrimSpace(result.Kind) != "" {
					return result, true
				}
			}
		}
	}
	return tools.DecodeToolResult(m.Content)
}

func doctorAPIChatMessage(m db.ChatMessage) map[string]any {
	content := m.Content
	meta := map[string]any{}
	if raw := strings.TrimSpace(m.PayloadJSON); raw != "" && raw != "{}" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil && payload != nil {
			for key, value := range payload {
				meta[key] = value
			}
		}
	}
	if m.Role == "user" && isDoctorAppSessionKey(m.SessionKey) {
		content = scrubDoctorUserMessageContent(content)
	}
	if result, ok := doctorToolResultFromMessage(m); ok && strings.HasPrefix(result.Kind, "doctor_") {
		meta["doctor_tool_result"] = result
	}
	return map[string]any{
		"id":         m.ID,
		"session_key": m.SessionKey,
		"role":       m.Role,
		"content":    content,
		"created_at": m.CreatedAt,
		"meta":       meta,
	}
}

func doctorAPIChatMessages(messages []db.ChatMessage) []map[string]any {
	if len(messages) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		out = append(out, doctorAPIChatMessage(message))
	}
	return out
}
