package main

import (
	"testing"

	"or3-intern/internal/config"
)

func TestFilterConfigureFields_RunnerFirstHidesRunnerToggle(t *testing.T) {
	cfg := config.Default()
	fields := filterConfigureFields(cfg, []configureField{
		{Key: "runners_default", Label: "Default runner"},
	})
	if len(fields) != 0 {
		t.Fatalf("expected runner-first to hide runner host fields, got %d", len(fields))
	}
}

func TestFilterConfigureFields_RunnerFirstHidesChatModel(t *testing.T) {
	cfg := config.Default()
	fields := filterConfigureFields(cfg, []configureField{
		{Key: "provider_model", Label: "Chat model", Description: "Default chat model for turns."},
	})
	if len(fields) != 0 {
		t.Fatalf("expected runner-first to hide provider_model, got %d", len(fields))
	}
}
