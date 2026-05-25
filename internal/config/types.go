// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"strings"
)

// RuntimeProfile names the intended execution posture for this instance.
type RuntimeProfile string

const (
	// DefaultMaxToolBytes is the default direct tool-result budget before artifact spillover.
	DefaultMaxToolBytes = 96 * 1024

	// ProfileLocalDev is the default profile for local development workflows.
	ProfileLocalDev RuntimeProfile = "local-dev"
	// ProfileSingleUserHardened tightens local defaults for a single trusted user.
	ProfileSingleUserHardened RuntimeProfile = "single-user-hardened"
	// ProfileHostedService enables the safeguards required for hosted operation.
	ProfileHostedService RuntimeProfile = "hosted-service"
	// ProfileHostedNoExec disables direct execution in hosted deployments.
	ProfileHostedNoExec RuntimeProfile = "hosted-no-exec"
	// ProfileHostedRemoteSandbox requires sandboxed execution for hosted use.
	ProfileHostedRemoteSandbox RuntimeProfile = "hosted-remote-sandbox-only"
)

// RuntimeProfileSpec describes the intended operating posture for a runtime profile.
type RuntimeProfileSpec struct {
	Name                    RuntimeProfile
	Hosted                  bool
	RequireSecretStore      bool
	RequireAudit            bool
	RequireNetworkPolicy    bool
	RequireStrictAudit      bool
	RequireAuditVerifyStart bool
	RequireSecretStoreKey   bool
	ForbidExecShell         bool
	ForbidPrivilegedTools   bool
	RequireSandboxForExec   bool
}

// ProfileSpec returns the effective runtime-profile rules for p.
func ProfileSpec(p RuntimeProfile) RuntimeProfileSpec {
	switch p {
	case ProfileSingleUserHardened:
		return RuntimeProfileSpec{
			Name: p,
		}
	case ProfileHostedService:
		return RuntimeProfileSpec{
			Name:                    p,
			Hosted:                  true,
			RequireSecretStore:      true,
			RequireAudit:            true,
			RequireNetworkPolicy:    true,
			RequireStrictAudit:      true,
			RequireAuditVerifyStart: true,
			RequireSecretStoreKey:   true,
		}
	case ProfileHostedNoExec:
		return RuntimeProfileSpec{
			Name:                    p,
			Hosted:                  true,
			RequireSecretStore:      true,
			RequireAudit:            true,
			RequireNetworkPolicy:    true,
			RequireStrictAudit:      true,
			RequireAuditVerifyStart: true,
			RequireSecretStoreKey:   true,
			ForbidExecShell:         true,
			ForbidPrivilegedTools:   true,
		}
	case ProfileHostedRemoteSandbox:
		return RuntimeProfileSpec{
			Name:                    p,
			Hosted:                  true,
			RequireSecretStore:      true,
			RequireAudit:            true,
			RequireNetworkPolicy:    true,
			RequireStrictAudit:      true,
			RequireAuditVerifyStart: true,
			RequireSecretStoreKey:   true,
			RequireSandboxForExec:   true,
		}
	default:
		return RuntimeProfileSpec{Name: p}
	}
}

