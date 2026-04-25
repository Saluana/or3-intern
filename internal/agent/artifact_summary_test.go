package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/db"
)

func TestArtifactSummaryStoredAsMemoryNote(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	artifactsDir := t.TempDir()
	artStore := &artifacts.Store{
		Dir: artifactsDir,
		DB:  d,
	}

	if err := d.EnsureSession(ctx, "sess-artifact"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	r := &Runtime{
		DB:           d,
		Artifacts:    artStore,
		MaxToolBytes: 10, // very small so any reasonable text spills
	}

	largeText := strings.Repeat("hello world artifact content ", 5)
	stored, preview, artifactID := r.boundTextResult(ctx, "sess-artifact", largeText)

	if artifactID == "" {
		t.Fatalf("expected artifact to be stored, got stored=%q preview=%q", stored, preview)
	}

	// Check that a memory note was created
	rows, err := d.SearchFTS(ctx, "sess-artifact", "hello world artifact", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(rows) == 0 {
		t.Errorf("expected memory note for artifact summary, got none")
	}
	found := false
	for _, row := range rows {
		if row.Kind == db.MemoryKindArtifactSummary {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected memory note with kind %q", db.MemoryKindArtifactSummary)
	}
}

// Ensure the artifacts package path is recognized (just a compile check)
var _ = filepath.Join
