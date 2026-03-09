package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	tool := &ExecTool{Timeout: 5 * time.Second}
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

func TestExecTool_BubblewrapUnavailable(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second, Sandbox: BubblewrapConfig{Enabled: true, BubblewrapPath: filepath.Join(t.TempDir(), "missing-bwrap")}}
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"}); err == nil || !strings.Contains(err.Error(), "bubblewrap unavailable") {
		t.Fatalf("expected bubblewrap unavailable error, got %v", err)
	}
}

func TestExecTool_BubblewrapEnabled(t *testing.T) {
	bwrap := writeFakeBubblewrap(t)
	tool := &ExecTool{Timeout: 5 * time.Second, Sandbox: BubblewrapConfig{Enabled: true, BubblewrapPath: bwrap}}
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
	tool := &ExecTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "  ",
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecTool_MissingCommandParam(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command param")
	}
}

func TestExecTool_BlockedPattern(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
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
		Timeout:         5 * time.Second,
		BlockedPatterns: []string{"forbidden_cmd"},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "forbidden_cmd arg",
	})
	if err == nil {
		t.Fatal("expected error for custom blocked pattern")
	}
}

func TestExecTool_ExitError(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "exit 1",
	})
	if err != nil {
		t.Fatalf("Execute should not return error on exit code (got: %v)", err)
	}
	if !strings.Contains(out, "exit error") {
		t.Errorf("expected 'exit error' in output for non-zero exit, got %q", out)
	}
}

func TestExecTool_StderrOutput(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
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
		Timeout:        5 * time.Second,
		OutputMaxBytes: 10,
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
		Timeout:     5 * time.Second,
		RestrictDir: dir,
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
		Timeout:     5 * time.Second,
		RestrictDir: dir,
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
	tool := &ExecTool{Timeout: 10 * time.Second}
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
	tool := &ExecTool{Timeout: 5 * time.Second}
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
	os.WriteFile(script, []byte("#!/bin/sh\necho fromscript"), 0o755)

	tool := &ExecTool{
		Timeout:    5 * time.Second,
		PathAppend: dir,
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
