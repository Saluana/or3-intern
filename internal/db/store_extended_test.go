package db

import (
	"context"
	"path/filepath"
	"testing"
)

func openTestDBForExtended(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestMemoryNotesExtendedColumns(t *testing.T) {
	d := openTestDBForExtended(t)
	ctx := context.Background()

	id, err := d.InsertMemoryNoteTyped(ctx, "sess-ext", TypedNoteInput{
		Text:             "artifact summary text",
		Kind:             MemoryKindArtifactSummary,
		Status:           MemoryStatusActive,
		Summary:          "short summary",
		SourceArtifactID: "artifact-123",
		Confidence:       0.8,
		ExpiresAt:        9999999999,
		SupersedesID:     42,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	var summary, sourceArtifactID string
	var confidence float64
	var expiresAt, supersedesID int64
	row := d.SQL.QueryRowContext(ctx,
		`SELECT summary, source_artifact_id, confidence, expires_at, supersedes_id FROM memory_notes WHERE id=?`, id)
	if err := row.Scan(&summary, &sourceArtifactID, &confidence, &expiresAt, &supersedesID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if summary != "short summary" {
		t.Errorf("summary = %q, want %q", summary, "short summary")
	}
	if sourceArtifactID != "artifact-123" {
		t.Errorf("source_artifact_id = %q, want %q", sourceArtifactID, "artifact-123")
	}
	if confidence != 0.8 {
		t.Errorf("confidence = %f, want 0.8", confidence)
	}
	if expiresAt != 9999999999 {
		t.Errorf("expires_at = %d, want 9999999999", expiresAt)
	}
	if supersedesID != 42 {
		t.Errorf("supersedes_id = %d, want 42", supersedesID)
	}
}

func TestMarkMemoryNoteStale(t *testing.T) {
	d := openTestDBForExtended(t)
	ctx := context.Background()

	id, err := d.InsertMemoryNoteTyped(ctx, "sess-stale", TypedNoteInput{
		Text:   "note to be made stale",
		Kind:   MemoryKindFact,
		Status: MemoryStatusActive,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	if err := d.MarkMemoryNoteStale(ctx, id); err != nil {
		t.Fatalf("MarkMemoryNoteStale: %v", err)
	}

	var status string
	row := d.SQL.QueryRowContext(ctx, `SELECT status FROM memory_notes WHERE id=?`, id)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if status != MemoryStatusStale {
		t.Errorf("status = %q, want %q", status, MemoryStatusStale)
	}
}

func TestMemoryNoteBackwardCompat(t *testing.T) {
	d := openTestDBForExtended(t)
	ctx := context.Background()

	// Insert without new fields (defaults should apply)
	id, err := d.InsertMemoryNoteTyped(ctx, "sess-compat", TypedNoteInput{
		Text:   "old style note",
		Kind:   MemoryKindNote,
		Status: MemoryStatusActive,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	var summary, sourceArtifactID string
	var confidence float64
	var expiresAt, supersedesID int64
	row := d.SQL.QueryRowContext(ctx,
		`SELECT summary, source_artifact_id, confidence, expires_at, supersedes_id FROM memory_notes WHERE id=?`, id)
	if err := row.Scan(&summary, &sourceArtifactID, &confidence, &expiresAt, &supersedesID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// summary and source_artifact_id default to ''
	if summary != "" {
		t.Errorf("summary = %q, want empty", summary)
	}
	if sourceArtifactID != "" {
		t.Errorf("source_artifact_id = %q, want empty", sourceArtifactID)
	}
	// confidence defaults to 1.0
	if confidence != 1.0 {
		t.Errorf("confidence = %f, want 1.0", confidence)
	}
}
