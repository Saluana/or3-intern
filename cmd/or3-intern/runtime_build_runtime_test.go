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

func TestBuildRuntimeRunnerManagerAlwaysAvailable(t *testing.T) {
	// Runner-only mode is always on; the runner manager is always built.
	cfg := config.Default()
	if manager := buildRuntimeRunnerManager(cfg, nil, nil); manager == nil {
		t.Fatalf("expected runner manager to be built")
	}
}

func TestBuildRuntimeCronServiceDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Cron.Enabled = false
	if svc := buildRuntimeCronService(cfg, bus.New(1), nil, nil); svc != nil {
		t.Fatalf("expected nil cron service when disabled")
	}
}
