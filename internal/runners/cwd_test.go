package runners

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRunnerCwd_NoRestriction(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// Empty request + empty restrict → current working dir
	got, err := resolveRunnerCwd("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != cwd {
		t.Fatalf("expected %q, got %q", cwd, got)
	}

	// Absolute path with no restriction → passed through
	got, err = resolveRunnerCwd("/tmp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/tmp" {
		t.Fatalf("expected /tmp, got %q", got)
	}

	// Relative path with no restriction → resolved from cwd
	got, err = resolveRunnerCwd("foo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(cwd, "foo")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveRunnerCwd_WithRestriction(t *testing.T) {
	tmp := t.TempDir()
	restrictDir, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}

	// Empty request → defaults to restrict dir
	got, err := resolveRunnerCwd("", restrictDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != restrictDir {
		t.Fatalf("expected %q, got %q", restrictDir, got)
	}

	// Relative path → resolved below restrict dir
	got, err = resolveRunnerCwd("sub/dir", restrictDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(restrictDir, "sub", "dir")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	// Absolute path inside restrict dir → allowed
	got, err = resolveRunnerCwd(filepath.Join(restrictDir, "project"), restrictDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = filepath.Join(restrictDir, "project")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	// Absolute path outside restrict dir → allowed (explicit user choice)
	got, err = resolveRunnerCwd("/etc", restrictDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/etc" {
		t.Fatalf("expected /etc, got %q", got)
	}

	// Relative path escaping restrict dir via .. → rejected
	_, err = resolveRunnerCwd("../escape", restrictDir)
	if err == nil {
		t.Fatal("expected error for cwd escaping restrict dir")
	}
	if !strings.Contains(err.Error(), "outside allowed directory") {
		t.Fatalf("expected 'outside allowed directory' error, got: %v", err)
	}
}

func TestResolveRunnerCwd_Whitespace(t *testing.T) {
	restrictDir := t.TempDir()

	got, err := resolveRunnerCwd("  ", restrictDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != restrictDir {
		t.Fatalf("expected default for whitespace-only cwd, got %q", got)
	}
}
