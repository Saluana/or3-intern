package agent

import (
	"strings"
	"testing"
)

func TestBudgetEnforcesSectionCaps(t *testing.T) {
	b := &Builder{
		ContextSectionBudgets: ContextSectionBudgets{
			MemoryDigest:    8,
			RetrievedMemory: 10,
		},
	}
	packet := b.buildContextPacket("(none)", strings.Repeat("digest ", 80), strings.Repeat("retrieved ", 80), "", "", "", "", "", "")

	report := estimatePacketBudget(packet, nil)
	assertSectionTruncated(t, report, "Memory Digest")
	assertSectionTruncated(t, report, "Retrieved Memory")
}

func TestProtectedSectionsRetainedUnderEmergencyPressure(t *testing.T) {
	b := &Builder{
		Soul:              "SOUL PROTECTED " + strings.Repeat("soul ", 100),
		AgentInstructions: "AGENTS PROTECTED " + strings.Repeat("agent ", 100),
		ToolNotes:         "TOOLS PROTECTED " + strings.Repeat("tool ", 100),
		ContextSectionBudgets: ContextSectionBudgets{
			SoulIdentity: 6,
			ToolPolicy:   6,
			PinnedMemory: 6,
		},
	}
	packet := b.buildContextPacket("PINNED PROTECTED "+strings.Repeat("pinned ", 100), "", "(none)", "IDENTITY PROTECTED "+strings.Repeat("identity ", 100), "", "", "", "", "")

	stable := renderStablePrefix(packet)
	for _, want := range []string{"SOUL PROTECTED", "IDENTITY", "AGENTS PROTECTED", "TOOLS PROTECTED", "PINNED"} {
		if !strings.Contains(stable, want) {
			t.Fatalf("protected section content %q was dropped from %q", want, stable)
		}
	}
	for _, usage := range estimatePacketBudget(packet, nil).Sections {
		switch usage.Name {
		case "SOUL.md", "Identity", "AGENTS.md", "TOOLS.md", "Pinned Memory":
			if !usage.Protected {
				t.Fatalf("section %q should be marked protected", usage.Name)
			}
		}
	}
}

func TestOutputReserveReducesInputBudget(t *testing.T) {
	packet := (&Builder{
		ContextMaxInputTokens:      1000,
		ContextOutputReserveTokens: 900,
		ContextSafetyMarginTokens:  50,
	}).buildContextPacket("(none)", "", "(none)", "", "", "", "", "", "")

	report := estimatePacketBudget(packet, nil)
	if report.OutputReserveTokens != 900 {
		t.Fatalf("expected output reserve in report, got %+v", report)
	}
	if report.Pressure != "high" && report.Pressure != "emergency" {
		t.Fatalf("expected reserve-heavy packet to report pressure, got %+v", report)
	}
	found := false
	for _, ev := range report.Pruned {
		if ev.Section == "Prompt" && strings.Contains(ev.Reason, "output reserve") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected output reserve budget event, got %+v", report.Pruned)
	}
}

func TestPressureStatesNormalWarningHighEmergency(t *testing.T) {
	tests := []struct {
		used int
		want string
	}{
		{used: 100, want: "normal"},
		{used: 700, want: "warning"},
		{used: 850, want: "high"},
		{used: 950, want: "emergency"},
	}
	for _, tc := range tests {
		if got := pressureStateForBudget(tc.used, 1000); got != tc.want {
			t.Fatalf("pressureStateForBudget(%d, 1000)=%q, want %q", tc.used, got, tc.want)
		}
	}
}

func TestPruneEventsIncludeReasons(t *testing.T) {
	packet := (&Builder{ContextSectionBudgets: ContextSectionBudgets{WorkspaceContext: 6}}).
		buildContextPacket("(none)", "", "(none)", "", "", "", "", "", strings.Repeat("workspace ", 100))

	report := estimatePacketBudget(packet, nil)
	for _, ev := range report.Pruned {
		if ev.Section == "Workspace Context" && ev.Reason != "" {
			return
		}
	}
	t.Fatalf("expected workspace prune event with reason, got %+v", report.Pruned)
}

func TestLegacyPacketRenderingMatchesExistingPromptRenderer(t *testing.T) {
	b := &Builder{
		Soul:              "Soul",
		AgentInstructions: "Agent",
		ToolNotes:         "Tools",
		IdentityText:      "Identity",
		StaticMemory:      "Static",
	}
	packet := b.buildContextPacket("- p: v", "- [fact] digest", "1) [memory] retrieved", b.IdentityText, b.StaticMemory, "heartbeat", "trigger", "docs", "workspace")
	got := renderProviderMessages(packet, b)[0].Content.(string)
	want := b.composeSystemPrompt("- p: v", "- [fact] digest", "1) [memory] retrieved", b.IdentityText, b.StaticMemory, "heartbeat", "trigger", "docs", "workspace")
	if got != want {
		t.Fatalf("packet renderer changed legacy prompt:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func assertSectionTruncated(t *testing.T, report BudgetReport, name string) {
	t.Helper()
	for _, usage := range report.Sections {
		if usage.Name == name {
			if !usage.Truncated {
				t.Fatalf("expected %q to be truncated, report=%+v", name, report)
			}
			return
		}
	}
	t.Fatalf("section %q missing from report %+v", name, report)
}
