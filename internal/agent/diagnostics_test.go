package agent

import (
	"strings"
	"testing"
)

func TestFormatBudgetDiagnosticsSummarizesPressure(t *testing.T) {
	out := FormatBudgetDiagnostics(BudgetReport{
		EstimatedInputTokens: 900,
		MaxInputTokens:       1000,
		OutputReserveTokens:  100,
		BudgetUsedPercent:    90,
		Pressure:             "high",
		Sections: []SectionUsage{
			{Name: "small", EstimatedTokens: 10},
			{Name: "large", EstimatedTokens: 700, LimitTokens: 800, Truncated: true},
		},
		Pruned:   []PruneEvent{{Section: "Retrieved", Reason: "section token cap", TokensSaved: 50}},
		Rejected: []string{"memory:1 expired"},
	}, 5)
	for _, want := range []string{"pressure=high", "section large", "pruned", "rejected"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in diagnostics %q", want, out)
		}
	}
}

func TestFormatBudgetDiagnosticsRedactsSecretsAndLargeContent(t *testing.T) {
	out := FormatBudgetDiagnostics(BudgetReport{
		Pressure: "normal",
		Sections: []SectionUsage{{Name: "api_key sk-1234567890abcdefghijklmnopqrstuvwxyz", EstimatedTokens: 1}},
		Rejected: []string{"bearer token super-secret-value " + strings.Repeat("x", 200)},
	}, 5)
	if strings.Contains(strings.ToLower(out), "sk-123") || strings.Contains(strings.ToLower(out), "super-secret-value") {
		t.Fatalf("expected diagnostics to redact secrets, got %q", out)
	}
	if strings.Contains(out, strings.Repeat("x", 120)) {
		t.Fatalf("expected diagnostics to truncate large content, got %q", out)
	}
}
