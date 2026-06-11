package main

import (
	"testing"

	"or3-intern/internal/config"
)

func TestFilterConfigureFields_RunnerFirstHidesRunnerToggle(t *testing.T) {
	// The or3-intern built-in agent loop is gone; runner-first mode hides
	// every `agentCLI.*` field from the configure TUI. The values are
	// still read at runtime by the runner host, but no UI control writes
	// them.
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	fields := filterConfigureFields(cfg, []configureField{
		{Key: "agentCLI_default_runner", Label: "Default runner"},
		{Key: "agentCLI_enabled", Label: "Runners"},
		{Key: "docindex_enabled", Label: "Doc index"},
	})
	if len(fields) != 0 {
		t.Fatalf("expected runner-first to hide every agentCLI/docindex field, got %d", len(fields))
	}
}

func TestFilterConfigureFields_RunnerFirstHidesChatModel(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	fields := filterConfigureFields(cfg, []configureField{
		{Key: "provider_model", Label: "Chat model", Description: "Default chat model for turns."},
	})
	if len(fields) != 0 {
		t.Fatalf("expected runner-first to hide provider_model, got %d", len(fields))
	}
}
