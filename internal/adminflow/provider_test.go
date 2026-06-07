package adminflow

import (
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
)

func TestDetectAdminBrainProvider_DefaultRunner(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	cfg.AgentCLI.DefaultRunner = string(agentcli.RunnerCodex)

	got := DetectAdminBrainProvider(cfg, nil)
	if got.Kind != AdminBrainRunner || !got.Available || got.RunnerID != string(agentcli.RunnerCodex) {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
}

func TestDetectAdminBrainProvider_PrefersAvailableExternalRunner(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	cfg.AgentCLI.DefaultRunner = string(agentcli.RunnerCodex)
	runners := []agentcli.RunnerInfo{
		{ID: string(agentcli.RunnerOR3), DisplayName: "OR3"},
		{ID: string(agentcli.RunnerOpenCode), DisplayName: "OpenCode"},
	}

	got := DetectAdminBrainProvider(cfg, runners)
	if got.Kind != AdminBrainRunner || !got.Available || got.RunnerID != string(agentcli.RunnerOpenCode) || got.DisplayName != "OpenCode" {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
}

func TestDetectAdminBrainProvider_UnavailableWithoutExternalRunner(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = false

	got := DetectAdminBrainProvider(cfg, nil)
	if got.Kind != AdminBrainUnavailable || got.Available {
		t.Fatalf("DetectAdminBrainProvider() = %#v", got)
	}
}
