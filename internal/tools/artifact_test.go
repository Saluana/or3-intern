package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/db"
)

func openArtifactToolTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestReadArtifactToolUsesCurrentSessionAndLimit(t *testing.T) {
	d := openArtifactToolTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	ctx := context.Background()
	id, err := store.Save(ctx, "sess", "text/plain", []byte("abcdef"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	tool := &ReadArtifact{Store: store, MaxReadBytes: 4}
	out, err := tool.Execute(ContextWithSession(ctx, "sess"), map[string]any{"artifact_id": id})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "abcd") || !strings.Contains(out, "[truncated]") {
		t.Fatalf("expected bounded artifact output, got %q", out)
	}
	if _, err := tool.Execute(ContextWithSession(ctx, "other"), map[string]any{"artifact_id": id}); err == nil {
		t.Fatalf("expected cross-session artifact read denial")
	}
}
