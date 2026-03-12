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
		LoadToolNames: func(context.Context, config.Config) map[string]struct{} {
			return map[string]struct{}{"mcp_demo_echo": {}}
		},
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			if _, ok := toolNames["mcp_demo_echo"]; !ok {
				t.Fatalf("expected MCP tool names to be passed into inventory: %#v", toolNames)
			}
			return skills.Inventory{
				Skills: []skills.SkillMeta{
					{Name: "visible", Eligible: true, PermissionState: "approved", Source: skills.SourceWorkspace, Dir: "/tmp/visible"},
					{Name: "blocked", Eligible: false, PermissionState: "blocked", Source: skills.SourceManaged, Dir: "/tmp/blocked"},
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
	if !strings.Contains(text, "visible\teligible\tapproved\tworkspace") {
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
		LoadToolNames: func(context.Context, config.Config) map[string]struct{} {
			return map[string]struct{}{}
		},
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

func TestRunSkillsCommand_InfoAndCheckUseConfiguredToolNames(t *testing.T) {
	cfg := config.Default()
	root := t.TempDir()
	demoDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(demoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(demoDir, "SKILL.md"), []byte(`---
name: demo
description: demo skill
command-dispatch: tool
command-tool: mcp_demo_echo
command-arg-mode: raw
---
# Demo
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	toolNamesCalls := 0
	deps := skillsCommandDeps{
		LoadToolNames: func(context.Context, config.Config) map[string]struct{} {
			toolNamesCalls++
			return map[string]struct{}{"mcp_demo_echo": {}}
		},
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			if _, ok := toolNames["mcp_demo_echo"]; !ok {
				t.Fatalf("expected MCP tool names to be passed into inventory: %#v", toolNames)
			}
			return skills.ScanWithOptions(skills.LoadOptions{
				Roots:          []skills.Root{{Path: root, Source: skills.SourceWorkspace}},
				AvailableTools: toolNames,
			})
		},
	}

	var infoOut bytes.Buffer
	deps.Stdout = &infoOut
	deps.Stderr = &infoOut
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"info", "demo"}, deps); err != nil {
		t.Fatalf("info: %v", err)
	}
	if !strings.Contains(infoOut.String(), "Command Tool: mcp_demo_echo") {
		t.Fatalf("expected command tool in info output, got %q", infoOut.String())
	}
	if !strings.Contains(infoOut.String(), "Permission State: approved") {
		t.Fatalf("expected permission state in info output, got %q", infoOut.String())
	}

	var checkOut bytes.Buffer
	deps.Stdout = &checkOut
	deps.Stderr = &checkOut
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"check"}, deps); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut.String(), "[ok] demo") {
		t.Fatalf("expected eligible skill in check output, got %q", checkOut.String())
	}
	if toolNamesCalls != 2 {
		t.Fatalf("expected tool names loader to be used for both info and check, got %d calls", toolNamesCalls)
	}
}

func TestRunSkillsCommand_CheckShowsQuarantinedSkill(t *testing.T) {
	cfg := config.Default()
	deps := skillsCommandDeps{
		LoadToolNames: func(context.Context, config.Config) map[string]struct{} { return map[string]struct{}{} },
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			return skills.Inventory{Skills: []skills.SkillMeta{{Name: "runner", Eligible: true, PermissionState: "quarantined", PermissionNotes: []string{"operator approval required before script execution"}}}}
		},
	}
	var out bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &out
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"check"}, deps); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out.String(), "[quarantined] runner") {
		t.Fatalf("expected quarantined skill output, got %q", out.String())
	}
}

func TestRunSkillsCommand_InfoShowsSupplyChainMetadata(t *testing.T) {
	cfg := config.Default()
	root := t.TempDir()
	bundle := filepath.Join(root, "runner")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "SKILL.md"), []byte("---\nname: runner\ndescription: runner\n---\n# Runner\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile skill.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile run.sh: %v", err)
	}
	if err := clawhub.WriteOrigin(bundle, clawhub.SkillOrigin{
		Version:          2,
		Registry:         "https://clawhub.ai",
		Owner:            "demo-owner",
		Slug:             "runner",
		InstalledVersion: "1.0.0",
		InstalledAt:      1,
		Fingerprint:      "mismatch-on-purpose",
		ScanStatus:       "quarantined",
		ScanFindings:     []clawhub.ScanFinding{{Severity: "medium", Path: "run.sh", Rule: "curl-pipe-shell", Message: "downloads remote content directly into a shell"}},
	}); err != nil {
		t.Fatalf("WriteOrigin: %v", err)
	}
	deps := skillsCommandDeps{
		LoadToolNames: func(context.Context, config.Config) map[string]struct{} { return map[string]struct{}{} },
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			return skills.ScanWithOptions(skills.LoadOptions{Roots: []skills.Root{{Path: root, Source: skills.SourceManaged}}, ApprovalPolicy: skills.ApprovalPolicy{QuarantineByDefault: true}})
		},
	}
	var out bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &out
	if err := runSkillsCommandWithDeps(context.Background(), cfg, []string{"info", "runner"}, deps); err != nil {
		t.Fatalf("info: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Publisher: demo-owner") || !strings.Contains(text, "Registry: https://clawhub.ai") || !strings.Contains(text, "Scan Status: quarantined") {
		t.Fatalf("expected supply-chain metadata in info output, got %q", text)
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

func TestAvailableToolNames_IncludesCuratedMemoryReadTools(t *testing.T) {
	got := availableToolNames(false, false)
	for _, name := range []string{"memory_recent", "memory_get_pinned"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("expected tool name %q to be available", name)
		}
	}
}

func TestFilterAdvertisedToolNames_HostedNoExecHidesExecTools(t *testing.T) {
	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedNoExec
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Skills.EnableExec = true

	got := filterAdvertisedToolNames(cfg, availableToolNames(false, false))
	if _, ok := got["exec"]; ok {
		t.Fatalf("expected exec to be hidden in hosted-no-exec, got %#v", got)
	}
	if _, ok := got["run_skill_script"]; ok {
		t.Fatalf("expected run_skill_script to be hidden in hosted-no-exec, got %#v", got)
	}
}

func TestFilterAdvertisedToolNames_RemoteSandboxRequiresSandboxForExecTools(t *testing.T) {
	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
	cfg.Hardening.GuardedTools = true
	cfg.Skills.EnableExec = true

	got := filterAdvertisedToolNames(cfg, availableToolNames(false, false))
	if _, ok := got["exec"]; ok {
		t.Fatalf("expected exec to be hidden without sandbox, got %#v", got)
	}
	if _, ok := got["run_skill_script"]; ok {
		t.Fatalf("expected run_skill_script to be hidden without sandbox, got %#v", got)
	}

	cfg.Hardening.Sandbox.Enabled = true
	got = filterAdvertisedToolNames(cfg, availableToolNames(false, false))
	if _, ok := got["exec"]; !ok {
		t.Fatalf("expected exec to return when sandbox is enabled, got %#v", got)
	}
	if _, ok := got["run_skill_script"]; !ok {
		t.Fatalf("expected run_skill_script to return when sandbox is enabled, got %#v", got)
	}
}

func TestBuildSkillsInventory_HostedProfilesForceQuarantineByDefault(t *testing.T) {
	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedService
	cfg.Skills.Policy.QuarantineByDefault = false
	cfg.WorkspaceDir = t.TempDir()

	skillDir := filepath.Join(cfg.WorkspaceDir, "skills", "runner")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Runner\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "tool.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile script: %v", err)
	}

	inv := buildSkillsInventory(cfg, "", map[string]struct{}{"exec": {}})
	skill, ok := inv.Get("runner")
	if !ok {
		t.Fatal("expected runner skill in inventory")
	}
	if skill.PermissionState != "quarantined" {
		t.Fatalf("expected hosted profile to quarantine runnable skill by default, got %#v", skill)
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
