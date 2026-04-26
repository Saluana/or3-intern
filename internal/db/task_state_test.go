package db

import (
	"context"
	"testing"
)

func TestTaskStateUpsertFetchComplete(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.UpsertActiveTaskState(ctx, TaskStateRow{SessionKey: "sess", ScopeKey: "sess", Status: "active", Goal: "ship", PlanJSON: "[\"a\"]"}); err != nil {
		t.Fatalf("UpsertActiveTaskState: %v", err)
	}
	row, ok, err := d.GetActiveTaskState(ctx, "sess")
	if err != nil {
		t.Fatalf("GetActiveTaskState: %v", err)
	}
	if !ok || row.Goal != "ship" {
		t.Fatalf("expected active row, got ok=%v row=%+v", ok, row)
	}
	if err := d.CompleteActiveTaskState(ctx, "sess"); err != nil {
		t.Fatalf("CompleteActiveTaskState: %v", err)
	}
	_, ok, err = d.GetActiveTaskState(ctx, "sess")
	if err != nil {
		t.Fatalf("GetActiveTaskState after complete: %v", err)
	}
	if ok {
		t.Fatalf("expected no active task after completion")
	}
}

func TestMemoryNoteExtendedColumns(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	id, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{Text: "artifact summary", Summary: "summary", SourceArtifactID: "art-1", Kind: MemoryKindArtifact, Confidence: 0.7})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}
	var summary, source string
	var confidence float64
	if err := d.SQL.QueryRowContext(ctx, `SELECT summary, source_artifact_id, confidence FROM memory_notes WHERE id=?`, id).Scan(&summary, &source, &confidence); err != nil {
		t.Fatalf("query memory note: %v", err)
	}
	if summary == "" || source != "art-1" || confidence <= 0 {
		t.Fatalf("unexpected extended columns: summary=%q source=%q confidence=%f", summary, source, confidence)
	}
}
