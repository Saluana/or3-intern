package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_Values(t *testing.T) {
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
	if cfg.Provider.APIBase != "https://api.openai.com/v1" {
		t.Errorf("expected APIBase='https://api.openai.com/v1', got %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.TimeoutSeconds != 60 {
		t.Errorf("expected TimeoutSeconds=60, got %d", cfg.Provider.TimeoutSeconds)
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
}

func TestLoad_FileNotExist_CreatesDefault(t *testing.T) {
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
}

func TestLoad_ValidFile(t *testing.T) {
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
	os.WriteFile(path, b, 0o644)

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
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("{invalid json"), 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write valid default config
	b, _ := json.MarshalIndent(Default(), "", "  ")
	os.WriteFile(path, b, 0o644)

	// Set env vars
	os.Setenv("OR3_DB_PATH", "/env/test.db")
	os.Setenv("OR3_API_KEY", "env-key")
	os.Setenv("OR3_MODEL", "env-model")
	os.Setenv("OR3_EMBED_MODEL", "env-embed")
	os.Setenv("OR3_API_BASE", "https://env.api")
	defer func() {
		os.Unsetenv("OR3_DB_PATH")
		os.Unsetenv("OR3_API_KEY")
		os.Unsetenv("OR3_MODEL")
		os.Unsetenv("OR3_EMBED_MODEL")
		os.Unsetenv("OR3_API_BASE")
	}()

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
	if cfg.Provider.EmbedModel != "env-embed" {
		t.Errorf("expected EmbedModel='env-embed', got %q", cfg.Provider.EmbedModel)
	}
	if cfg.Provider.APIBase != "https://env.api" {
		t.Errorf("expected APIBase='https://env.api', got %q", cfg.Provider.APIBase)
	}
}

func TestLoad_ArtifactsDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, _ := json.MarshalIndent(Default(), "", "  ")
	os.WriteFile(path, b, 0o644)

	os.Setenv("OR3_ARTIFACTS_DIR", "/env/artifacts")
	defer os.Unsetenv("OR3_ARTIFACTS_DIR")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ArtifactsDir != "/env/artifacts" {
		t.Errorf("expected ArtifactsDir='/env/artifacts', got %q", cfg.ArtifactsDir)
	}
}

func TestLoad_ZeroValues_GetDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// config with zero values
	input := Config{}
	b, _ := json.MarshalIndent(input, "", "  ")
	os.WriteFile(path, b, 0o644)

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
}

func TestLoad_EmptyPath_UsesDefault(t *testing.T) {
	// Use a temp home dir to avoid touching real home
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey == "" {
		t.Error("expected non-empty DefaultSessionKey")
	}
}

func TestMustJSON(t *testing.T) {
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
