package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

func TestRunCapabilitiesCommand_TextOutputIncludesIngressAndApprovals(t *testing.T) {
	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
	cfg.Security.Approvals.Enabled = true
	cfg.Security.Approvals.Exec.Mode = config.ApprovalModeAsk
	cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAllowlist
	cfg.Security.Profiles.Enabled = true
	cfg.Security.Profiles.Default = "default"
	cfg.Security.Profiles.Channels["slack"] = "ops"
	cfg.Security.Profiles.Triggers["webhook"] = "default"
	cfg.Security.Profiles.Profiles["default"] = config.AccessProfileConfig{MaxCapability: "guarded", AllowSubagents: false}
	cfg.Security.Profiles.Profiles["ops"] = config.AccessProfileConfig{
		MaxCapability:  "guarded",
		AllowedTools:   []string{"read_file", "web_fetch"},
		AllowedHosts:   []string{"api.example.com"},
		AllowSubagents: true,
	}
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.InboundPolicy = config.InboundPolicyPairing
	cfg.Triggers.Webhook.Enabled = true
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.Sandbox.Enabled = true
	cfg.Hardening.EnableExecShell = true

	var out bytes.Buffer
	if err := runCapabilitiesCommand(cfg, &approval.Broker{Config: cfg.Security.Approvals, HostID: cfg.Security.Approvals.HostID, SignKey: []byte("key")}, []string{"--channel", "slack"}, &out, &out); err != nil {
		t.Fatalf("runCapabilitiesCommand: %v", err)
	}
	text := out.String()
	for _, needle := range []string{
		"runtime_profile: hosted-remote-sandbox-only",
		"approval_broker: enabled=true",
		"exec_available: true",
		"shell_mode_available: true",
		"pairing: allowlist",
		"- slack enabled=true inbound=pairing profile=ops max=guarded subagents=true tools=read_file,web_fetch hosts=api.example.com",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected %q in output, got %q", needle, text)
		}
	}
	if strings.Contains(text, "- telegram ") {
		t.Fatalf("expected channel filter to suppress unrelated channels, got %q", text)
	}
}

func TestRunCapabilitiesCommand_JSONOutputIncludesFilteredTrigger(t *testing.T) {
	cfg := config.Default()
	cfg.Security.Profiles.Enabled = true
	cfg.Security.Profiles.Triggers["file_change"] = "files"
	cfg.Security.Profiles.Profiles["files"] = config.AccessProfileConfig{
		MaxCapability:  "safe",
		AllowedTools:   []string{"read_file"},
		AllowSubagents: false,
	}
	cfg.Triggers.FileWatch.Enabled = true

	var out bytes.Buffer
	if err := runCapabilitiesCommand(cfg, nil, []string{"--trigger", "filewatch", "--json"}, &out, &out); err != nil {
		t.Fatalf("runCapabilitiesCommand: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	triggers, ok := payload["triggers"].([]any)
	if !ok || len(triggers) != 1 {
		t.Fatalf("expected one filtered trigger, got %#v", payload["triggers"])
	}
	item := triggers[0].(map[string]any)
	if item["name"] != "filewatch" {
		t.Fatalf("unexpected trigger item: %#v", item)
	}
}
