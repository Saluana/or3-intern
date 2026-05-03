package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
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

func makeBlockingSkillInventory(t *testing.T) *skills.Inventory {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "runner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: runner\ndescription: run scripts\n---\n# Runner\n"), 0o644); err != nil {
		t.Fatalf("skill write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte("#!/bin/sh\nsleep 2\necho late\n"), 0o755); err != nil {
		t.Fatalf("script write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"entrypoints":[{"name":"hello","command":["./tool.sh"]}]}`), 0o644); err != nil {
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

	// Use the "hello" token with a different timeout → different approval subject → blocked.
	ctx := ContextWithApprovalToken(context.Background(), issued.Token)
	_, err = tool.Execute(ctx, map[string]any{
		"skill":          "runner",
		"path":           "tool.sh",
		"args":           []any{"mismatch"},
		"timeoutSeconds": 45.0,
	})
	if err == nil {
		t.Fatal("expected subject-mismatch to block skill execution, got nil error")
	}
	if !strings.Contains(err.Error(), "approval required") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected approval required or blocked, got %q", err.Error())
	}
}

func TestSkillCommandHash_IgnoresAppendedArgsForScriptHash(t *testing.T) {
	inv := makeExecutableSkillInventory(t)
	skill, ok := inv.Get("runner")
	if !ok {
		t.Fatal("expected runner skill")
	}
	tool := &RunSkillScript{Inventory: inv, Enabled: true}

	baseCmd, err := tool.commandForSkill(skill, map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	if err != nil {
		t.Fatalf("commandForSkill base: %v", err)
	}
	withArgsCmd, err := tool.commandForSkill(skill, map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
		"args":       []any{"tail"},
	})
	if err != nil {
		t.Fatalf("commandForSkill with args: %v", err)
	}

	if got, want := skillCommandHash(baseCmd), skillCommandHash(withArgsCmd); got != want {
		t.Fatalf("expected identical script hashes, got %q vs %q", got, want)
	}
}

func TestSkillCommandHashSource_PicksScriptInsteadOfTrailingArg(t *testing.T) {
	inv := makeExecutableSkillInventory(t)
	skill, ok := inv.Get("runner")
	if !ok {
		t.Fatal("expected runner skill")
	}
	tool := &RunSkillScript{Inventory: inv, Enabled: true}
	cmd, err := tool.commandForSkill(skill, map[string]any{
		"skill": "runner",
		"path":  "tool.sh",
		"args":  []any{"tail"},
	})
	if err != nil {
		t.Fatalf("commandForSkill: %v", err)
	}
	got := skillCommandHashSource(cmd)
	if got == "" || filepath.Base(got) != "tool.sh" {
		raw, _ := json.Marshal(cmd)
		t.Fatalf("expected tool.sh hash source, got %q from %s", got, raw)
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

func TestRunSkill_PersistsPendingPlanAndResumesByPlanID(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skill-run.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	broker := makeTestBroker(t, config.ApprovalModeAsk)
	tool := &RunSkill{RunSkillScript: RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
		DB:             database,
	}}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
	})
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected ApprovalRequiredError, got %T: %v", err, err)
	}
	result, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured pending result, got %q", out)
	}
	if result.Status != "pending_approval" || result.PlanID == "" || result.RequestID == 0 {
		t.Fatalf("unexpected pending result: %#v", result)
	}

	issued, err := broker.ApproveRequest(context.Background(), result.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	out, err = tool.Execute(ContextWithApprovalToken(context.Background(), issued.Token), map[string]any{"plan_id": result.PlanID})
	if err != nil {
		t.Fatalf("expected plan resume to succeed, got %v", err)
	}
	result, ok = DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured result, got %q", out)
	}
	if result.Status != "succeeded" || !strings.Contains(result.Preview, "script:entry") {
		t.Fatalf("unexpected resumed result: %#v", result)
	}
	stored, err := database.GetSkillRunPlan(context.Background(), result.PlanID)
	if err != nil {
		t.Fatalf("GetSkillRunPlan: %v", err)
	}
	if stored.Status != "succeeded" {
		t.Fatalf("expected stored plan success, got %#v", stored)
	}
}

