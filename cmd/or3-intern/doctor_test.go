package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/security"
)

func TestDoctorFindings_SafeBaseline(t *testing.T) {
	cfg := safeDoctorConfig()
	findings := doctorFindings(cfg)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %#v", findings)
	}

	var out bytes.Buffer
	if err := runDoctorCommand("", cfg, "", nil, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runDoctorCommand: %v", err)
	}
	if !strings.Contains(out.String(), "[ok] configuration looks safe") {
		t.Fatalf("expected ok output, got %q", out.String())
	}
}

func TestDoctorFindings_ExpandedWarnings(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*config.Config)
		expectArea  string
		expectMatch string
	}{
		{
			name: "service weak secret",
			mutate: func(cfg *config.Config) {
				cfg.Service.Enabled = true
				cfg.Service.Secret = "short-secret"
			},
			expectArea:  "service",
			expectMatch: "weak shared secret",
		},
		{
			name: "service public bind",
			mutate: func(cfg *config.Config) {
				cfg.Service.Enabled = true
				cfg.Service.Secret = strings.Repeat("a", 32)
				cfg.Service.Listen = "0.0.0.0:9100"
			},
			expectArea:  "service",
			expectMatch: "not loopback-only",
		},
		{
			name: "approvals ask mode without broker key",
			mutate: func(cfg *config.Config) {
				cfg.Security.Approvals.Enabled = true
				cfg.Security.Approvals.KeyFile = ""
				cfg.Security.Approvals.Exec.Mode = config.ApprovalModeAsk
			},
			expectArea:  "approvals",
			expectMatch: "approval broker keyFile is required",
		},
		{
			name: "audit disabled",
			mutate: func(cfg *config.Config) {
				cfg.Security.Audit.Enabled = false
			},
			expectArea:  "security",
			expectMatch: "audit logging is disabled",
		},
		{
			name: "secret store disabled",
			mutate: func(cfg *config.Config) {
				cfg.Security.SecretStore.Enabled = false
			},
			expectArea:  "security",
			expectMatch: "secret store is disabled",
		},
		{
			name: "profiles disabled with public ingress",
			mutate: func(cfg *config.Config) {
				cfg.Security.Profiles.Enabled = false
				cfg.Channels.Slack.Enabled = true
				cfg.Channels.Slack.OpenAccess = true
			},
			expectArea:  "profiles",
			expectMatch: "public ingress is enabled while access profiles are disabled",
		},
		{
			name: "stdio mcp missing env allowlist",
			mutate: func(cfg *config.Config) {
				cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
					"local": {
						Enabled:   true,
						Transport: "stdio",
						Command:   "demo-mcp",
					},
				}
			},
			expectArea:  "mcp",
			expectMatch: "uses stdio without a server childEnvAllowlist",
		},
		{
			name: "http mcp insecure",
			mutate: func(cfg *config.Config) {
				cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
					"remote": {
						Enabled:           true,
						Transport:         "streamablehttp",
						URL:               "http://127.0.0.1:8080/mcp",
						AllowInsecureHTTP: true,
					},
				}
			},
			expectArea:  "mcp",
			expectMatch: "uses insecure HTTP transport",
		},
		{
			name: "webhook profile too permissive",
			mutate: func(cfg *config.Config) {
				cfg.Triggers.Webhook.Enabled = true
				cfg.Security.Profiles.Default = "danger"
				cfg.Security.Profiles.Profiles["danger"] = config.AccessProfileConfig{
					MaxCapability:  "privileged",
					AllowedTools:   []string{"exec", "run_skill_script"},
					AllowedHosts:   []string{"*.example.com"},
					WritablePaths:  []string{"/tmp"},
					AllowSubagents: true,
				}
			},
			expectArea:  "webhook",
			expectMatch: "webhook resolves to profile \"danger\" with privileged capability",
		},
		{
			name: "broad global allowed hosts",
			mutate: func(cfg *config.Config) {
				cfg.Security.Network.AllowedHosts = []string{"*"}
			},
			expectArea:  "network",
			expectMatch: "security.network.allowedHosts contains *",
		},
		{
			name: "exec shell posture",
			mutate: func(cfg *config.Config) {
				cfg.Hardening.PrivilegedTools = true
				cfg.Hardening.EnableExecShell = true
			},
			expectArea:  "exec",
			expectMatch: "exec shell command mode is enabled",
		},
		{
			name: "skill execution quarantine disabled",
			mutate: func(cfg *config.Config) {
				cfg.Skills.EnableExec = true
				cfg.Skills.Policy.QuarantineByDefault = false
			},
			expectArea:  "skills",
			expectMatch: "skill execution is enabled while quarantineByDefault is false",
		},
		{
			name: "skill execution missing trusted publisher policy",
			mutate: func(cfg *config.Config) {
				cfg.Skills.EnableExec = true
			},
			expectArea:  "skills",
			expectMatch: "skill execution is enabled without a trustedOwners policy",
		},
		{
			name: "public channel privileged profile",
			mutate: func(cfg *config.Config) {
				cfg.Channels.Discord.Enabled = true
				cfg.Channels.Discord.OpenAccess = true
				cfg.Security.Profiles.Default = "danger"
				cfg.Security.Profiles.Profiles["danger"] = config.AccessProfileConfig{
					MaxCapability: "privileged",
				}
			},
			expectArea:  "discord",
			expectMatch: "open-access channel resolves to profile \"danger\" with privileged capability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := safeDoctorConfig()
			tt.mutate(&cfg)
			findings := doctorFindings(cfg)
			if !findingContains(findings, tt.expectArea, tt.expectMatch) {
				t.Fatalf("expected finding %q in area %q, got %#v", tt.expectMatch, tt.expectArea, findings)
			}
		})
	}
}

