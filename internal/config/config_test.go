package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mustWriteTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENAI_API_KEY",
		"BRAVE_API_KEY",
		"OR3_DB_PATH",
		"OR3_ARTIFACTS_DIR",
		"OR3_API_BASE",
		"OR3_API_KEY",
		"OR3_MODEL",
		"OR3_CONSOLIDATION_MODEL",
		"OR3_EMBED_MODEL",
		"OR3_EMBED_DIMENSIONS",
		"OR3_TELEGRAM_TOKEN",
		"OR3_SLACK_APP_TOKEN",
		"OR3_SLACK_BOT_TOKEN",
		"OR3_DISCORD_TOKEN",
		"OR3_WHATSAPP_BRIDGE_URL",
		"OR3_WHATSAPP_BRIDGE_TOKEN",
		"OR3_EMAIL_IMAP_HOST",
		"OR3_EMAIL_IMAP_PORT",
		"OR3_EMAIL_IMAP_USERNAME",
		"OR3_EMAIL_IMAP_PASSWORD",
		"OR3_EMAIL_SMTP_HOST",
		"OR3_EMAIL_SMTP_PORT",
		"OR3_EMAIL_SMTP_USERNAME",
		"OR3_EMAIL_SMTP_PASSWORD",
		"OR3_EMAIL_FROM_ADDRESS",
		"OR3_SUBAGENTS_ENABLED",
		"OR3_SUBAGENTS_MAX_CONCURRENT",
		"OR3_SUBAGENTS_MAX_QUEUED",
		"OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS",
		"OR3_SERVICE_ENABLED",
		"OR3_SERVICE_LISTEN",
		"OR3_SERVICE_SECRET",
		"OR3_RUNTIME_PROFILE",
	} {
		t.Setenv(key, "")
	}
}

