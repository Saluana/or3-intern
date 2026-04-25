package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func writeFakeBubblewrap(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bwrap")
	content := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = \"--\" ]; then\n    shift\n    exec \"$@\"\n  fi\n  shift\ndone\nexit 97\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestExecTool_BasicCommand(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", out)
	}
}

func TestExecTool_ShellCommandDisabledByDefault(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo hello"}); err == nil {
		t.Fatal("expected shell execution to be disabled by default")
	}
}

func TestExecTool_ProgramArgs(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"program": "echo",
		"args":    []any{"hello", "argv"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hello argv") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecTool_RelativeProgramUsesCwd(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho fromcwd\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"program": "./tool.sh",
		"cwd":     dir,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "fromcwd") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecTool_RejectsSymlinkCwdEscapingRestrictDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	tool := &ExecTool{Timeout: 5 * time.Second, RestrictDir: root}
	_, err := tool.Execute(context.Background(), map[string]any{"program": "pwd", "cwd": link})
	if err == nil || !strings.Contains(err.Error(), "cwd outside allowed directory") {
		t.Fatalf("expected symlink cwd escape to be rejected, got %v", err)
	}
}

func TestExecTool_DisableShell(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, DisableShell: true}
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo hello"}); err == nil {
		t.Fatal("expected shell execution to be disabled")
	}
}

func TestExecTool_AllowedPrograms(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, AllowedPrograms: []string{"echo"}}
	if _, err := tool.Execute(context.Background(), map[string]any{"program": "pwd"}); err == nil {
		t.Fatal("expected program allowlist rejection")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"program": "echo", "args": []any{"ok"}})
	if err != nil {
		t.Fatalf("Execute allowed program: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecTool_AllowedProgramsRejectsPathByBasename(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hijacked\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tool := &ExecTool{Timeout: 5 * time.Second, AllowedPrograms: []string{"echo"}}
	if _, err := tool.Execute(context.Background(), map[string]any{"program": script}); err == nil {
		t.Fatal("expected basename-only allowlist bypass to be rejected")
	}
}

func TestExecTool_BubblewrapUnavailable(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true, Sandbox: BubblewrapConfig{Enabled: true, BubblewrapPath: filepath.Join(t.TempDir(), "missing-bwrap")}}
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"}); err == nil || !strings.Contains(err.Error(), "bubblewrap unavailable") {
		t.Fatalf("expected bubblewrap unavailable error, got %v", err)
	}
}

func TestExecTool_BubblewrapEnabled(t *testing.T) {
	bwrap := writeFakeBubblewrap(t)
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true, Sandbox: BubblewrapConfig{Enabled: true, BubblewrapPath: bwrap}}
	out, err := tool.Execute(context.Background(), map[string]any{"command": "echo sandboxed"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "sandboxed") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecTool_ChildEnvAllowlistScrubsInheritedEnv(t *testing.T) {
	t.Setenv("INHERITED_SECRET", "top-secret")
	dir := t.TempDir()
	script := filepath.Join(dir, "printenv.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf %s \"${INHERITED_SECRET:-missing}\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tool := &ExecTool{Timeout: 5 * time.Second, ChildEnvAllowlist: []string{"PATH"}}
	out, err := tool.Execute(context.Background(), map[string]any{"program": script})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out) != "missing" {
		t.Fatalf("expected inherited secret to be scrubbed, got %q", out)
	}
}

func TestExecTool_EmptyCommand(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "  ",
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecTool_MissingCommandParam(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command param")
	}
}

func TestExecTool_BlockedPattern(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "rm -rf /tmp/something",
	})
	if err == nil {
		t.Fatal("expected error for blocked pattern 'rm -rf'")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got %q", err.Error())
	}
}

func TestExecTool_CustomBlockedPatterns(t *testing.T) {
	tool := &ExecTool{
		Timeout:           5 * time.Second,
		EnableLegacyShell: true,
		BlockedPatterns:   []string{"forbidden_cmd"},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "forbidden_cmd arg",
	})
	if err == nil {
		t.Fatal("expected error for custom blocked pattern")
	}
}

