package app

import (
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/runnercontext"
)

func TestLoadRunnerBootstrapContextUsesConfiguredPaths(t *testing.T) {
	dir := t.TempDir()
	customSoul := filepath.Join(dir, "custom-soul.md")
	if err := os.WriteFile(customSoul, []byte("custom soul"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.WorkspaceDir = filepath.Join(dir, "workspace")
	cfg.SoulFile = customSoul

	bootstrap := LoadRunnerBootstrapContext(cfg)
	if bootstrap.Soul != "custom soul" {
		t.Fatalf("expected configured soul file, got %q", bootstrap.Soul)
	}
	if bootstrap.Soul == runnercontext.DefaultSoul {
		t.Fatal("expected custom soul instead of default soul")
	}
}
