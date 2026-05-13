package main

import (
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestApplyRuntimeConfigDefaultsUsesCwdForRestrictedWorkspace(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	cfg := config.Default()
	cfg.Tools.RestrictToWorkspace = true
	cfg.WorkspaceDir = ""

	applyRuntimeConfigDefaults(&cfg)

	if cfg.WorkspaceDir != cwd {
		t.Fatalf("expected workspace dir %q, got %q", cwd, cfg.WorkspaceDir)
	}
}

func TestLoadRuntimeConfigKeepsShellEnvPrecedence(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("OR3_SERVICE_LISTEN=127.0.0.1:9998\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	cfg := config.Default()
	cfg.Service.Listen = "127.0.0.1:1111"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("OR3_SERVICE_LISTEN", "127.0.0.1:9999")

	loaded, err := loadRuntimeConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	if loaded.Config.Service.Listen != "127.0.0.1:9999" {
		t.Fatalf("expected shell env to win, got %q", loaded.Config.Service.Listen)
	}
}

func TestValidateRuntimeStartupCommandAllowsUnsafeDevOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Enabled = true
	cfg.Service.Secret = "short"
	if err := validateRuntimeStartupCommand("service", cfg, true); err != nil {
		t.Fatalf("expected unsafe dev override to allow weak secret warning, got %v", err)
	}
}
