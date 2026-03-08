package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/clawhub"
	"or3-intern/internal/config"
	"or3-intern/internal/skills"
)

func TestRunSkillsCommand_ListEligible(t *testing.T) {
	cfg := config.Default()
	deps := skillsCommandDeps{
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			return skills.Inventory{
				Skills: []skills.SkillMeta{
					{Name: "visible", Eligible: true, Source: skills.SourceWorkspace, Dir: "/tmp/visible"},
					{Name: "blocked", Eligible: false, Source: skills.SourceManaged, Dir: "/tmp/blocked"},
				},
			}
		},
	}
	var out bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &out

	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"list", "--eligible"}, deps); err != nil {
		t.Fatalf("runSkillsCommandWithDeps: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "visible\teligible\tworkspace") {
		t.Fatalf("unexpected output: %q", text)
	}
	if strings.Contains(text, "blocked") {
		t.Fatalf("expected ineligible skill to be filtered, got %q", text)
	}
}

func TestRunSkillsCommand_InstallUpdateRefusesLocalEditsAndRemove(t *testing.T) {
	zipBytes := makeTestZip(t, map[string]string{
		"SKILL.md": "---\nname: demo\ndescription: demo\n---\n# Demo\n",
		"tool.sh":  "#!/bin/sh\necho demo\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/demo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{
					"slug":        "demo",
					"displayName": "Demo",
					"summary":     "demo",
				},
				"latestVersion": map[string]any{"version": "1.0.1"},
			})
		case r.URL.Path == "/api/v1/download":
			w.Write(zipBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Skills.ManagedDir = filepath.Join(t.TempDir(), "managed-skills")
	cfg.Skills.ClawHub.SiteURL = server.URL
	cfg.Skills.ClawHub.RegistryURL = server.URL
	client := clawhub.New(server.URL, server.URL)
	client.HTTP = server.Client()

	var out bytes.Buffer
	deps := skillsCommandDeps{
		Client: client,
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			return skills.Inventory{}
		},
		Stdout: &out,
		Stderr: &out,
	}
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"install", "--version", "1.0.0", "demo"}, deps); err != nil {
		t.Fatalf("install: %v", err)
	}
	installed := filepath.Join(resolveInstallRoot(cfg), "demo", "tool.sh")
	if err := os.WriteFile(installed, []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatalf("modify installed file: %v", err)
	}
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"update", "demo"}, deps); err == nil {
		t.Fatal("expected update to refuse local edits")
	}
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"remove", "demo"}, deps); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(resolveInstallRoot(cfg), "demo")); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory removed, stat err=%v", err)
	}
}

func TestResolveInstallRoot_PrefersManagedDirOverWorkspace(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = filepath.Join(t.TempDir(), "workspace")
	cfg.Skills.ManagedDir = filepath.Join(t.TempDir(), "managed")

	if got := resolveInstallRoot(cfg); got != cfg.Skills.ManagedDir {
		t.Fatalf("expected managed dir %q, got %q", cfg.Skills.ManagedDir, got)
	}
}

func makeTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, body := range files {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}
