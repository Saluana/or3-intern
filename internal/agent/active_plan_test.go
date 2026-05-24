package agent

import (
	"context"
	"strings"
	"testing"
)

func TestRenderCurrentTurnIncludesMessageID(t *testing.T) {
	rendered := renderCurrentTurn("fix the prompt builder", 42)
	if !strings.Contains(rendered, "Message ID: 42") || !strings.Contains(rendered, "fix the prompt builder") {
		t.Fatalf("unexpected current turn render: %q", rendered)
	}
}

func TestRenderActivePlanCompactBounded(t *testing.T) {
	meta := ActivePlanMetadata{
		Title:          "Ship plan runtime",
		CurrentRequest: "implement protected turn",
		NextStep:       "add tests",
		Tasks: []ActivePlanTask{
			{ID: "t1", Title: "wire prompt", Status: planTaskStatusInProgress},
			{ID: "t2", Title: "add gate", Status: planTaskStatusPending},
		},
		CompletionNotes: []string{"created metadata"},
	}
	rendered := renderActivePlanCompact(TaskCard{Goal: "ship"}, meta, 200)
	if !strings.Contains(rendered, "Current request:") || !strings.Contains(rendered, "Next step:") {
		t.Fatalf("expected compact plan render, got %q", rendered)
	}
}

func TestEnforceProtectedCompactionCutoff(t *testing.T) {
	messages := []contextManagerMessage{{ID: 10}, {ID: 20}, {ID: 30}}
	cutoff, adjusted, err := enforceProtectedCompactionCutoff(25, messages, 20)
	if err != nil {
		t.Fatalf("enforceProtectedCompactionCutoff: %v", err)
	}
	if cutoff != 10 || !adjusted {
		t.Fatalf("expected cutoff clamped to 10 adjusted=true, got %d adjusted=%v", cutoff, adjusted)
	}
	cutoff, adjusted, err = enforceProtectedCompactionCutoff(30, messages, 30)
	if err != nil {
		t.Fatalf("enforceProtectedCompactionCutoff protected clamp: %v", err)
	}
	if cutoff != 20 || !adjusted {
		t.Fatalf("expected cutoff clamped below protected message, got %d adjusted=%v", cutoff, adjusted)
	}
}

func TestCleanupActiveTurnTaskCompletesResolvedPlan(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	card := TaskCard{
		Goal:   "ship feature",
		Status: "active",
		Metadata: ActivePlanMetadata{
			Title:                   "Ship",
			CurrentRequest:          "do it now",
			CurrentRequestMessageID: 9,
			Tasks: []ActivePlanTask{
				{ID: "t1", Title: "implement", Status: planTaskStatusCompleted},
			},
		},
	}
	if err := saveTaskCard(ctx, d, "sess-cleanup", "", card); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	rt := &Runtime{DB: d, Builder: &Builder{}}
	rt.cleanupActiveTurnTask(ctx, "sess-cleanup")
	if _, ok, err := loadTaskCard(ctx, d, "sess-cleanup"); err != nil || ok {
		t.Fatalf("expected active task cleared, ok=%v err=%v", ok, err)
	}
}

func TestCleanupActiveTurnTaskKeepsOpenPlanClearsCurrentRequest(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	card := TaskCard{
		Goal:   "multi-step",
		Status: "active",
		Metadata: ActivePlanMetadata{
			Title:                   "Multi",
			CurrentRequest:          "finish step two",
			CurrentRequestMessageID: 12,
			Tasks: []ActivePlanTask{
				{ID: "t1", Title: "step one", Status: planTaskStatusCompleted},
				{ID: "t2", Title: "step two", Status: planTaskStatusInProgress},
			},
		},
	}
	if err := saveTaskCard(ctx, d, "sess-open", "", card); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	rt := &Runtime{DB: d, Builder: &Builder{}}
	rt.cleanupActiveTurnTask(ctx, "sess-open")
	loaded, ok, err := loadTaskCard(ctx, d, "sess-open")
	if err != nil || !ok {
		t.Fatalf("expected active plan retained, ok=%v err=%v", ok, err)
	}
	if loaded.Metadata.CurrentRequest != "" || loaded.Metadata.CurrentRequestMessageID != 0 {
		t.Fatalf("expected current turn fields cleared, got %+v", loaded.Metadata)
	}
	if !activePlanHasOpenWork(loaded.Metadata) {
		t.Fatalf("expected unfinished task to remain active")
	}
}

func TestPlanToolsCreateUpdateCompleteRemove(t *testing.T) {
	d := openTestDB(t)
	ctx := ContextWithTurnState(context.Background(), TurnState{
		SessionKey:    "sess-plan",
		UserMessageID: 7,
		UserMessage:   "ship protected turn",
	})
	ctx = ContextWithConversationSession(ctx, "sess-plan")
	base := NewPlanToolBase(d)

	create := &CreatePlanTool{PlanToolBase: base}
	out, err := create.Execute(ctx, map[string]any{
		"title": "Protected turn",
		"tasks": []any{
			map[string]any{"id": "t1", "title": "wire prompt"},
			map[string]any{"id": "t2", "title": "add tests"},
		},
		"next_step": "wire prompt",
	})
	if err != nil {
		t.Fatalf("create_plan: %v", err)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Fatalf("unexpected create output: %s", out)
	}

	complete := &CompletePlanTaskTool{PlanToolBase: base}
	if _, err := complete.Execute(ctx, map[string]any{
		"task_id":            "t1",
		"completion_summary": "prompt wired",
		"next_step":          "add tests",
	}); err != nil {
		t.Fatalf("complete_plan_task: %v", err)
	}

	card, ok, err := loadTaskCard(ctx, d, "sess-plan")
	if err != nil || !ok {
		t.Fatalf("load task card: ok=%v err=%v", ok, err)
	}
	if card.Metadata.CurrentRequestMessageID != 7 || !activePlanHasOpenWork(card.Metadata) {
		t.Fatalf("expected persisted plan metadata, got %+v", card.Metadata)
	}
	if len(card.Plan) == 0 {
		t.Fatalf("expected legacy plan_json sync")
	}

	remove := &RemovePlanTool{PlanToolBase: base}
	if _, err := remove.Execute(ctx, map[string]any{"reason": "done"}); err != nil {
		t.Fatalf("remove_plan: %v", err)
	}
	if _, ok, err := loadTaskCard(ctx, d, "sess-plan"); err != nil || ok {
		t.Fatalf("expected active plan cleared, ok=%v err=%v", ok, err)
	}
}
