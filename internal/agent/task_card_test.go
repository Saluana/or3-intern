package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/db"
)

func TestTaskCardPersistAndRender(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	card := TaskCard{
		Goal:          "ship context packets",
		Plan:          []string{"implement", "test"},
		Constraints:   []string{"no new top-level package"},
		Decisions:     []string{"use additive migrations"},
		OpenQuestions: []string{"when to enable balanced mode"},
		ArtifactRefs:  []string{"art-1"},
		ActiveFiles:   []string{"internal/agent/prompt.go"},
		Status:        "active",
	}
	if err := saveTaskCard(ctx, d, "sess", "sess", card); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	loaded, ok, err := loadTaskCard(ctx, d, "sess")
	if err != nil {
		t.Fatalf("loadTaskCard: %v", err)
	}
	if !ok || loaded.Goal == "" {
		t.Fatalf("expected persisted task card, got ok=%v loaded=%+v", ok, loaded)
	}
	rendered := renderTaskCard(loaded, 0)
	if !strings.Contains(rendered, "Goal:") || !strings.Contains(rendered, "Decision:") {
		t.Fatalf("expected labeled semantic rendering, got %q", rendered)
	}
}

func TestTaskCardCompleteViaDB(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := saveTaskCard(ctx, d, "sess2", "sess2", TaskCard{Goal: "x", Status: "active"}); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	if err := d.CompleteActiveTaskState(ctx, "sess2"); err != nil {
		t.Fatalf("CompleteActiveTaskState: %v", err)
	}
	_, ok, err := loadTaskCard(ctx, d, "sess2")
	if err != nil {
		t.Fatalf("loadTaskCard: %v", err)
	}
	if ok {
		t.Fatalf("expected no active task card after completion")
	}
}

func TestTaskCardUsesTaskStateTable(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := saveTaskCard(ctx, d, "sess3", "sess3", TaskCard{Goal: "x", Status: "active"}); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	var count int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_state WHERE session_key=?`, "sess3").Scan(&count); err != nil {
		t.Fatalf("query task_state: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected task_state rows")
	}
	_ = db.NowMS()
}
