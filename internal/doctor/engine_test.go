package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestProbeFindings_DoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.sqlite")
	cfg := config.Default()
	cfg.DBPath = path

	findings := probeFindings(cfg, Options{Probe: true})
	if len(findings) != 1 {
		t.Fatalf("expected one probe finding, got %#v", findings)
	}
	if findings[0].ID != "probe.sqlite_open_failed" {
		t.Fatalf("expected sqlite probe failure, got %#v", findings)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected probe to avoid creating %q, stat err=%v", path, err)
	}
}
