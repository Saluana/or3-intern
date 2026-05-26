package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestGetLastMessages_ClampLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "msg", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	msgs, err := d.GetLastMessages(ctx, "sess", 9999)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) > maxMessageHistoryLimit {
		t.Fatalf("expected clamp at %d, got %d messages", maxMessageHistoryLimit, len(msgs))
	}
}

func TestReplaceMemoryNoteEmbedding_SetsEmbeddingUpdatedAt(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	id, err := d.InsertMemoryNote(ctx, "sess", "hello", []byte{0, 0, 0, 0}, sqlNullInt64(), "")
	if err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	before, err := d.SQL.QueryContext(ctx, `SELECT embedding_updated_at FROM memory_notes WHERE id=?`, id)
	if err != nil {
		t.Fatalf("query before: %v", err)
	}
	var ts int64
	if !before.Next() {
		t.Fatal("missing note row")
	}
	_ = before.Scan(&ts)
	before.Close()
	if err := d.ReplaceMemoryNoteEmbedding(ctx, id, []byte{0, 0, 128, 63}, "fp"); err != nil {
		t.Fatalf("ReplaceMemoryNoteEmbedding: %v", err)
	}
	row := d.SQL.QueryRowContext(ctx, `SELECT embedding_updated_at FROM memory_notes WHERE id=?`, id)
	var after int64
	if err := row.Scan(&after); err != nil {
		t.Fatalf("query after: %v", err)
	}
	if after <= ts {
		t.Fatalf("expected embedding_updated_at to advance: before=%d after=%d", ts, after)
	}
}

func sqlNullInt64() sql.NullInt64 {
	return sql.NullInt64{}
}
