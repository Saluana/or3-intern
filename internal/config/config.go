package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	DBPath         string `json:"dbPath"`
	ArtifactsDir   string `json:"artifactsDir"`
	WorkspaceDir   string `json:"workspaceDir"`
	AllowedDir     string `json:"allowedDir"`
	DefaultSessionKey string `json:"defaultSessionKey"`
	SoulFile string `json:"soulFile"`
	AgentsFile string `json:"agentsFile"`
	ToolsFile string `json:"toolsFile"`
	BootstrapMaxChars int `json:"bootstrapMaxChars"`
	BootstrapTotalMaxChars int `json:"bootstrapTotalMaxChars"`
	SessionCache   int    `json:"sessionCacheLimit"`
	HistoryMax     int    `json:"historyMaxMessages"`
	MaxToolBytes   int    `json:"maxToolBytes"`
	MaxToolLoops   int    `json:"maxToolLoops"`
	MemoryRetrieve int    `json:"memoryRetrieveLimit"`
	VectorK        int    `json:"vectorSearchK"`
	FTSK           int    `json:"ftsSearchK"`
	VectorScanLimit int   `json:"vectorScanLimit"`
	WorkerCount    int    `json:"workerCount"`

	Provider ProviderConfig `json:"provider"`
	Tools    ToolsConfig    `json:"tools"`
	Cron     CronConfig     `json:"cron"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
}

type ProviderConfig struct {
	APIBase string `json:"apiBase"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	Temperature float64 `json:"temperature"`
	EmbedModel string `json:"embedModel"`
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type ToolsConfig struct {
	BraveAPIKey string `json:"braveApiKey"`
	WebProxy    string `json:"webProxy"`
	ExecTimeoutSeconds int `json:"execTimeoutSeconds"`
	RestrictToWorkspace bool `json:"restrictToWorkspace"`
	PathAppend string `json:"pathAppend"`
}

type CronConfig struct {
	Enabled bool `json:"enabled"`
	StorePath string `json:"storePath"`
}

type HeartbeatConfig struct {
	Enabled bool `json:"enabled"`
	IntervalMinutes int `json:"intervalMinutes"`
	TasksFile string `json:"tasksFile"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".or3-intern")
	return Config{
		DBPath: filepath.Join(root, "or3-intern.sqlite"),
		ArtifactsDir: filepath.Join(root, "artifacts"),
		WorkspaceDir: "",
		AllowedDir: "",
		DefaultSessionKey: "cli:default",
		SoulFile: filepath.Join(root, "SOUL.md"),
		AgentsFile: filepath.Join(root, "AGENTS.md"),
		ToolsFile: filepath.Join(root, "TOOLS.md"),
		BootstrapMaxChars: 20000,
		BootstrapTotalMaxChars: 150000,
		SessionCache: 64,
		HistoryMax: 40,
		MaxToolBytes: 24 * 1024,
		MaxToolLoops: 6,
		MemoryRetrieve: 8,
		VectorK: 8,
		FTSK: 8,
		VectorScanLimit: 2000,
		WorkerCount: 4,
		Provider: ProviderConfig{
			APIBase: "https://api.openai.com/v1",
			APIKey: os.Getenv("OPENAI_API_KEY"),
			Model: "gpt-4.1-mini",
			Temperature: 0,
			EmbedModel: "text-embedding-3-small",
			TimeoutSeconds: 60,
		},
		Tools: ToolsConfig{
			BraveAPIKey: os.Getenv("BRAVE_API_KEY"),
			WebProxy: "",
			ExecTimeoutSeconds: 60,
			RestrictToWorkspace: false,
			PathAppend: "",
		},
		Cron: CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.json")},
		Heartbeat: HeartbeatConfig{Enabled: false, IntervalMinutes: 30, TasksFile: filepath.Join(root, "HEARTBEAT.md")},
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
	if v := os.Getenv("OR3_DB_PATH"); v != "" { cfg.DBPath = v }
	if v := os.Getenv("OR3_ARTIFACTS_DIR"); v != "" { cfg.ArtifactsDir = v }
	if v := os.Getenv("OR3_API_BASE"); v != "" { cfg.Provider.APIBase = v }
	if v := os.Getenv("OR3_API_KEY"); v != "" { cfg.Provider.APIKey = v }
	if v := os.Getenv("OR3_MODEL"); v != "" { cfg.Provider.Model = v }
	if v := os.Getenv("OR3_EMBED_MODEL"); v != "" { cfg.Provider.EmbedModel = v }
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, mustJSON(cfg), 0o644)
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

	if cfg.Provider.TimeoutSeconds <= 0 { cfg.Provider.TimeoutSeconds = int((60*time.Second).Seconds()) }
	if cfg.DefaultSessionKey == "" { cfg.DefaultSessionKey = "cli:default" }
	if cfg.BootstrapMaxChars <= 0 { cfg.BootstrapMaxChars = 20000 }
	if cfg.BootstrapTotalMaxChars <= 0 { cfg.BootstrapTotalMaxChars = 150000 }
	if cfg.HistoryMax <= 0 { cfg.HistoryMax = 40 }
	if cfg.MaxToolBytes <= 0 { cfg.MaxToolBytes = 24*1024 }
	if cfg.MaxToolLoops <= 0 { cfg.MaxToolLoops = 6 }
	if cfg.VectorScanLimit <= 0 { cfg.VectorScanLimit = 2000 }
	if cfg.WorkerCount <= 0 { cfg.WorkerCount = 4 }
	return cfg, nil
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}
