// Package config defines the persisted runtime configuration and validation
// rules for or3-intern.
package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RuntimeProfile names the intended execution posture for this instance.
type RuntimeProfile string

const (
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
	DBPath                 string `json:"dbPath"`
	ArtifactsDir           string `json:"artifactsDir"`
	WorkspaceDir           string `json:"workspaceDir"`
	AllowedDir             string `json:"allowedDir"`
	DefaultSessionKey      string `json:"defaultSessionKey"`
	SoulFile               string `json:"soulFile"`
	AgentsFile             string `json:"agentsFile"`
	ToolsFile              string `json:"toolsFile"`
	BootstrapMaxChars      int    `json:"bootstrapMaxChars"`
	BootstrapTotalMaxChars int    `json:"bootstrapTotalMaxChars"`
	SessionCache           int    `json:"sessionCacheLimit"`
	HistoryMax             int    `json:"historyMaxMessages"`
	MaxToolBytes           int    `json:"maxToolBytes"`
	MaxMediaBytes          int    `json:"maxMediaBytes"`
	MaxToolLoops           int    `json:"maxToolLoops"`
	MemoryRetrieve         int    `json:"memoryRetrieveLimit"`
	VectorK                int    `json:"vectorSearchK"`
	FTSK                   int    `json:"ftsSearchK"`
	VectorScanLimit        int    `json:"vectorScanLimit"`
	WorkerCount            int    `json:"workerCount"`

	ConsolidationEnabled             bool            `json:"consolidationEnabled"`
	ConsolidationWindowSize          int             `json:"consolidationWindowSize"`
	ConsolidationMaxMessages         int             `json:"consolidationMaxMessages"`
	ConsolidationMaxInputChars       int             `json:"consolidationMaxInputChars"`
	ConsolidationAsyncTimeoutSeconds int             `json:"consolidationAsyncTimeoutSeconds"`
	Subagents                        SubagentsConfig `json:"subagents"`
	RuntimeProfile                   RuntimeProfile  `json:"runtimeProfile"`

	IdentityFile string         `json:"identityFile"`
	MemoryFile   string         `json:"memoryFile"`
	DocIndex     DocIndexConfig `json:"docIndex"`
	Skills       SkillsConfig   `json:"skills"`
	Triggers     TriggerConfig  `json:"triggers"`
	Session      SessionConfig  `json:"session"`
	Security     SecurityConfig `json:"security"`

	Provider  ProviderConfig  `json:"provider"`
	Tools     ToolsConfig     `json:"tools"`
	Hardening HardeningConfig `json:"hardening"`
	Cron      CronConfig      `json:"cron"`
	Service   ServiceConfig   `json:"service"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
	Channels  ChannelsConfig  `json:"channels"`
}

// HardeningConfig controls sandboxing, privilege gates, and per-tool quotas.
type HardeningConfig struct {
	GuardedTools        bool                 `json:"guardedTools"`
	PrivilegedTools     bool                 `json:"privilegedTools"`
	EnableExecShell     bool                 `json:"enableExecShell"`
	ExecAllowedPrograms []string             `json:"execAllowedPrograms"`
	ChildEnvAllowlist   []string             `json:"childEnvAllowlist"`
	IsolateChannelPeers bool                 `json:"isolateChannelPeers"`
	Sandbox             SandboxConfig        `json:"sandbox"`
	Quotas              HardeningQuotaConfig `json:"quotas"`
}

// SandboxConfig defines how exec-capable tools are isolated.
type SandboxConfig struct {
	Enabled        bool     `json:"enabled"`
	BubblewrapPath string   `json:"bubblewrapPath"`
	AllowNetwork   bool     `json:"allowNetwork"`
	WritablePaths  []string `json:"writablePaths"`
}

// HardeningQuotaConfig limits how many sensitive tool calls a turn may issue.
type HardeningQuotaConfig struct {
	Enabled          bool `json:"enabled"`
	MaxToolCalls     int  `json:"maxToolCalls"`
	MaxExecCalls     int  `json:"maxExecCalls"`
	MaxWebCalls      int  `json:"maxWebCalls"`
	MaxSubagentCalls int  `json:"maxSubagentCalls"`
}

// ProviderConfig selects the LLM and embedding provider endpoints and limits.
type ProviderConfig struct {
	APIBase        string  `json:"apiBase"`
	APIKey         string  `json:"apiKey"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	EmbedModel     string  `json:"embedModel"`
	EnableVision   bool    `json:"enableVision"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
}

// ToolsConfig configures built-in tools and external MCP server integrations.
type ToolsConfig struct {
	BraveAPIKey         string                     `json:"braveApiKey"`
	WebProxy            string                     `json:"webProxy"`
	ExecTimeoutSeconds  int                        `json:"execTimeoutSeconds"`
	RestrictToWorkspace bool                       `json:"restrictToWorkspace"`
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
	Env                   map[string]string `json:"env"`
	ChildEnvAllowlist     []string          `json:"childEnvAllowlist"`
	URL                   string            `json:"url"`
	Headers               map[string]string `json:"headers"`
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
	Enabled bool   `json:"enabled"`
	Listen  string `json:"listen"`
	Secret  string `json:"secret"`
}

// SubagentsConfig limits the internal subagent queue and worker pool.
type SubagentsConfig struct {
	Enabled            bool `json:"enabled"`
	MaxConcurrent      int  `json:"maxConcurrent"`
	MaxQueued          int  `json:"maxQueued"`
	TaskTimeoutSeconds int  `json:"taskTimeoutSeconds"`
}

// TelegramChannelConfig configures Telegram ingress and delivery.
type TelegramChannelConfig struct {
	Enabled        bool     `json:"enabled"`
	OpenAccess     bool     `json:"openAccess"`
	Token          string   `json:"token"`
	APIBase        string   `json:"apiBase"`
	PollSeconds    int      `json:"pollSeconds"`
	DefaultChatID  string   `json:"defaultChatId"`
	AllowedChatIDs []string `json:"allowedChatIds"`
}

// SlackChannelConfig configures Slack socket-mode ingress and delivery.
type SlackChannelConfig struct {
	Enabled          bool     `json:"enabled"`
	OpenAccess       bool     `json:"openAccess"`
	AppToken         string   `json:"appToken"`
	BotToken         string   `json:"botToken"`
	APIBase          string   `json:"apiBase"`
	SocketModeURL    string   `json:"socketModeUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

// DiscordChannelConfig configures Discord ingress and delivery.
type DiscordChannelConfig struct {
	Enabled          bool     `json:"enabled"`
	OpenAccess       bool     `json:"openAccess"`
	Token            string   `json:"token"`
	APIBase          string   `json:"apiBase"`
	GatewayURL       string   `json:"gatewayUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

// WhatsAppBridgeConfig configures the external WhatsApp bridge connection.
type WhatsAppBridgeConfig struct {
	Enabled     bool     `json:"enabled"`
	OpenAccess  bool     `json:"openAccess"`
	BridgeURL   string   `json:"bridgeUrl"`
	BridgeToken string   `json:"bridgeToken"`
	DefaultTo   string   `json:"defaultTo"`
	AllowedFrom []string `json:"allowedFrom"`
}

// EmailChannelConfig configures email polling, access control, and replies.
type EmailChannelConfig struct {
	Enabled             bool     `json:"enabled"`
	OpenAccess          bool     `json:"openAccess"`
	ConsentGranted      bool     `json:"consentGranted"`
	AllowedSenders      []string `json:"allowedSenders"`
	DefaultTo           string   `json:"defaultTo"`
	AutoReplyEnabled    bool     `json:"autoReplyEnabled"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds"`
	MarkSeen            bool     `json:"markSeen"`
	MaxBodyChars        int      `json:"maxBodyChars"`
	SubjectPrefix       string   `json:"subjectPrefix"`
	FromAddress         string   `json:"fromAddress"`
	IMAPMailbox         string   `json:"imapMailbox"`
	IMAPHost            string   `json:"imapHost"`
	IMAPPort            int      `json:"imapPort"`
	IMAPUseSSL          bool     `json:"imapUseSSL"`
	IMAPUsername        string   `json:"imapUsername"`
	IMAPPassword        string   `json:"imapPassword"`
	SMTPHost            string   `json:"smtpHost"`
	SMTPPort            int      `json:"smtpPort"`
	SMTPUseTLS          bool     `json:"smtpUseTLS"`
	SMTPUseSSL          bool     `json:"smtpUseSSL"`
	SMTPUsername        string   `json:"smtpUsername"`
	SMTPPassword        string   `json:"smtpPassword"`
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
	ExtraDirs       []string `json:"extraDirs"`
	Watch           bool     `json:"watch"`
	WatchDebounceMS int      `json:"watchDebounceMs"`
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
	Secret    string `json:"secret"`
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

// Default returns the baseline configuration used for new installations.
func Default() Config {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".or3-intern")
	return Config{
		DBPath:                           filepath.Join(root, "or3-intern.sqlite"),
		ArtifactsDir:                     filepath.Join(root, "artifacts"),
		WorkspaceDir:                     "",
		AllowedDir:                       "",
		DefaultSessionKey:                "cli:default",
		SoulFile:                         filepath.Join(root, "SOUL.md"),
		AgentsFile:                       filepath.Join(root, "AGENTS.md"),
		ToolsFile:                        filepath.Join(root, "TOOLS.md"),
		IdentityFile:                     filepath.Join(root, "IDENTITY.md"),
		MemoryFile:                       filepath.Join(root, "MEMORY.md"),
		BootstrapMaxChars:                20000,
		BootstrapTotalMaxChars:           150000,
		SessionCache:                     64,
		HistoryMax:                       40,
		MaxToolBytes:                     24 * 1024,
		MaxMediaBytes:                    20 * 1024 * 1024,
		MaxToolLoops:                     6,
		MemoryRetrieve:                   8,
		VectorK:                          8,
		FTSK:                             8,
		VectorScanLimit:                  2000,
		WorkerCount:                      4,
		ConsolidationEnabled:             true,
		ConsolidationWindowSize:          10,
		ConsolidationMaxMessages:         50,
		ConsolidationMaxInputChars:       12000,
		ConsolidationAsyncTimeoutSeconds: 30,
		Subagents: SubagentsConfig{
			Enabled:            false,
			MaxConcurrent:      1,
			MaxQueued:          32,
			TaskTimeoutSeconds: 300,
		},
		DocIndex: DocIndexConfig{
			Enabled:        false,
			MaxFiles:       100,
			MaxFileBytes:   64 * 1024,
			MaxChunks:      500,
			EmbedMaxBytes:  8 * 1024,
			RefreshSeconds: 300,
			RetrieveLimit:  5,
		},
		Skills: SkillsConfig{
			EnableExec:    false,
			MaxRunSeconds: 30,
			ManagedDir:    filepath.Join(root, "skills"),
			Policy: SkillPolicyConfig{
				QuarantineByDefault: true,
				Approved:            []string{},
				TrustedOwners:       []string{},
				BlockedOwners:       []string{},
				TrustedRegistries:   []string{},
			},
			Load: SkillsLoadConfig{
				Watch:           false,
				WatchDebounceMS: 250,
			},
			Entries: map[string]SkillEntryConfig{},
			ClawHub: ClawHubConfig{
				SiteURL:     "https://clawhub.ai",
				RegistryURL: "https://clawhub.ai",
				InstallDir:  "skills",
			},
		},
		Triggers: TriggerConfig{
			Webhook: WebhookConfig{
				Enabled:   false,
				Addr:      "127.0.0.1:8765",
				MaxBodyKB: 64,
			},
			FileWatch: FileWatchConfig{
				Enabled:         false,
				PollSeconds:     5,
				DebounceSeconds: 2,
			},
		},
		Session: SessionConfig{
			DirectMessagesShareDefault: false,
			IdentityLinks:              []SessionIdentityLink{},
		},
		Security: SecurityConfig{
			SecretStore: SecretStoreConfig{
				Enabled:  false,
				Required: false,
				KeyFile:  filepath.Join(root, "master.key"),
			},
			Audit: AuditConfig{
				Enabled:       false,
				Strict:        false,
				KeyFile:       filepath.Join(root, "audit.key"),
				VerifyOnStart: false,
			},
			Approvals: ApprovalConfig{
				Enabled:                 false,
				HostID:                  "local",
				KeyFile:                 filepath.Join(root, "approvals.key"),
				PairingCodeTTLSeconds:   300,
				PendingTTLSeconds:       900,
				ApprovalTokenTTLSeconds: 300,
				Pairing:                 ApprovalDomainConfig{Mode: ApprovalModeAsk},
				Exec:                    ApprovalDomainConfig{Mode: ApprovalModeTrusted},
				SkillExecution:          ApprovalDomainConfig{Mode: ApprovalModeTrusted},
				SecretAccess:            ApprovalDomainConfig{Mode: ApprovalModeAsk},
				MessageSend:             ApprovalDomainConfig{Mode: ApprovalModeAsk},
			},
			Profiles: AccessProfilesConfig{
				Enabled:  false,
				Default:  "",
				Channels: map[string]string{},
				Triggers: map[string]string{},
				Profiles: map[string]AccessProfileConfig{},
			},
			Network: NetworkPolicyConfig{
				Enabled:       false,
				DefaultDeny:   false,
				AllowedHosts:  []string{},
				AllowLoopback: false,
				AllowPrivate:  false,
			},
		},
		Provider: ProviderConfig{
			APIBase:        "https://api.openai.com/v1",
			APIKey:         os.Getenv("OPENAI_API_KEY"),
			Model:          "gpt-4.1-mini",
			Temperature:    0,
			EmbedModel:     "text-embedding-3-small",
			TimeoutSeconds: 60,
		},
		Tools: ToolsConfig{
			BraveAPIKey:         os.Getenv("BRAVE_API_KEY"),
			WebProxy:            "",
			ExecTimeoutSeconds:  60,
			RestrictToWorkspace: true,
			PathAppend:          "",
			MCPServers:          map[string]MCPServerConfig{},
		},
		Hardening: HardeningConfig{
			GuardedTools:        false,
			PrivilegedTools:     false,
			EnableExecShell:     false,
			ExecAllowedPrograms: []string{"cat", "echo", "find", "git", "grep", "head", "ls", "pwd", "sed", "tail"},
			ChildEnvAllowlist:   []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"},
			IsolateChannelPeers: true,
			Sandbox: SandboxConfig{
				Enabled:        false,
				BubblewrapPath: "bwrap",
				AllowNetwork:   false,
				WritablePaths:  []string{},
			},
			Quotas: HardeningQuotaConfig{
				Enabled:          true,
				MaxToolCalls:     16,
				MaxExecCalls:     2,
				MaxWebCalls:      4,
				MaxSubagentCalls: 2,
			},
		},
		Cron: CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.json")},
		Service: ServiceConfig{
			Enabled: false,
			Listen:  "127.0.0.1:9100",
			Secret:  "",
		},
		Heartbeat: HeartbeatConfig{
			Enabled:         false,
			IntervalMinutes: 30,
			TasksFile:       filepath.Join(root, "HEARTBEAT.md"),
			SessionKey:      DefaultHeartbeatSessionKey,
		},
		Channels: ChannelsConfig{
			Telegram: TelegramChannelConfig{Enabled: false, APIBase: "https://api.telegram.org", PollSeconds: 2},
			Slack:    SlackChannelConfig{Enabled: false, APIBase: "https://slack.com/api", RequireMention: true},
			Discord:  DiscordChannelConfig{Enabled: false, APIBase: "https://discord.com/api/v10", RequireMention: true},
			WhatsApp: WhatsAppBridgeConfig{Enabled: false, BridgeURL: "ws://127.0.0.1:3001/ws"},
			Email: EmailChannelConfig{
				Enabled:             false,
				ConsentGranted:      false,
				AutoReplyEnabled:    false,
				PollIntervalSeconds: 30,
				MarkSeen:            true,
				MaxBodyChars:        4000,
				SubjectPrefix:       "Re: ",
				IMAPMailbox:         "INBOX",
				IMAPPort:            993,
				IMAPUseSSL:          true,
				SMTPPort:            587,
				SMTPUseTLS:          true,
				SMTPUseSSL:          false,
			},
		},
	}
}

// DefaultPath returns the default on-disk config file path.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

// ApplyEnvOverrides applies supported OR3_* environment variable overrides in place.
func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv("OR3_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("OR3_ARTIFACTS_DIR"); v != "" {
		cfg.ArtifactsDir = v
	}
	if v := os.Getenv("OR3_API_BASE"); v != "" {
		cfg.Provider.APIBase = v
	}
	if v := os.Getenv("OR3_API_KEY"); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := os.Getenv("OR3_MODEL"); v != "" {
		cfg.Provider.Model = v
	}
	if v := os.Getenv("OR3_EMBED_MODEL"); v != "" {
		cfg.Provider.EmbedModel = v
	}
	if v := os.Getenv("OR3_TELEGRAM_TOKEN"); v != "" {
		cfg.Channels.Telegram.Token = v
	}
	if v := os.Getenv("OR3_SLACK_APP_TOKEN"); v != "" {
		cfg.Channels.Slack.AppToken = v
	}
	if v := os.Getenv("OR3_SLACK_BOT_TOKEN"); v != "" {
		cfg.Channels.Slack.BotToken = v
	}
	if v := os.Getenv("OR3_DISCORD_TOKEN"); v != "" {
		cfg.Channels.Discord.Token = v
	}
	if v := os.Getenv("OR3_WHATSAPP_BRIDGE_URL"); v != "" {
		cfg.Channels.WhatsApp.BridgeURL = v
	}
	if v := os.Getenv("OR3_WHATSAPP_BRIDGE_TOKEN"); v != "" {
		cfg.Channels.WhatsApp.BridgeToken = v
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_HOST"); v != "" {
		cfg.Channels.Email.IMAPHost = v
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_PORT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Channels.Email.IMAPPort = parsed
		}
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_USERNAME"); v != "" {
		cfg.Channels.Email.IMAPUsername = v
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_PASSWORD"); v != "" {
		cfg.Channels.Email.IMAPPassword = v
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_HOST"); v != "" {
		cfg.Channels.Email.SMTPHost = v
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_PORT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Channels.Email.SMTPPort = parsed
		}
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_USERNAME"); v != "" {
		cfg.Channels.Email.SMTPUsername = v
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_PASSWORD"); v != "" {
		cfg.Channels.Email.SMTPPassword = v
	}
	if v := os.Getenv("OR3_EMAIL_FROM_ADDRESS"); v != "" {
		cfg.Channels.Email.FromAddress = v
	}
	if v := os.Getenv("OR3_SUBAGENTS_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Subagents.Enabled = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_MAX_CONCURRENT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.MaxConcurrent = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_MAX_QUEUED"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.MaxQueued = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.TaskTimeoutSeconds = parsed
		}
	}
	if v := os.Getenv("OR3_SERVICE_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Service.Enabled = parsed
		}
	}
	if v := os.Getenv("OR3_SERVICE_LISTEN"); v != "" {
		cfg.Service.Listen = v
	}
	if v := os.Getenv("OR3_SERVICE_SECRET"); v != "" {
		cfg.Service.Secret = v
	}
	if v := os.Getenv("OR3_RUNTIME_PROFILE"); v != "" {
		cfg.RuntimeProfile = RuntimeProfile(strings.ToLower(strings.TrimSpace(v)))
	}
}

