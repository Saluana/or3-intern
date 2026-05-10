package agentcli

import (
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
)

func openAgentCLITestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
	})
	return d
}
