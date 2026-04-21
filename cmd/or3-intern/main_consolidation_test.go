package main

import (
	"testing"
	"time"

	"or3-intern/internal/config"
)

func TestEffectiveConsolidationTimeout_UsesProviderTimeoutFloor(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.TimeoutSeconds = 60
	cfg.ConsolidationAsyncTimeoutSeconds = 30

	got := effectiveConsolidationTimeout(cfg)
	want := 65 * time.Second
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestEffectiveConsolidationTimeout_PreservesLongerAsyncTimeout(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.TimeoutSeconds = 20
	cfg.ConsolidationAsyncTimeoutSeconds = 90

	got := effectiveConsolidationTimeout(cfg)
	want := 90 * time.Second
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
