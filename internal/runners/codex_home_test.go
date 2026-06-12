package runners

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestCodexHomeEnvMaterializesShadowHome(t *testing.T) {
	sharedHome := t.TempDir()
	shadowHome := filepath.Join(t.TempDir(), "shadow")
	mustWriteFile(t, filepath.Join(sharedHome, "config.toml"), []byte("model = \"gpt-5-codex\"\n"))
	mustWriteFile(t, filepath.Join(sharedHome, "auth.json"), []byte(`{"shared":true}`))
	mustWriteFile(t, filepath.Join(sharedHome, "models_cache.json"), []byte(`{"models":["shared"]}`))
	if err := os.MkdirAll(shadowHome, 0o700); err != nil {
		t.Fatalf("MkdirAll shadow: %v", err)
	}
	mustWriteFile(t, filepath.Join(shadowHome, "auth.json"), []byte(`{"shadow":true}`))
	if err := os.Symlink(filepath.Join(sharedHome, "models_cache.json"), filepath.Join(shadowHome, "models_cache.json")); err != nil {
		t.Fatalf("Symlink models_cache: %v", err)
	}

	env, err := codexHomeEnv(config.RunnersConfig{CodexHomePath: sharedHome, CodexShadowHomePath: shadowHome})
	if err != nil {
		t.Fatalf("codexHomeEnv: %v", err)
	}
	if env["CODEX_HOME"] != shadowHome {
		t.Fatalf("CODEX_HOME = %q, want %q", env["CODEX_HOME"], shadowHome)
	}
	configTarget, err := os.Readlink(filepath.Join(shadowHome, "config.toml"))
	if err != nil || configTarget != filepath.Join(sharedHome, "config.toml") {
		t.Fatalf("config.toml link = %q, %v", configTarget, err)
	}
	sessionsTarget, err := os.Readlink(filepath.Join(shadowHome, "sessions"))
	if err != nil || sessionsTarget != filepath.Join(sharedHome, "sessions") {
		t.Fatalf("sessions link = %q, %v", sessionsTarget, err)
	}
	if _, err := os.Readlink(filepath.Join(shadowHome, "auth.json")); err == nil {
		t.Fatal("auth.json must remain private in the shadow home")
	}
	if _, err := os.Lstat(filepath.Join(shadowHome, "models_cache.json")); !os.IsNotExist(err) {
		t.Fatalf("models_cache.json symlink should be removed, got %v", err)
	}
}

func TestCodexHomeEnvRejectsShadowConflicts(t *testing.T) {
	sharedHome := t.TempDir()
	shadowHome := filepath.Join(t.TempDir(), "shadow")
	if err := os.MkdirAll(shadowHome, 0o700); err != nil {
		t.Fatalf("MkdirAll shadow: %v", err)
	}
	mustWriteFile(t, filepath.Join(sharedHome, "config.toml"), []byte("model = \"gpt-5-codex\"\n"))
	mustWriteFile(t, filepath.Join(shadowHome, "config.toml"), []byte("model = \"local\"\n"))

	_, err := codexHomeEnv(config.RunnersConfig{CodexHomePath: sharedHome, CodexShadowHomePath: shadowHome})
	if err == nil || !strings.Contains(err.Error(), "already exists and is not a symlink") {
		t.Fatalf("expected shadow conflict error, got %v", err)
	}
}

func TestCodexHomeEnvExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	env, err := codexHomeEnv(config.RunnersConfig{CodexHomePath: "~/.codex-test"})
	if err != nil {
		t.Fatalf("codexHomeEnv: %v", err)
	}
	want := filepath.Join(home, ".codex-test")
	if env["CODEX_HOME"] != want {
		t.Fatalf("CODEX_HOME = %q, want %q", env["CODEX_HOME"], want)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