// Config is the top-level persisted runtime configuration.
type Config struct {
	DBPath                     string              `json:"dbPath"`
	ArtifactsDir               string              `json:"artifactsDir"`
	WorkspaceDir               string              `json:"workspaceDir"`
	AllowedDir                 string              `json:"allowedDir"`
	DefaultSessionKey          string              `json:"defaultSessionKey"`
	SoulFile                   string              `json:"soulFile"`
	AgentsFile                 string              `json:"agentsFile"`
	ToolsFile                  string              `json:"toolsFile"`
	BootstrapMaxChars          int                 `json:"bootstrapMaxChars"`
	BootstrapTotalMaxChars     int                 `json:"bootstrapTotalMaxChars"`
	SessionCache               int                 `json:"sessionCacheLimit"`
	HistoryMax                 int                 `json:"historyMaxMessages"`
	MaxToolBytes               int                 `json:"maxToolBytes"`
	MaxMediaBytes              int                 `json:"maxMediaBytes"`
	MaxToolLoops               int                 `json:"maxToolLoops"`
	MaxToolLoopsExceededAction QuotaExceededAction `json:"maxToolLoopsExceededAction"`
	MemoryRetrieve             int                 `json:"memoryRetrieveLimit"`
	VectorK                    int                 `json:"vectorSearchK"`
	FTSK                       int                 `json:"ftsSearchK"`
	VectorScanLimit            int                 `json:"vectorScanLimit"`
	WorkerCount                int                 `json:"workerCount"`

	ConsolidationEnabled             bool            `json:"consolidationEnabled"`
	ConsolidationModel               string          `json:"consolidationModel"`
	ConsolidationWindowSize          int             `json:"consolidationWindowSize"`
	ConsolidationMaxMessages         int             `json:"consolidationMaxMessages"`
	ConsolidationMaxInputChars       int             `json:"consolidationMaxInputChars"`
	ConsolidationAsyncTimeoutSeconds int             `json:"consolidationAsyncTimeoutSeconds"`
	Subagents                        SubagentsConfig `json:"subagents"`
	AgentCLI                         AgentCLIConfig  `json:"agentCLI"`
	RuntimeProfile                   RuntimeProfile  `json:"runtimeProfile"`

	IdentityFile string         `json:"identityFile"`
	MemoryFile   string         `json:"memoryFile"`
	DocIndex     DocIndexConfig `json:"docIndex"`
	Skills       SkillsConfig   `json:"skills"`
	Triggers     TriggerConfig  `json:"triggers"`
	Session      SessionConfig  `json:"session"`
	Auth         AuthConfig     `json:"auth"`
	Security     SecurityConfig `json:"security"`

	Provider            ProviderConfig          `json:"provider"`
	Providers           ProviderProfiles        `json:"providers,omitempty"`
	ModelRouting        ModelRoutingConfig      `json:"modelRouting,omitempty"`
	FavoriteModels      FavoriteModelsConfig    `json:"favoriteModels,omitempty"`
	Tools               ToolsConfig             `json:"tools"`
	Hardening           HardeningConfig         `json:"hardening"`
	Cron                CronConfig              `json:"cron"`
	Service             ServiceConfig           `json:"service"`
	Heartbeat           HeartbeatConfig         `json:"heartbeat"`
	Channels            ChannelsConfig          `json:"channels"`
	Context             ContextConfig           `json:"context"`
	ContextManager      ContextManagerConfig    `json:"contextManager"`
	ContextConfigured   bool                    `json:"-"`
	IntegrationWarnings []IntegrationQuarantine `json:"-"`

	// Milestones tracks completed onboarding milestones.
	// Keys are milestone names (e.g., "setup_complete", "pairing_complete", "first_chat_complete").
	// Values are RFC3339 timestamps when the milestone was completed.
	Milestones map[string]string `json:"milestones,omitempty"`
}

type ContextConfig struct {
	Mode                string                 `json:"mode"`
	MaxInputTokens      int                    `json:"maxInputTokens"`
	OutputReserveTokens int                    `json:"outputReserveTokens"`
	SafetyMarginTokens  int                    `json:"safetyMarginTokens"`
	Sections            ContextSectionBudgets  `json:"sections"`
	Retrieval           ContextRetrievalConfig `json:"retrieval"`
	Pressure            ContextPressureConfig  `json:"pressure"`
	Tools               ContextToolConfig      `json:"tools"`
	Artifacts           ContextArtifactConfig  `json:"artifacts"`
	TaskCard            ContextTaskCardConfig  `json:"taskCard"`
}

type ContextSectionBudgets struct {
	SystemCore       int `json:"systemCore"`
	SoulIdentity     int `json:"soulIdentity"`
	ToolPolicy       int `json:"toolPolicy"`
	ActiveTaskCard   int `json:"activeTaskCard"`
	PinnedMemory     int `json:"pinnedMemory"`
	MemoryDigest     int `json:"memoryDigest"`
	RecentHistory    int `json:"recentHistory"`
	RetrievedMemory  int `json:"retrievedMemory"`
	WorkspaceContext int `json:"workspaceContext"`
	ToolSchemas      int `json:"toolSchemas"`
}

type ContextRetrievalConfig struct {
	CandidateMultiplier int     `json:"candidateMultiplier"`
	MinScore            float64 `json:"minScore"`
}

