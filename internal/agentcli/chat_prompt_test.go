package agentcli

import (
	"strings"
	"testing"
)

func TestBuildReplayPromptPreservesNewestTurnsAndMarksTruncation(t *testing.T) {
	turns := []RunnerChatTurn{
		{Sequence: 1, UserText: "old user", FinalText: "old assistant", Status: "succeeded"},
		{Sequence: 2, UserText: "new user", FinalText: "new assistant", Status: "succeeded"},
	}
	prompt := BuildReplayPromptBounded(turns, "next question", 1, 4096)
	if strings.Contains(prompt, "old user") {
		t.Fatalf("oldest turn should be truncated, prompt=%q", prompt)
	}
	for _, want := range []string{"Earlier turns were truncated", "new user", "new assistant", "next question"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildReplayPromptSkipsIncompleteTurns(t *testing.T) {
	prompt := BuildReplayPromptBounded([]RunnerChatTurn{
		{Sequence: 1, UserText: "running", FinalText: "partial", Status: "running"},
		{Sequence: 2, UserText: "done", FinalText: "answer", Status: "completed"},
	}, "next", 10, 4096)
	if strings.Contains(prompt, "running") {
		t.Fatalf("running turn leaked into prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "done") || !strings.Contains(prompt, "answer") {
		t.Fatalf("completed turn missing: %s", prompt)
	}
}

func TestBuildReplayPromptByteLimitKeepsUserMessage(t *testing.T) {
	prompt := BuildReplayPromptBounded([]RunnerChatTurn{
		{Sequence: 1, UserText: strings.Repeat("u", 2048), FinalText: strings.Repeat("a", 2048), Status: "succeeded"},
	}, "final user input", 10, 400)
	if !strings.Contains(prompt, "final user input") {
		t.Fatalf("new message must be preserved: %s", prompt)
	}
	if len(prompt) > 900 {
		t.Fatalf("prompt did not respect byte bound enough: len=%d", len(prompt))
	}
}

func TestBuildReplayPromptSanitizesAssistantJSONEnvelope(t *testing.T) {
	prompt := BuildReplayPromptBounded([]RunnerChatTurn{
		{Sequence: 1, UserText: "you working?", FinalText: `{"session_id":"old","response":"I'm ready.","stats":{"models":{}}}`, Status: "succeeded"},
	}, "next", 10, 4096)
	if !strings.Contains(prompt, "Assistant: I'm ready.") {
		t.Fatalf("expected sanitized assistant response in prompt: %s", prompt)
	}
	if strings.Contains(prompt, "session_id") || strings.Contains(prompt, "stats") {
		t.Fatalf("expected metadata to be removed from replay prompt: %s", prompt)
	}
}
