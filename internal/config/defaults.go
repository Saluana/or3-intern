// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"os"
	"path/filepath"
)

const (
	defaultConfigDirName                  = ".or3-intern"
	defaultSessionKey                     = "cli:default"
	defaultBootstrapMaxChars              = 20000
	defaultBootstrapTotalMaxChars         = 150000
	defaultSessionCacheLimit              = 64
	defaultHistoryMaxMessages             = 40
	defaultMaxMediaBytes                  = 20 * 1024 * 1024
	defaultMaxToolLoops                   = 6
	defaultMemoryRetrieveLimit            = 8
	defaultVectorSearchK                  = 8
	defaultFTSSearchK                     = 8
	defaultVectorScanLimit                = 2000
	defaultWorkerCount                    = 4
	defaultConsolidationWindowSize        = 10
	defaultConsolidationMaxMessages       = 50
	defaultConsolidationMaxInputChars     = 12000
	defaultConsolidationAsyncTimeoutSecs  = 30
	defaultOpenAIProviderKey              = "openai"
	defaultOpenAIAPIBase                  = "https://api.openai.com/v1"
	defaultOpenAIChatModel                = "gpt-4.1-mini"
	defaultOpenAIEmbedModel               = "text-embedding-3-small"
	defaultOpenRouterProviderKey          = "openrouter"
	defaultOpenRouterAPIBase              = "https://openrouter.ai/api/v1"
	defaultOpenRouterChatModel            = "openai/gpt-4o-mini"
	defaultProviderTimeoutSeconds         = 60
	defaultServiceListen                  = "127.0.0.1:9100"
	defaultServiceSharedSecretRole        = "service-client"
	defaultServiceMaxCapability           = "safe"
	defaultServiceMutationRateLimitMinute = 60
	defaultContextMode                    = "quality"
	defaultContextMaxInputTokens          = 16000
	defaultContextOutputReserveTokens     = 1200
	defaultContextSafetyMarginTokens      = 400
	defaultContextManagerTimeoutSeconds   = 15
	defaultContextManagerIdlePruneSeconds = 300
	defaultContextManagerMaxInputTokens   = 1200
	defaultContextManagerMaxOutputTokens  = 600
	defaultEmailPollIntervalSeconds       = 30
	defaultEmailMaxBodyChars              = 4000
	defaultEmailSubjectPrefix             = "Re: "
	defaultEmailIMAPMailbox               = "INBOX"
	defaultEmailIMAPPort                  = 993
	defaultEmailSMTPPort                  = 587
	defaultSkillsMaxRunSeconds            = 30
	defaultSkillsWatchDebounceMS          = 250
	defaultClawHubURL                     = "https://clawhub.ai"
	defaultClawHubInstallDir              = "skills"
	defaultTriggerWebhookAddr             = "127.0.0.1:8765"
	defaultTriggerWebhookMaxBodyKB        = 64
	defaultFileWatchPollSeconds           = 5
	defaultFileWatchDebounceSeconds       = 2
	defaultHeartbeatIntervalMinutes       = 30
	defaultAuthRPDisplayName              = "OR3"
	defaultAuthSessionIdleTTLSeconds      = 1800
	defaultAuthSessionAbsoluteTTLSeconds  = 43200
	defaultAuthStepUpTTLSeconds           = 300
	defaultApprovalHostID                 = "local"
	defaultApprovalPairingCodeTTLSeconds  = 300
	defaultApprovalPendingTTLSeconds      = 900
	defaultApprovalTokenTTLSeconds        = 300
)

var defaultChildEnvAllowlist = []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"}