func TestDefault_Values(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()

	if cfg.DefaultSessionKey != "cli:default" {
		t.Errorf("expected DefaultSessionKey='cli:default', got %q", cfg.DefaultSessionKey)
	}
	if cfg.HistoryMax != 40 {
		t.Errorf("expected HistoryMax=40, got %d", cfg.HistoryMax)
	}
	if cfg.MaxToolBytes != 24*1024 {
		t.Errorf("expected MaxToolBytes=%d, got %d", 24*1024, cfg.MaxToolBytes)
	}
	if cfg.MaxMediaBytes != 20*1024*1024 {
		t.Errorf("expected MaxMediaBytes=%d, got %d", 20*1024*1024, cfg.MaxMediaBytes)
	}
	if cfg.MaxToolLoops != 6 {
		t.Errorf("expected MaxToolLoops=6, got %d", cfg.MaxToolLoops)
	}
	if cfg.VectorK != 8 {
		t.Errorf("expected VectorK=8, got %d", cfg.VectorK)
	}
	if cfg.FTSK != 8 {
		t.Errorf("expected FTSK=8, got %d", cfg.FTSK)
	}
	if cfg.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", cfg.VectorScanLimit)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("expected WorkerCount=4, got %d", cfg.WorkerCount)
	}
	if cfg.Provider.Model != "gpt-4.1-mini" {
		t.Errorf("expected Model='gpt-4.1-mini', got %q", cfg.Provider.Model)
	}
	if cfg.ConsolidationModel != "" {
		t.Errorf("expected ConsolidationModel to default empty for provider-model fallback, got %q", cfg.ConsolidationModel)
	}
	if cfg.Provider.APIBase != "https://api.openai.com/v1" {
		t.Errorf("expected APIBase='https://api.openai.com/v1', got %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.TimeoutSeconds != 60 {
		t.Errorf("expected TimeoutSeconds=60, got %d", cfg.Provider.TimeoutSeconds)
	}
	if cfg.Provider.EmbedDimensions != 0 {
		t.Errorf("expected EmbedDimensions=0, got %d", cfg.Provider.EmbedDimensions)
	}
	if cfg.Provider.EnableVision {
		t.Error("expected Provider.EnableVision=false by default")
	}
	if cfg.Cron.Enabled != true {
		t.Error("expected Cron.Enabled=true")
	}
	if cfg.BootstrapMaxChars != 20000 {
		t.Errorf("expected BootstrapMaxChars=20000, got %d", cfg.BootstrapMaxChars)
	}
	if cfg.BootstrapTotalMaxChars != 150000 {
		t.Errorf("expected BootstrapTotalMaxChars=150000, got %d", cfg.BootstrapTotalMaxChars)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Error("expected RestrictToWorkspace=true by default")
	}
	if cfg.ConsolidationMaxMessages != 50 {
		t.Errorf("expected ConsolidationMaxMessages=50, got %d", cfg.ConsolidationMaxMessages)
	}
	if cfg.ConsolidationMaxInputChars != 12000 {
		t.Errorf("expected ConsolidationMaxInputChars=12000, got %d", cfg.ConsolidationMaxInputChars)
	}
	if cfg.ConsolidationAsyncTimeoutSeconds != 30 {
		t.Errorf("expected ConsolidationAsyncTimeoutSeconds=30, got %d", cfg.ConsolidationAsyncTimeoutSeconds)
	}
	if cfg.Subagents.Enabled {
		t.Error("expected Subagents.Enabled=false by default")
	}
	if cfg.Subagents.MaxConcurrent != 1 {
		t.Errorf("expected Subagents.MaxConcurrent=1, got %d", cfg.Subagents.MaxConcurrent)
	}
	if cfg.Subagents.MaxQueued != 32 {
		t.Errorf("expected Subagents.MaxQueued=32, got %d", cfg.Subagents.MaxQueued)
	}
	if cfg.Subagents.TaskTimeoutSeconds != 300 {
		t.Errorf("expected Subagents.TaskTimeoutSeconds=300, got %d", cfg.Subagents.TaskTimeoutSeconds)
	}
	if cfg.Service.Enabled {
		t.Error("expected Service.Enabled=false by default")
	}
	if cfg.Service.Listen != "127.0.0.1:9100" {
		t.Fatalf("expected Service.Listen default, got %q", cfg.Service.Listen)
	}
	if cfg.Service.Secret != "" {
		t.Fatalf("expected Service.Secret empty by default, got %q", cfg.Service.Secret)
	}
	if cfg.Service.SharedSecretRole != "service-client" {
		t.Fatalf("expected Service.SharedSecretRole='service-client', got %q", cfg.Service.SharedSecretRole)
	}
	if cfg.Service.MaxCapability != "safe" {
		t.Fatalf("expected Service.MaxCapability='safe', got %q", cfg.Service.MaxCapability)
	}
	if cfg.Service.AllowUnauthenticatedPairing {
		t.Fatal("expected unauthenticated pairing to be disabled by default")
	}
	if cfg.Service.MutationRateLimitPerMinute != 60 {
		t.Fatalf("expected Service.MutationRateLimitPerMinute=60, got %d", cfg.Service.MutationRateLimitPerMinute)
	}
	if cfg.Channels.Telegram.OpenAccess || cfg.Channels.Slack.OpenAccess || cfg.Channels.Discord.OpenAccess || cfg.Channels.WhatsApp.OpenAccess {
		t.Error("expected external channels to default to closed access")
	}
	if cfg.Channels.Email.Enabled || cfg.Channels.Email.ConsentGranted || cfg.Channels.Email.OpenAccess {
		t.Error("expected email channel to default to disabled closed access without consent")
	}
	if cfg.Channels.Email.PollIntervalSeconds != 30 || cfg.Channels.Email.MaxBodyChars != 4000 {
		t.Fatalf("unexpected email defaults: %+v", cfg.Channels.Email)
	}
	if cfg.Session.DirectMessagesShareDefault {
		t.Error("expected direct messages to stay isolated by default")
	}
	if cfg.Heartbeat.IntervalMinutes != 30 {
		t.Fatalf("expected Heartbeat.IntervalMinutes=30, got %d", cfg.Heartbeat.IntervalMinutes)
	}
	if cfg.Heartbeat.SessionKey != DefaultHeartbeatSessionKey {
		t.Fatalf("expected Heartbeat.SessionKey=%q, got %q", DefaultHeartbeatSessionKey, cfg.Heartbeat.SessionKey)
	}
	if cfg.Hardening.GuardedTools {
		t.Error("expected guarded tools to be disabled by default")
	}
	if cfg.Hardening.PrivilegedTools {
		t.Error("expected privileged tools to be disabled by default")
	}
	if cfg.Hardening.EnableExecShell {
		t.Error("expected exec shell mode to be disabled by default")
	}
	if !cfg.Hardening.IsolateChannelPeers {
		t.Error("expected channel peer isolation to be enabled by default")
	}
	if !cfg.Hardening.Quotas.Enabled {
		t.Error("expected quotas to be enabled by default")
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		t.Fatal("expected default exec allowlist")
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		t.Fatal("expected default child environment allowlist")
	}
	if !cfg.Skills.Policy.QuarantineByDefault {
		t.Fatal("expected skills to quarantine by default")
	}
	if cfg.Hardening.Sandbox.BubblewrapPath != "bwrap" {
		t.Fatalf("expected default bubblewrap path, got %q", cfg.Hardening.Sandbox.BubblewrapPath)
	}
	if cfg.Security.SecretStore.KeyFile == "" || cfg.Security.Audit.KeyFile == "" {
		t.Fatal("expected default phase 3 key file paths")
	}
	if cfg.Security.Approvals.HostID != "local" {
		t.Fatalf("expected default approval host ID, got %q", cfg.Security.Approvals.HostID)
	}
	if cfg.Security.Approvals.Exec.Mode != ApprovalModeTrusted {
		t.Fatalf("expected trusted exec approval default, got %q", cfg.Security.Approvals.Exec.Mode)
	}
}

