package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/security"
	"or3-intern/internal/tools"
)

type objectArrayTool struct {
	tools.Base
}

func (t *objectArrayTool) Name() string        { return "object_array_tool" }
func (t *objectArrayTool) Description() string { return "object array tool" }
func (t *objectArrayTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "object"},
			},
		},
	}
}
func (t *objectArrayTool) Execute(context.Context, map[string]any) (string, error) { return "", nil }
func (t *objectArrayTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func TestRuntime_ExposedToolsForTurn_HandlesNilRuntimeAndRegistry(t *testing.T) {
	var rt *Runtime
	if got := rt.exposedToolsForTurn(context.Background(), nil, nil, ""); got != nil {
		t.Fatalf("expected nil registry to stay nil, got %#v", got)
	}

	reg := tools.NewRegistry()
	reg.Register(&namedRuntimeTool{name: "read_file", meta: tools.ToolMetadata{Groups: []string{tools.ToolGroupRead}}})
	exposed := rt.exposedToolsForTurn(context.Background(), reg, nil, "")
	if exposed == nil || len(exposed.Names()) != 1 || exposed.Get("read_file") == nil {
		t.Fatalf("expected nil runtime to keep filtered registry intact, got %#v", exposed)
	}
}

func TestRuntime_FilterToolsForContext_HandlesNilRuntimeAndEmptyProfileAllowlist(t *testing.T) {
	var rt *Runtime
	if got := rt.filterToolsForContext(context.Background(), nil); got != nil {
		t.Fatalf("expected nil registry to stay nil, got %#v", got)
	}

	reg := tools.NewRegistry()
	reg.Register(&namedRuntimeTool{name: "read_file", meta: tools.ToolMetadata{Groups: []string{tools.ToolGroupRead}}})
	reg.Register(&namedRuntimeTool{name: "write_file", meta: tools.ToolMetadata{Groups: []string{tools.ToolGroupWrite}}})
	filtered := rt.filterToolsForContext(context.Background(), reg)
	if len(filtered.Names()) != 2 {
		t.Fatalf("expected nil runtime to preserve registry, got %#v", filtered.Names())
	}

	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:          "empty-allowlist",
		MaxCapability: tools.CapabilityPrivileged,
		AllowedTools:  map[string]struct{}{},
	})
	filtered = (&Runtime{}).filterToolsForContext(ctx, reg)
	if len(filtered.Names()) != 2 {
		t.Fatalf("expected empty allowlist map to preserve tools, got %#v", filtered.Names())
	}
}

func TestSelectedToolGroups_EmptyIntentOmitsChannels(t *testing.T) {
	groups := selectedToolGroups("")
	if _, ok := groups[tools.ToolGroupChannels]; ok {
		t.Fatalf("expected empty intent to omit channels group, got %#v", groups)
	}
	if _, ok := groups[tools.ToolGroupRead]; !ok {
		t.Fatalf("expected read group by default, got %#v", groups)
	}
	if _, ok := groups[tools.ToolGroupMemory]; !ok {
		t.Fatalf("expected memory group by default, got %#v", groups)
	}
}

func TestRuntime_GuardToolExecution_EdgeBranches(t *testing.T) {
	rt := &Runtime{}
	if err := rt.guardToolExecution(context.Background(), nil, tools.CapabilitySafe, nil); err != nil {
		t.Fatalf("expected nil tool to be ignored, got %v", err)
	}

	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Exec.Mode = config.ApprovalModeAsk
	rt = &Runtime{
		Hardening: config.HardeningConfig{PrivilegedTools: true},
		ApprovalBroker: &approval.Broker{
			Config: approvalCfg,
		},
	}
	if err := rt.guardToolExecution(context.Background(), &tools.ExecTool{}, tools.CapabilityPrivileged, map[string]any{"command": "pwd"}); err == nil || !strings.Contains(err.Error(), "approval broker unavailable") {
		t.Fatalf("expected empty sign key to fail closed, got %v", err)
	}

	rt = &Runtime{
		Hardening: config.HardeningConfig{PrivilegedTools: true},
		Audit:     &security.AuditLogger{Strict: true},
	}
	if err := rt.guardToolExecution(context.Background(), &privilegedEchoTool{}, tools.CapabilityPrivileged, map[string]any{}); err == nil || !strings.Contains(err.Error(), "audit logger unavailable") {
		t.Fatalf("expected audit error to be returned, got %v", err)
	}
}

func TestRuntime_EnforceSkillPolicy_EdgeBranches(t *testing.T) {
	ctx := tools.ContextWithSkillPolicy(context.Background(), tools.SkillPolicy{
		Name:         "demo-skill",
		AllowNetwork: true,
	})
	rt := &Runtime{}
	if err := rt.enforceSkillPolicy(ctx, nil, nil); err != nil {
		t.Fatalf("expected nil tool to be ignored, got %v", err)
	}
	if err := rt.enforceSkillPolicy(ctx, &tools.WebSearch{}, map[string]any{"query": "hello"}); err != nil {
		t.Fatalf("expected web search to skip host validation when hosts are empty, got %v", err)
	}
}