func TestRunDoctorCommand_PrintsWarnings(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Tools.RestrictToWorkspace = false
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	cfg.Triggers.Webhook.Enabled = true
	cfg.Triggers.Webhook.Secret = ""
	cfg.Triggers.Webhook.Addr = "0.0.0.0:8765"
	cfg.Security.Profiles.Default = "danger"
	cfg.Security.Profiles.Profiles["danger"] = config.AccessProfileConfig{MaxCapability: "privileged", AllowedTools: []string{"exec"}}
	var out bytes.Buffer
	if err := runDoctorCommand("", cfg, "", nil, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runDoctorCommand: %v", err)
	}
	text := out.String()
	for _, area := range []string{"filesystem", "exec", "privileged-exec", "webhook"} {
		if !strings.Contains(text, area) {
			t.Fatalf("expected %q warning in %q", area, text)
		}
	}
}

func TestRunDoctorCommand_StrictFailsOnWarnings(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	var out bytes.Buffer
	if err := runDoctorCommand("", cfg, "", []string{"--strict"}, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("expected strict doctor run to fail on warnings")
	}
}

func TestRunDoctorCommand_JSONOutput(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	var out bytes.Buffer
	if err := runDoctorCommand("", cfg, "", []string{"--json"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runDoctorCommand --json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON output, got %v (%q)", err, out.String())
	}
	if payload["summary"] == nil || payload["findings"] == nil {
		t.Fatalf("expected summary and findings in JSON output, got %#v", payload)
	}
}

func TestRunDoctorCommand_FixRepairsInvalidChannelIngress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := config.Default()
	cfg.WorkspaceDir = dir
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "telegram-token"
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out bytes.Buffer
	if err := runDoctorCommand(path, cfg, "telegram enabled: set channels.telegram.allowedChatIds, channels.telegram.inboundPolicy=pairing, or channels.telegram.openAccess=true", []string{"--fix"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatalf("runDoctorCommand --fix: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load repaired config: %v", err)
	}
	if loaded.Channels.Telegram.InboundPolicy != config.InboundPolicyDeny {
		t.Fatalf("expected deny inbound repair, got %q", loaded.Channels.Telegram.InboundPolicy)
	}
}

func TestRunDoctorCommand_InteractiveFixGeneratesServiceSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := safeDoctorConfig()
	cfg.Service.Enabled = true
	cfg.Service.Secret = ""
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	if err := runDoctorCommand(path, cfg, "", []string{"--fix", "--interactive"}, strings.NewReader("1\n"), &out, &out); err != nil {
		t.Fatalf("runDoctorCommand --fix --interactive: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.TrimSpace(loaded.Service.Secret) == "" {
		t.Fatal("expected generated service secret")
	}
}

func TestDoctorFindings_ProfileHostThreshold(t *testing.T) {
	cfg := safeDoctorConfig()
	hosts := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		hosts = append(hosts, fmt.Sprintf("host-%d.example.com", i))
	}
	cfg.Security.Profiles.Profiles["safe"] = config.AccessProfileConfig{
		MaxCapability: "safe",
		AllowedTools:  []string{"web_fetch"},
		AllowedHosts:  hosts,
	}
	findings := doctorFindings(cfg)
	if !findingContains(findings, "profiles", "profile \"safe\" has broad allowedHosts") {
		t.Fatalf("expected broad host warning, got %#v", findings)
	}
}