func TestLoad_ApprovalsRemainBackwardCompatibleWhenMissing(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Security.Approvals = ApprovalConfig{}

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Security.Approvals.HostID != "local" {
		t.Fatalf("expected host ID default, got %q", loaded.Security.Approvals.HostID)
	}
	if loaded.Security.Approvals.Pairing.Mode != ApprovalModeAsk {
		t.Fatalf("expected pairing default mode, got %q", loaded.Security.Approvals.Pairing.Mode)
	}
}

func TestLoad_RejectsUnknownApprovalMode(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Security.Approvals.Exec.Mode = ApprovalMode("maybe")

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown approval mode to fail")
	}
}

func TestValidateAuthConfigRejectsUnsafeRPAndOrigins(t *testing.T) {
	base := AuthConfig{
		Enabled:                   true,
		RPID:                      "or3.chat",
		RPDisplayName:             "OR3",
		AllowedOrigins:            []string{"https://or3.chat"},
		SessionIdleTTLSeconds:     300,
		SessionAbsoluteTTLSeconds: 3600,
		StepUpTTLSeconds:          120,
		FallbackPolicy:            AuthFallbackPairedTokenPlusWarn,
		EnforcementMode:           AuthEnforcementWarn,
	}
	tests := []struct {
		name string
		edit func(*AuthConfig)
	}{
		{name: "rp id is url", edit: func(cfg *AuthConfig) { cfg.RPID = "https://or3.chat" }},
		{name: "rp id is raw ip", edit: func(cfg *AuthConfig) { cfg.RPID = "192.168.1.2" }},
		{name: "wildcard origin", edit: func(cfg *AuthConfig) { cfg.AllowedOrigins = []string{"https://*.or3.chat"} }},
		{name: "http production origin", edit: func(cfg *AuthConfig) { cfg.AllowedOrigins = []string{"http://or3.chat"} }},
		{name: "origin outside rp id", edit: func(cfg *AuthConfig) { cfg.AllowedOrigins = []string{"https://example.com"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.edit(&cfg)
			if err := validateAuthConfig(cfg); err == nil {
				t.Fatal("expected auth config validation to reject unsafe RP/origin")
			}
		})
	}
}

func TestApplyEnvOverrides_ServiceConfig(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()
	t.Setenv("OR3_SERVICE_ENABLED", "true")
	t.Setenv("OR3_SERVICE_LISTEN", "127.0.0.1:9200")
	t.Setenv("OR3_SERVICE_SECRET", "top-secret-value")

	ApplyEnvOverrides(&cfg)

	if !cfg.Service.Enabled {
		t.Fatal("expected service enabled override")
	}
	if cfg.Service.Listen != "127.0.0.1:9200" {
		t.Fatalf("unexpected service listen override: %q", cfg.Service.Listen)
	}
	if cfg.Service.Secret != "top-secret-value" {
		t.Fatalf("unexpected service secret override: %q", cfg.Service.Secret)
	}
}

func TestLoad_ServiceHardeningDefaultsAndNormalization(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Service.SharedSecretRole = " OPERATOR "
	cfg.Service.MaxCapability = " GUARDED "
	cfg.Service.AllowUnauthenticatedPairing = true
	cfg.Service.MutationRateLimitPerMinute = 0

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Service.SharedSecretRole != "operator" {
		t.Fatalf("expected normalized shared secret role, got %q", loaded.Service.SharedSecretRole)
	}
	if loaded.Service.MaxCapability != "guarded" {
		t.Fatalf("expected normalized max capability, got %q", loaded.Service.MaxCapability)
	}
	if !loaded.Service.AllowUnauthenticatedPairing {
		t.Fatal("expected explicit unauthenticated pairing setting to round trip")
	}
	if loaded.Service.MutationRateLimitPerMinute != 60 {
		t.Fatalf("expected non-positive rate limit to default to 60, got %d", loaded.Service.MutationRateLimitPerMinute)
	}
}

func TestLoad_ServiceHardeningInvalidValuesDefaultSafely(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Service.SharedSecretRole = "root"
	cfg.Service.MaxCapability = "god-mode"

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Service.SharedSecretRole != "service-client" {
		t.Fatalf("expected invalid shared secret role to default safely, got %q", loaded.Service.SharedSecretRole)
	}
	if loaded.Service.MaxCapability != "safe" {
		t.Fatalf("expected invalid max capability to default safely, got %q", loaded.Service.MaxCapability)
	}
}

func TestLoad_HardeningDefaultsAndOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	cfg.Hardening.ExecAllowedPrograms = []string{"go", "git"}
	cfg.Hardening.ChildEnvAllowlist = []string{"PATH"}
	cfg.Hardening.Quotas = HardeningQuotaConfig{
		Enabled:          true,
		MaxToolCalls:     3,
		MaxExecCalls:     1,
		MaxWebCalls:      2,
		MaxSubagentCalls: 1,
	}

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Hardening.GuardedTools || !loaded.Hardening.PrivilegedTools || !loaded.Hardening.EnableExecShell {
		t.Fatalf("expected hardening overrides to survive, got %+v", loaded.Hardening)
	}
	if got := loaded.Hardening.ExecAllowedPrograms; len(got) != 2 || got[0] != "go" || got[1] != "git" {
		t.Fatalf("unexpected exec allowlist: %#v", got)
	}
	if got := loaded.Hardening.ChildEnvAllowlist; len(got) != 1 || got[0] != "PATH" {
		t.Fatalf("unexpected child env allowlist: %#v", got)
	}
	if loaded.Hardening.Quotas.MaxToolCalls != 3 || loaded.Hardening.Quotas.MaxExecCalls != 1 || loaded.Hardening.Quotas.MaxWebCalls != 2 || loaded.Hardening.Quotas.MaxSubagentCalls != 1 {
		t.Fatalf("unexpected quota overrides: %+v", loaded.Hardening.Quotas)
	}
}

