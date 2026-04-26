package main

import (
	"testing"

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
)

func TestApplyContextConfigToBuilder_UsesContextOnlyWhenConfigured(t *testing.T) {
	b := &agent.Builder{}
	legacy := config.Default()
	legacy.ContextConfigured = false
	legacy.Context.MaxInputTokens = 4321
	legacy.Context.OutputReserveTokens = 321
	legacy.Context.SafetyMarginTokens = 123
	legacy.Context.Sections.MemoryDigest = 777
	got := applyContextConfigToBuilder(legacy, b)
	if got.ContextMaxInputTokens != 0 || got.ContextOutputReserveTokens != 0 || got.ContextSafetyMarginTokens != 0 || got.ContextSectionBudgets.MemoryDigest != 0 {
		t.Fatalf("expected legacy config without explicit context block to leave builder context fields unset, got %+v", got)
	}

	explicit := config.Default()
	explicit.ContextConfigured = true
	explicit.Context.MaxInputTokens = 4321
	explicit.Context.OutputReserveTokens = 321
	explicit.Context.SafetyMarginTokens = 123
	explicit.Context.Sections.MemoryDigest = 777
	got = applyContextConfigToBuilder(explicit, &agent.Builder{})
	if got.ContextMaxInputTokens != 4321 || got.ContextOutputReserveTokens != 321 || got.ContextSafetyMarginTokens != 123 || got.ContextSectionBudgets.MemoryDigest != 777 {
		t.Fatalf("expected explicit context block to populate builder fields, got %+v", got)
	}
}
