package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestRunSetupWithIO_PreservesExistingProviderModels(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := config.Default()
	cfg.Provider.APIBase = "https://api.openai.com/v1"
	cfg.Provider.APIKey = "test-key"
	cfg.Provider.Model = "custom-chat-model"
	cfg.Provider.EmbedModel = "custom-embed-model"
	cfg.WorkspaceDir = tmp
	cfg.DBPath = filepath.Join(tmp, ".or3", "or3-intern.sqlite")
	cfg.ArtifactsDir = filepath.Join(tmp, ".or3", "artifacts")
	cfg.Security.SecretStore.KeyFile = filepath.Join(tmp, "master.key")
	cfg.Security.Audit.KeyFile = filepath.Join(tmp, "audit.key")
	cfg.Security.Approvals.KeyFile = filepath.Join(tmp, "approvals.key")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	input := strings.Join([]string{
		"",  // keep provider
		"",  // keep API key
		"",  // keep workspace
		"5", // advanced/manual scenario
		"2", // balanced safety mode
		"n", // do not start chat
		"",
	}, "\n")
	result, err := runSetupWithIO(strings.NewReader(input), &bytes.Buffer{}, cfgPath, tmp)
	if err != nil {
		t.Fatalf("runSetupWithIO: %v", err)
	}
	if result.Config.Provider.Model != "custom-chat-model" {
		t.Fatalf("expected chat model to be preserved, got %q", result.Config.Provider.Model)
	}
	if result.Config.Provider.EmbedModel != "custom-embed-model" {
		t.Fatalf("expected embed model to be preserved, got %q", result.Config.Provider.EmbedModel)
	}
}

func TestParseStatusArgs_AcceptsSubcommandAdvancedFlag(t *testing.T) {
	detailed, err := parseStatusArgs([]string{"--advanced"}, false)
	if err != nil {
		t.Fatalf("parseStatusArgs: %v", err)
	}
	if !detailed {
		t.Fatal("expected detailed status output")
	}
}

func TestRunConnectDeviceCommand_RejectsUnknownSubcommand(t *testing.T) {
	err := runConnectDeviceCommand(context.Background(), "", &config.Config{}, nil, nil, []string{"lisst"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected unknown subcommand error")
	}
	if !strings.Contains(err.Error(), "usage: connect-device") {
		t.Fatalf("expected usage error, got %v", err)
	}
}