// Save writes cfg to path using a private file mode.
func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, mustJSON(cfg), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Load reads configuration from path, creating a default file when missing.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := Save(path, cfg); err != nil {
				return cfg, err
			}
		} else {
			return cfg, err
		}
	} else {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	ApplyEnvOverrides(&cfg)

	if cfg.Provider.TimeoutSeconds <= 0 {
		cfg.Provider.TimeoutSeconds = int((60 * time.Second).Seconds())
	}
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
		cfg.MaxToolBytes = 24 * 1024
	}
	if cfg.MaxMediaBytes <= 0 {
		cfg.MaxMediaBytes = 20 * 1024 * 1024
	}
	if cfg.MaxToolLoops <= 0 {
		cfg.MaxToolLoops = 6
	}
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
	if strings.TrimSpace(cfg.Service.Listen) == "" {
		cfg.Service.Listen = Default().Service.Listen
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
	if cfg.Skills.MaxRunSeconds <= 0 {
		cfg.Skills.MaxRunSeconds = 30
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) == "" {
		cfg.Skills.ManagedDir = filepath.Join(filepath.Dir(DefaultPath()), "skills")
	}
	if cfg.Skills.Load.WatchDebounceMS <= 0 {
		cfg.Skills.Load.WatchDebounceMS = 250
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
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 {
		cfg.Hardening.Quotas.MaxToolCalls = Default().Hardening.Quotas.MaxToolCalls
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
	cfg.RuntimeProfile = RuntimeProfile(strings.ToLower(strings.TrimSpace(string(cfg.RuntimeProfile))))
	if err := validateRuntimeProfile(cfg.RuntimeProfile); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

func validateChannelAccess(cfg Config) error {
	if cfg.Channels.Telegram.Enabled && !cfg.Channels.Telegram.OpenAccess && !hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs) {
		return errors.New("telegram enabled: set channels.telegram.allowedChatIds or channels.telegram.openAccess=true")
	}
	if cfg.Channels.Slack.Enabled && !cfg.Channels.Slack.OpenAccess && !hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs) {
		return errors.New("slack enabled: set channels.slack.allowedUserIds or channels.slack.openAccess=true")
	}
	if cfg.Channels.Discord.Enabled && !cfg.Channels.Discord.OpenAccess && !hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs) {
		return errors.New("discord enabled: set channels.discord.allowedUserIds or channels.discord.openAccess=true")
	}
	if cfg.Channels.WhatsApp.Enabled && !cfg.Channels.WhatsApp.OpenAccess && !hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom) {
		return errors.New("whatsApp enabled: set channels.whatsApp.allowedFrom or channels.whatsApp.openAccess=true")
	}
	if cfg.Channels.Email.Enabled {
		if !cfg.Channels.Email.ConsentGranted {
			return errors.New("email enabled: set channels.email.consentGranted=true after explicit permission")
		}
		if !cfg.Channels.Email.OpenAccess && !hasNonEmpty(cfg.Channels.Email.AllowedSenders) {
			return errors.New("email enabled: set channels.email.allowedSenders or channels.email.openAccess=true")
		}
		if strings.TrimSpace(cfg.Channels.Email.IMAPHost) == "" || strings.TrimSpace(cfg.Channels.Email.IMAPUsername) == "" || strings.TrimSpace(cfg.Channels.Email.IMAPPassword) == "" {
			return errors.New("email enabled: imapHost, imapUsername, and imapPassword are required")
		}
		if strings.TrimSpace(cfg.Channels.Email.SMTPHost) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPUsername) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPPassword) == "" {
			return errors.New("email enabled: smtpHost, smtpUsername, and smtpPassword are required")
		}
	}
	return nil
}

