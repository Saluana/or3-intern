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

func TestEffectiveConsolidationModel_FallsBackToProviderModel(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.Model = "main-model"
	cfg.ConsolidationModel = ""

	if got := effectiveConsolidationModel(cfg); got != "main-model" {
		t.Fatalf("expected provider model fallback, got %q", got)
	}
}

func TestEffectiveConsolidationModel_UsesDedicatedModel(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.Model = "main-model"
	cfg.ConsolidationModel = "fast-summary-model"

	if got := effectiveConsolidationModel(cfg); got != "fast-summary-model" {
		t.Fatalf("expected dedicated consolidation model, got %q", got)
	}
}

func TestNewConsolidationProviderClient_UsesExtendedTimeout(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.TimeoutSeconds = 60
	cfg.ConsolidationAsyncTimeoutSeconds = 30

	prov := newConsolidationProviderClient(cfg)
	if prov == nil || prov.HTTP == nil {
		t.Fatalf("expected consolidation provider client")
	}
	if got := prov.HTTP.Timeout; got != 65*time.Second {
		t.Fatalf("expected consolidation provider timeout 65s, got %v", got)
	}
}

func TestNewConsolidationProviderClient_PreservesLongerAsyncTimeout(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.TimeoutSeconds = 20
	cfg.ConsolidationAsyncTimeoutSeconds = 90

	prov := newConsolidationProviderClient(cfg)
	if prov == nil || prov.HTTP == nil {
		t.Fatalf("expected consolidation provider client")
	}
	if got := prov.HTTP.Timeout; got != 90*time.Second {
		t.Fatalf("expected consolidation provider timeout 90s, got %v", got)
	}
}
