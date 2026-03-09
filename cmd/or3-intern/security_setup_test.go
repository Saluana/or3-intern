package main

import (
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

func TestValidateConfiguredOutboundEndpoints_IgnoresDisabledChannels(t *testing.T) {
	cfg := config.Default()
	policy := security.HostPolicy{
		Enabled:      true,
		DefaultDeny:  true,
		AllowedHosts: []string{"api.openai.com"},
	}
	if err := validateConfiguredOutboundEndpoints(context.Background(), cfg, policy); err != nil {
		t.Fatalf("expected disabled channel defaults to be ignored, got %v", err)
	}
}

func TestSetupSecurity_FailsWhenSecretRefsRemainUnresolved(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = d.Close() }()
	cfg := config.Default()
	cfg.Security.SecretStore.Enabled = true
	cfg.Security.SecretStore.Required = false
	cfg.Security.SecretStore.KeyFile = filepath.Join(t.TempDir(), "missing.key")
	cfg.Provider.APIKey = "secret:provider.apiKey"
	if _, _, _, err := setupSecurity(context.Background(), cfg, d); err == nil {
		t.Fatal("expected unresolved secret ref failure")
	}
}
