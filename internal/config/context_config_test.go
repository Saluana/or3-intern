package config

import (
	"testing"
)

func TestContextConfigDefaults(t *testing.T) {
	q := DefaultContextBudgets(ContextModeQuality)
	if q.SystemCore < 10000 {
		t.Errorf("quality SystemCore should be large, got %d", q.SystemCore)
	}
	if q.History < 50000 {
		t.Errorf("quality History should be large, got %d", q.History)
	}
	p := DefaultContextBudgets(ContextModePoor)
	if p.SystemCore >= q.SystemCore {
		t.Errorf("poor SystemCore (%d) should be less than quality (%d)", p.SystemCore, q.SystemCore)
	}
	b := DefaultContextBudgets(ContextModeBalanced)
	if b.SystemCore <= p.SystemCore || b.SystemCore >= q.SystemCore {
		t.Errorf("balanced SystemCore (%d) should be between poor (%d) and quality (%d)", b.SystemCore, p.SystemCore, q.SystemCore)
	}
}

func TestContextConfigValidation(t *testing.T) {
	// Invalid mode
	c := &ContextConfig{Mode: "invalid"}
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid mode")
	}

	// Valid modes
	for _, m := range []ContextMode{ContextModeQuality, ContextModeBalanced, ContextModePoor, ContextModeCustom, ""} {
		c := &ContextConfig{Mode: m}
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected error for mode %q: %v", m, err)
		}
	}

	// Negative budgets clamped to 0
	c = &ContextConfig{
		Mode: ContextModeQuality,
		Budgets: ContextSectionBudgets{
			SystemCore: -1, PinnedMemory: -5, MemoryDigest: -10,
			RetrievedMemory: -3, WorkspaceContext: -2, TaskCard: -1,
			History: -100, OutputReserve: -8,
		},
		TotalTokenBudget: -50,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.TotalTokenBudget != 0 {
		t.Errorf("TotalTokenBudget should be clamped to 0, got %d", c.TotalTokenBudget)
	}
	if c.Budgets.SystemCore != 0 {
		t.Errorf("SystemCore should be clamped to 0, got %d", c.Budgets.SystemCore)
	}
	if c.Budgets.History != 0 {
		t.Errorf("History should be clamped to 0, got %d", c.Budgets.History)
	}
}
