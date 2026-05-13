package main

import (
	"testing"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func TestBuildServiceJobRegistryOnlyForService(t *testing.T) {
	if buildServiceJobRegistry("chat") != nil {
		t.Fatalf("expected nil job registry for chat")
	}
	if buildServiceJobRegistry("service") == nil {
		t.Fatalf("expected job registry for service")
	}
}

func TestBuildRuntimeAgentCLIManagerDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = false
	if manager := buildRuntimeAgentCLIManager(cfg, nil, nil); manager != nil {
		t.Fatalf("expected nil agent CLI manager when disabled")
	}
}

func TestBuildRuntimeCronServiceDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Cron.Enabled = false
	if svc := buildRuntimeCronService(cfg, bus.New(1), nil); svc != nil {
		t.Fatalf("expected nil cron service when disabled")
	}
}
