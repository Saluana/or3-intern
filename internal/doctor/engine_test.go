package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestProbeFindings_DoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.sqlite")
	cfg := config.Default()
	cfg.DBPath = path

	findings := probeFindings(cfg, Options{Probe: true})
	if len(findings) != 1 {
		t.Fatalf("expected one probe finding, got %#v", findings)
	}
	if findings[0].ID != "probe.sqlite_open_failed" {
		t.Fatalf("expected sqlite probe failure, got %#v", findings)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected probe to avoid creating %q, stat err=%v", path, err)
	}
}

func TestDoctorFindingsHaveConsumerRepairFields(t *testing.T) {
	fixtures := []config.Config{
		config.Default(),
		func() config.Config {
			cfg := config.Default()
			cfg.RuntimeProfile = config.ProfileHostedService
			cfg.Service.Enabled = true
			cfg.Service.Listen = "0.0.0.0:9100"
			cfg.Service.AllowUnauthenticatedPairing = true
			cfg.Service.SharedSecretRole = "operator"
			cfg.Service.MaxCapability = "guarded"
			cfg.Triggers.Webhook.Enabled = true
			cfg.Triggers.Webhook.Addr = "0.0.0.0:8765"
			cfg.Channels.Slack.Enabled = true
			return cfg
		}(),
		func() config.Config {
			cfg := config.Default()
			cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
			cfg.Hardening.PrivilegedTools = true
			return cfg
		}(),
	}
	for _, cfg := range fixtures {
		report := Evaluate(cfg, Options{Mode: ModeStartupService, ValidationError: "invalid config value"})
		for _, finding := range report.Findings {
			if strings.TrimSpace(finding.Summary) == "" {
				t.Fatalf("%s missing summary", finding.ID)
			}
			if finding.Severity == SeverityInfo {
				continue
			}
			if strings.TrimSpace(finding.Detail) == "" {
				t.Fatalf("%s missing detail", finding.ID)
			}
			if strings.TrimSpace(finding.FixHint) == "" {
				t.Fatalf("%s missing fix hint", finding.ID)
			}
			if finding.FixMode == "" {
				t.Fatalf("%s missing fix mode", finding.ID)
			}
		}
	}
}

func TestApplyAutomaticFixes_ServiceUnsafeSharedSecretPosture(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.Default()
	cfg.Service.Enabled = true
	cfg.Service.Listen = "0.0.0.0:9100"
	cfg.Service.Secret = strings.Repeat("s", 32)
	cfg.Service.AllowUnauthenticatedPairing = true
	cfg.Service.SharedSecretRole = "operator"
	cfg.Service.MaxCapability = "guarded"
	cfg.RuntimeProfile = config.ProfileHostedService
	report := Evaluate(cfg, Options{Mode: ModeStartupService})

	applied, err := ApplyAutomaticFixes(cfgPath, &cfg, report)
	if err != nil {
		t.Fatalf("ApplyAutomaticFixes: %v", err)
	}
	for _, want := range []string{"service.unauthenticated_pairing_remote", "service.shared_secret_role_unsafe", "service.max_capability_unsafe"} {
		found := false
		for _, fix := range applied {
			if fix.ID == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected applied fix %s in %#v", want, applied)
		}
	}
	if cfg.Service.AllowUnauthenticatedPairing || cfg.Service.SharedSecretRole != "service-client" || cfg.Service.MaxCapability != "safe" {
		t.Fatalf("unsafe service posture not repaired: %#v", cfg.Service)
	}
}

func TestValidateConfigSnapshotDoesNotApplyEnvOverrides(t *testing.T) {
	t.Setenv("OR3_RUNTIME_PROFILE", string(config.ProfileLocalDev))

	cfg := config.Default()
	cfg.RuntimeProfile = config.RuntimeProfile("invalid-profile")

	err := validateConfigSnapshot(cfg)
	if err == nil {
		t.Fatal("expected snapshot validation to reject in-memory config")
	}
	if !strings.Contains(err.Error(), "unrecognized runtimeProfile") {
		t.Fatalf("expected runtime profile validation error, got %v", err)
	}
}

func TestValidateConfigSnapshotDoesNotMutateInputMaps(t *testing.T) {
	cfg := config.Default()
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"broken": {
			Enabled:   true,
			Transport: "stdio",
		},
	}

	_ = validateConfigSnapshot(cfg)

	if !cfg.Tools.MCPServers["broken"].Enabled {
		t.Fatal("expected snapshot validation not to quarantine caller MCP map")
	}
}

