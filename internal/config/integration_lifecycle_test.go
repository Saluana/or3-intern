package config

import "testing"

func TestQuarantineInvalidOptionalIntegrations(t *testing.T) {
	cfg := Default()
	cfg.Tools.MCPServers = map[string]MCPServerConfig{
		"broken": {Enabled: true, Transport: "stdio"},
	}
	cfg.Triggers.Webhook.Enabled = true
	cfg.Triggers.Webhook.Addr = "0.0.0.0:8765"
	cfg.Channels.Telegram.Enabled = true

	quarantined := QuarantineInvalidOptionalIntegrations(&cfg)
	if len(quarantined) != 3 {
		t.Fatalf("expected three quarantined integrations, got %#v", quarantined)
	}
	if cfg.Tools.MCPServers["broken"].Enabled {
		t.Fatal("expected broken MCP server to be disabled")
	}
	if cfg.Triggers.Webhook.Enabled {
		t.Fatal("expected unsafe webhook to be disabled")
	}
	if cfg.Channels.Telegram.Enabled {
		t.Fatal("expected telegram without token to be disabled")
	}
}
