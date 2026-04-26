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
	if err := d.EnsureSession(ctx, "sess1"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

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
	info, err := os.Stat(filepath.Join(dir, id))
	if err != nil {
		t.Fatalf("Stat artifact: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected artifact mode 0600, got %#o", info.Mode().Perm())
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
	if err := d.EnsureSession(ctx, "sess1"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

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
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("expected artifacts dir mode 0700, got %#o", info.Mode().Perm())
	}
}

func TestStore_Save_MultipleArtifacts(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

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

func TestStore_SaveNamedAndLookup(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	store := &Store{Dir: dir, DB: d}
	att, err := store.SaveNamed(ctx, "sess", "photo.png", "image/png", []byte("png-data"))
	if err != nil {
		t.Fatalf("SaveNamed: %v", err)
	}
	if att.ArtifactID == "" {
		t.Fatal("expected artifact id")
	}
	if att.Kind != KindImage {
		t.Fatalf("expected image kind, got %q", att.Kind)
	}
	if att.Filename != "photo.png" {
		t.Fatalf("expected filename to round-trip, got %q", att.Filename)
	}

	stored, err := store.Lookup(ctx, att.ArtifactID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if stored.Mime != "image/png" {
		t.Fatalf("expected mime image/png, got %q", stored.Mime)
	}
	content, err := os.ReadFile(stored.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "png-data" {
		t.Fatalf("unexpected stored content: %q", string(content))
	}
}

func TestStore_ReadCapped_ChecksSessionAndBoundsContent(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "sess", "text/plain", []byte("abcdef"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.ReadCapped(ctx, "sess", id, 3)
	if err != nil {
		t.Fatalf("ReadCapped: %v", err)
	}
	if got.Content != "abc" || !got.Truncated || got.ReadBytes != 3 {
		t.Fatalf("expected bounded truncated read, got %+v", got)
	}
	if _, err := store.ReadCapped(ctx, "other", id, 3); err == nil {
		t.Fatalf("expected cross-session read to be denied")
	}
}

func TestStore_Save_CreatesSessionAutomatically(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "fresh-session", "text/plain", []byte("artifact content"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Fatal("expected artifact id")
	}
	var count int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE key=?`, "fresh-session").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected session to be created, got count=%d", count)
	}
}

func TestStore_SaveNamed_NoDirSet(t *testing.T) {
	d := openTestDB(t)
	store := &Store{DB: d}
	if _, err := store.SaveNamed(context.Background(), "sess", "photo.png", "image/png", []byte("png-data")); err == nil {
		t.Fatal("expected error when artifacts dir is not configured")
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
