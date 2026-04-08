package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

func TestValidateConfiguredOutboundEndpoints_IgnoresDisabledChannels(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.APIBase = ""
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

func TestSetupSecurity_HostedProfileRequiresWorkingSecretStore(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = d.Close() }()

	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedService
	cfg.Security.SecretStore.Enabled = true
	cfg.Security.SecretStore.Required = false
	cfg.Security.SecretStore.KeyFile = filepath.Join(t.TempDir(), "missing.key")

	_, _, _, err = setupSecurity(context.Background(), cfg, d)
	if err == nil {
		t.Fatal("expected hosted profile secret-store failure")
	}
	if !strings.Contains(err.Error(), "secret store unavailable") {
		t.Fatalf("expected secret store availability error, got %v", err)
	}
}

func TestSetupSecurity_HostedProfileRequiresWorkingAuditLogger(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = d.Close() }()

	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedService
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Strict = false
	cfg.Security.Audit.KeyFile = t.TempDir()

	_, _, _, err = setupSecurity(context.Background(), cfg, d)
	if err == nil {
		t.Fatal("expected hosted profile audit failure")
	}
	if !strings.Contains(err.Error(), "audit logger unavailable") {
		t.Fatalf("expected audit logger availability error, got %v", err)
	}
}

func TestSetupApprovalBroker_EnabledWithoutKeyStillReturnsBroker(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = d.Close() }()

	cfg := config.Default()
	cfg.Security.Approvals.Enabled = true
	cfg.Security.Approvals.KeyFile = ""
	cfg.Security.Approvals.Exec.Mode = config.ApprovalModeDeny

	broker, err := setupApprovalBroker(cfg, d, nil)
	if err != nil {
		t.Fatalf("setupApprovalBroker: %v", err)
	}
	if broker == nil {
		t.Fatal("expected approval broker when approvals are enabled")
	}
	if len(broker.SignKey) != 0 {
		t.Fatalf("expected keyless broker for deny mode, got %d-byte key", len(broker.SignKey))
	}
}