func TestServiceFindingsRequireEffectiveProfileForExposedIngress(t *testing.T) {
	cfg := config.Default()
	cfg.RuntimeProfile = config.ProfileHostedService
	cfg.Service.Enabled = true
	cfg.Service.Listen = "0.0.0.0:9100"
	cfg.Service.Secret = strings.Repeat("s", 32)

	report := Evaluate(cfg, Options{Mode: ModeStartupService})
	if !doctorReportHasFinding(report, "service.effective_profile_missing") {
		t.Fatalf("expected exposed service ingress to require an effective profile, findings=%#v", report.Findings)
	}

	cfg.Security.Profiles.Enabled = true
	cfg.Security.Profiles.Default = "service-safe"
	cfg.Security.Profiles.Profiles = map[string]config.AccessProfileConfig{
		"service-safe": {MaxCapability: "safe", AllowedTools: []string{"search"}},
	}
	report = Evaluate(cfg, Options{Mode: ModeStartupService})
	if doctorReportHasFinding(report, "service.effective_profile_missing") {
		t.Fatalf("expected default access profile to satisfy service ingress, findings=%#v", report.Findings)
	}
}

func doctorReportHasFinding(report Report, id string) bool {
	for _, finding := range report.Findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func TestDoctorStartupFixtureCoverageForStabilityProfiles(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{
			name: "local",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.Provider.APIKey = strings.Repeat("k", 24)
				return cfg
			}(),
			want: "service.secret_missing",
		},
		{
			name: "private-service",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.Service.Enabled = true
				cfg.Service.Listen = "127.0.0.1:9100"
				cfg.Service.Secret = strings.Repeat("s", 32)
				return cfg
			}(),
			want: "provider.api_key_missing",
		},
		{
			name: "exposed-ingress",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.RuntimeProfile = config.ProfileHostedService
				cfg.Service.Enabled = true
				cfg.Service.Listen = "0.0.0.0:9100"
				cfg.Service.Secret = strings.Repeat("s", 32)
				return cfg
			}(),
			want: "service.effective_profile_missing",
		},
		{
			name: "remote-mcp",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.RuntimeProfile = config.ProfileHostedService
				cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
					"remote": {Enabled: true, Transport: "streamablehttp", URL: "https://mcp.example"},
				}
				return cfg
			}(),
			want: "mcp.http_no_default_deny",
		},
		{
			name: "privileged-exec",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
				cfg.Hardening.PrivilegedTools = true
				return cfg
			}(),
			want: "privileged-exec.sandbox_disabled",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := Evaluate(tc.cfg, Options{Mode: ModeStartupService})
			if !doctorReportHasFinding(report, tc.want) {
				t.Fatalf("expected finding %q, got %#v", tc.want, report.Findings)
			}
		})
	}
}

func TestCollectChannels_ReturnsAllFiveChannels(t *testing.T) {
	cfg := config.Default()
	channels := collectChannels(cfg)
	if len(channels) != 5 {
		t.Fatalf("expected 5 channels, got %d", len(channels))
	}
	names := make(map[string]bool)
	for _, ch := range channels {
		names[ch.Name] = true
	}
	for _, name := range []string{"telegram", "slack", "discord", "whatsapp", "email"} {
		if !names[name] {
			t.Errorf("expected channel %q in snapshot", name)
		}
	}
}

func TestOpenAccessChannelNames_EmptyByDefault(t *testing.T) {
	cfg := config.Default()
	names := openAccessChannelNames(cfg)
	if len(names) != 0 {
		t.Fatalf("expected no open access channels by default, got %v", names)
	}
}

func TestOpenAccessChannelNames_ReportsEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.OpenAccess = true
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.OpenAccess = true
	names := openAccessChannelNames(cfg)
	if len(names) != 2 {
		t.Fatalf("expected 2 open access channels, got %v", names)
	}
}

func TestChannelExposureFindings_PerChannel(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.OpenAccess = true
	cfg.Channels.Discord.Enabled = true
	cfg.Channels.Discord.OpenAccess = true
	opts := Options{Mode: ModeAdvisory}
	findings := channelExposureFindings(cfg, opts)
	if len(findings) < 2 {
		t.Fatalf("expected at least 2 findings for open channels, got %d", len(findings))
	}
	areas := make(map[string]bool)
	for _, f := range findings {
		areas[f.Area] = true
	}
	if !areas["telegram"] {
		t.Error("expected telegram channel finding")
	}
	if !areas["discord"] {
		t.Error("expected discord channel finding")
	}
}

func TestChannelIngressFindings_InvalidIngress(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.InboundPolicy = "" // no policy means requires allowlist or open access
	opts := Options{Mode: ModeStartupServe}
	findings := channelIngressFindings(cfg, opts)
	if len(findings) != 1 {
		t.Fatalf("expected 1 invalid ingress finding, got %d", len(findings))
	}
	if findings[0].ID != "channels.invalid_ingress" {
		t.Fatalf("expected channels.invalid_ingress, got %s", findings[0].ID)
	}
}
