package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestPrepareRuntimeStorageCreatesDirsAndBootstrapFiles(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.DBPath = filepath.Join(tmp, "state", "or3.db")
	cfg.ArtifactsDir = filepath.Join(tmp, "artifacts")
	cfg.SoulFile = filepath.Join(tmp, "SOUL.md")
	cfg.AgentsFile = filepath.Join(tmp, "AGENTS.md")
	cfg.ToolsFile = filepath.Join(tmp, "TOOLS.md")
	cfg.IdentityFile = filepath.Join(tmp, "IDENTITY.md")
	cfg.MemoryFile = filepath.Join(tmp, "MEMORY.md")

	if err := prepareRuntimeStorage(cfg); err != nil {
		t.Fatalf("prepareRuntimeStorage: %v", err)
	}
	for _, path := range []string{filepath.Dir(cfg.DBPath), cfg.ArtifactsDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", path)
		}
	}
	for _, path := range []string{cfg.SoulFile, cfg.AgentsFile, cfg.ToolsFile, cfg.IdentityFile, cfg.MemoryFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("expected %s to contain bootstrap text", path)
		}
	}
}

func TestOpenRuntimeDatabaseReportsSanitizedPrefix(t *testing.T) {
	cfg := config.Default()
	cfg.DBPath = filepath.Join(t.TempDir(), "missing", "state.db")
	_, err := openRuntimeDatabase(cfg)
	if err == nil || !strings.Contains(err.Error(), "db:") {
		t.Fatalf("expected prefixed db error, got %v", err)
	}
}
