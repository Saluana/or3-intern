package security

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openSecurityTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestSecretManager_RoundTripAndResolveConfigSecrets(t *testing.T) {
	d := openSecurityTestDB(t)
	ctx := context.Background()
	mgr := &SecretManager{DB: d, Key: []byte("01234567890123456789012345678901")}
	if err := mgr.Put(ctx, "provider.apiKey", "super-secret"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	value, ok, err := mgr.Get(ctx, "provider.apiKey")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || value != "super-secret" {
		t.Fatalf("unexpected secret round trip: ok=%v value=%q", ok, value)
	}
	cfg := config.Default()
	cfg.Provider.APIKey = "secret:provider.apiKey"
	resolved, err := ResolveConfigSecrets(ctx, cfg, mgr)
	if err != nil {
		t.Fatalf("ResolveConfigSecrets: %v", err)
	}
	if resolved.Provider.APIKey != "super-secret" {
		t.Fatalf("expected resolved secret, got %q", resolved.Provider.APIKey)
	}
}

func TestResolveConfigSecrets_ResolvesMCPServerSecrets(t *testing.T) {
	d := openSecurityTestDB(t)
	ctx := context.Background()
	mgr := &SecretManager{DB: d, Key: []byte("01234567890123456789012345678901")}
	if err := mgr.Put(ctx, "mcp.url", "https://mcp.example.com/sse"); err != nil {
		t.Fatalf("Put url: %v", err)
	}
	if err := mgr.Put(ctx, "mcp.auth", "Bearer top-secret"); err != nil {
		t.Fatalf("Put auth: %v", err)
	}
	if err := mgr.Put(ctx, "mcp.env.token", "env-secret"); err != nil {
		t.Fatalf("Put env: %v", err)
	}
	cfg := config.Default()
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"demo": {
			Enabled:   true,
			Transport: "sse",
			URL:       "secret:mcp.url",
			Headers:   map[string]string{"Authorization": "secret:mcp.auth"},
			Env:       map[string]string{"TOKEN": "secret:mcp.env.token"},
		},
	}
	resolved, err := ResolveConfigSecrets(ctx, cfg, mgr)
	if err != nil {
		t.Fatalf("ResolveConfigSecrets: %v", err)
	}
	server := resolved.Tools.MCPServers["demo"]
	if server.URL != "https://mcp.example.com/sse" {
		t.Fatalf("expected resolved MCP url, got %q", server.URL)
	}
	if server.Headers["Authorization"] != "Bearer top-secret" {
		t.Fatalf("expected resolved MCP header, got %#v", server.Headers)
	}
	if server.Env["TOKEN"] != "env-secret" {
		t.Fatalf("expected resolved MCP env, got %#v", server.Env)
	}
}

func TestValidateNoSecretRefs_DetectsRemainingSecretRefs(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.APIKey = "secret:provider.apiKey"
	err := ValidateNoSecretRefs(cfg)
	if err == nil || !strings.Contains(err.Error(), "unresolved secret ref") {
		t.Fatalf("expected unresolved secret ref error, got %v", err)
	}
}
