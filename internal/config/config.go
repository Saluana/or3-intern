package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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

	IdentityFile string         `json:"identityFile"`
	MemoryFile   string         `json:"memoryFile"`
	DocIndex     DocIndexConfig `json:"docIndex"`
	Skills       SkillsConfig   `json:"skills"`
	Triggers     TriggerConfig  `json:"triggers"`
	Session      SessionConfig  `json:"session"`

	Provider  ProviderConfig  `json:"provider"`
	Tools     ToolsConfig     `json:"tools"`
	Cron      CronConfig      `json:"cron"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
	Channels  ChannelsConfig  `json:"channels"`
}

type ProviderConfig struct {
	APIBase        string  `json:"apiBase"`
	APIKey         string  `json:"apiKey"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	EmbedModel     string  `json:"embedModel"`
	EnableVision   bool    `json:"enableVision"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
}

type ToolsConfig struct {
	BraveAPIKey         string `json:"braveApiKey"`
	WebProxy            string `json:"webProxy"`
	ExecTimeoutSeconds  int    `json:"execTimeoutSeconds"`
	RestrictToWorkspace bool   `json:"restrictToWorkspace"`
	PathAppend          string `json:"pathAppend"`
}

type CronConfig struct {
	Enabled   bool   `json:"enabled"`
	StorePath string `json:"storePath"`
}

const DefaultHeartbeatSessionKey = "heartbeat:default"

type HeartbeatConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	TasksFile       string `json:"tasksFile"`
	SessionKey      string `json:"sessionKey"`
}

type SubagentsConfig struct {
	Enabled            bool `json:"enabled"`
	MaxConcurrent      int  `json:"maxConcurrent"`
	MaxQueued          int  `json:"maxQueued"`
	TaskTimeoutSeconds int  `json:"taskTimeoutSeconds"`
}

type TelegramChannelConfig struct {
	Enabled        bool     `json:"enabled"`
	OpenAccess     bool     `json:"openAccess"`
	Token          string   `json:"token"`
	APIBase        string   `json:"apiBase"`
	PollSeconds    int      `json:"pollSeconds"`
	DefaultChatID  string   `json:"defaultChatId"`
	AllowedChatIDs []string `json:"allowedChatIds"`
}

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

type WhatsAppBridgeConfig struct {
	Enabled     bool     `json:"enabled"`
	OpenAccess  bool     `json:"openAccess"`
	BridgeURL   string   `json:"bridgeUrl"`
	BridgeToken string   `json:"bridgeToken"`
	DefaultTo   string   `json:"defaultTo"`
	AllowedFrom []string `json:"allowedFrom"`
}

type ChannelsConfig struct {
	Telegram TelegramChannelConfig `json:"telegram"`
	Slack    SlackChannelConfig    `json:"slack"`
	Discord  DiscordChannelConfig  `json:"discord"`
	WhatsApp WhatsAppBridgeConfig  `json:"whatsApp"`
}

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

type SkillsConfig struct {
	EnableExec    bool                        `json:"enableExec"`
	MaxRunSeconds int                         `json:"maxRunSeconds"`
	ManagedDir    string                      `json:"managedDir"`
	Load          SkillsLoadConfig            `json:"load"`
	Entries       map[string]SkillEntryConfig `json:"entries"`
	ClawHub       ClawHubConfig               `json:"clawHub"`
}

type SkillsLoadConfig struct {
	ExtraDirs       []string `json:"extraDirs"`
	Watch           bool     `json:"watch"`
	WatchDebounceMS int      `json:"watchDebounceMs"`
}

type SkillEntryConfig struct {
	Enabled *bool             `json:"enabled,omitempty"`
	APIKey  string            `json:"apiKey"`
	Env     map[string]string `json:"env"`
	Config  map[string]any    `json:"config"`
}

type ClawHubConfig struct {
	SiteURL     string `json:"siteUrl"`
	RegistryURL string `json:"registryUrl"`
	InstallDir  string `json:"installDir"`
}

type WebhookConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Secret    string `json:"secret"`
	MaxBodyKB int    `json:"maxBodyKB"`
}

type FileWatchConfig struct {
	Enabled         bool     `json:"enabled"`
	Paths           []string `json:"paths"`
	PollSeconds     int      `json:"pollSeconds"`
	DebounceSeconds int      `json:"debounceSeconds"`
}

type TriggerConfig struct {
	Webhook   WebhookConfig   `json:"webhook"`
	FileWatch FileWatchConfig `json:"fileWatch"`
}

type SessionConfig struct {
	DirectMessagesShareDefault bool                  `json:"directMessagesShareDefault"`
	IdentityLinks              []SessionIdentityLink `json:"identityLinks"`
}

type SessionIdentityLink struct {
	Canonical string   `json:"canonical"`
	Peers     []string `json:"peers"`
}

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
		},
		Cron: CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.json")},
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
		},
	}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

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
}

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
	if cfg.Skills.Entries == nil {
		cfg.Skills.Entries = map[string]SkillEntryConfig{}
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
	if err := validateChannelAccess(cfg); err != nil {
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
	return nil
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}
