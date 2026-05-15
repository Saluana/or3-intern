package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
)

func TestSemanticCompressionReadableAndPreservesRefs(t *testing.T) {
	line := renderSemanticMemoryDigestLine(retrievedMemoryLine{memory.Retrieved{ID: 42, Kind: db.MemoryKindDecision, Text: "use sqlite for context packets", Ref: "memory:42"}})
	if !strings.Contains(line, "Decision:") || !strings.Contains(line, "Ref: memory:42") {
		t.Fatalf("expected readable semantic label with ref, got %q", line)
	}
	if strings.Contains(line, "DecisionUseSqlite") || strings.Contains(line, "use_sqlite_for_context_packets") {
		t.Fatalf("semantic compression should remain natural text, got %q", line)
	}
}

func TestCompactSemanticJSONStoresStructuredRefs(t *testing.T) {
	raw := compactSemanticJSON(db.MemoryKindArtifact, "large tool output", []string{"artifact:abc", "message:7", "artifact:abc"})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("compactSemanticJSON invalid JSON: %v", err)
	}
	refs, _ := decoded["refs"].([]any)
	if decoded["kind"] != db.MemoryKindArtifact || decoded["summary"] == "" || len(refs) != 2 {
		t.Fatalf("expected compact structured refs, got %s", raw)
	}
}

func TestSummaryBuildersKeepSourceReferences(t *testing.T) {
	tool := buildToolOutputSummary("exec", "line one\nline two", "art-1", 80)
	file := buildFileSummary("internal/agent/prompt.go", "keeps decisions and constraints", 99)
	artifact := buildArtifactSummary("art-2", "text/plain", "preview text", 123)
	for _, got := range []string{tool, file, artifact} {
		if !strings.Contains(got, "Ref:") {
			t.Fatalf("expected ref-preserving summary, got %q", got)
		}
	}
}

func TestBuildHistorySummary(t *testing.T) {
	if got := buildHistorySummary(nil, 3); got != "" {
		t.Fatalf("expected empty summary, got %q", got)
	}

	rows := []db.Message{
		{ID: 1, Role: "user", Content: "first"},
		{ID: 2, Role: "", Content: "second"},
		{ID: 3, Role: "assistant", Content: "third"},
		{ID: 4, Role: "tool", Content: "fourth"},
		{ID: 5, Role: "user", Content: "fifth"},
		{ID: 6, Role: "assistant", Content: "sixth"},
		{ID: 7, Role: "user", Content: "seventh"},
	}
	got := buildHistorySummary(rows[:2], 5)
	if !strings.Contains(got, "- User Msg:1 first") || !strings.Contains(got, "- Message Msg:2 second") {
		t.Fatalf("expected roles and fallback labels, got %q", got)
	}
	got = buildHistorySummary(rows, 2)
	if strings.Contains(got, "Msg:5") || !strings.Contains(got, "Msg:6") || !strings.Contains(got, "Msg:7") {
		t.Fatalf("expected last two rows only, got %q", got)
	}
	got = buildHistorySummary(rows, 0)
	if strings.Contains(got, "Msg:1") || !strings.Contains(got, "Msg:2") || !strings.Contains(got, "Msg:7") {
		t.Fatalf("expected default maxItems=6 to keep the last six rows, got %q", got)
	}
}

func TestCleanRefs(t *testing.T) {
	tests := []struct {
		name string
		refs []string
		want []string
	}{
		{name: "all empty", refs: []string{"", "   "}, want: []string{}},
		{name: "all duplicates", refs: []string{"artifact:1", "artifact:1", " artifact:1 "}, want: []string{"artifact:1"}},
		{name: "mixed", refs: []string{" message:1 ", "", "artifact:2", "message:1", "artifact:3"}, want: []string{"message:1", "artifact:2", "artifact:3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanRefs(tc.refs)
			if len(got) != len(tc.want) {
				t.Fatalf("cleanRefs(%q)=%q, want %q", tc.refs, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cleanRefs(%q)=%q, want %q", tc.refs, got, tc.want)
				}
			}
		})
	}
}
