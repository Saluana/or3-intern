package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_DBPathWithURICharacters(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weird?name#1.db")
	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.SQL.ExecContext(t.Context(), `CREATE TABLE IF NOT EXISTS uri_probe(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db file at %q: %v", dbPath, err)
	}
}
