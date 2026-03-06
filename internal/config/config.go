package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	MaxToolLoops           int    `json:"maxToolLoops"`
	MemoryRetrieve         int    `json:"memoryRetrieveLimit"`
	VectorK                int    `json:"vectorSearchK"`
	FTSK                   int    `json:"ftsSearchK"`
	VectorScanLimit        int    `json:"vectorScanLimit"`
	WorkerCount            int    `json:"workerCount"`

	ConsolidationEnabled             bool `json:"consolidationEnabled"`
	ConsolidationWindowSize          int  `json:"consolidationWindowSize"`
	ConsolidationMaxMessages         int  `json:"consolidationMaxMessages"`
	ConsolidationMaxInputChars       int  `json:"consolidationMaxInputChars"`
	ConsolidationAsyncTimeoutSeconds int  `json:"consolidationAsyncTimeoutSeconds"`

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

type HeartbeatConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	TasksFile       string `json:"tasksFile"`
}

type TelegramChannelConfig struct {
	Enabled        bool     `json:"enabled"`
	Token          string   `json:"token"`
	APIBase        string   `json:"apiBase"`
	PollSeconds    int      `json:"pollSeconds"`
	DefaultChatID  string   `json:"defaultChatId"`
	AllowedChatIDs []string `json:"allowedChatIds"`
}

type SlackChannelConfig struct {
	Enabled          bool     `json:"enabled"`
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
	Token            string   `json:"token"`
	APIBase          string   `json:"apiBase"`
	GatewayURL       string   `json:"gatewayUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

type WhatsAppBridgeConfig struct {
	Enabled     bool     `json:"enabled"`
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
		BootstrapMaxChars:                20000,
		BootstrapTotalMaxChars:           150000,
		SessionCache:                     64,
		HistoryMax:                       40,
		MaxToolBytes:                     24 * 1024,
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
		Cron:      CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.json")},
		Heartbeat: HeartbeatConfig{Enabled: false, IntervalMinutes: 30, TasksFile: filepath.Join(root, "HEARTBEAT.md")},
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
	return cfg, nil
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}
