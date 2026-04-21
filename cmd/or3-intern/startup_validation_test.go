package main

import (
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func hostedStartupConfig() config.Config {
	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedService
	cfg.Security.SecretStore.Enabled = true
	cfg.Security.SecretStore.Required = true
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Strict = true
	cfg.Security.Audit.VerifyOnStart = true
	cfg.Security.Network.Enabled = true
	cfg.Security.Network.DefaultDeny = true
	return cfg
}

func TestValidateStartupCommand_ChatRejectsInvalidHostedProfile(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.Security.SecretStore.Enabled = false

	err := validateStartupCommand("chat", cfg)
	if err == nil {
		t.Fatal("expected hosted chat startup validation to fail")
	}
	if !strings.Contains(err.Error(), "chat startup refused") {
		t.Fatalf("expected chat startup refusal, got %v", err)
	}
}

func TestValidateStartupCommand_ChatAllowsLocalStdioMCPWithGlobalAllowlist(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"local": {
			Enabled:   true,
			Transport: "stdio",
			Command:   "mcp-local",
		},
	}

	if err := validateStartupCommand("chat", cfg); err != nil {
		t.Fatalf("expected hosted chat with local stdio MCP to pass, got %v", err)
	}
}

func TestValidateStartupCommand_HostedNoExecAllowsRemoteMCPWithSafeNetworkPosture(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.RuntimeProfile = config.ProfileHostedNoExec
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"remote": {
			Enabled:   true,
			Transport: "sse",
			URL:       "https://mcp.example.com/stream",
		},
	}
	cfg.Security.Network.AllowedHosts = []string{"mcp.example.com"}

	if err := validateStartupCommand("chat", cfg); err != nil {
		t.Fatalf("expected hosted-no-exec remote MCP flow to pass, got %v", err)
	}
}

func TestValidateStartupCommand_RemoteSandboxRejectsBroadLocalExecWithoutSandbox(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
	cfg.Service.Secret = strings.Repeat("s", 32)
	cfg.Hardening.PrivilegedTools = true

	err := validateStartupCommand("service", cfg)
	if err == nil {
		t.Fatal("expected hosted-remote-sandbox-only startup validation to fail")
	}
	if !strings.Contains(err.Error(), "Bubblewrap sandboxing") {
		t.Fatalf("expected sandbox refusal, got %v", err)
	}
}

func TestValidateStartupCommand_RemoteSandboxAllowsRemoteFlowWithSandbox(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
	cfg.Service.Secret = strings.Repeat("s", 32)
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.ExecAllowedPrograms = []string{"git"}
	cfg.Hardening.Sandbox.Enabled = true
	cfg.Hardening.Sandbox.BubblewrapPath = "sh"
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"remote": {
			Enabled:   true,
			Transport: "streamablehttp",
			URL:       "https://mcp.example.com/stream",
		},
	}
	cfg.Security.Network.AllowedHosts = []string{"mcp.example.com"}

	if err := validateStartupCommand("service", cfg); err != nil {
		t.Fatalf("expected sandboxed hosted-remote-sandbox-only remote flow to pass, got %v", err)
	}
}

func TestValidateStartupCommand_ServiceRejectsWeakSecret(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.Service.Secret = "short-secret"

	err := validateStartupCommand("service", cfg)
	if err == nil {
		t.Fatal("expected weak service secret to fail hosted startup validation")
	}
	if !strings.Contains(err.Error(), "weak shared secret") {
		t.Fatalf("expected weak shared secret error, got %v", err)
	}
}

func TestValidateStartupCommand_ServeRejectsWebhookWithoutProfile(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.Triggers.Webhook.Enabled = true
	cfg.Triggers.Webhook.Secret = strings.Repeat("w", 32)
	cfg.Security.Profiles.Enabled = false

	err := validateStartupCommand("serve", cfg)
	if err == nil {
		t.Fatal("expected hosted serve validation to fail when webhook has no effective profile")
	}
	if !strings.Contains(err.Error(), "effective access profile") {
		t.Fatalf("expected effective profile error, got %v", err)
	}
}

func TestValidateStartupCommand_ServiceRejectsApprovalAskModeWithoutBrokerKey(t *testing.T) {
	cfg := hostedStartupConfig()
	cfg.Service.Secret = strings.Repeat("s", 32)
	cfg.Service.Listen = "0.0.0.0:8080"
	cfg.Security.Approvals.Enabled = true
	cfg.Security.Approvals.KeyFile = ""
	cfg.Security.Approvals.Exec.Mode = config.ApprovalModeAsk

	err := validateStartupCommand("service", cfg)
	if err == nil {
		t.Fatal("expected hosted service validation to fail when approvals need a broker key")
	}
	if !strings.Contains(err.Error(), "approval broker keyFile") {
		t.Fatalf("expected approval broker key error, got %v", err)
	}
}
