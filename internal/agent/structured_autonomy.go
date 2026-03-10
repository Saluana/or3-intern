package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

func (r *Runtime) handleStructuredAutonomy(ctx context.Context, ev bus.Event, msgID int64) (bool, error) {
	if !isAutonomousEvent(ev.Type) || r.Tools == nil {
		return false, nil
	}
	env, ok := triggers.StructuredTasksFromMeta(ev.Meta)
	if !ok || len(env.Tasks) == 0 {
		return false, nil
	}
	replyTarget := deliveryTarget(ev)
	replyMeta := channels.ReplyMeta(ev.Meta)
	scopeKey := ev.SessionKey
	if r.DB != nil && strings.TrimSpace(ev.SessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, ev.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	toolCtx := tools.ContextWithSession(ctx, scopeKey)
	toolCtx = tools.ContextWithDelivery(toolCtx, ev.Channel, replyTarget)
	toolCtx = tools.ContextWithDeliveryMeta(toolCtx, replyMeta)
	toolCtx = r.contextWithTrustedToolAccess(toolCtx, ev)
	toolCtx = tools.ContextWithToolGuard(toolCtx, r.guardToolExecution)

	succeeded := 0
	failures := make([]string, 0)
	for index, task := range env.Tasks {
		toolName := strings.TrimSpace(task.Tool)
		params := cloneMap(task.Params)
		tool := r.Tools.Get(toolName)
		if tool == nil {
			failures = append(failures, fmt.Sprintf("#%d %s: tool not found", index+1, toolName))
			continue
		}
		if err := validateStructuredToolParams(tool, params); err != nil {
			failures = append(failures, fmt.Sprintf("#%d %s: %v", index+1, toolName, err))
			continue
		}
		out, err := r.Tools.ExecuteParams(toolCtx, toolName, params)
		payload := map[string]any{
			"tool":            toolName,
			"args":            params,
			"structured_task": true,
			"task_index":      index,
		}
		if err != nil {
			out = "tool error: " + err.Error()
			failures = append(failures, fmt.Sprintf("#%d %s: %v", index+1, toolName, err))
		} else {
			succeeded++
		}
		sendOut, preview, artifactID := r.boundTextResult(ctx, ev.SessionKey, out)
		if artifactID != "" {
			payload["artifact_id"] = artifactID
			payload["preview"] = preview
		}
		if _, appendErr := r.DB.AppendMessage(ctx, ev.SessionKey, "tool", sendOut, payload); appendErr != nil {
			return true, appendErr
		}
	}
	summary := structuredAutonomySummary(succeeded, len(env.Tasks), failures)
	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, summary, replyMeta, false, false)
	return true, nil
}

func structuredAutonomySummary(succeeded, total int, failures []string) string {
	base := fmt.Sprintf("structured autonomous tasks executed: %d/%d succeeded", succeeded, total)
	if len(failures) == 0 {
		return base
	}
	return base + "\nfailures:\n- " + strings.Join(failures, "\n- ")
}

func validateStructuredToolParams(tool tools.Tool, params map[string]any) error {
	if tool == nil {
		return fmt.Errorf("tool not found")
	}
	if params == nil {
		params = map[string]any{}
	}
	return validateStructuredValue(tool.Parameters(), params, "params")
}

func validateStructuredValue(schema map[string]any, value any, path string) error {
	typeName := strings.TrimSpace(fmt.Sprint(schema["type"]))
	switch typeName {
	case "", "object":
		mapped, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		for _, name := range requiredSchemaFields(schema["required"]) {
			if _, ok := mapped[name]; !ok {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
			for key := range mapped {
				if _, known := properties[key]; !known {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
			}
		}
		for key, raw := range mapped {
			childSchema, ok := properties[key].(map[string]any)
			if !ok {
				continue
			}
			if err := validateStructuredValue(childSchema, raw, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, ok := sliceItems(value)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if len(itemSchema) == 0 {
				continue
			}
			if err := validateStructuredValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
		return nil
	case "integer":
		if !isIntegerValue(value) {
			return fmt.Errorf("%s must be an integer", path)
		}
		return nil
	case "number":
		if !isNumericValue(value) {
			return fmt.Errorf("%s must be a number", path)
		}
		return nil
	default:
		return nil
	}
}

func requiredSchemaFields(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return append([]string{}, typed...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(fmt.Sprint(item))
		if name != "" && name != "<nil>" {
			out = append(out, name)
		}
	}
	return out
}

func sliceItems(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if items, ok := value.([]any); ok {
		return items, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for index := 0; index < rv.Len(); index++ {
		out[index] = rv.Index(index).Interface()
	}
	return out, true
}

func isNumericValue(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func isIntegerValue(value any) bool {
	switch cast := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return float32(int64(cast)) == cast
	case float64:
		return float64(int64(cast)) == cast
	default:
		return false
	}
}