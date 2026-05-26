package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/tools"
)

func TestPlanGateDisabledAllowsWriteWithoutPlan(t *testing.T) {
	d := openTestDB(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: t.TempDir(), WriteRoot: t.TempDir()}})
	rt := &Runtime{DB: d, Tools: reg, EnforceActivePlan: false}
	ctx := tools.ContextWithSession(context.Background(), "sess-open")
	ctx = tools.ContextWithToolGuard(ctx, rt.guardToolExecution)
	tool := reg.Get(tools.ToolNameWriteFile)
	if err := rt.guardToolExecution(ctx, tool, tools.CapabilitySafe, map[string]any{"path": "x.txt", "content": "hi"}); err != nil {
		t.Fatalf("expected write without plan when gate disabled, got %v", err)
	}
}

func TestPlanGateBlocksWriteWithoutPlan(t *testing.T) {
	d := openTestDB(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: t.TempDir(), WriteRoot: t.TempDir()}})
	rt := &Runtime{DB: d, Tools: reg, EnforceActivePlan: true}
	ctx := tools.ContextWithSession(context.Background(), "sess-gate")
	ctx = tools.ContextWithToolGuard(ctx, rt.guardToolExecution)
	tool := reg.Get(tools.ToolNameWriteFile)
	if err := rt.guardToolExecution(ctx, tool, tools.CapabilitySafe, map[string]any{"path": "x.txt", "content": "hi"}); err == nil || !strings.Contains(err.Error(), "active plan required") {
		t.Fatalf("expected plan gate error, got %v", err)
	}
}

func TestPlanGateSkipsDoctorToolsWithoutTaskCardPlan(t *testing.T) {
	d := openTestDB(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: t.TempDir(), WriteRoot: t.TempDir()}})
	rt := &Runtime{DB: d, Tools: reg, EnforceActivePlan: true}
	ctx := tools.ContextWithSession(context.Background(), "doctor-app-test-session")
	ctx = tools.ContextWithToolGuard(ctx, rt.guardToolExecution)
	meta := tools.ToolMetadata{Groups: []string{tools.ToolGroupService}}
	if rt.enforcePlanBeforeTool(ctx, &doctorPlanGateProbeTool{name: "doctor_create_plan", meta: meta}, "doctor-app-test-session") != nil {
		t.Fatalf("expected doctor_create_plan without task-card plan")
	}
	if rt.enforcePlanBeforeTool(ctx, &doctorPlanGateProbeTool{name: "doctor_logs", meta: meta}, "doctor-app-test-session") != nil {
		t.Fatalf("expected doctor_logs without task-card plan")
	}
}

type doctorPlanGateProbeTool struct {
	name string
	meta tools.ToolMetadata
}

func (t *doctorPlanGateProbeTool) Name() string        { return t.name }
func (t *doctorPlanGateProbeTool) Description() string { return "probe" }
func (t *doctorPlanGateProbeTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (t *doctorPlanGateProbeTool) Schema() map[string]any { return t.Parameters() }
func (t *doctorPlanGateProbeTool) Capability() tools.CapabilityLevel { return tools.CapabilitySafe }
func (t *doctorPlanGateProbeTool) Metadata() tools.ToolMetadata        { return t.meta }
func (t *doctorPlanGateProbeTool) Execute(context.Context, map[string]any) (string, error) {
	return "", nil
}

func TestPlanGateAllowsWriteAfterCreatePlan(t *testing.T) {
	d := openTestDB(t)
	ctx := ContextWithConversationSession(context.Background(), "sess-gate-ok")
	ctx = ContextWithTurnState(ctx, TurnState{SessionKey: "sess-gate-ok", UserMessage: "write a file"})
	base := NewPlanToolBase(d)
	if _, err := (&CreatePlanTool{PlanToolBase: base}).Execute(ctx, map[string]any{
		"title": "Write file",
		"tasks": []any{map[string]any{"id": "t1", "title": "write"}},
		"next_step": "write",
	}); err != nil {
		t.Fatalf("create_plan: %v", err)
	}
	reg := tools.NewRegistry()
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: t.TempDir(), WriteRoot: t.TempDir()}})
	rt := &Runtime{DB: d, Tools: reg, EnforceActivePlan: true}
	toolCtx := tools.ContextWithSession(ctx, "sess-gate-ok")
	toolCtx = tools.ContextWithToolGuard(toolCtx, rt.guardToolExecution)
	if err := rt.guardToolExecution(toolCtx, reg.Get(tools.ToolNameWriteFile), tools.CapabilitySafe, map[string]any{"path": "ok.txt", "content": "ok"}); err != nil {
		t.Fatalf("expected write allowed after plan, got %v", err)
	}
}