func validateMCPServers(servers map[string]MCPServerConfig) error {
	for name, server := range servers {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("tools.mcpServers contains an empty server name")
		}
		if !server.Enabled {
			continue
		}
		switch server.Transport {
		case "stdio":
			if server.Command == "" {
				return errors.New("tools.mcpServers." + name + ": stdio transport requires command")
			}
		case "sse", "streamablehttp":
			if err := validateMCPHTTPURL(name, server); err != nil {
				return err
			}
		default:
			return errors.New("tools.mcpServers." + name + ": unsupported transport " + strconv.Quote(server.Transport))
		}
	}
	return nil
}

func validateMCPHTTPURL(name string, server MCPServerConfig) error {
	if server.URL == "" {
		return errors.New("tools.mcpServers." + name + ": transport " + strconv.Quote(server.Transport) + " requires url")
	}
	u, err := url.Parse(server.URL)
	if err != nil {
		return errors.New("tools.mcpServers." + name + ": invalid url")
	}
	if u.User != nil {
		return errors.New("tools.mcpServers." + name + ": url must not embed credentials")
	}
	if u.Host == "" {
		return errors.New("tools.mcpServers." + name + ": url must include host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if !server.AllowInsecureHTTP {
			return errors.New("tools.mcpServers." + name + ": insecure http requires allowInsecureHttp=true")
		}
		if !isLoopbackHost(u.Hostname()) {
			return errors.New("tools.mcpServers." + name + ": insecure http is limited to localhost or loopback hosts")
		}
		return nil
	default:
		return errors.New("tools.mcpServers." + name + ": url scheme must be https or http")
	}
}

