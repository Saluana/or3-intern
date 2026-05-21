package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
)

func TestInitDefaults_UsesAppDataPaths(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	cfg := initDefaults("/tmp/project")
	if strings.Contains(cfg.DBPath, "/tmp/project/.or3") {
		t.Fatalf("DB path should not default inside workspace: %q", cfg.DBPath)
	}
	if strings.Contains(cfg.ArtifactsDir, "/tmp/project/.or3") {
		t.Fatalf("artifacts dir should not default inside workspace: %q", cfg.ArtifactsDir)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Fatal("expected workspace restriction enabled")
	}
	if cfg.WorkspaceDir != "/tmp/project" {
		t.Fatalf("unexpected workspace dir: %q", cfg.WorkspaceDir)
	}
}

func TestRunInitWithIO_DelegatesToSetup(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	input := strings.NewReader(strings.Join([]string{
		// Provider choice
		"2",
		// API key
		"test-key",
		// Workspace folder
		"",
		// Storage location
		"1",
		// Scenario (solo computer)
		"1",
		// Safety mode (balanced)
		"2",
		// Start chat
		"n",
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
	if !strings.Contains(out.String(), "compatibility alias") {
		t.Fatalf("expected compatibility alias message, got %q", out.String())
	}
	if !strings.Contains(out.String(), "You did it") {
		t.Fatalf("expected setup success output, got %q", out.String())
	}
}

func TestInitPrintsCompatibilityMessage(t *testing.T) {
	var out bytes.Buffer
	_, err := runSetupWithIO(strings.NewReader("1\ntest-key\n\n1\n1\n2\nn\n"), &out, filepath.Join(t.TempDir(), "config.json"), "/tmp")
	if err != nil {
		t.Fatalf("runSetupWithIO: %v", err)
	}
	if !strings.Contains(out.String(), "OR3 setup") {
		t.Fatalf("expected setup header, got %q", out.String())
	}
}

func TestConfigureSectionFlagStillWorks(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	cfg := config.Default()
	cfg.WorkspaceDir = dir
	cfg.Provider.APIBase = "https://api.openai.com/v1"
	cfg.Provider.Model = "gpt-4.1-mini"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	args, err := parseConfigureArgs([]string{"--section", "provider"})
	if err != nil {
		t.Fatalf("parseConfigureArgs: %v", err)
	}
	if len(args.Sections) != 1 || args.Sections[0] != "provider" {
		t.Fatalf("expected provider section, got %#v", args.Sections)
	}
}

func TestHelpShowsSetupAsRecommendedFirstRun(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, []string{"init"}); err != nil {
		t.Fatalf("printHelpTopic init: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Compatibility alias") {
		t.Fatalf("expected init help to describe compatibility alias, got %q", got)
	}
	if !strings.Contains(got, "or3-intern setup") {
		t.Fatalf("expected init help to reference setup command, got %q", got)
	}
}

func TestHelpConfigureLabeledAdvanced(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, []string{"configure"}); err != nil {
		t.Fatalf("printHelpTopic configure: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Advanced configuration wizard") {
		t.Fatalf("expected configure help to be labeled advanced, got %q", got)
	}
	if !strings.Contains(got, "Most users should use") {
		t.Fatalf("expected configure help to reference setup/settings, got %q", got)
	}
}

func TestBuildChannelManager_RegistersEnabledChannels(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "test-token"
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.AppToken = "app"
	cfg.Channels.Slack.BotToken = "bot"
	cfg.Channels.Email.Enabled = true
	cfg.Channels.Email.ConsentGranted = true
	cfg.Channels.Email.OpenAccess = true
	cfg.Channels.Email.IMAPHost = "imap.example.com"
	cfg.Channels.Email.IMAPUsername = "imap-user"
	cfg.Channels.Email.IMAPPassword = "imap-pass"
	cfg.Channels.Email.SMTPHost = "smtp.example.com"
	cfg.Channels.Email.SMTPUsername = "smtp-user"
	cfg.Channels.Email.SMTPPassword = "smtp-pass"

	mgr, err := buildChannelManager(cfg, &cli.Deliverer{}, nil, 0, nil)
	if err != nil {
		t.Fatalf("buildChannelManager: %v", err)
	}
	names := strings.Join(mgr.Names(), ",")
	if !strings.Contains(names, "cli") || !strings.Contains(names, "telegram") || !strings.Contains(names, "slack") || !strings.Contains(names, "email") {
		t.Fatalf("expected registered channels, got %q", names)
	}
}

func TestHeartbeatServiceForCommand_OnlyServeAndEnabled(t *testing.T) {
	cfg := config.Default()
	eventBus := bus.New(1)

	if svc := heartbeatServiceForCommand("chat", cfg, eventBus); svc != nil {
		t.Fatal("expected no heartbeat service for chat command")
	}

	cfg.Heartbeat.Enabled = true
	if svc := heartbeatServiceForCommand("agent", cfg, eventBus); svc != nil {
		t.Fatal("expected no heartbeat service for agent command")
	}

	svc := heartbeatServiceForCommand("serve", cfg, eventBus)
	if svc == nil {
		t.Fatal("expected heartbeat service for serve command when enabled")
	}
	if svc.Config.SessionKey != config.DefaultHeartbeatSessionKey {
		t.Fatalf("expected normalized heartbeat session key, got %q", svc.Config.SessionKey)
	}
}

func TestSubagentsEnabledForCommand(t *testing.T) {
	cfg := config.Default()
	cfg.Subagents.Enabled = true
	if !subagentsEnabledForCommand("chat", cfg) {
		t.Fatal("expected chat to enable subagents")
	}
	if !subagentsEnabledForCommand("serve", cfg) {
		t.Fatal("expected serve to enable subagents")
	}
	if subagentsEnabledForCommand("agent", cfg) {
		t.Fatal("expected one-shot agent mode to disable subagents")
	}
	cfg.Subagents.Enabled = false
	if subagentsEnabledForCommand("serve", cfg) {
		t.Fatal("expected disabled config to win")
	}
}