func TestRunSkill_ReusesFrozenPlanWhenOriginalArgsAreRetried(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skill-run-retry.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	broker := makeTestBroker(t, config.ApprovalModeAsk)
	tool := &RunSkill{RunSkillScript: RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
		DB:             database,
	}}
	params := map[string]any{"skill": "runner", "entrypoint": "hello"}
	out, err := tool.Execute(context.Background(), params)
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected ApprovalRequiredError, got %T: %v", err, err)
	}
	first, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured pending result, got %q", out)
	}
	issued, err := broker.ApproveRequest(context.Background(), first.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	out, err = tool.Execute(ContextWithApprovalToken(context.Background(), issued.Token), params)
	if err != nil {
		t.Fatalf("expected exact-args retry to reuse the frozen plan, got %v", err)
	}
	second, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured result, got %q", out)
	}
	if second.PlanID != first.PlanID || second.Status != "succeeded" {
		t.Fatalf("expected exact-args retry to reuse plan %q, got %#v", first.PlanID, second)
	}
}

func TestRunSkill_ResumeFailsPreflightOnEnvDrift(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skill-run-env.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	broker := makeTestBroker(t, config.ApprovalModeAsk)
	t.Setenv("RUNNER_MODE", "initial")
	tool := &RunSkill{RunSkillScript: RunSkillScript{
		Inventory:         makeExecutableSkillInventory(t),
		Enabled:           true,
		ApprovalBroker:    broker,
		ChildEnvAllowlist: []string{"RUNNER_MODE"},
		DB:                database,
	}}
	out, err := tool.Execute(context.Background(), map[string]any{"skill": "runner", "entrypoint": "hello"})
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected ApprovalRequiredError, got %T: %v", err, err)
	}
	first, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured pending result, got %q", out)
	}
	issued, err := broker.ApproveRequest(context.Background(), first.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	t.Setenv("RUNNER_MODE", "drifted")
	out, err = tool.Execute(ContextWithApprovalToken(context.Background(), issued.Token), map[string]any{"plan_id": first.PlanID})
	if err == nil || !strings.Contains(err.Error(), "environment binding changed") {
		t.Fatalf("expected env drift to fail preflight, got out=%q err=%v", out, err)
	}
	second, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured preflight result, got %q", out)
	}
	if second.Status != db.SkillRunStatusStalePlan {
		t.Fatalf("expected stale_plan status, got %#v", second)
	}
}

func TestRunSkill_PersistsEncryptedStdinAndClearsItAfterCompletion(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skill-run-stdin.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	broker := makeTestBrokerWithDB(t, config.ApprovalModeAsk, database)
	tool := &RunSkill{RunSkillScript: RunSkillScript{
		Inventory:      makeExecutableSkillInventory(t),
		Enabled:        true,
		ApprovalBroker: broker,
		DB:             database,
	}}
	stdinText := "super-secret-stdin"
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "runner",
		"entrypoint": "hello",
		"stdin":      stdinText,
	})
	var approvalErr *ApprovalRequiredError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("expected ApprovalRequiredError, got %T: %v", err, err)
	}
	first, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured pending result, got %q", out)
	}
	stored, err := database.GetSkillRunPlan(context.Background(), first.PlanID)
	if err != nil {
		t.Fatalf("GetSkillRunPlan before approval: %v", err)
	}
	if stored.StdinText == "" || stored.StdinText == stdinText {
		t.Fatalf("expected encrypted stdin at rest, got %#v", stored)
	}
	if len(stored.StdinNonce) == 0 || strings.TrimSpace(stored.StdinSHA256) == "" {
		t.Fatalf("expected stdin encryption metadata, got %#v", stored)
	}
	issued, err := broker.ApproveRequest(context.Background(), first.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	out, err = tool.Execute(ContextWithApprovalToken(context.Background(), issued.Token), map[string]any{"plan_id": first.PlanID})
	if err != nil {
		t.Fatalf("expected plan resume to succeed, got %v", err)
	}
	second, ok := DecodeToolResult(out)
	if !ok || second.Status != string(db.SkillRunStatusSucceeded) {
		t.Fatalf("expected successful result, got out=%q", out)
	}
	stored, err = database.GetSkillRunPlan(context.Background(), first.PlanID)
	if err != nil {
		t.Fatalf("GetSkillRunPlan after completion: %v", err)
	}
	if stored.StdinText != "" || len(stored.StdinNonce) != 0 || stored.StdinSHA256 != "" {
		t.Fatalf("expected stdin metadata to be cleared, got %#v", stored)
	}
}

