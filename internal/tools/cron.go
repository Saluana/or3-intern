package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/cron"
)

type CronTool struct {
	Base
	Svc *cron.Service
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Manage scheduled jobs: add/list/remove/run/status."
}
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []any{"add", "list", "remove", "run", "status"}},
		"job":    map[string]any{"type": "object", "description": "job object for add"},
		"id":     map[string]any{"type": "string", "description": "job id for remove/run"},
		"force":  map[string]any{"type": "boolean", "description": "force run"},
	}, "required": []string{"action"}}
}
func (t *CronTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *CronTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Svc == nil {
		return "", fmt.Errorf("cron service not configured")
	}
	act := strings.TrimSpace(fmt.Sprint(params["action"]))
	switch act {
	case "status":
		s, err := t.Svc.Status()
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(s, "", "  ")
		return string(b), nil
	case "list":
		j, err := t.Svc.List()
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(j, "", "  ")
		return string(b), nil
	case "remove":
		id := strings.TrimSpace(fmt.Sprint(params["id"]))
		ok, err := t.Svc.Remove(id)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("removed: %v", ok), nil
	case "run":
		id := strings.TrimSpace(fmt.Sprint(params["id"]))
		force, _ := params["force"].(bool)
		ok, err := t.Svc.RunNow(ctx, id, force)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("ran: %v", ok), nil
	case "add":
		raw, _ := params["job"].(map[string]any)
		if raw == nil {
			return "", fmt.Errorf("missing job")
		}
		b, _ := json.Marshal(raw)
		var j cron.CronJob
		if err := json.Unmarshal(b, &j); err != nil {
			return "", err
		}
		// defaults
		if !j.Enabled && raw["enabled"] == nil {
			j.Enabled = true
		}
		if j.Payload.Kind == "" {
			j.Payload.Kind = "agent_turn"
		}
		if j.Schedule.Kind == "" {
			j.Schedule.Kind = cron.KindEvery
			j.Schedule.EveryMS = int64((24 * time.Hour).Milliseconds())
		}
		if err := t.Svc.Add(j); err != nil {
			return "", err
		}
		return "ok", nil
	default:
		return "", fmt.Errorf("unknown action")
	}
}
