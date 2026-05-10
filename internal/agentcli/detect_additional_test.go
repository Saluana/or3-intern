package agentcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectGeminiDualFailureReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "gemini-bad", `exit 1`)
	info := Detect(context.Background(), RunnerSpec{ID: RunnerGemini, DisplayName: "Gemini", Binary: "gemini-bad", VersionArgs: []string{"--version"}}, DetectOptions{Env: []string{"PATH=" + dir}})
	if info.Status != RunnerStatusError {
		t.Fatalf("expected error status, got %#v", info)
	}
}

func TestDetectEmptyBinaryWorkDirAndFirstLineEdges(t *testing.T) {
	missing := Detect(context.Background(), RunnerSpec{ID: "empty", DisplayName: "Empty", Binary: "", VersionArgs: []string{"--version"}}, DetectOptions{})
	if missing.Status != RunnerStatusMissing {
		t.Fatalf("expected empty binary to be missing, got %#v", missing)
	}

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expectedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	writeFakeBinary(t, dir, "probecli", `case "$1" in
  --version) pwd; exit 0 ;;
  auth) pwd > auth.out; exit 0 ;;
esac`)
	info := Detect(context.Background(), RunnerSpec{
		ID:          "probecli",
		DisplayName: "ProbeCLI",
		Binary:      "probecli",
		VersionArgs: []string{"--version"},
		AuthCheck:   &SmallCommandSpec{Args: []string{"auth"}, Timeout: 1},
	}, DetectOptions{Env: []string{"PATH=" + dir}, WorkDir: workDir})
	if info.Version != expectedWorkDir || info.AuthStatus != AuthReady {
		t.Fatalf("expected WorkDir propagation, got %#v", info)
	}
	authProbe, err := os.ReadFile(filepath.Join(workDir, "auth.out"))
	if err != nil {
		t.Fatalf("ReadFile auth.out: %v", err)
	}
	if got := strings.TrimSpace(string(authProbe)); got != expectedWorkDir {
		t.Fatalf("expected auth probe in %q, got %q", expectedWorkDir, got)
	}

	cases := map[string]string{
		"":              "",
		"   \n\t  ":     "",
		"first\nsecond": "first",
		"single":        "single",
	}
	for input, want := range cases {
		if got := firstLine([]byte(input)); got != want {
			t.Fatalf("firstLine(%q)=%q want %q", input, got, want)
		}
	}
}
