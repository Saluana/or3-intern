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
