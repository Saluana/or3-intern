package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/db"
)

func TestRenderTaskCard(t *testing.T) {
	tc := TaskCard{
		Goal:          "Build a feature",
		Plan:          "1. Design 2. Implement 3. Test",
		Constraints:   []string{"must be backward compatible", "no new dependencies"},
		Decisions:     []string{"use SQLite for storage"},
		OpenQuestions: []string{"what about performance?"},
		ActiveFiles:   []string{"main.go", "handler.go"},
	}
	out := RenderTaskCard(tc, 0)
	if !strings.Contains(out, "Goal: Build a feature") {
		t.Error("expected Goal in output")
	}
	if !strings.Contains(out, "Plan:") {
		t.Error("expected Plan in output")
	}
	if !strings.Contains(out, "must be backward compatible") {
		t.Error("expected constraint in output")
	}
	if !strings.Contains(out, "use SQLite for storage") {
		t.Error("expected decision in output")
	}
	if !strings.Contains(out, "what about performance?") {
		t.Error("expected open question in output")
	}
	if !strings.Contains(out, "main.go") {
		t.Error("expected active file in output")
	}
}

func TestRenderTaskCard_Empty(t *testing.T) {
	tc := TaskCard{}
	out := RenderTaskCard(tc, 0)
	if out != "" {
		t.Errorf("expected empty output for empty task card, got %q", out)
	}
}

func TestRenderTaskCard_Truncated(t *testing.T) {
	tc := TaskCard{
		Goal: "Build a very long feature description that exceeds the max chars limit",
	}
	out := RenderTaskCard(tc, 20)
	if len(out) > 20+len("\n…[truncated]") {
		t.Errorf("output not properly truncated: len=%d", len(out))
	}
	if !strings.Contains(out, "…[truncated]") {
		t.Error("expected truncation marker")
	}
}

func TestTaskCardPersistence(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	tc := TaskCard{
		Goal:        "Test goal",
		Plan:        "Test plan",
		Constraints: []string{"constraint 1", "constraint 2"},
		Status:      "active",
	}
	state := TaskCardToDB("sess-persist", tc)
	if err := d.UpsertTaskState(ctx, state); err != nil {
		t.Fatalf("UpsertTaskState: %v", err)
	}

	retrieved, found, err := d.GetTaskState(ctx, "sess-persist")
	if err != nil {
		t.Fatalf("GetTaskState: %v", err)
	}
	if !found {
		t.Fatal("task state not found after upsert")
	}
	if retrieved.Goal != "Test goal" {
		t.Errorf("Goal = %q, want %q", retrieved.Goal, "Test goal")
	}
	if retrieved.Plan != "Test plan" {
		t.Errorf("Plan = %q, want %q", retrieved.Plan, "Test plan")
	}

	tc2 := TaskCardFromDB(retrieved)
	if len(tc2.Constraints) != 2 {
		t.Errorf("Constraints len = %d, want 2", len(tc2.Constraints))
	}
}

func TestTaskCardSessionIsolation(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	state1 := db.TaskState{SessionKey: "sess-a", Goal: "goal for session A", Status: "active"}
	state2 := db.TaskState{SessionKey: "sess-b", Goal: "goal for session B", Status: "active"}

	if err := d.UpsertTaskState(ctx, state1); err != nil {
		t.Fatalf("UpsertTaskState sess-a: %v", err)
	}
	if err := d.UpsertTaskState(ctx, state2); err != nil {
		t.Fatalf("UpsertTaskState sess-b: %v", err)
	}

	retrieved1, found1, err1 := d.GetTaskState(ctx, "sess-a")
	retrieved2, found2, err2 := d.GetTaskState(ctx, "sess-b")

	if err1 != nil || !found1 {
		t.Fatalf("GetTaskState sess-a: err=%v found=%v", err1, found1)
	}
	if err2 != nil || !found2 {
		t.Fatalf("GetTaskState sess-b: err=%v found=%v", err2, found2)
	}
	if retrieved1.Goal != "goal for session A" {
		t.Errorf("sess-a Goal = %q", retrieved1.Goal)
	}
	if retrieved2.Goal != "goal for session B" {
		t.Errorf("sess-b Goal = %q", retrieved2.Goal)
	}
}
