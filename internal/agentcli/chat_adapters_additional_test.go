package agentcli

import (
	"encoding/json"
	"testing"
)

func TestChatAdapterSessionRefHelpersAndUnknownTypes(t *testing.T) {
	nested := map[string]any{
		"data": []any{
			map[string]any{"info": map[string]any{"id": "short"}},
			map[string]any{"session": map[string]any{"session_id": "session_deep_123"}},
		},
	}
	if got := findSessionRef(nested); got != "session_deep_123" {
		t.Fatalf("expected deep session ref, got %q", got)
	}
	cases := map[string]bool{"": false, "   ": false, "short": false, "12345678": true, "session_abc": true, "ses_1": true}
	for value, want := range cases {
		if got := looksSessionID(value); got != want {
			t.Fatalf("looksSessionID(%q)=%v want %v", value, got, want)
		}
	}

	codex := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	if events := codex.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: json.RawMessage(`{"type":"unknown"}`)}); len(events) != 1 || events[0].Type != "runner_output" {
		t.Fatalf("expected unknown codex payload to become diagnostics, got %#v", events)
	}
	opencode := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	if events := opencode.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: json.RawMessage(`{"type":"unknown"}`)}); len(events) != 1 || events[0].Type != "runner_output" {
		t.Fatalf("expected unknown opencode payload to become diagnostics, got %#v", events)
	}
}

func TestChatAdapterHelperFunctions(t *testing.T) {
	if text, ok := openCodeTextPart(map[string]any{"text": "hello"}); !ok || text != "hello" {
		t.Fatalf("expected openCodeTextPart to extract text, got %q ok=%v", text, ok)
	}
	if _, ok := openCodeTextPart(map[string]any{"type": "tool", "text": "ignore"}); ok {
		t.Fatal("expected non-text openCode part to be rejected")
	}
	if got := extractCodexAgentMessageText(map[string]any{"item": map[string]any{"type": "agent_message", "content": []any{"first", "second"}}}); got != "first\n\nsecond" {
		t.Fatalf("unexpected codex content extraction: %q", got)
	}
	payload := []byte(`{"data":[{"id":"ignore"},{"session":{"sessionId":"session_nested"}}]}`)
	if got := extractOpenCodeSessionRefJSON(payload); got != "session_nested" {
		t.Fatalf("unexpected extracted session ref: %q", got)
	}
}