func validateAccessProfiles(cfg AccessProfilesConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Default) != "" {
		if _, ok := cfg.Profiles[strings.TrimSpace(cfg.Default)]; !ok {
			return errors.New("security.profiles.default references unknown profile")
		}
	}
	for name, profile := range cfg.Profiles {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("security.profiles.profiles contains an empty profile name")
		}
		switch profile.MaxCapability {
		case "", "safe", "guarded", "privileged":
		default:
			return errors.New("security.profiles.profiles." + name + ": unsupported maxCapability")
		}
	}
	for channel, profileName := range cfg.Channels {
		if strings.TrimSpace(channel) == "" {
			return errors.New("security.profiles.channels contains an empty channel name")
		}
		if _, ok := cfg.Profiles[strings.TrimSpace(profileName)]; !ok {
			return errors.New("security.profiles.channels." + channel + " references unknown profile")
		}
	}
	for trigger, profileName := range cfg.Triggers {
		if strings.TrimSpace(trigger) == "" {
			return errors.New("security.profiles.triggers contains an empty trigger name")
		}
		if _, ok := cfg.Profiles[strings.TrimSpace(profileName)]; !ok {
			return errors.New("security.profiles.triggers." + trigger + " references unknown profile")
		}
	}
	return nil
}

func validateApprovals(cfg ApprovalConfig) error {
	if strings.TrimSpace(cfg.HostID) == "" {
		return errors.New("security.approvals.hostId is required")
	}
	for name, mode := range map[string]ApprovalMode{
		"pairing":        cfg.Pairing.Mode,
		"exec":           cfg.Exec.Mode,
		"skillExecution": cfg.SkillExecution.Mode,
		"secretAccess":   cfg.SecretAccess.Mode,
		"messageSend":    cfg.MessageSend.Mode,
	} {
		if !isValidApprovalMode(mode) {
			return errors.New("security.approvals." + name + ": unsupported mode")
		}
	}
	return nil
}

