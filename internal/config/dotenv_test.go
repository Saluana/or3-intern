package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvLoadsCurrentAndParentWithoutOverriding(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("OR3_MODEL=from-parent\nOR3_API_KEY=from-parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".env"), []byte("OR3_API_KEY=from-child\nOR3_EMBED_MODEL='embed-child'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldAPIKey, hadAPIKey := os.LookupEnv("OR3_API_KEY")
	oldEmbedModel, hadEmbedModel := os.LookupEnv("OR3_EMBED_MODEL")
	defer func() {
		if hadAPIKey {
			_ = os.Setenv("OR3_API_KEY", oldAPIKey)
		} else {
			_ = os.Unsetenv("OR3_API_KEY")
		}
		if hadEmbedModel {
			_ = os.Setenv("OR3_EMBED_MODEL", oldEmbedModel)
		} else {
			_ = os.Unsetenv("OR3_EMBED_MODEL")
		}
	}()
	t.Setenv("OR3_MODEL", "from-shell")
	t.Setenv("OR3_LOAD_DOTENV", "true")
	_ = os.Unsetenv("OR3_API_KEY")
	_ = os.Unsetenv("OR3_EMBED_MODEL")
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}

	LoadDotEnv()

	if got := os.Getenv("OR3_MODEL"); got != "from-shell" {
		t.Fatalf("expected shell value to win, got %q", got)
	}
	if got := os.Getenv("OR3_API_KEY"); got != "from-child" {
		t.Fatalf("expected current .env to win over parent, got %q", got)
	}
	if got := os.Getenv("OR3_EMBED_MODEL"); got != "embed-child" {
		t.Fatalf("expected quoted value to load, got %q", got)
	}
}

func TestLoadDotEnvCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OR3_MODEL=from-env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldModel, hadModel := os.LookupEnv("OR3_MODEL")
	defer func() {
		if hadModel {
			_ = os.Setenv("OR3_MODEL", oldModel)
		} else {
			_ = os.Unsetenv("OR3_MODEL")
		}
	}()
	_ = os.Unsetenv("OR3_MODEL")
	t.Setenv("OR3_LOAD_DOTENV", "false")
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	LoadDotEnv()

	if got := os.Getenv("OR3_MODEL"); got != "" {
		t.Fatalf("expected disabled dotenv load, got %q", got)
	}
}