func TestExecTool_ExitError(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "exit 1",
	})
	if err == nil {
		t.Fatal("expected exit code to return an error")
	}
	if !strings.Contains(err.Error(), "exec failed") {
		t.Fatalf("expected wrapped exec error, got %v", err)
	}
	if !strings.Contains(out, "stdout:") || !strings.Contains(out, "stderr:") {
		t.Errorf("expected stdout/stderr sections in output, got %q", out)
	}
}

func TestExecTool_StderrOutput(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo err >&2",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// stderr is non-empty so output includes "stderr:"
	if !strings.Contains(out, "stderr") {
		t.Errorf("expected 'stderr' in output, got %q", out)
	}
}

func TestExecTool_OutputTruncation(t *testing.T) {
	tool := &ExecTool{
		Timeout:           5 * time.Second,
		EnableLegacyShell: true,
		OutputMaxBytes:    10,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo " + strings.Repeat("a", 100),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected '[truncated]' in output, got %q", out)
	}
}

func TestExecTool_RestrictDir_Inside(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecTool{
		Timeout:           5 * time.Second,
		EnableLegacyShell: true,
		RestrictDir:       dir,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"cwd":     dir,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected 'ok' in output, got %q", out)
	}
}

func TestExecTool_RestrictDir_Outside(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecTool{
		Timeout:           5 * time.Second,
		EnableLegacyShell: true,
		RestrictDir:       dir,
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo outside",
		"cwd":     "/tmp",
	})
	if err == nil {
		t.Fatal("expected error for cwd outside restrict dir")
	}
	if !strings.Contains(err.Error(), "outside allowed") {
		t.Errorf("expected 'outside allowed' in error, got %q", err.Error())
	}
}

func TestExecTool_TimeoutParam(t *testing.T) {
	tool := &ExecTool{Timeout: 10 * time.Second, EnableLegacyShell: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command":        "echo timeout_test",
		"timeoutSeconds": float64(5),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "timeout_test") {
		t.Errorf("expected 'timeout_test', got %q", out)
	}
}

func TestExecTool_WithCwd(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "pwd",
		"cwd":     dir,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Output should contain the temp dir path
	if !strings.Contains(out, filepath.Base(dir)) {
		t.Errorf("expected cwd in output, got %q", out)
	}
}

func TestExecTool_PathAppend(t *testing.T) {
	dir := t.TempDir()
	// Create a small script in the dir
	script := filepath.Join(dir, "myscript")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho fromscript"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &ExecTool{
		Timeout:           5 * time.Second,
		EnableLegacyShell: true,
		PathAppend:        dir,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "myscript",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "fromscript") {
		t.Errorf("expected 'fromscript', got %q", out)
	}
}

func TestExecTool_Name(t *testing.T) {
	tool := &ExecTool{}
	if tool.Name() != "exec" {
		t.Errorf("expected 'exec', got %q", tool.Name())
	}
}

func TestExecTool_Description(t *testing.T) {
	tool := &ExecTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestExecTool_Parameters(t *testing.T) {
	tool := &ExecTool{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected type 'object', got %v", params["type"])
	}
}

func TestExecTool_Schema(t *testing.T) {
	tool := &ExecTool{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected type 'function', got %v", schema["type"])
	}
}

// makeTestBroker creates an approval.Broker backed by a temporary SQLite database.
// The caller must close the returned database when done.
func makeTestBroker(t *testing.T, mode config.ApprovalMode) *approval.Broker {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "exec-broker-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := config.Default().Security.Approvals
	cfg.Enabled = true
	cfg.HostID = "test-host"
	cfg.Exec.Mode = mode
	cfg.SkillExecution.Mode = mode
	return &approval.Broker{
		DB:      database,
		Config:  cfg,
		HostID:  "test-host",
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
		Now:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
}

func TestExecTool_ApprovalRequired_Blocks(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAsk)
	tool := &ExecTool{
		Timeout:        5 * time.Second,
		ApprovalBroker: broker,
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"program": "echo",
		"args":    []any{"hello"},
	})
	if err == nil {
		t.Fatal("expected approval-required error, got nil")
	}
	if !strings.Contains(err.Error(), "approval required") {
		t.Errorf("expected 'approval required' in error, got %q", err.Error())
	}
}

