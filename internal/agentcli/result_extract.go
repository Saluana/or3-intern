package agentcli

import (
	"encoding/json"
	"strings"
)

type finalTextExtractor struct {
	runnerID      RunnerID
	bestScore     int
	bestSequence  int
	bestCandidate string
}

func newFinalTextExtractor(runnerID RunnerID) *finalTextExtractor {
	return &finalTextExtractor{runnerID: runnerID}
}

func (e *finalTextExtractor) Consider(raw json.RawMessage) {
	if e == nil || len(raw) == 0 {
		return
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	score, candidate := extractFinalTextCandidate(e.runnerID, payload)
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || score <= 0 {
		return
	}
	e.bestSequence++
	if score > e.bestScore || (score == e.bestScore && e.bestSequence > 0) {
		e.bestScore = score
		e.bestCandidate = candidate
	}
}

func (e *finalTextExtractor) Text() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.bestCandidate)
}

func extractFinalTextCandidate(runnerID RunnerID, payload any) (int, string) {
	obj, ok := payload.(map[string]any)
	if !ok {
		return 0, ""
	}

	switch runnerID {
	case RunnerGemini:
		if response := extractString(obj["response"]); response != "" {
			return 100, response
		}
		if payloadType := stringField(obj, "type"); payloadType == "result" {
			if response := extractString(obj["response"]); response != "" {
				return 95, response
			}
			if result := extractString(obj["result"]); result != "" {
				return 90, result
			}
		}
	case RunnerClaude:
		if stringField(obj, "type") == "result" && stringField(obj, "subtype") == "success" {
			if result := extractString(obj["result"]); result != "" {
				return 100, result
			}
		}
		if stringField(obj, "type") == "assistant" {
			if message := extractClaudeAssistantText(obj["message"]); message != "" {
				return 85, message
			}
		}
	case RunnerCodex:
		if stringField(obj, "type") == "item.completed" {
			if item, ok := obj["item"].(map[string]any); ok && stringField(item, "type") == "agent_message" {
				if text := extractString(item["text"]); text != "" {
					return 100, text
				}
				if content := extractString(item["content"]); content != "" {
					return 95, content
				}
			}
		}
	case RunnerOpenCode:
		if payloadType := stringField(obj, "type"); payloadType == "assistant_message" || payloadType == "assistant" {
			if message := extractString(obj["message"]); message != "" {
				return 100, message
			}
			if content := extractString(obj["content"]); content != "" {
				return 95, content
			}
		}
	}

	return extractGenericFinalText(obj)
}

func extractGenericFinalText(obj map[string]any) (int, string) {
	payloadType := stringField(obj, "type")
	switch payloadType {
	case "assistant_message", "assistant":
		if message := extractString(obj["message"]); message != "" {
			return 90, message
		}
		if content := extractString(obj["content"]); content != "" {
			return 85, content
		}
	case "message":
		role := stringField(obj, "role")
		if role == "" {
			role = stringField(obj, "source")
		}
		if role == "assistant" || role == "model" {
			if message := extractString(obj["message"]); message != "" {
				return 80, message
			}
			if content := extractString(obj["content"]); content != "" {
				return 78, content
			}
			if text := extractString(obj["text"]); text != "" {
				return 75, text
			}
		}
	case "result":
		if response := extractString(obj["response"]); response != "" {
			return 92, response
		}
		if result := extractString(obj["result"]); result != "" {
			return 88, result
		}
	}

	if item, ok := obj["item"].(map[string]any); ok && stringField(item, "type") == "agent_message" {
		if text := extractString(item["text"]); text != "" {
			return 90, text
		}
		if content := extractString(item["content"]); content != "" {
			return 86, content
		}
	}

	if response := extractString(obj["response"]); response != "" {
		return 70, response
	}
	if result := extractString(obj["result"]); result != "" && payloadType != "tool_result" {
		return 68, result
	}
	if message := extractString(obj["message"]); message != "" && !looksMachineOriented(message) {
		return 60, message
	}
	if text := extractString(obj["text"]); text != "" && !looksMachineOriented(text) {
		return 55, text
	}

	return 0, ""
}

func extractClaudeAssistantText(value any) string {
	msg, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	content, ok := msg["content"].([]any)
	if !ok {
		return extractString(msg["content"])
	}
	parts := make([]string, 0, len(content))
	for _, item := range content {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringField(part, "type") != "text" {
			continue
		}
		if text := extractString(part["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func extractString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	case map[string]any:
		for _, key := range []string{"text", "message", "content", "response", "result"} {
			if text := extractString(v[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func stringField(obj map[string]any, key string) string {
	value, _ := obj[key].(string)
	return strings.TrimSpace(value)
}

func looksMachineOriented(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