func TestLoad_HardeningAllowsDisablingPeerIsolation(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Hardening.IsolateChannelPeers = false

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Hardening.IsolateChannelPeers {
		t.Fatalf("expected peer isolation disable to persist, got %+v", loaded.Hardening)
	}
}

func TestLoad_SkillsPolicyAllowsDisablingQuarantineByDefault(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Skills.Policy.QuarantineByDefault = false
	cfg.Skills.Policy.Approved = []string{"demo"}

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Skills.Policy.QuarantineByDefault {
		t.Fatalf("expected quarantineByDefault=false to persist, got %+v", loaded.Skills.Policy)
	}
	if len(loaded.Skills.Policy.Approved) != 1 || loaded.Skills.Policy.Approved[0] != "demo" {
		t.Fatalf("unexpected approved skills: %#v", loaded.Skills.Policy.Approved)
	}
}

func TestLoad_RejectsUnknownAccessProfileReference(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Security.Profiles.Enabled = true
	cfg.Security.Profiles.Default = "missing"

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown access profile reference to fail")
	}
}

func TestLoad_EnabledExternalChannelRequiresAllowlistOrOpenAccess(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Channels.Telegram.Enabled = true

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error when telegram is enabled without allowlist or openAccess")
	}
}

func TestLoad_EnabledExternalChannelAllowsExplicitOpenAccess(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.OpenAccess = true

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Channels.Telegram.OpenAccess {
		t.Fatal("expected telegram openAccess to remain true")
	}
}

func TestLoad_EnabledExternalChannelAllowsPairingPolicy(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.InboundPolicy = InboundPolicyPairing

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Channels.Slack.InboundPolicy != InboundPolicyPairing {
		t.Fatalf("expected pairing policy, got %q", loaded.Channels.Slack.InboundPolicy)
	}
}

func TestLoad_EmailChannelRequiresConsentAndCredentials(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Channels.Email.Enabled = true
	cfg.Channels.Email.OpenAccess = true
	cfg.Channels.Email.IMAPHost = "imap.example.com"
	cfg.Channels.Email.IMAPUsername = "imap-user"
	cfg.Channels.Email.IMAPPassword = "imap-pass"
	cfg.Channels.Email.SMTPHost = "smtp.example.com"
	cfg.Channels.Email.SMTPUsername = "smtp-user"
	cfg.Channels.Email.SMTPPassword = "smtp-pass"

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error when email is enabled without consent")
	}

	cfg.Channels.Email.ConsentGranted = true
	cfg.Channels.Email.IMAPPassword = ""
	b, _ = json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error when email credentials are incomplete")
	}
}

func TestLoad_EmailChannelEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, _ := json.MarshalIndent(Default(), "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("OR3_EMAIL_IMAP_HOST", "imap.env.test")
	t.Setenv("OR3_EMAIL_IMAP_PORT", "1993")
	t.Setenv("OR3_EMAIL_IMAP_USERNAME", "imap-env-user")
	t.Setenv("OR3_EMAIL_IMAP_PASSWORD", "imap-env-pass")
	t.Setenv("OR3_EMAIL_SMTP_HOST", "smtp.env.test")
	t.Setenv("OR3_EMAIL_SMTP_PORT", "1587")
	t.Setenv("OR3_EMAIL_SMTP_USERNAME", "smtp-env-user")
	t.Setenv("OR3_EMAIL_SMTP_PASSWORD", "smtp-env-pass")
	t.Setenv("OR3_EMAIL_FROM_ADDRESS", "bot@env.test")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Channels.Email.IMAPHost != "imap.env.test" || cfg.Channels.Email.IMAPPort != 1993 || cfg.Channels.Email.IMAPUsername != "imap-env-user" || cfg.Channels.Email.IMAPPassword != "imap-env-pass" {
		t.Fatalf("unexpected IMAP env overrides: %+v", cfg.Channels.Email)
	}
	if cfg.Channels.Email.SMTPHost != "smtp.env.test" || cfg.Channels.Email.SMTPPort != 1587 || cfg.Channels.Email.SMTPUsername != "smtp-env-user" || cfg.Channels.Email.SMTPPassword != "smtp-env-pass" || cfg.Channels.Email.FromAddress != "bot@env.test" {
		t.Fatalf("unexpected SMTP env overrides: %+v", cfg.Channels.Email)
	}
}

