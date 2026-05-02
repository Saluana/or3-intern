package app

import (
	"context"
	"testing"

	"or3-intern/internal/agent"
	"or3-intern/internal/tools"
)

type registryProbeTool struct {
	name string
}

func (t registryProbeTool) Name() string               { return t.name }
func (t registryProbeTool) Description() string        { return t.name }
func (t registryProbeTool) Parameters() map[string]any { return map[string]any{} }
func (t registryProbeTool) Schema() map[string]any     { return map[string]any{} }
func (t registryProbeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	reg := agent.ToolRegistryFromContext(ctx)
	if reg == nil {
		return "", errString("missing tool registry in context")
	}
	if reg.Get("replay_probe") == nil {
		return "", errString("expected replay_probe in restricted registry")
	}
	if reg.Get("blocked_probe") != nil {
		return "", errString("blocked_probe should not be available in restricted registry")
	}
	return "ok", nil
}

type errString string

func (e errString) Error() string { return string(e) }

func TestReplayToolCall_UsesRestrictedRegistryContext(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(registryProbeTool{name: "replay_probe"})
	registry.Register(registryProbeTool{name: "blocked_probe"})

	app := &ServiceApp{
		runtime: &agent.Runtime{
			Tools: registry,
		},
	}

	out, err := app.ReplayToolCall(context.Background(), ReplayToolCallRequest{
		ToolName:      "replay_probe",
		ArgumentsJSON: `{}`,
		AllowedTools:  []string{"replay_probe"},
		RestrictTools: true,
	})
	if err != nil {
		t.Fatalf("ReplayToolCall: %v", err)
	}
	if out != "ok" {
		t.Fatalf("expected ok, got %q", out)
	}
}
