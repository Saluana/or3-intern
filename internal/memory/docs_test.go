package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

func openDocsTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSyncRoots(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	// create a temp directory with some files
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# Hello\nThis is a test document."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}},
	}

	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots: %v", err)
	}

	rows, err := d.SQL.QueryContext(ctx, `SELECT path, kind, active FROM memory_docs WHERE scope_key='scope1' ORDER BY path`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type row struct {
		path, kind string
		active     int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.path, &r.kind, &r.active); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 indexed doc, got %d: %v", len(got), got)
	}
	for _, r := range got {
		if r.active != 1 {
			t.Errorf("expected active=1 for %s, got %d", r.path, r.active)
		}
	}

	// verify kinds
	kinds := map[string]string{}
	for _, r := range got {
		kinds[filepath.Base(r.path)] = r.kind
	}
	if kinds["readme.md"] != "markdown" {
		t.Errorf("expected markdown, got %q", kinds["readme.md"])
	}
	if _, ok := kinds["main.go"]; ok {
		t.Errorf("did not expect code files to be indexed by default")
	}
}

func TestRetrieveDocs(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	err := UpsertDoc(ctx, d, "scope1", "/docs/guide.md", "markdown", "User Guide",
		"How to get started", "Getting started: install the tool and run it.", nil, "abc123", 0, 100)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}
	err = UpsertDoc(ctx, d, "scope1", "/docs/api.md", "markdown", "API Reference",
		"API endpoints list", "The API exposes REST endpoints for integration.", nil, "def456", 0, 200)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	r := &DocRetriever{DB: d}

	results, err := r.RetrieveDocs(ctx, "scope1", "install tool", 5)
	if err != nil {
		t.Fatalf("RetrieveDocs: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	found := false
	for _, res := range results {
		if res.Title == "User Guide" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected User Guide in results, got %v", results)
	}
}

func TestRetrieveDocs_Empty(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	r := &DocRetriever{DB: d}
	results, err := r.RetrieveDocs(ctx, "scope1", "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for blank query, got %d", len(results))
	}
}

func TestSyncRootsDeactivation(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	root := t.TempDir()
	filePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(filePath, []byte("important note content"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonicalPath, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}},
	}

	// first sync - file should be active
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots: %v", err)
	}
	var active int
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT active FROM memory_docs WHERE scope_key='scope1' AND path=?`, canonicalPath,
	).Scan(&active); err != nil {
		t.Fatalf("query after first sync: %v", err)
	}
	if active != 1 {
		t.Fatalf("expected active=1 after first sync, got %d", active)
	}

	// delete the file and sync again
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots after delete: %v", err)
	}

	if err := d.SQL.QueryRowContext(ctx,
		`SELECT active FROM memory_docs WHERE scope_key='scope1' AND path=?`, canonicalPath,
	).Scan(&active); err != nil {
		t.Fatalf("query after second sync: %v", err)
	}
	if active != 0 {
		t.Errorf("expected active=0 after file deleted, got %d", active)
	}
}

func TestSyncRootsCaps(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	root := t.TempDir()
	for i := 0; i < 10; i++ {
		name := filepath.Join(root, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(name, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}, MaxFiles: 3},
	}

	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots: %v", err)
	}

	var count int
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_docs WHERE scope_key='scope1' AND active=1`,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count > 3 {
		t.Errorf("expected at most 3 docs (MaxFiles=3), got %d", count)
	}
}

func TestSyncRoots_NoRoots(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{},
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("expected no error with no roots, got %v", err)
	}
}

func TestSyncRoots_Idempotent(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("key = \"value\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}},
	}

	// sync twice - should not error or duplicate
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("first SyncRoots: %v", err)
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("second SyncRoots: %v", err)
	}

	var count int
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_docs WHERE scope_key='scope1' AND active=1`,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 doc after idempotent sync, got %d", count)
	}
}

func TestSyncRoots_ReembedsWhenFingerprintChanges(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()
	root := t.TempDir()
	filePath := filepath.Join(root, "readme.md")
	if err := os.WriteFile(filePath, []byte("# Hello\nThis is a fingerprint test document."), 0o644); err != nil {
		t.Fatal(err)
	}

	embedCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		embedCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{1, 0}}},
		})
	}))
	defer srv.Close()

	p := providers.New(srv.URL, "test-key", 5*time.Second)
	p.HTTP = srv.Client()

	indexer := &DocIndexer{
		DB:               d,
		Provider:         p,
		EmbedModel:       "embed",
		EmbedFingerprint: "provider-a:embed",
		Config:           DocIndexConfig{Roots: []string{root}},
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("first SyncRoots: %v", err)
	}
	if embedCalls != 1 {
		t.Fatalf("expected 1 embed call after first sync, got %d", embedCalls)
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("second SyncRoots: %v", err)
	}
	if embedCalls != 1 {
		t.Fatalf("expected no re-embed when fingerprint unchanged, got %d calls", embedCalls)
	}
	indexer.EmbedFingerprint = "provider-b:embed"
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("third SyncRoots: %v", err)
	}
	if embedCalls != 2 {
		t.Fatalf("expected fingerprint change to trigger re-embed, got %d calls", embedCalls)
	}
	var fingerprint string
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT embed_fingerprint FROM memory_docs WHERE scope_key='scope1' AND active=1 LIMIT 1`,
	).Scan(&fingerprint); err != nil {
		t.Fatalf("fingerprint query: %v", err)
	}
	if fingerprint != "provider-b:embed" {
		t.Fatalf("expected updated fingerprint, got %q", fingerprint)
	}
}