func TestExecTool_DenyMode_Blocks(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeDeny)
	tool := &ExecTool{
		Timeout:        5 * time.Second,
		ApprovalBroker: broker,
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"program": "echo",
		"args":    []any{"hello"},
	})
	if err == nil {
		t.Fatal("expected exec blocked error, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got %q", err.Error())
	}
}

func TestExecTool_TrustedMode_Allows(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeTrusted)
	tool := &ExecTool{
		Timeout:        5 * time.Second,
		ApprovalBroker: broker,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"program": "echo",
		"args":    []any{"trusted-ok"},
	})
	if err != nil {
		t.Fatalf("expected trusted mode to allow execution, got error: %v", err)
	}
	if !strings.Contains(out, "trusted-ok") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestExecTool_ApproveOnce_AllowsWithToken(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAsk)
	tool := &ExecTool{
		Timeout:        5 * time.Second,
		ApprovalBroker: broker,
	}
	params := map[string]any{
		"program": "echo",
		"args":    []any{"approved"},
	}
	// First attempt should require approval and create a request.
	_, err := tool.Execute(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "approval required") {
		t.Fatalf("expected approval required on first attempt, got %v", err)
	}

	// Get the pending request and approve it.
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
		t.Fatalf("expected approved execution to succeed, got error: %v", err)
	}
	if !strings.Contains(out, "approved") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestExecTool_SubjectMismatch_Blocks(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAsk)
	toolEcho := &ExecTool{
		Timeout:        5 * time.Second,
		ApprovalBroker: broker,
	}
	// Trigger approval for "echo approved".
	_, _ = toolEcho.Execute(context.Background(), map[string]any{
		"program": "echo",
		"args":    []any{"approved"},
	})
	requests, _ := broker.ListApprovalRequests(context.Background(), "pending", 10)
	if len(requests) == 0 {
		t.Fatal("expected pending request")
	}
	issued, err := broker.ApproveRequest(context.Background(), requests[0].ID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}

	// Use the token for a different command → subject mismatch → should require approval again.
	ctx := ContextWithApprovalToken(context.Background(), issued.Token)
	_, err = toolEcho.Execute(ctx, map[string]any{
		"program": "echo",
		"args":    []any{"different-command"},
	})
	if err == nil {
		t.Fatal("expected subject-mismatch to block execution, got nil error")
	}
	if !strings.Contains(err.Error(), "approval required") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected approval required or blocked, got %q", err.Error())
	}
}

func TestExecTool_AllowlistMode_Allows(t *testing.T) {
	broker := makeTestBroker(t, config.ApprovalModeAllowlist)
	resolvedEcho, err := exec.LookPath("echo")
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	resolvedEcho, err = filepath.EvalSymlinks(resolvedEcho)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// Add an allowlist rule that matches the canonical echo path.
	_, err = broker.AddAllowlist(context.Background(), string(approval.SubjectExec),
		approval.AllowlistScope{HostID: "test-host", Tool: "exec"},
		approval.ExecAllowlistMatcher{ExecutablePath: resolvedEcho},
		"cli:test", 0)
	if err != nil {
		t.Fatalf("AddAllowlist: %v", err)
	}
	tool := &ExecTool{
		Timeout:        5 * time.Second,
		ApprovalBroker: broker,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"program": "echo",
		"args":    []any{"allowlisted"},
	})
	if err != nil {
		t.Fatalf("expected allowlist to allow execution, got error: %v", err)
	}
	if !strings.Contains(out, "allowlisted") {
		t.Errorf("unexpected output: %q", out)
	}
}
