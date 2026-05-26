package agent

import "testing"

func TestToolBudgetOverrides_EffectiveMaxToolLoops(t *testing.T) {
	overrides := DoctorAdminBrainToolBudget()
	if got := overrides.EffectiveMaxToolLoops(1); got != DoctorAdminBrainMaxToolLoops {
		t.Fatalf("expected doctor loop override %d, got %d", DoctorAdminBrainMaxToolLoops, got)
	}
	empty := ToolBudgetOverrides{}
	if got := empty.EffectiveMaxToolLoops(6); got != 6 {
		t.Fatalf("expected configured loop limit 6, got %d", got)
	}
}

func TestToolBudgetOverrides_EffectiveQuotaLimits(t *testing.T) {
	overrides := DoctorAdminBrainToolBudget()
	if got := overrides.effectiveLimit(1, overrides.MaxToolCalls); got != DoctorAdminBrainMaxToolCalls {
		t.Fatalf("expected doctor tool-call override %d, got %d", DoctorAdminBrainMaxToolCalls, got)
	}
	empty := ToolBudgetOverrides{}
	if got := empty.effectiveLimit(16, 0); got != 16 {
		t.Fatalf("expected configured quota limit 16, got %d", got)
	}
}