func TestLoad_FileNotExist_CreatesDefault(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey != "cli:default" {
		t.Errorf("expected DefaultSessionKey='cli:default', got %q", cfg.DefaultSessionKey)
	}
	// should have created the file
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestLoad_FileNotExist_AppliesEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	t.Setenv("OR3_API_BASE", "https://openrouter.ai/api/v1")
	t.Setenv("OR3_API_KEY", "env-key")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.APIBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("expected env API base override, got %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.APIKey != "env-key" {
		t.Fatalf("expected env API key override, got %q", cfg.Provider.APIKey)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to be created")
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	var saved Config
	if err := json.Unmarshal(stored, &saved); err != nil {
		t.Fatalf("unmarshal stored config: %v", err)
	}
	if saved.Provider.APIBase != Default().Provider.APIBase {
		t.Fatalf("expected on-disk config to keep default API base, got %q", saved.Provider.APIBase)
	}
}

func TestSave_ExistingFilePermissionsAreTightened(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, mustJSON(Default()), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config permissions 0600 after save, got %o", info.Mode().Perm())
	}
}

func TestLoad_ValidFile(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	input := Config{
		DBPath:            "/tmp/test.db",
		DefaultSessionKey: "test:session",
		HistoryMax:        20,
		MaxToolLoops:      3,
		Provider: ProviderConfig{
			APIBase:        "https://custom.api",
			TimeoutSeconds: 30,
		},
	}
	b, _ := json.MarshalIndent(input, "", "  ")
	mustWriteTestFile(t, path, b)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("expected DBPath='/tmp/test.db', got %q", cfg.DBPath)
	}
	if cfg.DefaultSessionKey != "test:session" {
		t.Errorf("expected DefaultSessionKey='test:session', got %q", cfg.DefaultSessionKey)
	}
	if cfg.HistoryMax != 20 {
		t.Errorf("expected HistoryMax=20, got %d", cfg.HistoryMax)
	}
	if cfg.MaxMediaBytes != Default().MaxMediaBytes {
		t.Errorf("expected missing MaxMediaBytes to default to %d, got %d", Default().MaxMediaBytes, cfg.MaxMediaBytes)
	}
	if cfg.Provider.EnableVision {
		t.Error("expected missing EnableVision to default to false")
	}
}

func TestLoad_DocIndexEnabledWithoutRootsDefaultsToWorkspace(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	workspace := filepath.Join(dir, "workspace")

	input := Config{
		WorkspaceDir: workspace,
		DocIndex: DocIndexConfig{
			Enabled: true,
		},
	}
	b, _ := json.MarshalIndent(input, "", "  ")
	mustWriteTestFile(t, path, b)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.DocIndex.Roots) != 1 || cfg.DocIndex.Roots[0] != workspace {
		t.Fatalf("expected doc index root to default to workspace %q, got %#v", workspace, cfg.DocIndex.Roots)
	}
}

