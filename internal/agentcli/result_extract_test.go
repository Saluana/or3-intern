package agentcli

import (
	"encoding/json"
	"testing"
)

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return payload
}

func TestFinalTextExtractorPrefersBestAndLatestCandidate(t *testing.T) {
	extractor := newFinalTextExtractor(RunnerCodex)
	extractor.Consider(rawJSON(t, map[string]any{"type": "message", "role": "assistant", "message": "generic"}))
	extractor.Consider(rawJSON(t, map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "runner specific"}}))
	if got := extractor.Text(); got != "runner specific" {
		t.Fatalf("expected runner specific candidate, got %q", got)
	}
	extractor.Consider(rawJSON(t, map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "latest winner"}}))
	if got := extractor.Text(); got != "latest winner" {
		t.Fatalf("expected latest equal-score candidate, got %q", got)
	}
}

func TestExtractFinalTextCandidateRunnerSpecificAndGeneric(t *testing.T) {
	cases := []struct {
		name   string
		runner RunnerID
		value  any
		score  int
		text   string
	}{
		{name: "gemini response", runner: RunnerGemini, value: map[string]any{"response": "Gemini done"}, score: 100, text: "Gemini done"},
		{name: "claude assistant", runner: RunnerClaude, value: map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "part one"}, map[string]any{"type": "text", "text": "part two"}}}}, score: 85, text: "part one\n\npart two"},
		{name: "claude success result", runner: RunnerClaude, value: map[string]any{"type": "result", "subtype": "success", "result": "final answer"}, score: 100, text: "final answer"},
		{name: "codex content fallback", runner: RunnerCodex, value: map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "content": "codex content"}}, score: 95, text: "codex content"},
		{name: "opencode text part", runner: RunnerOpenCode, value: map[string]any{"type": "text", "part": map[string]any{"type": "text", "text": "delta"}}, score: 100, text: "delta"},
		{name: "generic assistant", runner: "", value: map[string]any{"type": "assistant", "message": "assistant text"}, score: 90, text: "assistant text"},
		{name: "generic assistant message role", runner: "", value: map[string]any{"type": "message", "role": "assistant", "text": "assistant role text"}, score: 75, text: "assistant role text"},
		{name: "machine oriented suppressed", runner: "", value: map[string]any{"message": "{\"json\":true}"}, score: 0, text: ""},
		{name: "tool result suppressed", runner: "", value: map[string]any{"type": "tool_result", "result": "ignore me"}, score: 0, text: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score, text := extractFinalTextCandidate(tc.runner, tc.value)
			if score != tc.score || text != tc.text {
				t.Fatalf("expected (%d,%q), got (%d,%q)", tc.score, tc.text, score, text)
			}
		})
	}
}

func TestResultExtractHelpers(t *testing.T) {
	if got := extractClaudeAssistantText(map[string]any{"content": "single string"}); got != "single string" {
		t.Fatalf("expected string fallback, got %q", got)
	}
	if got := extractClaudeAssistantText("nope"); got != "" {
		t.Fatalf("expected empty assistant text, got %q", got)
	}
	if got := extractTextPart(map[string]any{"type": "tool", "text": "ignore"}); got != "" {
		t.Fatalf("expected non-text part to be ignored, got %q", got)
	}
	if got := extractTextPart(map[string]any{"text": "plain text"}); got != "plain text" {
		t.Fatalf("expected text part, got %q", got)
	}
	if got := extractString([]any{"one", map[string]any{"text": "two"}}); got != "one\n\ntwo" {
		t.Fatalf("unexpected slice extraction: %q", got)
	}
	if got := extractString(map[string]any{"result": "done"}); got != "done" {
		t.Fatalf("unexpected map extraction: %q", got)
	}
	if got := extractString(42); got != "" {
		t.Fatalf("expected unsupported type to be empty, got %q", got)
	}
	if !looksMachineOriented("") || !looksMachineOriented("{\"ok\":true}") || !looksMachineOriented("[]") {
		t.Fatal("expected empty and JSON-looking text to be machine oriented")
	}
	if looksMachineOriented("human readable") {
		t.Fatal("expected plain text to be human-oriented")
	}
}