func Default() Config {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, defaultConfigDirName)
	return Config{
		DBPath:                           filepath.Join(root, "or3-intern.sqlite"),
		ArtifactsDir:                     filepath.Join(root, "artifacts"),
		WorkspaceDir:                     "",
		AllowedDir:                       "",
		DefaultSessionKey:                defaultSessionKey,
		SoulFile:                         filepath.Join(root, "SOUL.md"),
		AgentsFile:                       filepath.Join(root, "AGENTS.md"),
		ToolsFile:                        filepath.Join(root, "TOOLS.md"),
		IdentityFile:                     filepath.Join(root, "IDENTITY.md"),
		MemoryFile:                       filepath.Join(root, "MEMORY.md"),
		BootstrapMaxChars:                defaultBootstrapMaxChars,
		BootstrapTotalMaxChars:           defaultBootstrapTotalMaxChars,
		SessionCache:                     defaultSessionCacheLimit,
		HistoryMax:                       defaultHistoryMaxMessages,
		MaxToolBytes:                     DefaultMaxToolBytes,
		MaxMediaBytes:                    defaultMaxMediaBytes,
		MaxToolLoops:                     defaultMaxToolLoops,
		MaxToolLoopsExceededAction:       QuotaExceededActionAsk,
		MemoryRetrieve:                   defaultMemoryRetrieveLimit,
		VectorK:                          defaultVectorSearchK,
		FTSK:                             defaultFTSSearchK,
		VectorScanLimit:                  defaultVectorScanLimit,
		WorkerCount:                      defaultWorkerCount,
		ConsolidationEnabled:             true,
		ConsolidationWindowSize:          defaultConsolidationWindowSize,
		ConsolidationMaxMessages:         defaultConsolidationMaxMessages,
		ConsolidationMaxInputChars:       defaultConsolidationMaxInputChars,
		ConsolidationAsyncTimeoutSeconds: defaultConsolidationAsyncTimeoutSecs,
		Subagents:                        defaultSubagentsConfig(),
		AgentCLI:                         defaultAgentCLIConfig(),
		DocIndex:                         defaultDocIndexConfig(),
		Skills:                           defaultSkillsConfig(home, root),
		Triggers:                         defaultTriggerConfig(),
		Session:                          defaultSessionConfig(),
		Auth:                             defaultAuthConfig(),
		Security:                         defaultSecurityConfig(root),
		Provider:                         defaultProviderConfig(),
		Providers:                        defaultProviderProfiles(),
		ModelRouting:                     defaultModelRoutingConfig(),
		FavoriteModels:                   defaultFavoriteModelsConfig(),
		Tools:                            defaultToolsConfig(),
		Hardening:                        defaultHardeningConfig(),
		Cron:                             CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.db")},
		Service:                          defaultServiceConfig(),
		Heartbeat: HeartbeatConfig{
			Enabled:         false,
			IntervalMinutes: defaultHeartbeatIntervalMinutes,
			TasksFile:       filepath.Join(root, "HEARTBEAT.md"),
			SessionKey:      DefaultHeartbeatSessionKey,
		},
		Channels:       defaultChannelsConfig(),
		Context:        defaultContextConfig(),
		ContextManager: defaultContextManagerConfig(),
	}
}

func defaultSubagentsConfig() SubagentsConfig {
	return SubagentsConfig{Enabled: false, MaxConcurrent: 1, MaxQueued: 32, TaskTimeoutSeconds: 300}
}

func defaultAgentCLIConfig() AgentCLIConfig {
	return AgentCLIConfig{
		Enabled:                    false,
		DisabledRunners:            []string{},
		RuntimeMode:                map[string]string{"opencode": "auto", "codex": "auto"},
		DefaultModels:              map[string]string{},
		NativeServerURLs:           map[string]string{},
		NativeServerStartupSeconds: 10,
		NativeServerIdleSeconds:    900,
		MaxConcurrent:              1,
		MaxQueued:                  16,
		DefaultTimeoutSeconds:      900,
		MaxTimeoutSeconds:          7200,
		AllowSandboxAuto:           false,
		DefaultMode:                "safe_edit",
		DefaultIsolation:           "host_workspace_write",
		EventChunkMaxBytes:         16384,
		PreviewMaxBytes:            65536,
		MaxPersistedOutputBytes:    10485760,
		ChildEnvAllowlist:          append([]string{}, defaultChildEnvAllowlist...),
	}
}

