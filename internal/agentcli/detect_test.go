package agentcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFakeBinary(t *testing.T, dir, name, script string) string {
	t.Helper()
	var path string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, name+".bat")
		if err := os.WriteFile(path, []byte("@echo off\r\n"+script+"\r\nexit /b %errorlevel%"), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	} else {
		path = filepath.Join(dir, name)
		script = "#!/bin/sh\n" + script
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	return path
}

func TestDetect_MissingBinary(t *testing.T) {
	spec := RunnerSpec{ID: "missing-cli", DisplayName: "Missing", Binary: "nonexistent-binary-xyz"}
	opts := DetectOptions{}
	info := Detect(context.Background(), spec, opts)
	if info.Status != RunnerStatusMissing {
		t.Errorf("expected missing, got %q", info.Status)
	}
}

func TestDetect_DisabledByConfig(t *testing.T) {
	spec := RunnerSpec{ID: RunnerOpenCode, DisplayName: "OpenCode", Binary: "opencode"}
	opts := DetectOptions{DisabledRunners: []string{"opencode"}}
	info := Detect(context.Background(), spec, opts)
	if info.Status != RunnerStatusDisabledByConfig {
		t.Errorf("expected disabled_by_config, got %q", info.Status)
	}
}

func TestDetect_VersionSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fakecli", `echo "fakecli v1.2.3"`)
	t.Setenv("PATH", dir)

	spec := RunnerSpec{
		ID:          "fakecli",
		DisplayName: "FakeCLI",
		Binary:      "fakecli",
		VersionArgs: []string{"--version"},
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusAvailable {
		t.Errorf("expected available, got %q", info.Status)
	}
	if info.Version != "fakecli v1.2.3" {
		t.Errorf("expected version, got %q", info.Version)
	}
}

func TestDetect_VersionFailure(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "badcli", `echo "error" >&2; exit 1`)
	t.Setenv("PATH", dir)

	spec := RunnerSpec{
		ID:          "badcli",
		DisplayName: "BadCLI",
		Binary:      "badcli",
		VersionArgs: []string{"--version"},
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusError {
		t.Errorf("expected error, got %q", info.Status)
	}
}

func TestDetect_AuthReady(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fakeauth", `echo "auth ok"; exit 0`)
	t.Setenv("PATH", dir)

	spec := RunnerSpec{
		ID:          "fakeauth",
		DisplayName: "FakeAuth",
		Binary:      "fakeauth",
		VersionArgs: []string{"--version"},
		AuthCheck:   &SmallCommandSpec{Args: []string{"auth", "list"}, Timeout: 1},
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.AuthStatus != AuthReady {
		t.Errorf("expected auth ready, got %q", info.AuthStatus)
	}
}

func TestDetect_AuthMissing(t *testing.T) {
	dir := t.TempDir()
	script := `case "$1" in
  --version) echo "v1.0"; exit 0;;
  login) echo "not logged in" >&2; exit 1;;
esac`
	writeFakeBinary(t, dir, "noauth", script)
	t.Setenv("PATH", dir)

	spec := RunnerSpec{
		ID:          "noauth",
		DisplayName: "NoAuth",
		Binary:      "noauth",
		VersionArgs: []string{"--version"},
		AuthCheck:   &SmallCommandSpec{Args: []string{"login", "status"}, Timeout: 1},
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusAuthMissing {
		t.Errorf("expected auth_missing status, got %q", info.Status)
	}
	if info.AuthStatus != AuthMissing {
		t.Errorf("expected auth missing, got %q", info.AuthStatus)
	}
}

func TestDetect_GeminiAuthUnknown(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "gemini", `echo "Gemini CLI v1.0"`)
	t.Setenv("PATH", dir)

	spec := RunnerSpec{
		ID:          RunnerGemini,
		DisplayName: "Gemini CLI",
		Binary:      "gemini",
		VersionArgs: []string{"--help"},
		AuthCheck:   nil,
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusAvailable {
		t.Errorf("expected available, got %q", info.Status)
	}
	if info.AuthStatus != AuthUnknown {
		t.Errorf("expected auth unknown for gemini, got %q", info.AuthStatus)
	}
}

func TestDetect_GeminiFallbackToHelp(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "gemini2", `case "$1" in
  --version) echo "version not supported" >&2; exit 1;;
  --help) echo "Gemini CLI help"; exit 0;;
esac`)
	t.Setenv("PATH", dir)

	spec := RunnerSpec{
		ID:          RunnerGemini,
		DisplayName: "Gemini CLI",
		Binary:      "gemini2",
		VersionArgs: []string{"--version"},
		AuthCheck:   nil,
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusAvailable {
		t.Errorf("expected available after help fallback, got %q", info.Status)
	}
	if !strings.Contains(info.Version, "Gemini CLI help") {
		t.Errorf("expected help output in version, got %q", info.Version)
	}
}

func TestDetect_OR3AlwaysAvailable(t *testing.T) {
	spec := RunnerSpec{
		ID:          RunnerOR3,
		DisplayName: "OR3 Intern",
		Binary:      "",
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusAvailable {
		t.Errorf("expected available, got %q", info.Status)
	}
	if info.AuthStatus != AuthReady {
		t.Errorf("expected auth ready, got %q", info.AuthStatus)
	}
}

func TestDetect_UsesLookPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	if _, err := exec.LookPath("nonexistent-cli-test"); err == nil {
		t.Skip("unexpected binary in PATH")
	}

	spec := RunnerSpec{
		ID:          "missing-test",
		DisplayName: "MissingTest",
		Binary:      "nonexistent-cli-test",
		VersionArgs: []string{"--version"},
	}
	info := Detect(context.Background(), spec, DetectOptions{})
	if info.Status != RunnerStatusMissing {
		t.Errorf("expected missing via LookPath, got %q", info.Status)
	}
}

func TestValidateRunPolicy(t *testing.T) {
	tests := []struct {
		mode            RunnerMode
		isolation       RunIsolation
		allowSandbox    bool
		wantErrContains string
	}{
		{"review", IsolationHostReadOnly, false, ""},
		{"review", IsolationSandboxWrite, false, ""},
		{"review", IsolationHostWorkspaceWrite, false, "review mode requires"},
		{"safe_edit", IsolationHostWorkspaceWrite, false, ""},
		{"safe_edit", IsolationSandboxWrite, false, ""},
		{"", IsolationHostWorkspaceWrite, false, ""},
		{"safe_edit", IsolationHostReadOnly, false, "safe_edit mode requires"},
		{"sandbox_auto", IsolationSandboxDangerous, true, ""},
		{"sandbox_auto", IsolationHostReadOnly, true, "requires sandbox_dangerous"},
		{"sandbox_auto", IsolationSandboxDangerous, false, "disabled by config"},
		{"sandbox_auto", IsolationHostWorkspaceWrite, false, "requires sandbox_dangerous"},
		{"invalid_mode", IsolationHostReadOnly, false, "unsupported mode"},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode)+"_"+string(tt.isolation), func(t *testing.T) {
			err := ValidateRunPolicy(tt.mode, tt.isolation, tt.allowSandbox)
			if tt.wantErrContains == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q", tt.wantErrContains)
				} else if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("expected error containing %q, got %q", tt.wantErrContains, err.Error())
				}
			}
		})
	}
}
