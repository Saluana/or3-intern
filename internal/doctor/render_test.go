package doctor

import (
	"strings"
	"testing"
)

func TestRenderText_SeparatesAutomaticAndInteractiveFixGuidance(t *testing.T) {
	report := NewReport(ModeAdvisory, []Finding{
		{ID: "auto", Area: "filesystem", Severity: SeverityWarn, Summary: "automatic fix", FixMode: FixModeAutomatic},
		{ID: "interactive", Area: "security", Severity: SeverityWarn, Summary: "interactive fix", FixMode: FixModeInteractive},
		{ID: "manual", Area: "security", Severity: SeverityWarn, Summary: "manual fix", FixMode: FixModeManual},
	})
	text := RenderText(report)
	if !strings.Contains(text, "1 finding(s) support safe automatic repair via `or3-intern doctor --fix`.") {
		t.Fatalf("expected automatic guidance, got %q", text)
	}
	if !strings.Contains(text, "1 finding(s) require guided repair via `or3-intern doctor --fix --interactive`.") {
		t.Fatalf("expected interactive guidance, got %q", text)
	}
	if strings.Contains(text, "Run `or3-intern doctor --fix` for safe automatic repairs.") {
		t.Fatalf("expected old generic guidance to be removed, got %q", text)
	}
}

func TestRenderText_SeparatesSeveritySections(t *testing.T) {
	report := NewReport(ModeAdvisory, []Finding{
		{ID: "block", Area: "runtime", Severity: SeverityBlock, Summary: "blocked"},
		{ID: "error", Area: "runtime", Severity: SeverityError, Summary: "errored"},
		{ID: "warn", Area: "runtime", Severity: SeverityWarn, Summary: "warned"},
		{ID: "info", Area: "runtime", Severity: SeverityInfo, Summary: "noted"},
	})
	text := RenderText(report)
	for _, section := range []string{"Blockers:", "Errors:", "Warnings:", "Info:"} {
		if !strings.Contains(text, section) {
			t.Fatalf("expected %q section, got %q", section, text)
		}
	}
	if !strings.Contains(text, "Errors:\n- error: errored") {
		t.Fatalf("expected error finding under Errors section, got %q", text)
	}
	if strings.Contains(text, "Warnings:\n- error: errored") {
		t.Fatalf("expected error finding to stay out of Warnings section, got %q", text)
	}
}