func TestLoad_HeartbeatDefaultsRemainBackwardCompatible(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := map[string]any{
		"heartbeat": map[string]any{
			"enabled":         true,
			"intervalMinutes": 0,
			"tasksFile":       filepath.Join(dir, "HEARTBEAT.md"),
		},
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Heartbeat.IntervalMinutes != 30 {
		t.Fatalf("expected default heartbeat interval after normalization, got %d", cfg.Heartbeat.IntervalMinutes)
	}
	if cfg.Heartbeat.SessionKey != DefaultHeartbeatSessionKey {
		t.Fatalf("expected default heartbeat session key, got %q", cfg.Heartbeat.SessionKey)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	mustWriteTestFile(t, path, []byte("{invalid json"))

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write valid default config
	b, _ := json.MarshalIndent(Default(), "", "  ")
	mustWriteTestFile(t, path, b)

	// Set env vars
	t.Setenv("OR3_DB_PATH", "/env/test.db")
	t.Setenv("OR3_API_KEY", "env-key")
	t.Setenv("OR3_MODEL", "env-model")
	t.Setenv("OR3_CONSOLIDATION_MODEL", "env-consolidation-model")
	t.Setenv("OR3_EMBED_MODEL", "env-embed")
	t.Setenv("OR3_EMBED_DIMENSIONS", "768")
	t.Setenv("OR3_API_BASE", "https://env.api")
	t.Setenv("OR3_SUBAGENTS_ENABLED", "true")
	t.Setenv("OR3_SUBAGENTS_MAX_CONCURRENT", "3")
	t.Setenv("OR3_SUBAGENTS_MAX_QUEUED", "12")
	t.Setenv("OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS", "90")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBPath != "/env/test.db" {
		t.Errorf("expected DBPath='/env/test.db', got %q", cfg.DBPath)
	}
	if cfg.Provider.APIKey != "env-key" {
		t.Errorf("expected APIKey='env-key', got %q", cfg.Provider.APIKey)
	}
	if cfg.Provider.Model != "env-model" {
		t.Errorf("expected Model='env-model', got %q", cfg.Provider.Model)
	}
	if cfg.ConsolidationModel != "env-consolidation-model" {
		t.Errorf("expected ConsolidationModel='env-consolidation-model', got %q", cfg.ConsolidationModel)
	}
	if cfg.Provider.EmbedModel != "env-embed" {
		t.Errorf("expected EmbedModel='env-embed', got %q", cfg.Provider.EmbedModel)
	}
	if cfg.Provider.EmbedDimensions != 768 {
		t.Errorf("expected EmbedDimensions=768, got %d", cfg.Provider.EmbedDimensions)
	}
	if cfg.Provider.APIBase != "https://env.api" {
		t.Errorf("expected APIBase='https://env.api', got %q", cfg.Provider.APIBase)
	}
	if !cfg.Subagents.Enabled {
		t.Error("expected subagents enabled from env override")
	}
	if cfg.Subagents.MaxConcurrent != 3 {
		t.Errorf("expected MaxConcurrent=3, got %d", cfg.Subagents.MaxConcurrent)
	}
	if cfg.Subagents.MaxQueued != 12 {
		t.Errorf("expected MaxQueued=12, got %d", cfg.Subagents.MaxQueued)
	}
	if cfg.Subagents.TaskTimeoutSeconds != 90 {
		t.Errorf("expected TaskTimeoutSeconds=90, got %d", cfg.Subagents.TaskTimeoutSeconds)
	}
}

func TestLoad_SubagentNormalization(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	input := Default()
	input.Subagents.MaxConcurrent = 0
	input.Subagents.MaxQueued = 0
	input.Subagents.TaskTimeoutSeconds = 0
	b, _ := json.MarshalIndent(input, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Subagents.MaxConcurrent != 1 || cfg.Subagents.MaxQueued != 32 || cfg.Subagents.TaskTimeoutSeconds != 300 {
		t.Fatalf("expected normalized subagent defaults, got %+v", cfg.Subagents)
	}
}

func TestLoad_ArtifactsDirEnvOverride(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, _ := json.MarshalIndent(Default(), "", "  ")
	mustWriteTestFile(t, path, b)

	t.Setenv("OR3_ARTIFACTS_DIR", "/env/artifacts")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ArtifactsDir != "/env/artifacts" {
		t.Errorf("expected ArtifactsDir='/env/artifacts', got %q", cfg.ArtifactsDir)
	}
}

func TestLoad_ChannelEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, _ := json.MarshalIndent(Default(), "", "  ")
	mustWriteTestFile(t, path, b)

	t.Setenv("OR3_TELEGRAM_TOKEN", "telegram-token")
	t.Setenv("OR3_SLACK_APP_TOKEN", "slack-app")
	t.Setenv("OR3_SLACK_BOT_TOKEN", "slack-bot")
	t.Setenv("OR3_DISCORD_TOKEN", "discord-token")
	t.Setenv("OR3_WHATSAPP_BRIDGE_URL", "ws://127.0.0.1:3001/ws")
	t.Setenv("OR3_WHATSAPP_BRIDGE_TOKEN", "bridge-token")
	t.Setenv("OR3_EMAIL_IMAP_HOST", "imap.example.com")
	t.Setenv("OR3_EMAIL_SMTP_HOST", "smtp.example.com")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Channels.Telegram.Token != "telegram-token" || cfg.Channels.Slack.AppToken != "slack-app" || cfg.Channels.Slack.BotToken != "slack-bot" || cfg.Channels.Discord.Token != "discord-token" || cfg.Channels.WhatsApp.BridgeToken != "bridge-token" || cfg.Channels.Email.IMAPHost != "imap.example.com" || cfg.Channels.Email.SMTPHost != "smtp.example.com" {
		t.Fatalf("unexpected channel env overrides: %#v", cfg.Channels)
	}
}

func TestLoad_ZeroValues_GetDefaults(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// config with zero values
	input := Config{}
	b, _ := json.MarshalIndent(input, "", "  ")
	mustWriteTestFile(t, path, b)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey != "cli:default" {
		t.Errorf("expected DefaultSessionKey='cli:default', got %q", cfg.DefaultSessionKey)
	}
	if cfg.HistoryMax != 40 {
		t.Errorf("expected HistoryMax=40, got %d", cfg.HistoryMax)
	}
	if cfg.MaxToolBytes != 24*1024 {
		t.Errorf("expected MaxToolBytes=%d, got %d", 24*1024, cfg.MaxToolBytes)
	}
	if cfg.MaxToolLoops != 6 {
		t.Errorf("expected MaxToolLoops=6, got %d", cfg.MaxToolLoops)
	}
	if cfg.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", cfg.VectorScanLimit)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("expected WorkerCount=4, got %d", cfg.WorkerCount)
	}
	if cfg.Provider.TimeoutSeconds != 60 {
		t.Errorf("expected TimeoutSeconds=60, got %d", cfg.Provider.TimeoutSeconds)
	}
	if cfg.ConsolidationMaxMessages != 50 {
		t.Errorf("expected ConsolidationMaxMessages=50, got %d", cfg.ConsolidationMaxMessages)
	}
	if cfg.ConsolidationMaxInputChars != 12000 {
		t.Errorf("expected ConsolidationMaxInputChars=12000, got %d", cfg.ConsolidationMaxInputChars)
	}
	if cfg.ConsolidationAsyncTimeoutSeconds != 30 {
		t.Errorf("expected ConsolidationAsyncTimeoutSeconds=30, got %d", cfg.ConsolidationAsyncTimeoutSeconds)
	}
}

func TestLoad_EmptyPath_UsesDefault(t *testing.T) {
	clearConfigEnv(t)
	// Use a temp home dir to avoid touching real home
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey == "" {
		t.Error("expected non-empty DefaultSessionKey")
	}
}

func TestMustJSON(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()
	b := mustJSON(cfg)
	if len(b) == 0 {
		t.Fatal("expected non-empty JSON output")
	}
	var out Config
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
}

func TestRuntimeProfileEnvOverride(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	t.Run("sets profile from env", func(t *testing.T) {
		t.Setenv("OR3_RUNTIME_PROFILE", "local-dev")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RuntimeProfile != ProfileLocalDev {
			t.Errorf("expected ProfileLocalDev, got %q", cfg.RuntimeProfile)
		}
	})

	t.Run("normalizes case", func(t *testing.T) {
		t.Setenv("OR3_RUNTIME_PROFILE", "Local-Dev")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RuntimeProfile != ProfileLocalDev {
			t.Errorf("expected ProfileLocalDev after normalisation, got %q", cfg.RuntimeProfile)
		}
	})

	t.Run("empty env leaves profile empty", func(t *testing.T) {
		t.Setenv("OR3_RUNTIME_PROFILE", "")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RuntimeProfile != "" {
			t.Errorf("expected empty profile, got %q", cfg.RuntimeProfile)
		}
	})

	t.Run("unknown profile returns error", func(t *testing.T) {
		t.Setenv("OR3_RUNTIME_PROFILE", "not-a-real-profile")
		_, err := Load("")
		if err == nil {
			t.Fatal("expected error for unknown profile, got nil")
		}
	})
}

func hostedConfig() Config {
	cfg := Default()
	cfg.Security.SecretStore.Enabled = true
	cfg.Security.SecretStore.Required = true
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Strict = true
	cfg.Security.Audit.VerifyOnStart = true
	cfg.Security.Network.Enabled = true
	cfg.Security.Network.DefaultDeny = true
	return cfg
}

func TestValidateProfile(t *testing.T) {
	t.Run("empty profile always passes", func(t *testing.T) {
		cfg := Default()
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("local-dev always passes", func(t *testing.T) {
		cfg := Default()
		cfg.RuntimeProfile = ProfileLocalDev
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("single-user-hardened remains advisory", func(t *testing.T) {
		cfg := Default()
		cfg.RuntimeProfile = ProfileSingleUserHardened
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("hosted-service requires secretStore", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.SecretStore.Enabled = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted profiles require security.secretStore.enabled" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service requires audit", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Audit.Enabled = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted profiles require security.audit.enabled" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service requires network policy", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Network.Enabled = false
		cfg.Security.Network.DefaultDeny = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted profiles require security.network policy to be configured" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service passes with only network.enabled set", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Network.Enabled = true
		cfg.Security.Network.DefaultDeny = false
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil when network.enabled=true, got %v", err)
		}
	})

	t.Run("hosted-service passes with only defaultDeny set", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Network.Enabled = false
		cfg.Security.Network.DefaultDeny = true
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil when defaultDeny=true, got %v", err)
		}
	})

	t.Run("hosted-service passes with valid config", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("hosted-service requires secretStore.required", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.SecretStore.Required = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "profile requires security.secretStore.required" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service requires audit.strict", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Audit.Strict = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "profile requires security.audit.strict" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service requires audit.verifyOnStart", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Audit.VerifyOnStart = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "profile requires security.audit.verifyOnStart" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service requires deny-by-default for remote MCP http", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Network.DefaultDeny = false
		cfg.Tools.MCPServers = map[string]MCPServerConfig{
			"remote": {
				Enabled:   true,
				Transport: "sse",
				URL:       "https://mcp.example.com/stream",
			},
		}
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted profiles require deny-by-default security.network for remote MCP HTTP" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-service rejects broad allowlist for remote MCP http", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedService
		cfg.Security.Network.AllowedHosts = []string{"*.example.com"}
		cfg.Tools.MCPServers = map[string]MCPServerConfig{
			"remote": {
				Enabled:   true,
				Transport: "streamablehttp",
				URL:       "https://mcp.example.com/stream",
			},
		}
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted profiles require a narrow security.network.allowedHosts for remote MCP HTTP" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-no-exec rejects enableExecShell", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedNoExec
		cfg.Hardening.EnableExecShell = true
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted-no-exec profile does not allow enableExecShell" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-no-exec rejects privilegedTools", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedNoExec
		cfg.Hardening.PrivilegedTools = true
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted-no-exec profile does not allow privilegedTools" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-no-exec passes with safe config", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedNoExec
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("hosted-remote-sandbox-only rejects exec without sandbox", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedRemoteSandbox
		cfg.Hardening.EnableExecShell = true
		cfg.Hardening.Sandbox.Enabled = false
		err := ValidateProfile(cfg)
		if err == nil || err.Error() != "hosted-remote-sandbox-only profile requires sandbox for exec" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("hosted-remote-sandbox-only allows exec with sandbox", func(t *testing.T) {
		cfg := hostedConfig()
		cfg.RuntimeProfile = ProfileHostedRemoteSandbox
		cfg.Hardening.EnableExecShell = true
		cfg.Hardening.Sandbox.Enabled = true
		if err := ValidateProfile(cfg); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})
}

func TestLoad_ContextDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Default()
	cfg.Context.Mode = "invalid-mode"
	cfg.Context.MaxInputTokens = -10
	cfg.Context.OutputReserveTokens = -1
	cfg.Context.Pressure.WarningPercent = 0
	cfg.Context.Pressure.HighPercent = 10
	cfg.Context.Pressure.EmergencyPercent = 20
	cfg.ContextManager.TimeoutSeconds = 0
	cfg.ContextManager.IdlePruneSeconds = 0
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Context.Mode != "quality" {
		t.Fatalf("expected invalid mode to clamp to quality, got %q", got.Context.Mode)
	}
	if got.Context.MaxInputTokens <= 0 || got.Context.OutputReserveTokens <= 0 {
		t.Fatalf("expected positive context budgets, got %+v", got.Context)
	}
	if got.Context.Pressure.WarningPercent <= 0 || got.Context.Pressure.HighPercent <= got.Context.Pressure.WarningPercent || got.Context.Pressure.EmergencyPercent <= got.Context.Pressure.HighPercent {
		t.Fatalf("unexpected pressure thresholds: %+v", got.Context.Pressure)
	}
	if got.ContextManager.TimeoutSeconds <= 0 {
		t.Fatalf("expected context manager timeout default, got %+v", got.ContextManager)
	}
	if got.ContextManager.IdlePruneSeconds != 300 {
		t.Fatalf("expected context manager idle prune default, got %+v", got.ContextManager)
	}
	if !got.ContextConfigured {
		t.Fatalf("expected context block written by Save(Default()) to be treated as configured")
	}
}

func TestLoad_LegacyConfigWithoutContextBlockPreservesLegacyMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
	  "historyMaxMessages": 17,
	  "memoryRetrieveLimit": 9,
	  "vectorSearchK": 11,
	  "ftsSearchK": 12,
	  "bootstrapMaxChars": 1234,
	  "bootstrapTotalMaxChars": 5678,
	  "maxToolBytes": 4321,
	  "provider": {"apiBase": "https://api.openai.com/v1", "model": "gpt-4.1-mini"},
	  "tools": {},
	  "hardening": {},
	  "skills": {},
	  "triggers": {},
	  "heartbeat": {},
	  "cron": {},
	  "service": {},
	  "channels": {},
	  "security": {},
	  "docIndex": {},
	  "subagents": {},
	  "session": {}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ContextConfigured {
		t.Fatalf("expected legacy config without context block to leave ContextConfigured=false")
	}
	if got.HistoryMax != 17 || got.MemoryRetrieve != 9 || got.VectorK != 11 || got.FTSK != 12 || got.BootstrapMaxChars != 1234 || got.BootstrapTotalMaxChars != 5678 || got.MaxToolBytes != 4321 {
		t.Fatalf("expected legacy runtime knobs preserved, got %+v", got)
	}
}
