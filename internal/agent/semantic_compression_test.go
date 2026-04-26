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
