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

func TestResolveServiceToolAllowlist_RejectsDenyListMode(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&toolPolicyStubTool{name: "read_file"})
	registry.Register(&toolPolicyStubTool{name: "exec"})

	_, _, err := ResolveServiceToolAllowlist(registry, &ServiceToolPolicy{
		Mode:         "deny_list",
		BlockedTools: []string{"exec"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported tool_policy mode") {
		t.Fatalf("expected deny_list to be rejected, got %v", err)
	}
}

func TestResolveServiceToolAllowlist_BlockedToolsAreValidatedButNotAppliedInAllowList(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&toolPolicyStubTool{name: "read_file"})
	registry.Register(&toolPolicyStubTool{name: "exec"})

	allowed, explicit, err := ResolveServiceToolAllowlist(registry, &ServiceToolPolicy{
		Mode:         "allow_list",
		AllowedTools: []string{"read_file", "exec"},
		BlockedTools: []string{"exec"},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveServiceToolAllowlist: %v", err)
	}
	if !explicit {
		t.Fatal("expected explicit allowlist")
	}
	if len(allowed) != 2 || allowed[0] != "read_file" || allowed[1] != "exec" {
		t.Fatalf("expected blockedTools to be validation-only for allow_list, got %#v", allowed)
	}

	_, _, err = ResolveServiceToolAllowlist(registry, &ServiceToolPolicy{
		Mode:         "allow_list",
		AllowedTools: []string{"read_file"},
		BlockedTools: []string{"unknown_tool"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown blocked tool to be rejected, got %v", err)
	}
}

func TestResolveServiceToolAllowlist_DenyDefaultAndLegacyFailure(t *testing.T) {
	allowed, explicit, err := ResolveServiceToolAllowlist(nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveServiceToolAllowlist nil policy: %v", err)
	}
	if !explicit || len(allowed) != 0 {
		t.Fatalf("expected nil policy to deny all with explicit restriction, got explicit=%v allowed=%#v", explicit, allowed)
	}
	if _, _, err := ResolveServiceToolAllowlist(nil, nil, []string{"read_file"}); err == nil || !strings.Contains(err.Error(), "tool_policy.mode is required") {
		t.Fatalf("expected legacy allowed_tools without tool_policy to fail, got %v", err)
	}
}
