package adminflow

import (
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
)

func TestDetectAdminBrainProvider_NoRunnerNoProvider(t *testing.T) {
	got := DetectAdminBrainProvider(config.Default(), nil)
	if got.Kind != AdminBrainUnavailable || got.Available {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
	if got.DisplayName != "Admin Brain" {
		t.Fatalf("display name = %q", got.DisplayName)
	}
}

func TestDetectAdminBrainProvider_RunnerInstalledButAuthBrokenFallsBackToProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.APIKey = "sk-test"
	cfg.Provider.APIBase = "https://api.openai.com/v1"
	runners := []agentcli.RunnerInfo{{
		ID:         string(agentcli.RunnerCodex),
		Status:     agentcli.RunnerStatusAvailable,
		AuthStatus: agentcli.AuthMissing,
		Supports: agentcli.RunnerSupports{
			Chat: agentcli.RunnerChatCapabilities{ChatSelectable: true},
		},
	}}
	got := DetectAdminBrainProvider(cfg, runners)
	if got.Kind != AdminBrainAPIKeyProvider || !got.Available || got.ProviderKey != "openai" {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
}

func TestDetectAdminBrainProvider_PrefersReadyRunner(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.APIKey = "sk-test"
	cfg.Provider.APIBase = "https://openrouter.ai/api/v1"
	runners := []agentcli.RunnerInfo{{
		ID:         string(agentcli.RunnerOpenCode),
		Status:     agentcli.RunnerStatusAvailable,
		AuthStatus: agentcli.AuthReady,
		Supports: agentcli.RunnerSupports{
			Chat: agentcli.RunnerChatCapabilities{ChatSelectable: true},
		},
	}}
	got := DetectAdminBrainProvider(cfg, runners)
	if got.Kind != AdminBrainRunner || got.RunnerID != string(agentcli.RunnerOpenCode) {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
}

func TestDetectAdminBrainProvider_UsesProviderProfileKey(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.APIKey = ""
	cfg.Providers = config.ProviderProfiles{
		"custom-provider": {APIBase: "https://models.example.com/v1", APIKey: "token"},
	}
	got := DetectAdminBrainProvider(cfg, nil)
	if got.Kind != AdminBrainAPIKeyProvider || got.ProviderKey != "custom-provider" {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
}