type ContextPressureConfig struct {
	WarningPercent   int `json:"warningPercent"`
	HighPercent      int `json:"highPercent"`
	EmergencyPercent int `json:"emergencyPercent"`
}

type ContextToolConfig struct {
	DynamicExpose bool `json:"dynamicExpose"`
}

type ContextArtifactConfig struct {
	SummaryMaxChars int `json:"summaryMaxChars"`
}

type ContextTaskCardConfig struct {
	Enabled      bool `json:"enabled"`
	EnforcePlan  bool `json:"enforcePlan"`
	MaxRefs      int  `json:"maxRefs"`
	MaxPlanItems int  `json:"maxPlanItems"`
}

type ContextManagerConfig struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	TimeoutSeconds    int    `json:"timeoutSeconds"`
	IdlePruneSeconds  int    `json:"idlePruneSeconds"`
	MaxInputTokens    int    `json:"maxInputTokens"`
	MaxOutputTokens   int    `json:"maxOutputTokens"`
	AllowTaskUpdates  bool   `json:"allowTaskUpdates"`
	AllowStalePropose bool   `json:"allowStalePropose"`
}

// HardeningConfig controls sandboxing, privilege gates, and per-tool quotas.
type HardeningConfig struct {
	GuardedTools        bool                  `json:"guardedTools"`
	PrivilegedTools     bool                  `json:"privilegedTools"`
	EnableExecShell     bool                  `json:"enableExecShell"`
	ExecAllowedPrograms []string              `json:"execAllowedPrograms"`
	ChildEnvAllowlist   []string              `json:"childEnvAllowlist"`
	IsolateChannelPeers bool                  `json:"isolateChannelPeers"`
	MetadataScanner     MetadataScannerConfig `json:"metadataScanner"`
	Sandbox             SandboxConfig         `json:"sandbox"`
	Quotas              HardeningQuotaConfig  `json:"quotas"`
}

type MetadataScannerConfig struct {
	Mode      string   `json:"mode"`
	Allowlist []string `json:"allowlist"`
}

// SandboxConfig defines how exec-capable tools are isolated.
type SandboxConfig struct {
	Enabled        bool     `json:"enabled"`
	BubblewrapPath string   `json:"bubblewrapPath"`
	AllowNetwork   bool     `json:"allowNetwork"`
	WritablePaths  []string `json:"writablePaths"`
}

type QuotaExceededAction string

const (
	QuotaExceededActionAsk  QuotaExceededAction = "ask"
	QuotaExceededActionFail QuotaExceededAction = "fail"
)

// HardeningQuotaConfig limits how many sensitive tool calls a message and session may issue.
type HardeningQuotaConfig struct {
	Enabled                 bool                `json:"enabled"`
	ExceededAction          QuotaExceededAction `json:"exceededAction"`
	MaxToolCalls            int                 `json:"maxToolCalls"`
	MaxExecCalls            int                 `json:"maxExecCalls"`
	MaxWebCalls             int                 `json:"maxWebCalls"`
	MaxSubagentCalls        int                 `json:"maxSubagentCalls"`
	MaxSessionToolCalls     int                 `json:"maxSessionToolCalls"`
	MaxSessionExecCalls     int                 `json:"maxSessionExecCalls"`
	MaxSessionWebCalls      int                 `json:"maxSessionWebCalls"`
	MaxSessionSubagentCalls int                 `json:"maxSessionSubagentCalls"`
}

// ProviderConfig selects the LLM and embedding provider endpoints and limits.
type ProviderConfig struct {
	APIBase         string  `json:"apiBase"`
	APIKey          string  `json:"apiKey" secret:"true"`
	Model           string  `json:"model"`
	Temperature     float64 `json:"temperature"`
	EmbedModel      string  `json:"embedModel"`
	EmbedDimensions int     `json:"embedDimensions"`
	EnableVision    bool    `json:"enableVision"`
	TimeoutSeconds  int     `json:"timeoutSeconds"`
}

type ProviderProfiles map[string]ProviderProfileConfig

