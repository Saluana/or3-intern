package main

import (
	"testing"

	"or3-intern/internal/config"
)

func TestFilterConfigureFields_RunnerFirstHidesDeprecated(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	fields := filterConfigureFields(cfg, []configureField{
		{Key: "agentCLI_default_runner", Label: "Default runner"},
	})
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].Status != "active" {
		t.Fatalf("default runner status = %q", fields[0].Status)
	}
}

func TestFilterConfigureFields_RunnerFirstRelabelsChatModel(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	fields := filterConfigureFields(cfg, []configureField{
		{Key: "provider_model", Label: "Chat model", Description: "Default chat model for turns."},
	})
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].Label != "Legacy provider default" {
		t.Fatalf("label = %q", fields[0].Label)
	}
	if fields[0].Status != "compatibility" {
		t.Fatalf("status = %q", fields[0].Status)
	}
}
