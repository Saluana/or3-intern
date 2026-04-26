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

func TestTaskCardUsesResolvedScopeButRemainsSessionIsolated(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.LinkSession(ctx, "session-a", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "session-b", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}
	if err := saveTaskCard(ctx, d, "session-a", "", TaskCard{Goal: "task-a", Status: "active"}); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	row, ok, err := d.GetActiveTaskState(ctx, "session-a")
	if err != nil {
		t.Fatalf("GetActiveTaskState: %v", err)
	}
	if !ok || row.ScopeKey != "scope-1" {
		t.Fatalf("expected resolved scope key scope-1, got ok=%v row=%+v", ok, row)
	}
	if _, ok, err := loadTaskCard(ctx, d, "session-b"); err != nil {
		t.Fatalf("loadTaskCard session-b: %v", err)
	} else if ok {
		t.Fatalf("expected task card isolation by session even with shared scope")
	}
}

func TestTaskCardSurvivesHistoryResetAndBoundsRefs(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	card := TaskCard{Goal: "ship", Status: "active"}
	for i := 1; i <= 20; i++ {
		card.MessageRefs = appendBoundedInt64(card.MessageRefs, int64(i), 12)
		card.ArtifactRefs = appendBoundedString(card.ArtifactRefs, "art-"+strings.Repeat("x", 0)+string(rune('a'+(i%26))), 12)
		card.ActiveFiles = appendBoundedString(card.ActiveFiles, "file-"+string(rune('a'+(i%26))), 12)
	}
	if err := saveTaskCard(ctx, d, "sess-reset", "", card); err != nil {
		t.Fatalf("saveTaskCard: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "sess-reset", "user", "hello", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := d.ResetSessionHistory(ctx, "sess-reset"); err != nil {
		t.Fatalf("ResetSessionHistory: %v", err)
	}
	loaded, ok, err := loadTaskCard(ctx, d, "sess-reset")
	if err != nil {
		t.Fatalf("loadTaskCard: %v", err)
	}
	if !ok {
		t.Fatalf("expected task card to survive history reset")
	}
	if len(loaded.MessageRefs) != 12 || len(loaded.ArtifactRefs) != 12 || len(loaded.ActiveFiles) != 12 {
		t.Fatalf("expected bounded refs to survive persist/load, got %+v", loaded)
	}
	if loaded.MessageRefs[0] != 9 || loaded.MessageRefs[len(loaded.MessageRefs)-1] != 20 {
		t.Fatalf("expected last 12 message refs preserved, got %+v", loaded.MessageRefs)
	}
}

func TestTaskCardLoadModifySavePreservesExistingFields(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := saveTaskCard(ctx, d, "sess-merge", "", TaskCard{Goal: "ship", Constraints: []string{"keep diffs small"}, Status: "active"}); err != nil {
		t.Fatalf("saveTaskCard initial: %v", err)
	}
	loaded, ok, err := loadTaskCard(ctx, d, "sess-merge")
	if err != nil || !ok {
		t.Fatalf("loadTaskCard initial: ok=%v err=%v", ok, err)
	}
	loaded.Decisions = append(loaded.Decisions, "use additive migrations")
	if err := saveTaskCard(ctx, d, "sess-merge", "", loaded); err != nil {
		t.Fatalf("saveTaskCard update: %v", err)
	}
	reloaded, ok, err := loadTaskCard(ctx, d, "sess-merge")
	if err != nil || !ok {
		t.Fatalf("loadTaskCard updated: ok=%v err=%v", ok, err)
	}
	if len(reloaded.Constraints) != 1 || reloaded.Constraints[0] != "keep diffs small" || len(reloaded.Decisions) != 1 || reloaded.Decisions[0] != "use additive migrations" {
		t.Fatalf("expected caller-style load/modify/save merge behavior, got %+v", reloaded)
	}
}