type ProviderProfileConfig struct {
	Label             string `json:"label,omitempty"`
	APIBase           string `json:"apiBase"`
	APIKey            string `json:"apiKey,omitempty" secret:"true"`
	TimeoutSeconds    int    `json:"timeoutSeconds,omitempty"`
	EnableVision      bool   `json:"enableVision,omitempty"`
	DefaultChatModel  string `json:"defaultChatModel,omitempty"`
	DefaultEmbedModel string `json:"defaultEmbedModel,omitempty"`
	DefaultDimensions int    `json:"defaultDimensions,omitempty"`
}

type ModelRoutingConfig struct {
	Chat           ModelRoleConfig `json:"chat,omitempty"`
	Agents         ModelRoleConfig `json:"agents,omitempty"`
	Subagents      ModelRoleConfig `json:"subagents,omitempty"`
	Summarization  ModelRoleConfig `json:"summarization,omitempty"`
	ContextManager ModelRoleConfig `json:"contextManager,omitempty"`
	Embeddings     ModelRoleConfig `json:"embeddings,omitempty"`
	Fallback       ModelRoleConfig `json:"fallback,omitempty"`
}

type ModelRoleConfig struct {
	Primary         ModelRef   `json:"primary,omitempty"`
	Fallbacks       []ModelRef `json:"fallbacks,omitempty"`
	Temperature     *float64   `json:"temperature,omitempty"`
	EmbedDimensions int        `json:"embedDimensions,omitempty"`
}

type ModelRef struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

type FavoriteModelsConfig map[string][]FavoriteModelConfig

type FavoriteModelConfig struct {
	Model string `json:"model"`
	Label string `json:"label,omitempty"`
}

const (
	ModelRoleChat           = "chat"
	ModelRoleAgents         = "agents"
	ModelRoleSubagents      = "subagents"
	ModelRoleSummarization  = "summarization"
	ModelRoleContextManager = "contextManager"
	ModelRoleEmbeddings     = "embeddings"
	ModelRoleFallback       = "fallback"
)

func (cfg Config) ModelRole(role string) ModelRoleConfig {
	switch strings.TrimSpace(role) {
	case ModelRoleAgents:
		return cfg.ModelRouting.Agents
	case ModelRoleSubagents:
		return cfg.ModelRouting.Subagents
	case ModelRoleSummarization:
		return cfg.ModelRouting.Summarization
	case ModelRoleContextManager:
		return cfg.ModelRouting.ContextManager
	case ModelRoleEmbeddings:
		return cfg.ModelRouting.Embeddings
	case ModelRoleFallback:
		return cfg.ModelRouting.Fallback
	default:
		return cfg.ModelRouting.Chat
	}
}

func (cfg Config) ProviderProfile(provider string) (ProviderProfileConfig, bool) {
	profile, ok := cfg.Providers[normalizeProviderKey(provider)]
	return profile, ok
}

// ToolsConfig configures built-in tools and external MCP server integrations.
type ToolsConfig struct {
	BraveAPIKey         string                     `json:"braveApiKey" secret:"true"`
	WebProxy            string                     `json:"webProxy"`
	EnableExec          bool                       `json:"enableExec"`
	ExecTimeoutSeconds  int                        `json:"execTimeoutSeconds"`
	RestrictToWorkspace bool                       `json:"restrictToWorkspace"`
	AllowFullFileRead   bool                       `json:"allowFullFileRead"`
	PathAppend          string                     `json:"pathAppend"`
	MCPServers          map[string]MCPServerConfig `json:"mcpServers"`
}

// CronConfig enables persistence for scheduled background jobs.
type CronConfig struct {
	Enabled   bool   `json:"enabled"`
	StorePath string `json:"storePath"`
}

// DefaultHeartbeatSessionKey is the fallback session key used by heartbeat turns.
const DefaultHeartbeatSessionKey = "heartbeat:default"

const (
	// DefaultMCPTransport is the default transport for MCP servers.
	DefaultMCPTransport = "stdio"
	// DefaultMCPConnectTimeoutSeconds is the default MCP dial timeout.
	DefaultMCPConnectTimeoutSeconds = 10
	// DefaultMCPToolTimeoutSeconds is the default timeout for a single MCP tool call.
	DefaultMCPToolTimeoutSeconds = 30
)