func defaultDocIndexConfig() DocIndexConfig {
	return DocIndexConfig{Enabled: false, MaxFiles: 100, MaxFileBytes: 64 * 1024, MaxChunks: 500, EmbedMaxBytes: 8 * 1024, RefreshSeconds: 300, RetrieveLimit: 5}
}

func defaultSkillsConfig(home, root string) SkillsConfig {
	return SkillsConfig{
		EnableExec:    false,
		MaxRunSeconds: defaultSkillsMaxRunSeconds,
		ManagedDir:    filepath.Join(root, "skills"),
		Policy: SkillPolicyConfig{
			QuarantineByDefault: true,
			Approved:            []string{},
			TrustedOwners:       []string{},
			BlockedOwners:       []string{},
			TrustedRegistries:   []string{},
		},
		Load: SkillsLoadConfig{
			GlobalDir:       filepath.Join(home, ".agents", "skills"),
			Watch:           false,
			WatchDebounceMS: defaultSkillsWatchDebounceMS,
		},
		Entries: map[string]SkillEntryConfig{},
		ClawHub: ClawHubConfig{
			SiteURL:     defaultClawHubURL,
			RegistryURL: defaultClawHubURL,
			InstallDir:  defaultClawHubInstallDir,
		},
	}
}

func defaultTriggerConfig() TriggerConfig {
	return TriggerConfig{
		Webhook:   WebhookConfig{Enabled: false, Addr: defaultTriggerWebhookAddr, MaxBodyKB: defaultTriggerWebhookMaxBodyKB},
		FileWatch: FileWatchConfig{Enabled: false, PollSeconds: defaultFileWatchPollSeconds, DebounceSeconds: defaultFileWatchDebounceSeconds},
	}
}

func defaultSessionConfig() SessionConfig {
	return SessionConfig{DirectMessagesShareDefault: false, IdentityLinks: []SessionIdentityLink{}}
}

func defaultAuthConfig() AuthConfig {
	return AuthConfig{
		Enabled:                    false,
		RPID:                       "",
		RPDisplayName:              defaultAuthRPDisplayName,
		AllowedOrigins:             []string{},
		RelatedOrigins:             []string{},
		SessionIdleTTLSeconds:      defaultAuthSessionIdleTTLSeconds,
		SessionAbsoluteTTLSeconds:  defaultAuthSessionAbsoluteTTLSeconds,
		StepUpTTLSeconds:           defaultAuthStepUpTTLSeconds,
		FallbackPolicy:             AuthFallbackPairedTokenPlusWarn,
		EnforcementMode:            AuthEnforcementOff,
		AllowPairedTokenFallback:   false,
		RequirePasskeyForSensitive: true,
	}
}

func defaultSecurityConfig(root string) SecurityConfig {
	return SecurityConfig{
		SecretStore: SecretStoreConfig{Enabled: false, Required: false, KeyFile: filepath.Join(root, "master.key")},
		Audit:       AuditConfig{Enabled: false, Strict: false, KeyFile: filepath.Join(root, "audit.key"), VerifyOnStart: false},
		Approvals: ApprovalConfig{
			Enabled:                 false,
			HostID:                  defaultApprovalHostID,
			KeyFile:                 filepath.Join(root, "approvals.key"),
			PairingCodeTTLSeconds:   defaultApprovalPairingCodeTTLSeconds,
			PendingTTLSeconds:       defaultApprovalPendingTTLSeconds,
			ApprovalTokenTTLSeconds: defaultApprovalTokenTTLSeconds,
			Moderator:               defaultApprovalModeratorConfig(),
			Pairing:                 ApprovalDomainConfig{Mode: ApprovalModeAsk},
			Exec:                    ApprovalDomainConfig{Mode: ApprovalModeTrusted},
			SkillExecution:          ApprovalDomainConfig{Mode: ApprovalModeTrusted},
			SecretAccess:            ApprovalDomainConfig{Mode: ApprovalModeAsk},
			MessageSend:             ApprovalDomainConfig{Mode: ApprovalModeAsk},
		},
		Profiles: AccessProfilesConfig{Enabled: false, Default: "", Channels: map[string]string{}, Triggers: map[string]string{}, Profiles: map[string]AccessProfileConfig{}},
		Network:  NetworkPolicyConfig{Enabled: false, DefaultDeny: false, AllowedHosts: []string{}, AllowLoopback: false, AllowPrivate: false},
	}
}

