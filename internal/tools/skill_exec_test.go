package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/skills"
)

func makeExecutableSkillInventory(t *testing.T) *skills.Inventory {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "runner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: runner\ndescription: run scripts\n---\n# Runner\n"), 0o644); err != nil {
		t.Fatalf("skill write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte("#!/bin/sh\necho script:$*\n"), 0o755); err != nil {
		t.Fatalf("script write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"entrypoints":[{"name":"hello","command":["./tool.sh","entry"]}]}`), 0o644); err != nil {
		t.Fatalf("manifest write: %v", err)
	}
	inv := skills.ScanWithOptions(skills.LoadOptions{Roots: []skills.Root{{Path: root, Source: skills.SourceWorkspace}}, ApprovalPolicy: skills.ApprovalPolicy{QuarantineByDefault: true, ApprovedSkills: map[string]struct{}{"runner": {}}}})
	return &inv
}

func TestRunSkillScript_PathExecution(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t), Enabled: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill": "runner",
		"path":  "tool.sh",
		"args":  []any{"arg1"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:arg1") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_EntrypointExecution(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t), Enabled: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:entry") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_EntrypointExecution_AppendsArgs(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t), Enabled: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
		"args":       []any{"tail"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:entry tail") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_RejectsPathEscape(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t), Enabled: true}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"skill": "runner",
		"path":  "../escape.sh",
	}); err == nil {
		t.Fatal("expected path escape to fail")
	}
}

func TestRunSkillScript_RejectsQuarantinedSkill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "runner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Runner\n"), 0o644); err != nil {
		t.Fatalf("skill write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("script write: %v", err)
	}
	inv := skills.ScanWithOptions(skills.LoadOptions{Roots: []skills.Root{{Path: root, Source: skills.SourceWorkspace}}, ApprovalPolicy: skills.ApprovalPolicy{QuarantineByDefault: true}})
	tool := &RunSkillScript{Inventory: &inv, Enabled: true}
	if _, err := tool.Execute(context.Background(), map[string]any{"skill": "runner", "path": "tool.sh"}); err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected approval error, got %v", err)
	}
}

func TestRunSkillScript_BubblewrapEnabled(t *testing.T) {
	tool := &RunSkillScript{Inventory: makeExecutableSkillInventory(t), Enabled: true, Sandbox: BubblewrapConfig{Enabled: true, BubblewrapPath: writeFakeBubblewrap(t)}}
	out, err := tool.Execute(context.Background(), map[string]any{"skill": "runner", "entrypoint": "hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "script:entry") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_EmptyAllowlistFallsBackToScrubbedEnv(t *testing.T) {
	t.Setenv("INHERITED_SECRET", "top-secret")
	root := t.TempDir()
	dir := filepath.Join(root, "runner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: runner\ndescription: run scripts\n---\n# Runner\n"), 0o644); err != nil {
		t.Fatalf("skill write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "env.sh"), []byte("#!/bin/sh\nprintf %s \"${INHERITED_SECRET:-missing}\"\n"), 0o755); err != nil {
		t.Fatalf("script write: %v", err)
	}
	inv := skills.ScanWithOptions(skills.LoadOptions{Roots: []skills.Root{{Path: root, Source: skills.SourceWorkspace}}, ApprovalPolicy: skills.ApprovalPolicy{QuarantineByDefault: true, ApprovedSkills: map[string]struct{}{"runner": {}}}})
	tool := &RunSkillScript{Inventory: &inv, Enabled: true}
	out, err := tool.Execute(context.Background(), map[string]any{"skill": "runner", "path": "env.sh"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "missing") || strings.Contains(out, "top-secret") {
		t.Fatalf("expected inherited secret to be scrubbed, got %q", out)
	}
}

func TestRunSkillScript_ApprovalRequired_Blocks(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAsk)
	tool := &RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err == nil {
		t.Fatal("expected approval required error, got nil")
	}
	if !strings.Contains(err.Error(), "approval required") {
		t.Errorf("expected 'approval required' in error, got %q", err.Error())
	}
}

func TestRunSkillScript_DenyMode_Blocks(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeDeny)
	tool := &RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err == nil {
		t.Fatal("expected skill execution blocked error, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got %q", err.Error())
	}
}

func TestRunSkillScript_TrustedMode_Allows(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeTrusted)
	tool := &RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err != nil {
		t.Fatalf("expected trusted mode to allow skill execution, got error: %v", err)
	}
	if !strings.Contains(out, "script:entry") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_ApproveOnce_AllowsWithToken(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAsk)
	tool := &RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
	}
	params := map[string]any{"skill": "runner", "entrypoint": "hello"}

	// First attempt should require approval.
	_, err := tool.Execute(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "approval required") {
		t.Fatalf("expected approval required on first attempt, got %v", err)
	}

	// Approve the pending request.
	requests, listErr := broker.ListApprovalRequests(context.Background(), "pending", 10)
	if listErr != nil {
		t.Fatalf("ListApprovalRequests: %v", listErr)
	}
	if len(requests) == 0 {
		t.Fatal("expected at least one pending approval request")
	}
	issued, approveErr := broker.ApproveRequest(context.Background(), requests[0].ID, "cli:test", false, "ok")
	if approveErr != nil {
		t.Fatalf("ApproveRequest: %v", approveErr)
	}
	if issued.Token == "" {
		t.Fatal("expected non-empty approval token")
	}

	// Second attempt with the approval token should succeed.
	ctx := ContextWithApprovalToken(context.Background(), issued.Token)
	out, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("expected approved skill execution to succeed, got error: %v", err)
	}
	if !strings.Contains(out, "script:entry") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunSkillScript_SubjectMismatch_Blocks(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAsk)
	inv := makeExecutableSkillInventory(t)
	tool := &RunSkillScript{
		Inventory:      inv,
		Enabled:        true,
		ApprovalBroker: broker,
	}

	// Trigger and approve a request for "hello" entrypoint.
	_, _ = tool.Execute(context.Background(), map[string]any{"skill": "runner", "entrypoint": "hello"})
	requests, _ := broker.ListApprovalRequests(context.Background(), "pending", 10)
	if len(requests) == 0 {
		t.Fatal("expected pending request")
	}
	issued, err := broker.ApproveRequest(context.Background(), requests[0].ID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}

	// Use the "hello" token for the "runner/tool.sh" path → different subject hash → blocked.
	ctx := ContextWithApprovalToken(context.Background(), issued.Token)
	_, err = tool.Execute(ctx, map[string]any{"skill": "runner", "path": "tool.sh", "args": []any{"mismatch"}})
	if err == nil {
		t.Fatal("expected subject-mismatch to block skill execution, got nil error")
	}
	if !strings.Contains(err.Error(), "approval required") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected approval required or blocked, got %q", err.Error())
	}
}

func TestRunSkillScript_AllowlistMode_Allows(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAllowlist)
	// Add an allowlist rule for the runner skill with any script hash.
	_, err := broker.AddAllowlist(context.Background(), string(approval.SubjectSkillExec),
		approval.AllowlistScope{HostID: "test-host", Tool: "run_skill_script"},
		approval.SkillAllowlistMatcher{SkillID: "runner"},
		"cli:test", 0)
	if err != nil {
		t.Fatalf("AddAllowlist: %v", err)
	}
	tool := &RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err != nil {
		t.Fatalf("expected allowlist to allow skill execution, got error: %v", err)
	}
	if !strings.Contains(out, "script:entry") {
		t.Errorf("unexpected output: %q", out)
	}
}