// MCPServerConfig describes one configured MCP server entry.
type MCPServerConfig struct {
	Enabled               bool              `json:"enabled"`
	Transport             string            `json:"transport"`
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	Env                   map[string]string `json:"env" secret:"true"`
	ChildEnvAllowlist     []string          `json:"childEnvAllowlist"`
	URL                   string            `json:"url"`
	Headers               map[string]string `json:"headers" secret:"true"`
	ToolTimeoutSeconds    int               `json:"toolTimeoutSeconds"`
	ConnectTimeoutSeconds int               `json:"connectTimeoutSeconds"`
	AllowInsecureHTTP     bool              `json:"allowInsecureHttp"`
}

// HeartbeatConfig controls recurring heartbeat turns sourced from a tasks file.
type HeartbeatConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	TasksFile       string `json:"tasksFile"`
	SessionKey      string `json:"sessionKey"`
}

// ServiceConfig configures the optional authenticated service listener.
type ServiceConfig struct {
	Enabled                           bool     `json:"enabled"`
	Listen                            string   `json:"listen"`
	UnixSocket                        string   `json:"unixSocket,omitempty"`
	Secret                            string   `json:"secret" secret:"true"`
	SharedSecretRole                  string   `json:"sharedSecretRole"`
	MaxCapability                     string   `json:"maxCapability"`
	AllowUnauthenticatedPairing       bool     `json:"allowUnauthenticatedPairing"`
	AllowRemoteUnauthenticatedPairing bool     `json:"allowRemoteUnauthenticatedPairing"`
	TrustedBrowserOrigins             []string `json:"trustedBrowserOrigins"`
	TrustedBrowserCIDRs               []string `json:"trustedBrowserCIDRs"`
	TrustedPairingOrigins             []string `json:"trustedPairingOrigins"`
	TrustedPairingCIDRs               []string `json:"trustedPairingCIDRs"`
	MutationRateLimitPerMinute        int      `json:"mutationRateLimitPerMinute"`
}

// SubagentsConfig limits the internal subagent queue and worker pool.
type SubagentsConfig struct {
	Enabled            bool `json:"enabled"`
	MaxConcurrent      int  `json:"maxConcurrent"`
	MaxQueued          int  `json:"maxQueued"`
	TaskTimeoutSeconds int  `json:"taskTimeoutSeconds"`
}

// AgentCLIConfig controls the external agent CLI delegation subsystem.
type AgentCLIConfig struct {
	Enabled                    bool              `json:"enabled"`
	DisabledRunners            []string          `json:"disabledRunners"`
	RuntimeMode                map[string]string `json:"runtimeMode,omitempty"`
	DefaultModels              map[string]string `json:"defaultModels,omitempty"`
	NativeServerURLs           map[string]string `json:"nativeServerUrls,omitempty"`
	NativeServerStartupSeconds int               `json:"nativeServerStartupSeconds"`
	NativeServerIdleSeconds    int               `json:"nativeServerIdleSeconds"`
	MaxConcurrent              int               `json:"maxConcurrent"`
	MaxQueued                  int               `json:"maxQueued"`
	DefaultTimeoutSeconds      int               `json:"defaultTimeoutSeconds"`
	MaxTimeoutSeconds          int               `json:"maxTimeoutSeconds"`
	AllowSandboxAuto           bool              `json:"allowSandboxAuto"`
	DefaultMode                string            `json:"defaultMode"`
	DefaultIsolation           string            `json:"defaultIsolation"`
	EventChunkMaxBytes         int               `json:"eventChunkMaxBytes"`
	PreviewMaxBytes            int               `json:"previewMaxBytes"`
	MaxPersistedOutputBytes    int64             `json:"maxPersistedOutputBytes"`
	ChildEnvAllowlist          []string          `json:"childEnvAllowlist"`
}

type InboundPolicy string

const (
	InboundPolicyDeny      InboundPolicy = "deny"
	InboundPolicyAllowlist InboundPolicy = "allowlist"
	InboundPolicyPairing   InboundPolicy = "pairing"
)

// TelegramChannelConfig configures Telegram ingress and delivery.
type TelegramChannelConfig struct {
	Enabled        bool          `json:"enabled"`
	InboundPolicy  InboundPolicy `json:"inboundPolicy"`
	OpenAccess     bool          `json:"openAccess"`
	Token          string        `json:"token" secret:"true"`
	APIBase        string        `json:"apiBase"`
	PollSeconds    int           `json:"pollSeconds"`
	DefaultChatID  string        `json:"defaultChatId"`
	AllowedChatIDs []string      `json:"allowedChatIds"`
}