func defaultProviderConfig() ProviderConfig {
	return ProviderConfig{APIBase: defaultOpenAIAPIBase, APIKey: os.Getenv("OPENAI_API_KEY"), Model: defaultOpenAIChatModel, Temperature: 0, EmbedModel: defaultOpenAIEmbedModel, EmbedDimensions: 0, TimeoutSeconds: defaultProviderTimeoutSeconds}
}

func defaultProviderProfiles() ProviderProfiles {
	return ProviderProfiles{
		defaultOpenAIProviderKey:     {Label: "OpenAI", APIBase: defaultOpenAIAPIBase, APIKey: os.Getenv("OPENAI_API_KEY"), TimeoutSeconds: defaultProviderTimeoutSeconds, DefaultChatModel: defaultOpenAIChatModel, DefaultEmbedModel: defaultOpenAIEmbedModel},
		defaultOpenRouterProviderKey: {Label: "OpenRouter", APIBase: defaultOpenRouterAPIBase, APIKey: os.Getenv("OPENROUTER_API_KEY"), TimeoutSeconds: defaultProviderTimeoutSeconds, DefaultChatModel: defaultOpenRouterChatModel, DefaultEmbedModel: ""},
		"custom":                     {Label: "Custom", APIBase: "", TimeoutSeconds: defaultProviderTimeoutSeconds},
	}
}

func defaultModelRoutingConfig() ModelRoutingConfig {
	chat := ModelRoleConfig{Primary: ModelRef{Provider: defaultOpenAIProviderKey, Model: defaultOpenAIChatModel}}
	return ModelRoutingConfig{
		Chat:           chat,
		Agents:         chat,
		Subagents:      chat,
		Summarization:  chat,
		ContextManager: chat,
		Embeddings:     ModelRoleConfig{Primary: ModelRef{Provider: defaultOpenAIProviderKey, Model: defaultOpenAIEmbedModel}},
	}
}

func defaultFavoriteModelsConfig() FavoriteModelsConfig {
	return FavoriteModelsConfig{
		defaultOpenAIProviderKey: {
			{Model: defaultOpenAIChatModel, Label: "GPT-4.1 mini"},
			{Model: defaultOpenAIEmbedModel, Label: "Small embeddings"},
		},
		defaultOpenRouterProviderKey: {
			{Model: defaultOpenRouterChatModel, Label: "GPT-4o mini"},
		},
	}
}

func defaultToolsConfig() ToolsConfig {
	return ToolsConfig{BraveAPIKey: os.Getenv("BRAVE_API_KEY"), WebProxy: "", EnableExec: false, ExecTimeoutSeconds: 60, RestrictToWorkspace: true, AllowFullFileRead: false, PathAppend: "", MCPServers: map[string]MCPServerConfig{}}
}

func defaultHardeningConfig() HardeningConfig {
	return HardeningConfig{
		GuardedTools:        false,
		PrivilegedTools:     false,
		EnableExecShell:     false,
		ExecAllowedPrograms: []string{"cat", "echo", "find", "git", "grep", "head", "ls", "pwd", "sed", "tail"},
		ChildEnvAllowlist:   append([]string{}, defaultChildEnvAllowlist...),
		IsolateChannelPeers: true,
		MetadataScanner:     MetadataScannerConfig{Mode: "warn", Allowlist: []string{}},
		Sandbox:             SandboxConfig{Enabled: false, BubblewrapPath: "bwrap", AllowNetwork: false, WritablePaths: []string{}},
		Quotas: HardeningQuotaConfig{
			Enabled:                 true,
			ExceededAction:          QuotaExceededActionAsk,
			MaxToolCalls:            16,
			MaxExecCalls:            2,
			MaxWebCalls:             4,
			MaxSubagentCalls:        2,
			MaxSessionToolCalls:     256,
			MaxSessionExecCalls:     32,
			MaxSessionWebCalls:      64,
			MaxSessionSubagentCalls: 16,
		},
	}
}

