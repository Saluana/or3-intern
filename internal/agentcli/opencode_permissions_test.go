package agentcli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestOpenCodeExternalDirectoriesFromConfig(t *testing.T) {
	workspace := t.TempDir()
	allowed := t.TempDir()
	managed := t.TempDir()
	global := t.TempDir()
	extra := t.TempDir()

	cfg := config.Default()
	cfg.WorkspaceDir = workspace
	cfg.AllowedDir = allowed
	cfg.Skills.ManagedDir = managed
	cfg.Skills.Load.GlobalDir = global
	cfg.Skills.Load.ExtraDirs = []string{extra, managed, "  "}

	got := OpenCodeExternalDirectoriesFromConfig(cfg)
	want := []string{workspace, allowed, managed, global, extra}
	assertArgsEqual(t, want, got)
}

func TestOpenCodeExternalDirectoriesFromConfig_SkipsDisabledGlobalDir(t *testing.T) {
	cfg := config.Default()
	cfg.Skills.Load.GlobalDir = t.TempDir()
	cfg.Skills.Load.DisableGlobalDir = true

	got := OpenCodeExternalDirectoriesFromConfig(cfg)
	for _, dir := range got {
		if dir == cfg.Skills.Load.GlobalDir {
			t.Fatalf("expected disabled global skills dir to be excluded: %v", got)
		}
	}
}

func TestOpenCodeConfigContent(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	raw, ok := openCodeConfigContent([]string{dirA, dirB, dirA})
	if !ok {
		t.Fatal("expected config content")
	}
	var payload struct {
		Permission struct {
			ExternalDirectory map[string]string `json:"external_directory"`
		} `json:"permission"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal config content: %v", err)
	}
	want := map[string]string{
		filepath.Join(dirA, "*"): "allow",
		filepath.Join(dirB, "*"): "allow",
	}
	if len(payload.Permission.ExternalDirectory) != len(want) {
		t.Fatalf("expected %d permission rules, got %d: %#v", len(want), len(payload.Permission.ExternalDirectory), payload.Permission.ExternalDirectory)
	}
	for key, wantValue := range want {
		if gotValue := payload.Permission.ExternalDirectory[key]; gotValue != wantValue {
			t.Fatalf("expected permission rule %q=%q, got %q", key, wantValue, gotValue)
		}
	}
}

func TestMergeEnvOverlay(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/tmp/home"}
	got := mergeEnvOverlay(base, map[string]string{
		"HOME":                      "/tmp/override",
		openCodeConfigContentEnvVar: "{\"permission\":{}}",
	})
	want := []string{"PATH=/usr/bin", "HOME=/tmp/override", openCodeConfigContentEnvVar + "={\"permission\":{}}"}
	assertArgsEqual(t, want, got)
}