// SlackChannelConfig configures Slack socket-mode ingress and delivery.
type SlackChannelConfig struct {
	Enabled          bool          `json:"enabled"`
	InboundPolicy    InboundPolicy `json:"inboundPolicy"`
	OpenAccess       bool          `json:"openAccess"`
	AppToken         string        `json:"appToken" secret:"true"`
	BotToken         string        `json:"botToken" secret:"true"`
	APIBase          string        `json:"apiBase"`
	SocketModeURL    string        `json:"socketModeUrl"`
	DefaultChannelID string        `json:"defaultChannelId"`
	AllowedUserIDs   []string      `json:"allowedUserIds"`
	RequireMention   bool          `json:"requireMention"`
}

// DiscordChannelConfig configures Discord ingress and delivery.
type DiscordChannelConfig struct {
	Enabled          bool          `json:"enabled"`
	InboundPolicy    InboundPolicy `json:"inboundPolicy"`
	OpenAccess       bool          `json:"openAccess"`
	Token            string        `json:"token" secret:"true"`
	APIBase          string        `json:"apiBase"`
	GatewayURL       string        `json:"gatewayUrl"`
	DefaultChannelID string        `json:"defaultChannelId"`
	AllowedUserIDs   []string      `json:"allowedUserIds"`
	RequireMention   bool          `json:"requireMention"`
}

// WhatsAppBridgeConfig configures the external WhatsApp bridge connection.
type WhatsAppBridgeConfig struct {
	Enabled       bool          `json:"enabled"`
	InboundPolicy InboundPolicy `json:"inboundPolicy"`
	OpenAccess    bool          `json:"openAccess"`
	BridgeURL     string        `json:"bridgeUrl"`
	BridgeToken   string        `json:"bridgeToken" secret:"true"`
	DefaultTo     string        `json:"defaultTo"`
	AllowedFrom   []string      `json:"allowedFrom"`
}

// EmailChannelConfig configures email polling, access control, and replies.
type EmailChannelConfig struct {
	Enabled             bool          `json:"enabled"`
	InboundPolicy       InboundPolicy `json:"inboundPolicy"`
	OpenAccess          bool          `json:"openAccess"`
	ConsentGranted      bool          `json:"consentGranted"`
	AllowedSenders      []string      `json:"allowedSenders"`
	DefaultTo           string        `json:"defaultTo"`
	AutoReplyEnabled    bool          `json:"autoReplyEnabled"`
	PollIntervalSeconds int           `json:"pollIntervalSeconds"`
	MarkSeen            bool          `json:"markSeen"`
	MaxBodyChars        int           `json:"maxBodyChars"`
	SubjectPrefix       string        `json:"subjectPrefix"`
	FromAddress         string        `json:"fromAddress"`
	IMAPMailbox         string        `json:"imapMailbox"`
	IMAPHost            string        `json:"imapHost"`
	IMAPPort            int           `json:"imapPort"`
	IMAPUseSSL          bool          `json:"imapUseSSL"`
	IMAPUsername        string        `json:"imapUsername"`
	IMAPPassword        string        `json:"imapPassword" secret:"true"`
	SMTPHost            string        `json:"smtpHost"`
	SMTPPort            int           `json:"smtpPort"`
	SMTPUseTLS          bool          `json:"smtpUseTLS"`
	SMTPUseSSL          bool          `json:"smtpUseSSL"`
	SMTPUsername        string        `json:"smtpUsername"`
	SMTPPassword        string        `json:"smtpPassword" secret:"true"`
}

// ChannelsConfig groups per-channel transport settings.
type ChannelsConfig struct {
	Telegram TelegramChannelConfig `json:"telegram"`
	Slack    SlackChannelConfig    `json:"slack"`
	Discord  DiscordChannelConfig  `json:"discord"`
	WhatsApp WhatsAppBridgeConfig  `json:"whatsApp"`
	Email    EmailChannelConfig    `json:"email"`
}

