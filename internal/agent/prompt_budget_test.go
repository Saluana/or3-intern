package agent

import (
	"fmt"
	"strings"
	"testing"

	"or3-intern/internal/providers"
)

func systemPromptText(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []map[string]any:
		var parts []string
		for _, block := range typed {
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return strings.TrimSpace(strings.Join(strings.Fields(fmt.Sprint(content)), " "))
	}
}

func TestBudgetEnforcesSectionCaps(t *testing.T) {
	b := &Builder{
		ContextSectionBudgets: ContextSectionBudgets{
			MemoryDigest:    8,
			RetrievedMemory: 10,
		},
	}
	packet := b.buildContextPacket(turnPromptInput{
		digestText: strings.Repeat("digest ", 80),
		memText:    strings.Repeat("retrieved ", 80),
	})

	report := estimatePacketBudget(&packet, nil)
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
	packet := b.buildContextPacket(turnPromptInput{
		pinnedText:   "PINNED PROTECTED " + strings.Repeat("pinned ", 100),
		identityText: "IDENTITY PROTECTED " + strings.Repeat("identity ", 100),
	})

	stable := renderStablePrefix(packet)
	for _, want := range []string{"SOUL PROTECTED", "IDENTITY", "AGENTS PROTECTED", "TOOLS PROTECTED", "PINNED"} {
		if !strings.Contains(stable, want) {
			t.Fatalf("protected section content %q was dropped from %q", want, stable)
		}
	}
	for _, usage := range estimatePacketBudget(&packet, nil).Sections {
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
	}).buildContextPacket(turnPromptInput{})

	report := estimatePacketBudget(&packet, nil)
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
		buildContextPacket(turnPromptInput{workspaceContextText: strings.Repeat("workspace ", 100)})

	report := estimatePacketBudget(&packet, nil)
	for _, ev := range report.Pruned {
		if ev.Section == "Workspace Context" && ev.Reason != "" {
			return
		}
	}
	t.Fatalf("expected workspace prune event with reason, got %+v", report.Pruned)
}

func TestRenderProviderMessagesUsesXMLTiers(t *testing.T) {
	b := &Builder{
		Soul:              "Soul",
		AgentInstructions: "Agent",
		ToolNotes:         "Tools",
		IdentityText:      "Identity",
		StaticMemory:      "Static",
	}
	packet := b.buildContextPacket(turnPromptInput{
		pinnedText:           "- p: v",
		digestText:           "- [fact] digest",
		memText:              "1) [memory] retrieved",
		identityText:         b.IdentityText,
		staticMemoryText:     b.StaticMemory,
		heartbeatText:        "heartbeat",
		eventContextText:     "trigger",
		docContextText:       "docs",
		workspaceContextText: "workspace",
		currentUserMessage:   "current ask",
		currentUserMessageID: 12,
	})
	got := systemPromptText(renderProviderMessages(&packet, b)[0].Content)
	for _, want := range []string{
		"<assistant_identity",
		"<coding_agent_rules",
		"<tool_policy",
		"<pinned_memory",
		"<current_user_request",
		"current ask",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected XML prompt to include %q, got:\n%s", want, got)
		}
	}
}

func TestEstimateMessagesTokensAndMessageContentString(t *testing.T) {
	content := []providers.ToolCall{toolCall("lookup", `{"id":1}`)}
	msgs := []providers.ChatMessage{
		{Role: "assistant", Content: content, Name: "planner", ToolCallID: "call-1"},
		{Role: "tool", Content: nil},
	}
	want := 0
	for _, msg := range msgs {
		want += 4
		want += estimateTextTokens(msg.Role)
		want += estimateTextTokens(messageContentString(msg.Content))
		want += estimateTextTokens(msg.Name)
		want += estimateTextTokens(msg.ToolCallID)
	}
	if got := estimateMessagesTokens(msgs); got != want {
		t.Fatalf("estimateMessagesTokens=%d, want %d", got, want)
	}
	if got := messageContentString(nil); got != "" {
		t.Fatalf("messageContentString(nil)=%q, want empty", got)
	}
	if got := messageContentString(content); !strings.Contains(got, "lookup") {
		t.Fatalf("expected array content to stringify tool calls, got %q", got)
	}
}

func TestMinProtectedTokensBoundaries(t *testing.T) {
	tests := map[int]int{
		0:   1,
		63:  63,
		64:  16,
		100: 25,
	}
	for cap, want := range tests {
		if got := minProtectedTokens(cap); got != want {
			t.Fatalf("minProtectedTokens(%d)=%d, want %d", cap, got, want)
		}
	}
}

func TestEstimatePacketBudgetFallsBackWhenUsableIsNonPositive(t *testing.T) {
	packet := (&Builder{
		ContextMaxInputTokens:      10000,
		ContextOutputReserveTokens: 6000,
		ContextSafetyMarginTokens:  5000,
	}).buildContextPacket(turnPromptInput{memText: "tiny"})

	report := estimatePacketBudget(&packet, nil)
	if report.MaxInputTokens != 10000 || report.OutputReserveTokens != 6000 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, ev := range report.Pruned {
		if ev.Section == "Prompt" {
			t.Fatalf("expected usable<=0 fallback to avoid prompt prune for this packet, got %+v", report.Pruned)
		}
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
