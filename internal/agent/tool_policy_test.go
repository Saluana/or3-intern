package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/tools"
)

type toolPolicyStubTool struct {
	tools.Base
	name string
}

func (t *toolPolicyStubTool) Name() string        { return t.name }
func (t *toolPolicyStubTool) Description() string { return t.name }
func (t *toolPolicyStubTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *toolPolicyStubTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *toolPolicyStubTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return "", nil
}

func TestResolveServiceToolAllowlist_RejectsMissingMode(t *testing.T) {
	_, _, err := ResolveServiceToolAllowlist(nil, &ServiceToolPolicy{AllowedTools: []string{"read_file"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "mode is required") {
		t.Fatalf("expected missing mode error, got %v", err)
	}
}

func TestResolveServiceToolAllowlist_DenyListUsesRegistry(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&toolPolicyStubTool{name: "read_file"})
	registry.Register(&toolPolicyStubTool{name: "exec"})

	allowed, explicit, err := ResolveServiceToolAllowlist(registry, &ServiceToolPolicy{
		Mode:         "deny_list",
		BlockedTools: []string{"exec"},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveServiceToolAllowlist: %v", err)
	}
	if !explicit {
		t.Fatal("expected deny_list to produce an explicit allowlist")
	}
	if len(allowed) != 1 || allowed[0] != "read_file" {
		t.Fatalf("expected only read_file to remain allowed, got %#v", allowed)
	}
}