// DocIndexConfig controls workspace document indexing for retrieval.
type DocIndexConfig struct {
	Enabled        bool     `json:"enabled"`
	Roots          []string `json:"roots"`
	MaxFiles       int      `json:"maxFiles"`
	MaxFileBytes   int      `json:"maxFileBytes"`
	MaxChunks      int      `json:"maxChunks"`
	EmbedMaxBytes  int      `json:"embedMaxBytes"`
	RefreshSeconds int      `json:"refreshSeconds"`
	RetrieveLimit  int      `json:"retrieveLimit"`
}

// SkillsConfig controls managed skill loading, policy, and runtime behavior.
type SkillsConfig struct {
	EnableExec    bool                        `json:"enableExec"`
	MaxRunSeconds int                         `json:"maxRunSeconds"`
	ManagedDir    string                      `json:"managedDir"`
	Policy        SkillPolicyConfig           `json:"policy"`
	Load          SkillsLoadConfig            `json:"load"`
	Entries       map[string]SkillEntryConfig `json:"entries"`
	ClawHub       ClawHubConfig               `json:"clawHub"`
}

// SkillPolicyConfig defines which external skills are trusted or blocked.
type SkillPolicyConfig struct {
	QuarantineByDefault bool     `json:"quarantineByDefault"`
	Approved            []string `json:"approved"`
	TrustedOwners       []string `json:"trustedOwners"`
	BlockedOwners       []string `json:"blockedOwners"`
	TrustedRegistries   []string `json:"trustedRegistries"`
}

// SkillsLoadConfig controls additional skill directories and file watching.
type SkillsLoadConfig struct {
	ExtraDirs        []string `json:"extraDirs"`
	GlobalDir        string   `json:"globalDir"`
	DisableGlobalDir bool     `json:"disableGlobalDir"`
	Watch            bool     `json:"watch"`
	WatchDebounceMS  int      `json:"watchDebounceMs"`
}

// SkillEntryConfig overrides configuration for a single named skill.
type SkillEntryConfig struct {
	Enabled *bool             `json:"enabled,omitempty"`
	APIKey  string            `json:"apiKey"`
	Env     map[string]string `json:"env"`
	Config  map[string]any    `json:"config"`
}

// ClawHubConfig configures the default remote skill registry.
type ClawHubConfig struct {
	SiteURL     string `json:"siteUrl"`
	RegistryURL string `json:"registryUrl"`
	InstallDir  string `json:"installDir"`
}

// WebhookConfig configures the webhook trigger listener.
type WebhookConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Secret    string `json:"secret" secret:"true"`
	MaxBodyKB int    `json:"maxBodyKB"`
}

// FileWatchConfig configures filesystem-based trigger polling.
type FileWatchConfig struct {
	Enabled         bool     `json:"enabled"`
	Paths           []string `json:"paths"`
	PollSeconds     int      `json:"pollSeconds"`
	DebounceSeconds int      `json:"debounceSeconds"`
}

// TriggerConfig groups external trigger sources.
type TriggerConfig struct {
	Webhook   WebhookConfig   `json:"webhook"`
	FileWatch FileWatchConfig `json:"fileWatch"`
}

// SessionConfig controls session-sharing defaults and identity links.
type SessionConfig struct {
	DirectMessagesShareDefault bool                  `json:"directMessagesShareDefault"`
	IdentityLinks              []SessionIdentityLink `json:"identityLinks"`
}

// SessionIdentityLink maps a canonical identity to equivalent peer identities.
type SessionIdentityLink struct {
	Canonical string   `json:"canonical"`
	Peers     []string `json:"peers"`
}

type AuthEnforcementMode string

const (
	AuthEnforcementOff              AuthEnforcementMode = "off"
	AuthEnforcementWarn             AuthEnforcementMode = "warn"
	AuthEnforcementSensitive        AuthEnforcementMode = "enforce-sensitive"
	AuthEnforcementSession          AuthEnforcementMode = "enforce-session"
	AuthFallbackPairedTokenOnly     string              = "paired-token-only"
	AuthFallbackPairedTokenPlusWarn string              = "paired-token-plus-warning"
	AuthFallbackAdminRecoveryOnly   string              = "admin-recovery-only"
)