func normalizeApprovalMode(mode ApprovalMode, fallback ApprovalMode) ApprovalMode {
	normalized := ApprovalMode(strings.ToLower(strings.TrimSpace(string(mode))))
	if normalized == "" {
		return fallback
	}
	return normalized
}

func isValidApprovalMode(mode ApprovalMode) bool {
	switch normalizeApprovalMode(mode, "") {
	case ApprovalModeDeny, ApprovalModeAsk, ApprovalModeAllowlist, ApprovalModeTrusted:
		return true
	default:
		return false
	}
}

func validateRuntimeProfile(p RuntimeProfile) error {
	switch p {
	case "", ProfileLocalDev, ProfileSingleUserHardened,
		ProfileHostedService, ProfileHostedNoExec, ProfileHostedRemoteSandbox:
		return nil
	}
	return errors.New("unrecognized runtimeProfile: " + string(p))
}

// ValidateProfile checks that the profile+config combination is safe.
// It returns the first constraint violation found.
func ValidateProfile(cfg Config) error {
	spec := ProfileSpec(cfg.RuntimeProfile)
	if spec.RequireSecretStore {
		if !cfg.Security.SecretStore.Enabled {
			return errors.New("hosted profiles require security.secretStore.enabled")
		}
	}
	if spec.RequireAudit {
		if !cfg.Security.Audit.Enabled {
			return errors.New("hosted profiles require security.audit.enabled")
		}
	}
	if spec.RequireNetworkPolicy {
		if !cfg.Security.Network.Enabled && !cfg.Security.Network.DefaultDeny {
			return errors.New("hosted profiles require security.network policy to be configured")
		}
	}
	if spec.Hosted && hasRemoteHTTPMCPServers(cfg.Tools.MCPServers) {
		if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
			return errors.New("hosted profiles require deny-by-default security.network for remote MCP HTTP")
		}
		if networkAllowlistTooBroad(cfg.Security.Network.AllowedHosts) {
			return errors.New("hosted profiles require a narrow security.network.allowedHosts for remote MCP HTTP")
		}
	}
	if spec.RequireStrictAudit {
		if !cfg.Security.Audit.Strict {
			return errors.New("profile requires security.audit.strict")
		}
	}
	if spec.RequireAuditVerifyStart {
		if !cfg.Security.Audit.VerifyOnStart {
			return errors.New("profile requires security.audit.verifyOnStart")
		}
	}
	if spec.RequireSecretStoreKey {
		if !cfg.Security.SecretStore.Required {
			return errors.New("profile requires security.secretStore.required")
		}
	}
	if spec.ForbidExecShell {
		if cfg.Hardening.EnableExecShell {
			return errors.New("hosted-no-exec profile does not allow enableExecShell")
		}
	}
	if spec.ForbidPrivilegedTools {
		if cfg.Hardening.PrivilegedTools {
			return errors.New("hosted-no-exec profile does not allow privilegedTools")
		}
	}
	if spec.RequireSandboxForExec {
		if cfg.Hardening.EnableExecShell && !cfg.Hardening.Sandbox.Enabled {
			return errors.New("hosted-remote-sandbox-only profile requires sandbox for exec")
		}
	}
	return nil
}

// IsHostedProfile reports whether p is one of the hosted runtime profiles.
func IsHostedProfile(p RuntimeProfile) bool {
	switch p {
	case ProfileHostedService, ProfileHostedNoExec, ProfileHostedRemoteSandbox:
		return true
	}
	return false
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func hasRemoteHTTPMCPServers(servers map[string]MCPServerConfig) bool {
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport != "sse" && transport != "streamablehttp" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(server.URL))
		if err != nil {
			return true
		}
		if u.Hostname() == "" || !isLoopbackHost(u.Hostname()) {
			return true
		}
	}
	return false
}

func networkAllowlistTooBroad(hosts []string) bool {
	if len(hosts) > 10 {
		return true
	}
	for _, host := range hosts {
		host = strings.TrimSpace(strings.ToLower(host))
		if host == "*" {
			return true
		}
		if strings.HasPrefix(host, "*.") {
			return true
		}
		if strings.Contains(host, "*") {
			return true
		}
	}
	return false
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}