func TestRunSkill_LoadingRunningPlanReturnsRunningState(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skill-run-running.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	inv := makeExecutableSkillInventory(t)
	ctx := context.Background()
	tool := &RunSkill{RunSkillScript: RunSkillScript{Inventory: inv, Enabled: true, DB: database}}
	skill, ok := inv.Get("runner")
	if !ok {
		t.Fatal("expected runner skill")
	}
	cmd, err := tool.commandForSkill(skill, map[string]any{"skill": "runner", "entrypoint": "hello"})
	if err != nil {
		t.Fatalf("commandForSkill: %v", err)
	}
	commandJSON, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal command: %v", err)
	}
	plan, err := database.CreateSkillRunPlan(ctx, db.SkillRunPlanRecord{
		ID:              "srp_running",
		SkillID:         skill.Name,
		Version:         skill.InstalledVersion,
		Origin:          skill.Registry,
		TrustState:      skill.PermissionState,
		SkillDir:        skill.Dir,
		Entrypoint:      "hello",
		ArgsJSON:        `[]`,
		TimeoutSeconds:  30,
		CommandJSON:     string(commandJSON),
		ScriptHash:      skillCommandHash(cmd),
		EnvBindingHash:  hashEnvBinding(BuildChildEnv(os.Environ(), nil, EnvFromContext(ctx), "")),
		PlanHash:        "plan-running",
		ExecutionHostID: "test-host",
		Status:          string(db.SkillRunStatusRunning),
		CreatedAt:       db.NowMS(),
		UpdatedAt:       db.NowMS(),
	})
	if err != nil {
		t.Fatalf("CreateSkillRunPlan: %v", err)
	}
	out, err := tool.Execute(ctx, map[string]any{"plan_id": plan.ID})
	if err != nil {
		t.Fatalf("expected running plan lookup to be non-fatal, got %v", err)
	}
	result, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured result, got %q", out)
	}
	if result.Status != string(db.SkillRunStatusRunning) {
		t.Fatalf("expected running status, got %#v", result)
	}
	if strings.Contains(result.Preview, "script:entry") {
		t.Fatalf("expected running plan not to execute again, got %#v", result)
	}
}

func TestRunSkill_TimeoutIsClassifiedAndPersisted(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "skill-run-timeout.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	tool := &RunSkill{RunSkillScript: RunSkillScript{
		Inventory: makeBlockingSkillInventory(t),
		Enabled:   true,
		Timeout:   time.Second,
		DB:        database,
	}}
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":          "runner",
		"entrypoint":     "hello",
		"timeoutSeconds": 1,
	})
	if err == nil || !strings.Contains(err.Error(), "exec failed") {
		t.Fatalf("expected timeout execution failure, got out=%q err=%v", out, err)
	}
	result, ok := DecodeToolResult(out)
	if !ok {
		t.Fatalf("expected structured result, got %q", out)
	}
	if result.Status != string(db.SkillRunStatusTimedOut) {
		t.Fatalf("expected timed_out status, got %#v", result)
	}
	stored, err := database.GetSkillRunPlan(context.Background(), result.PlanID)
	if err != nil {
		t.Fatalf("GetSkillRunPlan: %v", err)
	}
	if stored.Status != string(db.SkillRunStatusTimedOut) {
		t.Fatalf("expected timed_out plan status, got %#v", stored)
	}
}
