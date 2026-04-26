package agent

import "testing"

func TestContextEvaluationFixturesCoverRequiredScenarios(t *testing.T) {
	fixtures := ContextEvaluationFixtures()
	want := map[string]bool{"coding": false, "planning": false, "debugging": false, "long-running": false, "repeated-memories": false, "stale-memory": false, "large-tool-log": false, "channel-session": false, "workspace-retrieval": false}
	for _, fixture := range fixtures {
		if _, ok := want[fixture.Name]; ok {
			want[fixture.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing evaluation fixture %q", name)
		}
	}
}

func TestContextModesReducePacketSizeAndPreserveProtectedSections(t *testing.T) {
	fixture := ContextEvaluationFixture{
		Name:              "mode-compare",
		UserMessage:       "continue implementation",
		Soul:              "Soul protected " + repeatedEvalText("soul", 80),
		AgentInstructions: "Agent protected " + repeatedEvalText("agent", 80),
		ToolNotes:         "Tools protected " + repeatedEvalText("tools", 80),
		PinnedMemory:      "Pinned protected " + repeatedEvalText("pinned", 80),
		MemoryDigest:      repeatedEvalText("digest", 120),
		RetrievedMemory:   repeatedEvalText("retrieved", 240),
		WorkspaceContext:  repeatedEvalText("workspace", 240),
		TaskCard:          "Goal: preserve decisions\nDecision: keep protected sections",
		HistoryTurns:      12,
	}
	poor := packetForEvaluationFixture(fixture, ContextSectionBudgets{SoulIdentity: 80, ToolPolicy: 80, PinnedMemory: 80, MemoryDigest: 40, RetrievedMemory: 40, WorkspaceContext: 40, ActiveTaskCard: 40}, 5000)
	balanced := packetForEvaluationFixture(fixture, ContextSectionBudgets{SoulIdentity: 160, ToolPolicy: 160, PinnedMemory: 160, MemoryDigest: 120, RetrievedMemory: 120, WorkspaceContext: 120, ActiveTaskCard: 80}, 9000)
	quality := packetForEvaluationFixture(fixture, ContextSectionBudgets{SoulIdentity: 400, ToolPolicy: 400, PinnedMemory: 400, MemoryDigest: 300, RetrievedMemory: 300, WorkspaceContext: 300, ActiveTaskCard: 160}, 18000)
	if poor.Budget.EstimatedInputTokens > balanced.Budget.EstimatedInputTokens || balanced.Budget.EstimatedInputTokens > quality.Budget.EstimatedInputTokens {
		t.Fatalf("expected poor <= balanced <= quality token usage, got poor=%d balanced=%d quality=%d", poor.Budget.EstimatedInputTokens, balanced.Budget.EstimatedInputTokens, quality.Budget.EstimatedInputTokens)
	}
	for _, packet := range []ContextPacket{poor, balanced, quality} {
		stable := renderStablePrefix(packet)
		for _, want := range []string{"Soul protected", "Agent protected", "Tools protected", "Pinned protected"} {
			if !containsEval(stable, want) {
				t.Fatalf("expected protected section %q in stable prefix", want)
			}
		}
	}
}

func TestLegacyEvaluationPacketStablePrefixParity(t *testing.T) {
	fixture := ContextEvaluationFixture{Soul: "Soul", AgentInstructions: "Agent", ToolNotes: "Tools", PinnedMemory: "- x: y", RetrievedMemory: "1) memory", MemoryDigest: "- fact", WorkspaceContext: "README"}
	first := packetForEvaluationFixture(fixture, ContextSectionBudgets{}, 0)
	second := packetForEvaluationFixture(fixture, ContextSectionBudgets{}, 0)
	if renderStablePrefix(first) != renderStablePrefix(second) {
		t.Fatalf("expected legacy evaluation stable prefix to be byte-identical")
	}
}

func TestCachePrefixMeasurementReportsEligiblePercent(t *testing.T) {
	packet := packetForEvaluationFixture(ContextEvaluationFixture{Soul: "Soul", AgentInstructions: "Agent", ToolNotes: "Tools", PinnedMemory: "Pinned", TaskCard: "Goal: test"}, ContextSectionBudgets{}, 0)
	measurement := MeasureCachePrefix(packet, "user text")
	if measurement.StablePrefixBytes <= 0 || measurement.TotalInputBytes <= 0 || measurement.EligiblePercent <= 0 {
		t.Fatalf("expected non-zero cache prefix measurement, got %+v", measurement)
	}
}

func BenchmarkContextPacketConstructionLargeFixture(b *testing.B) {
	fixture := ContextEvaluationFixture{Soul: repeatedEvalText("soul", 200), AgentInstructions: repeatedEvalText("agent", 200), ToolNotes: repeatedEvalText("tool", 200), PinnedMemory: repeatedEvalText("pinned", 200), RetrievedMemory: repeatedEvalText("retrieved", 1000), WorkspaceContext: repeatedEvalText("workspace", 1000), TaskCard: repeatedEvalText("task", 100), HistoryTurns: 80}
	budgets := ContextSectionBudgets{SoulIdentity: 400, ToolPolicy: 400, PinnedMemory: 400, MemoryDigest: 300, RetrievedMemory: 600, WorkspaceContext: 600, ActiveTaskCard: 200}
	for i := 0; i < b.N; i++ {
		_ = packetForEvaluationFixture(fixture, budgets, 16000)
	}
}

func repeatedEvalText(word string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += word + " "
	}
	return out
}

func containsEval(text, want string) bool {
	return len(text) >= len(want) && (text == want || len(want) == 0 || indexEval(text, want) >= 0)
}

func indexEval(text, want string) int {
	for i := 0; i+len(want) <= len(text); i++ {
		if text[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}