func defaultServiceConfig() ServiceConfig {
	return ServiceConfig{Enabled: false, Listen: defaultServiceListen, Secret: "", SharedSecretRole: defaultServiceSharedSecretRole, MaxCapability: defaultServiceMaxCapability, AllowUnauthenticatedPairing: false, AllowRemoteUnauthenticatedPairing: false, TrustedBrowserOrigins: []string{}, TrustedBrowserCIDRs: []string{}, TrustedPairingOrigins: []string{}, TrustedPairingCIDRs: []string{}, MutationRateLimitPerMinute: defaultServiceMutationRateLimitMinute}
}

func defaultChannelsConfig() ChannelsConfig {
	return ChannelsConfig{
		Telegram: TelegramChannelConfig{Enabled: false, APIBase: "https://api.telegram.org", PollSeconds: 2},
		Slack:    SlackChannelConfig{Enabled: false, APIBase: "https://slack.com/api", RequireMention: true},
		Discord:  DiscordChannelConfig{Enabled: false, APIBase: "https://discord.com/api/v10", RequireMention: true},
		WhatsApp: WhatsAppBridgeConfig{Enabled: false, BridgeURL: "ws://127.0.0.1:3001/ws"},
		Email: EmailChannelConfig{
			Enabled:             false,
			ConsentGranted:      false,
			AutoReplyEnabled:    false,
			PollIntervalSeconds: defaultEmailPollIntervalSeconds,
			MarkSeen:            true,
			MaxBodyChars:        defaultEmailMaxBodyChars,
			SubjectPrefix:       defaultEmailSubjectPrefix,
			IMAPMailbox:         defaultEmailIMAPMailbox,
			IMAPPort:            defaultEmailIMAPPort,
			IMAPUseSSL:          true,
			SMTPPort:            defaultEmailSMTPPort,
			SMTPUseTLS:          true,
			SMTPUseSSL:          false,
		},
	}
}

func defaultContextConfig() ContextConfig {
	return ContextConfig{
		Mode:                defaultContextMode,
		MaxInputTokens:      defaultContextMaxInputTokens,
		OutputReserveTokens: defaultContextOutputReserveTokens,
		SafetyMarginTokens:  defaultContextSafetyMarginTokens,
		Sections: ContextSectionBudgets{
			SystemCore:       800,
			SoulIdentity:     2800,
			ToolPolicy:       900,
			ActiveTaskCard:   700,
			PinnedMemory:     1200,
			MemoryDigest:     900,
			RecentHistory:    2200,
			RetrievedMemory:  1500,
			WorkspaceContext: 1200,
			ToolSchemas:      1400,
		},
		Retrieval: ContextRetrievalConfig{CandidateMultiplier: 3, MinScore: 0.03},
		Pressure:  ContextPressureConfig{WarningPercent: 70, HighPercent: 85, EmergencyPercent: 95},
		Tools:     ContextToolConfig{DynamicExpose: true},
		Artifacts: ContextArtifactConfig{SummaryMaxChars: 500},
		TaskCard:  ContextTaskCardConfig{Enabled: true, EnforcePlan: false, MaxRefs: 12, MaxPlanItems: 8},
	}
}

func defaultContextManagerConfig() ContextManagerConfig {
	return ContextManagerConfig{Enabled: false, Provider: "", Model: "", TimeoutSeconds: defaultContextManagerTimeoutSeconds, IdlePruneSeconds: defaultContextManagerIdlePruneSeconds, MaxInputTokens: defaultContextManagerMaxInputTokens, MaxOutputTokens: defaultContextManagerMaxOutputTokens, AllowTaskUpdates: true, AllowStalePropose: true}
}

// DefaultPath returns the default on-disk config file path.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultConfigDirName, "config.json")
}