func TestDoctorFindings_SafeProfileCountsAsMeaningfulRestriction(t *testing.T) {
	cfg := safeDoctorConfig()
	cfg.Hardening.GuardedTools = true
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.OpenAccess = true
	cfg.Security.Profiles.Profiles["safe"] = config.AccessProfileConfig{
		MaxCapability: "safe",
	}
	findings := doctorFindings(cfg)
	if findingContains(findings, "slack", "without a meaningful tool restriction") {
		t.Fatalf("expected safe profile to avoid tool restriction warning, got %#v", findings)
	}
}

func TestDoctorFindings_ExecWarningsRespectEffectiveProfiles(t *testing.T) {
	t.Run("webhook safe profile suppresses generic exec ingress warning", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.Hardening.PrivilegedTools = true
		cfg.Hardening.EnableExecShell = true
		cfg.Triggers.Webhook.Enabled = true
		findings := doctorFindings(cfg)
		if findingContains(findings, "exec", "public or webhook-facing ingress can reach privileged exec posture") {
			t.Fatalf("expected webhook safe profile to suppress generic exec ingress warning, got %#v", findings)
		}
	})

	t.Run("guarded webhook profile with exec allowlist cannot reach exec", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.Hardening.PrivilegedTools = true
		cfg.Hardening.EnableExecShell = true
		cfg.Triggers.Webhook.Enabled = true
		cfg.Security.Profiles.Default = "guarded"
		cfg.Security.Profiles.Profiles["guarded"] = config.AccessProfileConfig{
			MaxCapability: "guarded",
			AllowedTools:  []string{"exec"},
		}
		findings := doctorFindings(cfg)
		if findingContains(findings, "webhook", "can reach exec shell mode via profile") {
			t.Fatalf("expected guarded webhook profile to avoid exec warning, got %#v", findings)
		}
	})

	t.Run("guarded public profile with exec allowlist cannot reach exec", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.Hardening.PrivilegedTools = true
		cfg.Hardening.EnableExecShell = true
		cfg.Channels.Discord.Enabled = true
		cfg.Channels.Discord.OpenAccess = true
		cfg.Security.Profiles.Default = "guarded"
		cfg.Security.Profiles.Profiles["guarded"] = config.AccessProfileConfig{
			MaxCapability: "guarded",
			AllowedTools:  []string{"exec"},
		}
		findings := doctorFindings(cfg)
		if findingContains(findings, "discord", "can reach exec shell mode via profile") {
			t.Fatalf("expected guarded public profile to avoid exec warning, got %#v", findings)
		}
	})

	t.Run("public profile does not report shell exposure when shell mode is off", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.Hardening.PrivilegedTools = true
		cfg.Hardening.EnableExecShell = false
		cfg.Channels.Discord.Enabled = true
		cfg.Channels.Discord.OpenAccess = true
		cfg.Security.Profiles.Default = "danger"
		cfg.Security.Profiles.Profiles["danger"] = config.AccessProfileConfig{
			MaxCapability: "privileged",
			AllowedTools:  []string{"exec"},
		}
		findings := doctorFindings(cfg)
		if findingContains(findings, "discord", "can reach exec shell mode via profile") {
			t.Fatalf("expected shell-mode warning to be suppressed when enableExecShell=false, got %#v", findings)
		}
	})
}

func findingContains(findings []doctorFinding, area, match string) bool {
	for _, finding := range findings {
		if finding.Area == area && strings.Contains(finding.Message, match) {
			return true
		}
	}
	return false
}

func safeDoctorConfig() config.Config {
	cfg := config.Default()
	root, err := os.MkdirTemp("", "or3-intern-doctor-safe-*")
	if err != nil {
		panic(err)
	}
	cfg.WorkspaceDir = root
	cfg.DBPath = root + "/or3-intern.sqlite"
	cfg.ArtifactsDir = root + "/artifacts"
	cfg.Security.SecretStore.KeyFile = root + "/master.key"
	cfg.Security.Audit.KeyFile = root + "/audit.key"
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		panic(err)
	}
	if _, err := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile); err != nil {
		panic(err)
	}
	if _, err := security.LoadOrCreateKey(cfg.Security.Audit.KeyFile); err != nil {
		panic(err)
	}
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Strict = true
	cfg.Security.Audit.VerifyOnStart = true
	cfg.Security.SecretStore.Enabled = true
	cfg.Security.SecretStore.Required = true
	cfg.Security.Profiles.Enabled = true
	cfg.Security.Profiles.Default = "safe"
	cfg.Security.Profiles.Profiles = map[string]config.AccessProfileConfig{
		"safe": {
			MaxCapability: "safe",
			AllowedTools:  []string{"read_file"},
		},
	}
	cfg.Security.Network.Enabled = true
	cfg.Security.Network.DefaultDeny = true
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{}
	cfg.RuntimeProfile = config.ProfileLocalDev
	return cfg
}

