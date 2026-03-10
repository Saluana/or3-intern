package triggers

import (
	"encoding/json"
	"fmt"
	"strings"
)

const MetaKeyStructuredTasks = "structured_tasks"

type StructuredToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params,omitempty"`
}

type StructuredTaskEnvelope struct {
	Version int                  `json:"version,omitempty"`
	Tasks   []StructuredToolCall `json:"tasks"`
}

func StructuredTasksMap(env StructuredTaskEnvelope) map[string]any {
	tasks := make([]map[string]any, 0, len(env.Tasks))
	for _, task := range env.Tasks {
		tool := strings.TrimSpace(task.Tool)
		if tool == "" {
			continue
		}
		entry := map[string]any{"tool": tool}
		if len(task.Params) > 0 {
			params := make(map[string]any, len(task.Params))
			for key, value := range task.Params {
				trimmed := strings.TrimSpace(key)
				if trimmed == "" {
					continue
				}
				params[trimmed] = value
			}
			if len(params) > 0 {
				entry["params"] = params
			}
		}
		tasks = append(tasks, entry)
	}
	out := map[string]any{"tasks": tasks}
	if env.Version > 0 {
		out["version"] = env.Version
	}
	return out
}

func StructuredTasksFromMeta(meta map[string]any) (StructuredTaskEnvelope, bool) {
	if len(meta) == 0 {
		return StructuredTaskEnvelope{}, false
	}
	raw, ok := meta[MetaKeyStructuredTasks]
	if !ok || raw == nil {
		return StructuredTaskEnvelope{}, false
	}
	return normalizeStructuredTasks(raw)
}

func ParseStructuredTasksJSON(data []byte) (StructuredTaskEnvelope, bool) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return StructuredTaskEnvelope{}, false
	}
	return normalizeStructuredTasks(raw)
}

func ParseStructuredTasksText(text string) (StructuredTaskEnvelope, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return StructuredTaskEnvelope{}, false
	}
	if env, ok := ParseStructuredTasksJSON([]byte(text)); ok {
		return env, true
	}
	block, ok := extractStructuredTasksFence(text)
	if !ok {
		return StructuredTaskEnvelope{}, false
	}
	return ParseStructuredTasksJSON([]byte(block))
}

func normalizeStructuredTasks(raw any) (StructuredTaskEnvelope, bool) {
	root, ok := raw.(map[string]any)
	if ok {
		if tasksRaw, exists := root["structured_tasks"]; exists {
			env, ok := normalizeStructuredTasks(tasksRaw)
			if !ok {
				return StructuredTaskEnvelope{}, false
			}
			if version := toInt(root["version"]); version > 0 && env.Version == 0 {
				env.Version = version
			}
			return env, true
		}
		tasksRaw, exists := root["tasks"]
		if !exists {
			return StructuredTaskEnvelope{}, false
		}
		tasks, ok := normalizeStructuredTaskList(tasksRaw)
		if !ok {
			return StructuredTaskEnvelope{}, false
		}
		version := toInt(root["version"])
		return StructuredTaskEnvelope{Version: version, Tasks: tasks}, len(tasks) > 0
	}
	if tasks, ok := normalizeStructuredTaskList(raw); ok {
		return StructuredTaskEnvelope{Tasks: tasks}, len(tasks) > 0
	}
	return StructuredTaskEnvelope{}, false
}

func normalizeStructuredTaskList(raw any) ([]StructuredToolCall, bool) {
	if typed, ok := raw.([]StructuredToolCall); ok {
		out := make([]StructuredToolCall, 0, len(typed))
		for _, task := range typed {
			tool := strings.TrimSpace(task.Tool)
			if tool == "" {
				return nil, false
			}
			out = append(out, StructuredToolCall{Tool: tool, Params: task.Params})
		}
		return out, len(out) > 0
	}
	if typed, ok := raw.([]map[string]any); ok {
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		raw = items
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]StructuredToolCall, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		tool := strings.TrimSpace(toString(entry["tool"]))
		if tool == "" {
			return nil, false
		}
		params := map[string]any{}
		if rawParams, exists := entry["params"]; exists && rawParams != nil {
			mapped, ok := rawParams.(map[string]any)
			if !ok {
				return nil, false
			}
			for key, value := range mapped {
				trimmed := strings.TrimSpace(key)
				if trimmed == "" {
					continue
				}
				params[trimmed] = value
			}
		}
		if len(params) == 0 {
			params = nil
		}
		out = append(out, StructuredToolCall{Tool: tool, Params: params})
	}
	return out, true
}

func extractStructuredTasksFence(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	inside := false
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inside {
			if !strings.HasPrefix(trimmed, "```") {
				continue
			}
			info := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")))
			if strings.Contains(info, "or3-tasks") || strings.Contains(info, "structured-tasks") || strings.Contains(info, "autonomous-tasks") {
				inside = true
				body = body[:0]
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			payload := strings.TrimSpace(strings.Join(body, "\n"))
			if payload == "" {
				return "", false
			}
			return payload, true
		}
		body = append(body, line)
	}
	return "", false
}

func toString(raw any) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "<nil>" {
		return ""
	}
	return text
}

func toInt(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
