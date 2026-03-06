package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestStore_Save_OK(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	// Create the session
	d.EnsureSession(ctx, "sess1")

	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "sess1", "text/plain", []byte("artifact content"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty artifact ID")
	}

	// Check file was created
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Errorf("expected 1 artifact file, got %d", len(files))
	}

	// Check content
	content, _ := os.ReadFile(filepath.Join(dir, id))
	if string(content) != "artifact content" {
		t.Errorf("expected 'artifact content', got %q", string(content))
	}
}

func TestStore_Save_NoDirSet(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	store := &Store{Dir: "", DB: d}
	_, err := store.Save(ctx, "sess1", "text/plain", []byte("data"))
	if err == nil {
		t.Fatal("expected error when Dir is not set")
	}
}

func TestStore_Save_CreatesDir(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	d.EnsureSession(ctx, "sess1")

	// Use a dir that doesn't exist yet
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "artifacts", "subdir")

	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "sess1", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty artifact ID")
	}

	// Dir should now exist
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected artifacts directory to be created")
	}
}

func TestStore_Save_MultipleArtifacts(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	d.EnsureSession(ctx, "sess")

	store := &Store{Dir: dir, DB: d}

	ids := map[string]bool{}
	for i := 0; i < 5; i++ {
		id, err := store.Save(ctx, "sess", "text/plain", []byte("data"))
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		if ids[id] {
			t.Errorf("duplicate artifact ID: %q", id)
		}
		ids[id] = true
	}
}

func TestRandID_NotEmpty(t *testing.T) {
	id := randID()
	if id == "" {
		t.Error("expected non-empty random ID")
	}
}

func TestRandID_Uniqueness(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := randID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %q", id)
		}
		ids[id] = true
	}
}