func TestRuntimeProfileFindings(t *testing.T) {
	t.Run("no profile set emits warn", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = ""
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "runtimeProfile is not set") {
			t.Fatalf("expected unset runtimeProfile warning, got %#v", findings)
		}
	})

	t.Run("local-dev profile produces no findings", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileLocalDev
		findings := runtimeProfileFindings(cfg)
		if len(findings) != 0 {
			t.Fatalf("expected no findings for local-dev, got %#v", findings)
		}
	})

	t.Run("hosted profile without secret store warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.SecretStore.Enabled = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "security.secretStore.enabled") {
			t.Fatalf("expected secretStore warning, got %#v", findings)
		}
	})

	t.Run("hosted profile without required secret store warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.SecretStore.Required = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "security.secretStore.required") {
			t.Fatalf("expected secretStore.required warning, got %#v", findings)
		}
	})

	t.Run("hosted profile without audit warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.Audit.Enabled = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "security.audit.enabled") {
			t.Fatalf("expected audit warning, got %#v", findings)
		}
	})

	t.Run("hosted profile without strict audit warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.Audit.Strict = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "security.audit.strict") {
			t.Fatalf("expected audit.strict warning, got %#v", findings)
		}
	})

	t.Run("hosted profile without audit verifyOnStart warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.Audit.VerifyOnStart = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "security.audit.verifyOnStart") {
			t.Fatalf("expected audit.verifyOnStart warning, got %#v", findings)
		}
	})

	t.Run("hosted profile without network warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.Network.Enabled = false
		cfg.Security.Network.DefaultDeny = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "security.network outbound policy") {
			t.Fatalf("expected network warning, got %#v", findings)
		}
	})

	t.Run("hosted profile with default deny and network disabled does not warn", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Security.Network.Enabled = false
		cfg.Security.Network.DefaultDeny = true
		findings := runtimeProfileFindings(cfg)
		if findingContains(findings, "runtime-profile", "security.network outbound policy") {
			t.Fatalf("expected no network warning with defaultDeny policy, got %#v", findings)
		}
	})

	t.Run("hosted-no-exec with exec shell enabled warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedNoExec
		cfg.Hardening.EnableExecShell = true
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "enableExecShell should be false") {
			t.Fatalf("expected enableExecShell warning, got %#v", findings)
		}
	})

	t.Run("hosted-no-exec with privileged tools warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedNoExec
		cfg.Hardening.PrivilegedTools = true
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "privilegedTools should be false") {
			t.Fatalf("expected privilegedTools warning, got %#v", findings)
		}
	})

	t.Run("hosted-remote-sandbox-only exec without sandbox warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
		cfg.Hardening.EnableExecShell = true
		cfg.Hardening.Sandbox.Enabled = false
		findings := runtimeProfileFindings(cfg)
		if !findingContains(findings, "runtime-profile", "exec requires sandbox to be enabled") {
			t.Fatalf("expected sandbox warning, got %#v", findings)
		}
	})

	t.Run("hosted-remote-sandbox-only exec with sandbox no warns", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
		cfg.Hardening.EnableExecShell = true
		cfg.Hardening.Sandbox.Enabled = true
		findings := runtimeProfileFindings(cfg)
		if findingContains(findings, "runtime-profile", "exec requires sandbox") {
			t.Fatalf("expected no sandbox warning when sandbox enabled, got %#v", findings)
		}
	})

	t.Run("well-configured hosted profile produces no profile findings", func(t *testing.T) {
		cfg := safeDoctorConfig()
		cfg.RuntimeProfile = config.ProfileHostedService
		findings := runtimeProfileFindings(cfg)
		if len(findings) != 0 {
			t.Fatalf("expected no findings for well-configured hosted profile, got %#v", findings)
		}
	})
}
