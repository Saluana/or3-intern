package tools

import (
	"context"
	"fmt"
	"strings"
)

type SpawnRequest struct {
	ParentSessionKey string
	Task             string
	Channel          string
	To               string
}

type SpawnJob struct {
	ID              string
	ChildSessionKey string
}

type SpawnEnqueuer interface {
	Enqueue(ctx context.Context, req SpawnRequest) (SpawnJob, error)
}

type SpawnSubagent struct {
	Base
	Manager        SpawnEnqueuer
	DefaultChannel string
	DefaultTo      string
}

func (t *SpawnSubagent) Capability() CapabilityLevel { return CapabilityGuarded }

func (t *SpawnSubagent) Name() string { return "spawn_subagent" }

func (t *SpawnSubagent) Description() string {
	return "Queue a longer background task and return immediately with a stable job ID."
}

func (t *SpawnSubagent) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":    map[string]any{"type": "string", "description": "Task for the background subagent"},
			"channel": map[string]any{"type": "string", "description": "Optional delivery channel override"},
			"to":      map[string]any{"type": "string", "description": "Optional recipient override"},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnSubagent) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *SpawnSubagent) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Manager == nil {
		return "", fmt.Errorf("background subagents disabled")
	}
	task := readOptionalString(params, "task")
	if task == "" {
		return "", fmt.Errorf("empty task")
	}
	channel := readOptionalString(params, "channel")
	to := readOptionalString(params, "to")
	ctxChannel, ctxTo := DeliveryFromContext(ctx)
	if channel == "" {
		channel = firstNonEmpty(ctxChannel, t.DefaultChannel)
	}
	if to == "" {
		to = firstNonEmpty(ctxTo, t.DefaultTo)
	}
	job, err := t.Manager.Enqueue(ctx, SpawnRequest{
		ParentSessionKey: SessionFromContext(ctx),
		Task:             task,
		Channel:          channel,
		To:               to,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("queued background job_id=%s", job.ID), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readOptionalString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}