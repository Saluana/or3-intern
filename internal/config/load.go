// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Load(path string) (Config, error) {
	path = resolveConfigPath(path)
	cfg, err := readConfigFile(path)
	if err != nil {
		return cfg, err
	}
	ApplyEnvOverrides(&cfg)
	return normalizeAndValidateConfig(cfg)
}

func resolveConfigPath(path string) string {
	if path != "" {
		return path
	}
	return DefaultPath()
}

func readConfigFile(path string) (Config, error) {
	cfg := Default()
	cfg.ContextConfigured = false

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.ContextConfigured = true
			if err := Save(path, cfg); err != nil {
				return cfg, err
			}
			return cfg, nil
		}
		return cfg, err
	}
	return parseConfigFile(b, cfg)
}

func parseConfigFile(data []byte, cfg Config) (Config, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err == nil {
		_, cfg.ContextConfigured = top["context"]
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func normalizeAndValidateConfig(cfg Config) (Config, error) {
	if cfg.Provider.TimeoutSeconds <= 0 {
		cfg.Provider.TimeoutSeconds = int((60 * time.Second).Seconds())
	}
	if cfg.Provider.EmbedDimensions < 0 {
		cfg.Provider.EmbedDimensions = 0
	}
	normalizeProviderRouting(&cfg)
	if cfg.DefaultSessionKey == "" {
		cfg.DefaultSessionKey = "cli:default"
	}
	if cfg.BootstrapMaxChars <= 0 {
		cfg.BootstrapMaxChars = 20000
	}
	if cfg.BootstrapTotalMaxChars <= 0 {
		cfg.BootstrapTotalMaxChars = 150000
	}
	if cfg.HistoryMax <= 0 {
		cfg.HistoryMax = 40
	}
	if cfg.MaxToolBytes <= 0 {
		cfg.MaxToolBytes = DefaultMaxToolBytes
	}
	if cfg.MaxMediaBytes <= 0 {
		cfg.MaxMediaBytes = 20 * 1024 * 1024
	}
	if cfg.MaxToolLoops <= 0 {
		cfg.MaxToolLoops = 6
	}
	cfg.MaxToolLoopsExceededAction = normalizeQuotaExceededAction(cfg.MaxToolLoopsExceededAction, Default().MaxToolLoopsExceededAction)
	if cfg.VectorScanLimit <= 0 {
		cfg.VectorScanLimit = 2000
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.ConsolidationWindowSize <= 0 {
		cfg.ConsolidationWindowSize = 10
	}
	if cfg.ConsolidationMaxMessages <= 0 {
		cfg.ConsolidationMaxMessages = 50
	}
	if cfg.ConsolidationMaxInputChars <= 0 {
		cfg.ConsolidationMaxInputChars = 12000
	}
	if cfg.ConsolidationAsyncTimeoutSeconds <= 0 {
		cfg.ConsolidationAsyncTimeoutSeconds = 30
	}
	if cfg.Subagents.MaxConcurrent <= 0 {
		cfg.Subagents.MaxConcurrent = 1
	}
	if cfg.Subagents.MaxQueued <= 0 {
		cfg.Subagents.MaxQueued = 32
	}
	if cfg.Subagents.TaskTimeoutSeconds <= 0 {
		cfg.Subagents.TaskTimeoutSeconds = 300
	}
	if cfg.AgentCLI.MaxConcurrent <= 0 {
		cfg.AgentCLI.MaxConcurrent = 1
	}
	if cfg.AgentCLI.MaxQueued <= 0 {
		cfg.AgentCLI.MaxQueued = 16
	}
	if cfg.AgentCLI.DefaultTimeoutSeconds <= 0 {
		cfg.AgentCLI.DefaultTimeoutSeconds = 900
	}
	if cfg.AgentCLI.DefaultTimeoutSeconds < 30 {
		cfg.AgentCLI.DefaultTimeoutSeconds = 30
	}
	if cfg.AgentCLI.MaxTimeoutSeconds <= 0 {
		cfg.AgentCLI.MaxTimeoutSeconds = 7200
	}
	if cfg.AgentCLI.MaxTimeoutSeconds < 30 {
		cfg.AgentCLI.MaxTimeoutSeconds = 30
	}
	if cfg.AgentCLI.EventChunkMaxBytes <= 0 {
		cfg.AgentCLI.EventChunkMaxBytes = 16384
	}
	if cfg.AgentCLI.PreviewMaxBytes <= 0 {
		cfg.AgentCLI.PreviewMaxBytes = 65536
	}
	if cfg.AgentCLI.MaxPersistedOutputBytes <= 0 {
		cfg.AgentCLI.MaxPersistedOutputBytes = 10485760
	}
	if strings.TrimSpace(cfg.AgentCLI.DefaultMode) == "" {
		cfg.AgentCLI.DefaultMode = "safe_edit"
	} else {
		cfg.AgentCLI.DefaultMode = strings.TrimSpace(cfg.AgentCLI.DefaultMode)
	}
	if strings.TrimSpace(cfg.AgentCLI.DefaultIsolation) == "" {
		cfg.AgentCLI.DefaultIsolation = "host_workspace_write"
	} else {
		cfg.AgentCLI.DefaultIsolation = strings.TrimSpace(cfg.AgentCLI.DefaultIsolation)
	}
	if cfg.AgentCLI.DisabledRunners == nil {
		cfg.AgentCLI.DisabledRunners = []string{}
	}
	cfg.AgentCLI.DisabledRunners = compactStrings(cfg.AgentCLI.DisabledRunners)
	if len(cfg.AgentCLI.ChildEnvAllowlist) == 0 {
		cfg.AgentCLI.ChildEnvAllowlist = []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"}
	}
	if strings.TrimSpace(cfg.Service.Listen) == "" {
		cfg.Service.Listen = Default().Service.Listen
	}
	cfg.Service.SharedSecretRole = normalizeServiceRole(cfg.Service.SharedSecretRole)
	if cfg.Service.SharedSecretRole == "" {
		cfg.Service.SharedSecretRole = Default().Service.SharedSecretRole
	}
	cfg.Service.MaxCapability = normalizeCapabilityValue(cfg.Service.MaxCapability)
	if cfg.Service.MaxCapability == "" {
		cfg.Service.MaxCapability = Default().Service.MaxCapability
	}
	if cfg.Service.TrustedBrowserOrigins == nil {
		cfg.Service.TrustedBrowserOrigins = []string{}
	}
	if cfg.Service.TrustedBrowserCIDRs == nil {
		cfg.Service.TrustedBrowserCIDRs = []string{}
	}
	if cfg.Service.TrustedPairingOrigins == nil {
		cfg.Service.TrustedPairingOrigins = []string{}
	}
	if cfg.Service.TrustedPairingCIDRs == nil {
		cfg.Service.TrustedPairingCIDRs = []string{}
	}
	cfg.Service.TrustedBrowserOrigins = compactStrings(cfg.Service.TrustedBrowserOrigins)
	cfg.Service.TrustedBrowserCIDRs = compactStrings(cfg.Service.TrustedBrowserCIDRs)
	cfg.Service.TrustedPairingOrigins = compactStrings(cfg.Service.TrustedPairingOrigins)
	cfg.Service.TrustedPairingCIDRs = compactStrings(cfg.Service.TrustedPairingCIDRs)
	if cfg.Service.MutationRateLimitPerMinute <= 0 {
		cfg.Service.MutationRateLimitPerMinute = Default().Service.MutationRateLimitPerMinute
	}
	if cfg.Channels.Telegram.APIBase == "" {
		cfg.Channels.Telegram.APIBase = "https://api.telegram.org"
	}
	if cfg.Channels.Telegram.PollSeconds <= 0 {
		cfg.Channels.Telegram.PollSeconds = 2
	}
	if cfg.Channels.Slack.APIBase == "" {
		cfg.Channels.Slack.APIBase = "https://slack.com/api"
	}
	if cfg.Channels.Discord.APIBase == "" {
		cfg.Channels.Discord.APIBase = "https://discord.com/api/v10"
	}
	if cfg.Channels.WhatsApp.BridgeURL == "" {
		cfg.Channels.WhatsApp.BridgeURL = "ws://127.0.0.1:3001/ws"
	}
	if cfg.Channels.Email.PollIntervalSeconds <= 0 {
		cfg.Channels.Email.PollIntervalSeconds = 30
	}
	if cfg.Channels.Email.MaxBodyChars <= 0 {
		cfg.Channels.Email.MaxBodyChars = 4000
	}
	if strings.TrimSpace(cfg.Channels.Email.SubjectPrefix) == "" {
		cfg.Channels.Email.SubjectPrefix = "Re: "
	}
	if strings.TrimSpace(cfg.Channels.Email.IMAPMailbox) == "" {
		cfg.Channels.Email.IMAPMailbox = "INBOX"
	}
	if cfg.Channels.Email.IMAPPort <= 0 {
		cfg.Channels.Email.IMAPPort = 993
	}
	if cfg.Channels.Email.SMTPPort <= 0 {
		cfg.Channels.Email.SMTPPort = 587
	}
	if cfg.DocIndex.MaxFiles <= 0 {
		cfg.DocIndex.MaxFiles = 100
	}
	if strings.TrimSpace(cfg.Context.Mode) == "" {
		cfg.Context.Mode = "quality"
	}
	switch cfg.Context.Mode {
	case "poor":
		if cfg.Context.MaxInputTokens <= 0 {
			cfg.Context.MaxInputTokens = 5000
		}
	case "balanced":
		if cfg.Context.MaxInputTokens <= 0 {
			cfg.Context.MaxInputTokens = 8000
		}
	case "quality", "custom":
		if cfg.Context.MaxInputTokens <= 0 {
			cfg.Context.MaxInputTokens = 16000
		}
	default:
		cfg.Context.Mode = "quality"
		if cfg.Context.MaxInputTokens <= 0 {
			cfg.Context.MaxInputTokens = 16000
		}
	}
	if cfg.Context.OutputReserveTokens <= 0 {
		cfg.Context.OutputReserveTokens = 1200
	}
	if cfg.Context.SafetyMarginTokens < 0 {
		cfg.Context.SafetyMarginTokens = 0
	}
	if cfg.Context.Pressure.WarningPercent <= 0 {
		cfg.Context.Pressure.WarningPercent = 70
	}
	if cfg.Context.Pressure.HighPercent <= cfg.Context.Pressure.WarningPercent {
		cfg.Context.Pressure.HighPercent = cfg.Context.Pressure.WarningPercent + 10
	}
	if cfg.Context.Pressure.EmergencyPercent <= cfg.Context.Pressure.HighPercent {
		cfg.Context.Pressure.EmergencyPercent = cfg.Context.Pressure.HighPercent + 10
	}
	if cfg.ContextManager.TimeoutSeconds <= 0 {
		cfg.ContextManager.TimeoutSeconds = 15
	}
	if cfg.ContextManager.IdlePruneSeconds <= 0 {
		cfg.ContextManager.IdlePruneSeconds = 300
	}
	if cfg.ContextManager.MaxInputTokens <= 0 {
		cfg.ContextManager.MaxInputTokens = 1200
	}
	if cfg.ContextManager.MaxOutputTokens <= 0 {
		cfg.ContextManager.MaxOutputTokens = 600
	}
	if cfg.DocIndex.MaxFileBytes <= 0 {
		cfg.DocIndex.MaxFileBytes = 64 * 1024
	}
	if cfg.DocIndex.MaxChunks <= 0 {
		cfg.DocIndex.MaxChunks = 500
	}
	if cfg.DocIndex.EmbedMaxBytes <= 0 {
		cfg.DocIndex.EmbedMaxBytes = 8 * 1024
	}
	if cfg.DocIndex.RefreshSeconds <= 0 {
		cfg.DocIndex.RefreshSeconds = 300
	}
	if cfg.DocIndex.RetrieveLimit <= 0 {
		cfg.DocIndex.RetrieveLimit = 5
	}
	if cfg.DocIndex.Enabled && len(cfg.DocIndex.Roots) == 0 {
		root := strings.TrimSpace(cfg.WorkspaceDir)
		if root == "" {
			root = "."
		}
		cfg.DocIndex.Roots = []string{root}
	}
	if cfg.Skills.MaxRunSeconds <= 0 {
		cfg.Skills.MaxRunSeconds = 30
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) == "" {
		cfg.Skills.ManagedDir = filepath.Join(filepath.Dir(DefaultPath()), "skills")
	}
	if cfg.Skills.Load.WatchDebounceMS <= 0 {
		cfg.Skills.Load.WatchDebounceMS = 250
	}
	if strings.TrimSpace(cfg.Skills.Load.GlobalDir) == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			cfg.Skills.Load.GlobalDir = filepath.Join(home, ".agents", "skills")
		}
	}
	if cfg.Skills.Policy.Approved == nil {
		cfg.Skills.Policy.Approved = []string{}
	}
	if cfg.Skills.Policy.TrustedOwners == nil {
		cfg.Skills.Policy.TrustedOwners = []string{}
	}
	if cfg.Skills.Policy.BlockedOwners == nil {
		cfg.Skills.Policy.BlockedOwners = []string{}
	}
	if cfg.Skills.Policy.TrustedRegistries == nil {
		cfg.Skills.Policy.TrustedRegistries = []string{}
	}
	cfg.Skills.Policy.Approved = compactStrings(cfg.Skills.Policy.Approved)
	cfg.Skills.Policy.TrustedOwners = compactStrings(cfg.Skills.Policy.TrustedOwners)
	cfg.Skills.Policy.BlockedOwners = compactStrings(cfg.Skills.Policy.BlockedOwners)
	cfg.Skills.Policy.TrustedRegistries = compactStrings(cfg.Skills.Policy.TrustedRegistries)
	if cfg.Skills.Entries == nil {
		cfg.Skills.Entries = map[string]SkillEntryConfig{}
	}
	if cfg.Tools.MCPServers == nil {
		cfg.Tools.MCPServers = map[string]MCPServerConfig{}
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		cfg.Hardening.ExecAllowedPrograms = append([]string{}, Default().Hardening.ExecAllowedPrograms...)
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		cfg.Hardening.ChildEnvAllowlist = append([]string{}, Default().Hardening.ChildEnvAllowlist...)
	}
	if strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
		cfg.Hardening.Sandbox.BubblewrapPath = Default().Hardening.Sandbox.BubblewrapPath
	}
	if cfg.Hardening.Sandbox.WritablePaths == nil {
		cfg.Hardening.Sandbox.WritablePaths = []string{}
	}
	cfg.Hardening.Quotas.ExceededAction = normalizeQuotaExceededAction(cfg.Hardening.Quotas.ExceededAction, Default().Hardening.Quotas.ExceededAction)
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 {
		cfg.Hardening.Quotas.MaxToolCalls = Default().Hardening.Quotas.MaxToolCalls
	}
	if strings.TrimSpace(cfg.Hardening.MetadataScanner.Mode) == "" {
		cfg.Hardening.MetadataScanner.Mode = Default().Hardening.MetadataScanner.Mode
	}
	if cfg.Hardening.Quotas.MaxExecCalls <= 0 {
		cfg.Hardening.Quotas.MaxExecCalls = Default().Hardening.Quotas.MaxExecCalls
	}
	if cfg.Hardening.Quotas.MaxWebCalls <= 0 {
		cfg.Hardening.Quotas.MaxWebCalls = Default().Hardening.Quotas.MaxWebCalls
	}
	if cfg.Hardening.Quotas.MaxSubagentCalls <= 0 {
		cfg.Hardening.Quotas.MaxSubagentCalls = Default().Hardening.Quotas.MaxSubagentCalls
	}
	if cfg.Hardening.Quotas.MaxSessionToolCalls <= 0 {
		cfg.Hardening.Quotas.MaxSessionToolCalls = Default().Hardening.Quotas.MaxSessionToolCalls
	}
	if cfg.Hardening.Quotas.MaxSessionExecCalls <= 0 {
		cfg.Hardening.Quotas.MaxSessionExecCalls = Default().Hardening.Quotas.MaxSessionExecCalls
	}
	if cfg.Hardening.Quotas.MaxSessionWebCalls <= 0 {
		cfg.Hardening.Quotas.MaxSessionWebCalls = Default().Hardening.Quotas.MaxSessionWebCalls
	}
	if cfg.Hardening.Quotas.MaxSessionSubagentCalls <= 0 {
		cfg.Hardening.Quotas.MaxSessionSubagentCalls = Default().Hardening.Quotas.MaxSessionSubagentCalls
	}
	for name, server := range cfg.Tools.MCPServers {
		server.Transport = strings.ToLower(strings.TrimSpace(server.Transport))
		if server.Transport == "" {
			server.Transport = DefaultMCPTransport
		}
		server.Command = strings.TrimSpace(server.Command)
		server.URL = strings.TrimSpace(server.URL)
		if server.Env == nil {
			server.Env = map[string]string{}
		}
		if len(server.ChildEnvAllowlist) == 0 {
			server.ChildEnvAllowlist = append([]string{}, cfg.Hardening.ChildEnvAllowlist...)
		}
		if server.Headers == nil {
			server.Headers = map[string]string{}
		}
		if server.ToolTimeoutSeconds <= 0 {
			server.ToolTimeoutSeconds = DefaultMCPToolTimeoutSeconds
		}
		if server.ConnectTimeoutSeconds <= 0 {
			server.ConnectTimeoutSeconds = DefaultMCPConnectTimeoutSeconds
		}
		cfg.Tools.MCPServers[name] = server
	}
	if strings.TrimSpace(cfg.Skills.ClawHub.SiteURL) == "" {
		cfg.Skills.ClawHub.SiteURL = "https://clawhub.ai"
	}
	if strings.TrimSpace(cfg.Skills.ClawHub.RegistryURL) == "" {
		cfg.Skills.ClawHub.RegistryURL = "https://clawhub.ai"
	}
	if strings.TrimSpace(cfg.Skills.ClawHub.InstallDir) == "" {
		cfg.Skills.ClawHub.InstallDir = "skills"
	}
	if cfg.Triggers.Webhook.Addr == "" {
		cfg.Triggers.Webhook.Addr = "127.0.0.1:8765"
	}
	if cfg.Triggers.Webhook.MaxBodyKB <= 0 {
		cfg.Triggers.Webhook.MaxBodyKB = 64
	}
	if cfg.Triggers.FileWatch.PollSeconds <= 0 {
		cfg.Triggers.FileWatch.PollSeconds = 5
	}
	if cfg.Triggers.FileWatch.DebounceSeconds <= 0 {
		cfg.Triggers.FileWatch.DebounceSeconds = 2
	}
	if cfg.Heartbeat.IntervalMinutes <= 0 {
		cfg.Heartbeat.IntervalMinutes = 30
	}
	if cfg.Heartbeat.IntervalMinutes < 1 {
		cfg.Heartbeat.IntervalMinutes = 1
	}
	if strings.TrimSpace(cfg.Heartbeat.SessionKey) == "" {
		cfg.Heartbeat.SessionKey = DefaultHeartbeatSessionKey
	}
	if cfg.Session.IdentityLinks == nil {
		cfg.Session.IdentityLinks = []SessionIdentityLink{}
	}
	if strings.TrimSpace(cfg.Auth.RPDisplayName) == "" {
		cfg.Auth.RPDisplayName = Default().Auth.RPDisplayName
	}
	if cfg.Auth.AllowedOrigins == nil {
		cfg.Auth.AllowedOrigins = []string{}
	}
	if cfg.Auth.RelatedOrigins == nil {
		cfg.Auth.RelatedOrigins = []string{}
	}
	cfg.Auth.AllowedOrigins = compactStrings(cfg.Auth.AllowedOrigins)
	cfg.Auth.RelatedOrigins = compactStrings(cfg.Auth.RelatedOrigins)
	if cfg.Auth.SessionIdleTTLSeconds <= 0 {
		cfg.Auth.SessionIdleTTLSeconds = Default().Auth.SessionIdleTTLSeconds
	}
	if cfg.Auth.SessionAbsoluteTTLSeconds <= 0 {
		cfg.Auth.SessionAbsoluteTTLSeconds = Default().Auth.SessionAbsoluteTTLSeconds
	}
	if cfg.Auth.StepUpTTLSeconds <= 0 {
		cfg.Auth.StepUpTTLSeconds = Default().Auth.StepUpTTLSeconds
	}
	cfg.Auth.FallbackPolicy = normalizeAuthFallbackPolicy(cfg.Auth.FallbackPolicy)
	if cfg.Auth.FallbackPolicy == "" {
		cfg.Auth.FallbackPolicy = Default().Auth.FallbackPolicy
	}
	if normalizedMode := normalizeAuthEnforcementMode(cfg.Auth.EnforcementMode); normalizedMode != "" {
		cfg.Auth.EnforcementMode = normalizedMode
	} else {
		cfg.Auth.EnforcementMode = Default().Auth.EnforcementMode
	}
	if strings.TrimSpace(cfg.Security.SecretStore.KeyFile) == "" {
		cfg.Security.SecretStore.KeyFile = Default().Security.SecretStore.KeyFile
	}
	if strings.TrimSpace(cfg.Security.Audit.KeyFile) == "" {
		cfg.Security.Audit.KeyFile = Default().Security.Audit.KeyFile
	}
	if strings.TrimSpace(cfg.Security.Approvals.HostID) == "" {
		cfg.Security.Approvals.HostID = Default().Security.Approvals.HostID
	}
	if strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
		cfg.Security.Approvals.KeyFile = Default().Security.Approvals.KeyFile
	}
	if cfg.Security.Approvals.PairingCodeTTLSeconds <= 0 {
		cfg.Security.Approvals.PairingCodeTTLSeconds = Default().Security.Approvals.PairingCodeTTLSeconds
	}
	if cfg.Security.Approvals.PendingTTLSeconds <= 0 {
		cfg.Security.Approvals.PendingTTLSeconds = Default().Security.Approvals.PendingTTLSeconds
	}
	if cfg.Security.Approvals.ApprovalTokenTTLSeconds <= 0 {
		cfg.Security.Approvals.ApprovalTokenTTLSeconds = Default().Security.Approvals.ApprovalTokenTTLSeconds
	}
	cfg.Security.Approvals.Pairing.Mode = normalizeApprovalMode(cfg.Security.Approvals.Pairing.Mode, Default().Security.Approvals.Pairing.Mode)
	cfg.Security.Approvals.Exec.Mode = normalizeApprovalMode(cfg.Security.Approvals.Exec.Mode, Default().Security.Approvals.Exec.Mode)
	cfg.Security.Approvals.SkillExecution.Mode = normalizeApprovalMode(cfg.Security.Approvals.SkillExecution.Mode, Default().Security.Approvals.SkillExecution.Mode)
	cfg.Security.Approvals.SecretAccess.Mode = normalizeApprovalMode(cfg.Security.Approvals.SecretAccess.Mode, Default().Security.Approvals.SecretAccess.Mode)
	cfg.Security.Approvals.MessageSend.Mode = normalizeApprovalMode(cfg.Security.Approvals.MessageSend.Mode, Default().Security.Approvals.MessageSend.Mode)
	if cfg.Security.Profiles.Channels == nil {
		cfg.Security.Profiles.Channels = map[string]string{}
	}
	if cfg.Security.Profiles.Triggers == nil {
		cfg.Security.Profiles.Triggers = map[string]string{}
	}
	if cfg.Security.Profiles.Profiles == nil {
		cfg.Security.Profiles.Profiles = map[string]AccessProfileConfig{}
	}
	if cfg.Security.Network.AllowedHosts == nil {
		cfg.Security.Network.AllowedHosts = []string{}
	}
	cfg.Channels.Telegram.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Telegram.InboundPolicy)
	cfg.Channels.Slack.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Slack.InboundPolicy)
	cfg.Channels.Discord.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Discord.InboundPolicy)
	cfg.Channels.WhatsApp.InboundPolicy = normalizeInboundPolicy(cfg.Channels.WhatsApp.InboundPolicy)
	cfg.Channels.Email.InboundPolicy = normalizeInboundPolicy(cfg.Channels.Email.InboundPolicy)
	for name, profile := range cfg.Security.Profiles.Profiles {
		profile.MaxCapability = strings.ToLower(strings.TrimSpace(profile.MaxCapability))
		profile.AllowedTools = compactStrings(profile.AllowedTools)
		profile.AllowedHosts = compactStrings(profile.AllowedHosts)
		profile.WritablePaths = compactStrings(profile.WritablePaths)
		cfg.Security.Profiles.Profiles[name] = profile
	}
	if err := validateMCPServers(cfg.Tools.MCPServers); err != nil {
		return cfg, err
	}
	if err := validateChannelAccess(cfg); err != nil {
		return cfg, err
	}
	if err := validateAccessProfiles(cfg.Security.Profiles); err != nil {
		return cfg, err
	}
	if err := validateApprovals(cfg.Security.Approvals); err != nil {
		return cfg, err
	}
	if err := validateAuthConfig(cfg.Auth); err != nil {
		return cfg, err
	}
	if err := validateAgentCLIConfig(cfg.AgentCLI); err != nil {
		return cfg, err
	}
	if err := validateProviderRouting(cfg); err != nil {
		return cfg, err
	}
	cfg.RuntimeProfile = RuntimeProfile(strings.ToLower(strings.TrimSpace(string(cfg.RuntimeProfile))))
	if err := validateRuntimeProfile(cfg.RuntimeProfile); err != nil {
		return cfg, err
	}
	return cfg, nil
}
