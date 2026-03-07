package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
)

func TestInitDefaults_UsesWorkspacePaths(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	cfg := initDefaults("/tmp/project")
	if cfg.DBPath != "/tmp/project/.or3/or3-intern.sqlite" {
		t.Fatalf("unexpected DB path: %q", cfg.DBPath)
	}
	if cfg.ArtifactsDir != "/tmp/project/.or3/artifacts" {
		t.Fatalf("unexpected artifacts dir: %q", cfg.ArtifactsDir)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Fatal("expected workspace restriction enabled")
	}
	if cfg.WorkspaceDir != "/tmp/project" {
		t.Fatalf("unexpected workspace dir: %q", cfg.WorkspaceDir)
	}
}

func TestRunInitWithIO_WritesConfig(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	input := strings.NewReader(strings.Join([]string{
		"2",
		"",
		"",
		"",
		"y",
		"test-key",
		"",
		"",
		"",
		"",
	}, "\n"))
	var out strings.Builder

	if err := runInitWithIO(input, &out, configPath, "/workspace/project"); err != nil {
		t.Fatalf("runInitWithIO: %v", err)
	}

	b, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Provider.APIBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("unexpected API base: %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.Model != "openai/gpt-4o-mini" {
		t.Fatalf("unexpected model: %q", cfg.Provider.Model)
	}
	if cfg.Provider.APIKey != "test-key" {
		t.Fatalf("unexpected API key: %q", cfg.Provider.APIKey)
	}
	if cfg.DBPath != "/workspace/project/.or3/or3-intern.sqlite" {
		t.Fatalf("unexpected DB path: %q", cfg.DBPath)
	}
	if !strings.Contains(out.String(), "Saved config") {
		t.Fatalf("expected success output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "go run ./cmd/or3-intern chat") {
		t.Fatalf("expected next-step instructions, got %q", out.String())
	}
}

func TestBuildChannelManager_RegistersEnabledChannels(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "test-token"
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.AppToken = "app"
	cfg.Channels.Slack.BotToken = "bot"

	mgr, err := buildChannelManager(cfg, cli.Deliverer{}, nil, 0)
	if err != nil {
		t.Fatalf("buildChannelManager: %v", err)
	}
	names := strings.Join(mgr.Names(), ",")
	if !strings.Contains(names, "cli") || !strings.Contains(names, "telegram") || !strings.Contains(names, "slack") {
		t.Fatalf("expected registered channels, got %q", names)
	}
}
