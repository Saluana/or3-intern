package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestParseConfigureArgs(t *testing.T) {
	parsed, err := parseConfigureArgs([]string{"--section", "provider", "--section", "web", "--section", "provider"})
	if err != nil {
		t.Fatalf("parseConfigureArgs: %v", err)
	}
	if len(parsed.Sections) != 2 || parsed.Sections[0] != "provider" || parsed.Sections[1] != "tools" {
		t.Fatalf("unexpected sections: %#v", parsed.Sections)
	}
	if _, err := parseConfigureArgs([]string{"--section", "nope"}); err == nil {
		t.Fatal("expected invalid section error")
	}
}

func TestRunConfigureWithIO_TargetedSections(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	input := strings.NewReader(strings.Join([]string{
		"2",
		"",
		"",
		"",
		"12345",
		"",
		"n",
		"router-key",
		"brave-key",
		"http://proxy.internal:8080",
		"75",
		"/opt/homebrew/bin",
	}, "\n"))
	var out strings.Builder

	if err := runConfigureWithIO(input, &out, configPath, "/workspace/project", []string{"--section", "provider", "--section", "tools"}); err != nil {
		t.Fatalf("runConfigureWithIO: %v", err)
	}

	var cfg config.Config
	if err := readConfigFile(configPath, &cfg); err != nil {
		t.Fatalf("readConfigFile: %v", err)
	}
	if cfg.Provider.APIBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("unexpected API base: %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.APIKey != "router-key" {
		t.Fatalf("unexpected API key: %q", cfg.Provider.APIKey)
	}
	if cfg.Tools.BraveAPIKey != "brave-key" {
		t.Fatalf("unexpected Brave key: %q", cfg.Tools.BraveAPIKey)
	}
	if cfg.Tools.WebProxy != "http://proxy.internal:8080" {
		t.Fatalf("unexpected proxy: %q", cfg.Tools.WebProxy)
	}
	if cfg.Tools.ExecTimeoutSeconds != 75 {
		t.Fatalf("unexpected exec timeout: %d", cfg.Tools.ExecTimeoutSeconds)
	}
	if cfg.Tools.PathAppend != "/opt/homebrew/bin" {
		t.Fatalf("unexpected path append: %q", cfg.Tools.PathAppend)
	}
	if !strings.Contains(out.String(), "Configuration complete.") {
		t.Fatalf("expected completion output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Saved provider settings.") || !strings.Contains(out.String(), "Saved tools settings.") {
		t.Fatalf("expected per-section save output, got %q", out.String())
	}
}

func TestRunConfigureWithIO_InteractiveSelection(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	input := strings.NewReader(strings.Join([]string{
		"2",
		"/tmp/or3.sqlite",
		"/tmp/artifacts",
		"14",
	}, "\n"))
	var out strings.Builder

	if err := runConfigureWithIO(input, &out, configPath, "/workspace/project", nil); err != nil {
		t.Fatalf("runConfigureWithIO: %v", err)
	}

	var cfg config.Config
	if err := readConfigFile(configPath, &cfg); err != nil {
		t.Fatalf("readConfigFile: %v", err)
	}
	if cfg.DBPath != "/tmp/or3.sqlite" {
		t.Fatalf("unexpected DB path: %q", cfg.DBPath)
	}
	if cfg.ArtifactsDir != "/tmp/artifacts" {
		t.Fatalf("unexpected artifacts path: %q", cfg.ArtifactsDir)
	}
	if !strings.Contains(out.String(), "Choose a section to configure") {
		t.Fatalf("expected interactive section picker, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Storage configuration") {
		t.Fatalf("expected selected section to run immediately, got %q", out.String())
	}
	if !strings.Contains(out.String(), "No config found yet") {
		t.Fatalf("expected first-run banner, got %q", out.String())
	}
	if !strings.Contains(out.String(), "or3-intern chat") {
		t.Fatalf("expected next-step guidance, got %q", out.String())
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}
}

func TestRunConfigureWithIO_RepairsInvalidTelegramChannelConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "telegram-token"
	cfg.Channels.Telegram.DefaultChatID = "12345"

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configPath, b, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	input := strings.NewReader(strings.Join([]string{
		"y",
		"",
		"",
		"",
		"",
		"n",
		"2",
		"",
		"n",
		"n",
		"n",
		"n",
	}, "\n"))
	var out strings.Builder

	if err := runConfigureWithIO(input, &out, configPath, "/workspace/project", []string{"--section", "channels"}); err != nil {
		t.Fatalf("runConfigureWithIO: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load repaired config: %v", err)
	}
	if loaded.Channels.Telegram.InboundPolicy != config.InboundPolicyAllowlist {
		t.Fatalf("expected allowlist policy, got %q", loaded.Channels.Telegram.InboundPolicy)
	}
	if len(loaded.Channels.Telegram.AllowedChatIDs) != 1 || loaded.Channels.Telegram.AllowedChatIDs[0] != "12345" {
		t.Fatalf("unexpected telegram allowlist: %#v", loaded.Channels.Telegram.AllowedChatIDs)
	}
	if !strings.Contains(out.String(), "Repair mode:") {
		t.Fatalf("expected repair-mode warning, got %q", out.String())
	}
	if !strings.Contains(out.String(), "or3-intern serve") {
		t.Fatalf("expected serve next step after repair, got %q", out.String())
	}
}

func TestRunConfigureWithIO_WarnsWhenSavedConfigIsStillInvalid(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "telegram-token"

	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configPath, b, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	input := strings.NewReader(strings.Join([]string{
		"",
		"",
		"",
		"n",
	}, "\n"))
	var out strings.Builder

	if err := runConfigureWithIO(input, &out, configPath, "/workspace/project", []string{"--section", "provider"}); err != nil {
		t.Fatalf("runConfigureWithIO: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Configuration saved, but the config still has validation issues.") {
		t.Fatalf("expected invalid-config warning, got %q", text)
	}
	if strings.Contains(text, "or3-intern serve") {
		t.Fatalf("did not expect serve next step while config is invalid, got %q", text)
	}
}

func TestRunConfigureWithIO_SecretPromptKeepsExistingValueWithoutLeakingIt(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	cfg := config.Default()
	cfg.Provider.APIKey = "super-secret-key"
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configPath, b, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	input := strings.NewReader(strings.Join([]string{"", "", "", "", ""}, "\n"))
	input = strings.NewReader(strings.Join([]string{"", "", "", "", "", "", "n", ""}, "\n"))
	var out strings.Builder
	if err := runConfigureWithIO(input, &out, configPath, "/workspace/project", []string{"--section", "provider"}); err != nil {
		t.Fatalf("runConfigureWithIO: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Provider.APIKey != "super-secret-key" {
		t.Fatalf("expected existing secret preserved, got %q", loaded.Provider.APIKey)
	}
	if strings.Contains(out.String(), "super-secret-key") {
		t.Fatalf("expected output not to leak existing secret, got %q", out.String())
	}
}

func TestRunConfigureWithIO_SecretPromptCanClearExistingValue(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	cfg := config.Default()
	cfg.Provider.APIKey = "super-secret-key"
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configPath, b, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	input := strings.NewReader(strings.Join([]string{"", "", "", "", "", "", "n", "clear"}, "\n"))
	var out strings.Builder
	if err := runConfigureWithIO(input, &out, configPath, "/workspace/project", []string{"--section", "provider"}); err != nil {
		t.Fatalf("runConfigureWithIO: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Provider.APIKey != "" {
		t.Fatalf("expected secret cleared, got %q", loaded.Provider.APIKey)
	}
}

func TestPromptSecretString_ExistingValueUsesSinglePromptContract(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("replacement-value\n"))
	var out strings.Builder
	value, err := promptSecretString(reader, &out, "API key", "current-secret")
	if err != nil {
		t.Fatalf("promptSecretString: %v", err)
	}
	if value != "replacement-value" {
		t.Fatalf("expected replacement value, got %q", value)
	}
	text := out.String()
	if !strings.Contains(text, "leave blank to keep current") {
		t.Fatalf("expected keep-current hint, got %q", text)
	}
	if strings.Contains(text, "Keep current value") {
		t.Fatalf("expected single prompt contract, got %q", text)
	}
}

func TestBuildSectionFields_CoversExpandedConfigAreas(t *testing.T) {
	cfg := config.Default()
	sections := map[string][]string{
		"runtime":    {"runtime_default_session", "runtime_worker_count", "runtime_consolidation_enabled"},
		"tools":      {"tools_brave", "tools_exec_timeout", "tools_path_append"},
		"skills":     {"skills_enable_exec", "skills_quarantine", "skills_clawhub_registry"},
		"security":   {"security_secret_store_enabled", "security_approval_exec_mode", "security_network_allowed_hosts"},
		"hardening":  {"hardening_guarded_tools", "hardening_sandbox_enabled", "hardening_max_tool_calls"},
		"automation": {"automation_cron_enabled", "automation_webhook_enabled", "automation_filewatch_paths"},
	}
	for section, wantKeys := range sections {
		fields := buildSectionFields(cfg, section, "/workspace/project")
		if len(fields) == 0 {
			t.Fatalf("expected fields for %s section", section)
		}
		for _, want := range wantKeys {
			found := false
			for _, field := range fields {
				if field.Key == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected %s field in %s section, got %#v", want, section, fields)
			}
		}
	}
}
