package agentcli

import (
	"testing"

	"or3-intern/internal/config"
)

func TestResolveRunnerIDForTurnMigratesLegacy(t *testing.T) {
	cfg := config.Default()
	active, legacy, migrated := ResolveRunnerIDForTurn(cfg, "or3-intern")
	if !migrated || legacy != string(RunnerOR3) || active != string(RunnerOpenCode) {
		t.Fatalf("unexpected migration: active=%q legacy=%q migrated=%v", active, legacy, migrated)
	}
}

func TestResolveRunnerIDForTurnDisabledAgentCLI(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = false
	active, legacy, migrated := ResolveRunnerIDForTurn(cfg, "or3-intern")
	if migrated || active != "" || legacy != string(RunnerOR3) {
		t.Fatalf("expected blocked legacy turn, got active=%q legacy=%q migrated=%v", active, legacy, migrated)
	}
}