func TestRuntime_ResolveProfile_EdgeBranches(t *testing.T) {
	rt := &Runtime{}
	if _, ok := rt.resolveProfile("missing"); ok {
		t.Fatal("expected disabled profiles to resolve nothing")
	}

	rt.AccessProfiles.Enabled = true
	rt.AccessProfiles.Profiles = map[string]config.AccessProfileConfig{
		"known": {MaxCapability: "safe"},
	}
	if _, ok := rt.resolveProfile("unknown"); ok {
		t.Fatal("expected unknown profile lookup to fail")
	}
}

func TestValidateProfileWritablePath_AllowsNilAndEmptyPath(t *testing.T) {
	for _, path := range []string{"", "   ", "<nil>"} {
		if err := validateProfileWritablePath(nil, path); err != nil {
			t.Fatalf("expected %q to bypass validation, got %v", path, err)
		}
	}
}

func TestRuntime_ProfileNameForEvent_CoercesNonStringMeta(t *testing.T) {
	rt := &Runtime{}
	if got := rt.profileNameForEvent(busEventWithProfileName(42)); got != "42" {
		t.Fatalf("expected integer profile name to coerce via fmt.Sprint, got %q", got)
	}
	if got := rt.profileNameForEvent(busEventWithProfileName(true)); got != "true" {
		t.Fatalf("expected boolean profile name to coerce via fmt.Sprint, got %q", got)
	}
}

func TestResolveServiceToolAllowlist_DenyAllAndHiddenToolExclusion(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&namedRuntimeTool{name: "visible", meta: tools.ToolMetadata{Groups: []string{tools.ToolGroupRead}}})
	reg.Register(&namedRuntimeTool{name: "hidden_meta", meta: tools.ToolMetadata{Hidden: true, Groups: []string{tools.ToolGroupRead}}})
	reg.Register(&namedRuntimeTool{name: "hidden_group", meta: tools.ToolMetadata{Groups: []string{tools.ToolGroupRead, tools.ToolGroupHidden}}})

	allowed, explicit, err := ResolveServiceToolAllowlist(reg, &ServiceToolPolicy{Mode: "deny_all"}, nil)
	if err != nil {
		t.Fatalf("deny_all: %v", err)
	}
	if !explicit || len(allowed) != 0 {
		t.Fatalf("expected deny_all to be explicit empty allowlist, got explicit=%v allowed=%#v", explicit, allowed)
	}

	admin, explicit, err := ResolveServiceToolAllowlist(reg, &ServiceToolPolicy{Mode: "admin"}, nil)
	if err != nil || !explicit {
		t.Fatalf("admin allowlist: explicit=%v err=%v", explicit, err)
	}
	if strings.Join(admin, ",") != "visible" {
		t.Fatalf("expected hidden tools to be excluded, got %#v", admin)
	}
}

func TestCapabilityRankForPolicy_CoversPrivilegedAndUnknown(t *testing.T) {
	if got := capabilityRankForPolicy(tools.CapabilityPrivileged); got != 3 {
		t.Fatalf("expected privileged rank 3, got %d", got)
	}
	if got := capabilityRankForPolicy(tools.CapabilityLevel("mystery")); got != 0 {
		t.Fatalf("expected unknown capability rank 0, got %d", got)
	}
}

func TestResolveServiceToolAllowlist_RejectsUnsupportedMode(t *testing.T) {
	_, _, err := ResolveServiceToolAllowlist(nil, &ServiceToolPolicy{Mode: "bogus"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported tool_policy mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}

func TestToolArgumentValidator_AllowsNilToolAndRejectsYesNoBooleans(t *testing.T) {
	result := ToolArgumentValidator{}.ValidateAndCoerce(nil, `{"count":2,"enabled":true}`)
	if len(result.Errors) != 0 || result.Params["count"] != float64(2) || result.Params["enabled"] != true {
		t.Fatalf("expected nil tool to pass through params, got %#v", result)
	}

	tool := &typedArgsTool{}
	for _, raw := range []string{
		`{"count":1,"enabled":"yes","tags":["one"],"payload":{"name":"x"},"mode":"fast"}`,
		`{"count":1,"enabled":"no","tags":["one"],"payload":{"name":"x"},"mode":"fast"}`,
	} {
		result = ToolArgumentValidator{}.ValidateAndCoerce(tool, raw)
		if len(result.Errors) == 0 || result.Errors[0].Code != "expected_boolean" {
			t.Fatalf("expected boolean coercion failure for %s, got %#v", raw, result.Errors)
		}
	}
}

func TestToolArgumentValidator_DoesNotCoerceScalarToObjectArray(t *testing.T) {
	result := ToolArgumentValidator{}.ValidateAndCoerce(&objectArrayTool{}, `{"items":"oops"}`)
	if len(result.Errors) == 0 || result.Errors[0].Code != "expected_array" {
		t.Fatalf("expected object-array coercion to fail, got %#v", result.Errors)
	}
}

func busEventWithProfileName(value any) bus.Event {
	return bus.Event{Meta: map[string]any{"profile_name": value}}
}
