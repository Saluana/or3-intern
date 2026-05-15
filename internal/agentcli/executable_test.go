package agentcli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveExecutableResolvesFromEnvAndProcessPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeBinary(t, dir, "resolve-me", `echo ok`)

	got, err := ResolveExecutable("resolve-me", []string{"PATH=" + dir})
	if err != nil {
		t.Fatalf("ResolveExecutable env PATH: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}

	t.Setenv("PATH", dir)
	got, err = ResolveExecutable("resolve-me", nil)
	if err != nil {
		t.Fatalf("ResolveExecutable process PATH: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestResolveExecutableRejectsEmptyAndMissingBinary(t *testing.T) {
	if _, err := ResolveExecutable("   ", nil); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected exec.ErrNotFound for empty binary, got %v", err)
	}
	if _, err := ResolveExecutable("missing", []string{"PATH="}); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected exec.ErrNotFound for empty PATH, got %v", err)
	}
	if _, err := ResolveExecutable("missing", []string{"PATH=" + t.TempDir()}); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected exec.ErrNotFound for missing binary, got %v", err)
	}
}

func TestExecutableHelpers(t *testing.T) {
	if got := envValue([]string{"path=/usr/bin", "HOME=/tmp/home"}, "PATH"); got != "/usr/bin" {
		t.Fatalf("expected PATH from envValue, got %q", got)
	}
	if got := envValue([]string{"HOME=/tmp/home"}, "PATH"); got != "" {
		t.Fatalf("expected empty missing env value, got %q", got)
	}

	dir := t.TempDir()
	nonExec := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(nonExec, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if isExecutableFile(nonExec) {
		t.Fatalf("expected non-executable file to be rejected")
	}
	if isExecutableFile(dir) {
		t.Fatalf("expected directory to be rejected")
	}
	execPath := writeFakeBinary(t, dir, "exec-file", `echo ok`)
	if !isExecutableFile(execPath) {
		t.Fatalf("expected executable file")
	}

	candidates := executableCandidates(filepath.Join(dir, "exec-file"), nil)
	if len(candidates) != 1 || candidates[0] != filepath.Join(dir, "exec-file") {
		t.Fatalf("unexpected executable candidates: %#v", candidates)
	}
}
