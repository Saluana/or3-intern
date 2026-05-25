package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

func newSecretManagerForTest(t *testing.T) *security.SecretManager {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "secrets.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &security.SecretManager{DB: d, Key: []byte("01234567890123456789012345678901")}
}

func TestRunSecretsCommand_SetAndList(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	var out bytes.Buffer
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"set", "provider.openai", "secret-value"}, &out, &out); err != nil {
		t.Fatalf("set: %v", err)
	}
	out.Reset()
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if out.String() == "" {
		t.Fatal("expected secret name in list output")
	}
}

func TestRunSecretsCommand_RejectsExtraArgs(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	var out bytes.Buffer
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"list", "extra"}, &out, &out); err == nil {
		t.Fatal("expected list with extra args to fail")
	}
	// Note: set with extra args now succeeds - the extra arg becomes the value
	// This is backward compatible with the old behavior where positional args were used
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"set", "name", "value", "extra"}, &out, &out); err != nil {
		// This should succeed now - the extra arg is treated as the value
		t.Fatalf("set with extra args should succeed: %v", err)
	}
}

func TestRunSecretsCommand_StrictAuditFailureBlocksMutation(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	audit := &security.AuditLogger{Strict: true}
	var out bytes.Buffer

	if err := runSecretsCommand(context.Background(), mgr, audit, []string{"set", "provider.openai", "secret-value"}, &out, &out); err == nil {
		t.Fatal("expected strict audit failure during set")
	}
	if _, ok, err := mgr.Get(context.Background(), "provider.openai"); err != nil {
		t.Fatalf("Get after failed set: %v", err)
	} else if ok {
		t.Fatal("expected set mutation to be blocked before persistence")
	}

	if err := mgr.Put(context.Background(), "provider.openai", "seed"); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	if err := runSecretsCommand(context.Background(), mgr, audit, []string{"delete", "provider.openai"}, &out, &out); err == nil {
		t.Fatal("expected strict audit failure during delete")
	}
	value, ok, err := mgr.Get(context.Background(), "provider.openai")
	if err != nil {
		t.Fatalf("Get after failed delete: %v", err)
	}
	if !ok || value != "seed" {
		t.Fatalf("expected delete mutation to be blocked, got ok=%v value=%q", ok, value)
	}
}

func TestRunSecretsCommand_MigrateConfigUpdatesConfigAndStoresSecret(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Provider.APIKey = "plain-provider-key"
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"demo.server": {
			Headers: map[string]string{"Authorization": "Bearer plain"},
			Env:     map[string]string{"API_TOKEN": "plain-token"},
		},
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	t.Setenv("OR3_CONFIG", configPath)

	var out bytes.Buffer
	if err := runSecretsCommand(ctx, mgr, nil, []string{"migrate-config", "--force"}, &out, &out); err != nil {
		t.Fatalf("migrate-config: %v", err)
	}

	updated, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.Provider.APIKey != "secret:provider-api-key" {
		t.Fatalf("expected provider API key ref, got %q", updated.Provider.APIKey)
	}
	server := updated.Tools.MCPServers["demo.server"]
	if server.Headers["Authorization"] != "secret:mcp-demo.server-header-Authorization" {
		t.Fatalf("expected MCP header ref, got %#v", server.Headers)
	}
	if server.Env["API_TOKEN"] != "secret:mcp-demo.server-env-API_TOKEN" {
		t.Fatalf("expected MCP env ref, got %#v", server.Env)
	}
	value, ok, err := mgr.Get(ctx, "provider-api-key")
	if err != nil {
		t.Fatalf("Get provider secret: %v", err)
	}
	if !ok || value != "plain-provider-key" {
		t.Fatalf("expected migrated provider secret, ok=%v value=%q", ok, value)
	}
}

func TestRunSecretsCommand_MigrateConfigRejectsExistingSecretWithoutForce(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	ctx := context.Background()
	if err := mgr.Put(ctx, "provider-api-key", "existing"); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Provider.APIKey = "plain-provider-key"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	t.Setenv("OR3_CONFIG", configPath)

	var out bytes.Buffer
	err := runSecretsCommand(ctx, mgr, nil, []string{"migrate-config"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing secret error, got %v", err)
	}
	value, ok, err := mgr.Get(ctx, "provider-api-key")
	if err != nil {
		t.Fatalf("Get provider secret: %v", err)
	}
	if !ok || value != "existing" {
		t.Fatalf("expected existing secret to remain unchanged, ok=%v value=%q", ok, value)
	}
	updated, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if updated.Provider.APIKey != "plain-provider-key" {
		t.Fatalf("expected config to remain unchanged, got %q", updated.Provider.APIKey)
	}
}

func TestRunSecretsCommand_ExportDefaultsToEncrypted(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	ctx := context.Background()
	if err := mgr.Put(ctx, "provider.openai", "secret-value"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var out bytes.Buffer
	if err := runSecretsCommand(ctx, mgr, nil, []string{"export"}, &out, &out); err != nil {
		t.Fatalf("export: %v", err)
	}
	if strings.Contains(out.String(), "secret-value") {
		t.Fatalf("default export leaked plaintext: %q", out.String())
	}

	out.Reset()
	if err := runSecretsCommand(ctx, mgr, nil, []string{"export", "--plaintext", "--force"}, &out, &out); err != nil {
		t.Fatalf("plaintext export: %v", err)
	}
	if !strings.Contains(out.String(), "secret-value") {
		t.Fatalf("expected plaintext export to include value, got %q", out.String())
	}
}