// AuthConfig configures passkey, session, and recent-auth behavior for the service API.
type AuthConfig struct {
	Enabled                    bool                `json:"enabled"`
	RPID                       string              `json:"rpId"`
	RPDisplayName              string              `json:"rpDisplayName"`
	AllowedOrigins             []string            `json:"allowedOrigins"`
	RelatedOrigins             []string            `json:"relatedOrigins"`
	SessionIdleTTLSeconds      int                 `json:"sessionIdleTtlSeconds"`
	SessionAbsoluteTTLSeconds  int                 `json:"sessionAbsoluteTtlSeconds"`
	StepUpTTLSeconds           int                 `json:"stepUpTtlSeconds"`
	FallbackPolicy             string              `json:"fallbackPolicy"`
	EnforcementMode            AuthEnforcementMode `json:"enforcementMode"`
	AllowPairedTokenFallback   bool                `json:"allowPairedTokenFallback"`
	RequirePasskeyForSensitive bool                `json:"requirePasskeyForSensitive"`
}

type ApprovalMode string

const (
	ApprovalModeDeny      ApprovalMode = "deny"
	ApprovalModeAsk       ApprovalMode = "ask"
	ApprovalModeAllowlist ApprovalMode = "allowlist"
	ApprovalModeTrusted   ApprovalMode = "trusted"
)

type ApprovalDomainConfig struct {
	Mode ApprovalMode `json:"mode"`
}

type ApprovalConfig struct {
	Enabled                 bool                 `json:"enabled"`
	HostID                  string               `json:"hostId"`
	KeyFile                 string               `json:"keyFile"`
	PairingCodeTTLSeconds   int                  `json:"pairingCodeTtlSeconds"`
	PendingTTLSeconds       int                  `json:"pendingTtlSeconds"`
	ApprovalTokenTTLSeconds int                  `json:"approvalTokenTtlSeconds"`
	Pairing                 ApprovalDomainConfig `json:"pairing"`
	Exec                    ApprovalDomainConfig `json:"exec"`
	SkillExecution          ApprovalDomainConfig `json:"skillExecution"`
	SecretAccess            ApprovalDomainConfig `json:"secretAccess"`
	MessageSend             ApprovalDomainConfig `json:"messageSend"`
}

// SecurityConfig groups secret storage, auditing, profiles, and network policy.
type SecurityConfig struct {
	SecretStore SecretStoreConfig    `json:"secretStore"`
	Audit       AuditConfig          `json:"audit"`
	Approvals   ApprovalConfig       `json:"approvals"`
	Profiles    AccessProfilesConfig `json:"profiles"`
	Network     NetworkPolicyConfig  `json:"network"`
}

// SecretStoreConfig configures encrypted secret persistence.
type SecretStoreConfig struct {
	Enabled  bool   `json:"enabled"`
	Required bool   `json:"required"`
	KeyFile  string `json:"keyFile"`
}

// AuditConfig configures append-only audit logging and verification.
type AuditConfig struct {
	Enabled       bool   `json:"enabled"`
	Strict        bool   `json:"strict"`
	KeyFile       string `json:"keyFile"`
	VerifyOnStart bool   `json:"verifyOnStart"`
}

// AccessProfilesConfig maps channels and triggers onto named access profiles.
type AccessProfilesConfig struct {
	Enabled  bool                           `json:"enabled"`
	Default  string                         `json:"default"`
	Channels map[string]string              `json:"channels"`
	Triggers map[string]string              `json:"triggers"`
	Profiles map[string]AccessProfileConfig `json:"profiles"`
}

// AccessProfileConfig limits tools, hosts, and write paths for a profile.
type AccessProfileConfig struct {
	MaxCapability  string   `json:"maxCapability"`
	AllowedTools   []string `json:"allowedTools"`
	AllowedHosts   []string `json:"allowedHosts"`
	WritablePaths  []string `json:"writablePaths"`
	AllowSubagents bool     `json:"allowSubagents"`
}

// NetworkPolicyConfig defines outbound network restrictions.
type NetworkPolicyConfig struct {
	Enabled       bool     `json:"enabled"`
	DefaultDeny   bool     `json:"defaultDeny"`
	AllowedHosts  []string `json:"allowedHosts"`
	AllowLoopback bool     `json:"allowLoopback"`
	AllowPrivate  bool     `json:"allowPrivate"`
}
