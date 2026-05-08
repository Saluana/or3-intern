package agentcli

import (
	"encoding/json"
	"testing"
)

func TestChatAdaptersBuildReplayCommands(t *testing.T) {
	tests := []struct {
		name    string
		adapter RunnerChatAdapter
		want    []string
	}{
		{"opencode", &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}, []string{"run", "--format", "json", "replay prompt"}},
		{"codex", &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}, []string{"--ask-for-approval", "never", "exec", "--json", "--color", "never", "--skip-git-repo-check", "--sandbox", "workspace-write", "replay prompt"}},
		{"claude", &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}, []string{"--bare", "-p", "replay prompt", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-mode", "acceptEdits"}},
		{"gemini", &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}, []string{"--prompt", "replay prompt", "--output-format", "json", "--approval-mode", "auto_edit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.adapter.BuildChatCommand(RunnerChatCommandRequest{
				ReplayPrompt: "replay prompt",
				Mode:         "safe_edit",
			})
			if err != nil {
				t.Fatalf("BuildChatCommand: %v", err)
			}
			assertArgsEqual(t, tt.want, cmd.Args)
		})
	}
}

func TestNormalizeGenericChatEventKeepsRawOutput(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "output", Stream: "stderr", Chunk: "warn", Seq: 7})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "runner_output" || events[0].Stream != "stderr" || events[0].Text != "warn" || events[0].Seq != 7 {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestNormalizeGenericChatEventMapsStdoutToTextDelta(t *testing.T) {
	adapter := &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "output", Stream: "stdout", Chunk: "hello", Seq: 3})
	if len(events) != 1 || events[0].Type != "text_delta" || events[0].Text != "hello" {
		t.Fatalf("unexpected normalized event: %#v", events)
	}
}

func TestCodexNormalizeStructuredAgentMessage(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	payload := json.RawMessage(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I’m here. What’s going on?"}}`)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 12})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "text_delta" || events[0].Text != "I’m here. What’s going on?" || events[0].Seq != 12 {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestCodexNormalizeSuppressesLifecycleJSON(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	for _, payload := range []json.RawMessage{
		json.RawMessage(`{"type":"thread.started","thread_id":"019e05e3-0fc3-7c01-a899-f2efc92c55de"}`),
		json.RawMessage(`{"type":"turn.started"}`),
		json.RawMessage(`{"type":"turn.completed","usage":{"input_tokens":24776}}`),
	} {
		events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 13})
		if len(events) != 0 {
			t.Fatalf("expected lifecycle event to be suppressed, got %#v", events)
		}
	}
}

func TestCodexNormalizeSuppressesRawJSONStdout(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	chunk := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"hello"}}`
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "output", Stream: "stdout", Chunk: chunk, Seq: 14})
	if len(events) != 1 {
		t.Fatalf("expected one suppression event, got %d", len(events))
	}
	if events[0].Type != "runner_output" || events[0].Text != "" || events[0].Stream != "stdout" {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestOpenCodeNormalizeStructuredText(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"text","part":{"type":"text","text":"I'd need to know your location first."}}`)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 9})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "text_delta" || events[0].Text != "I'd need to know your location first." || events[0].Seq != 9 {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestOpenCodeNormalizeSuppressesStructuredStepEvents(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"step_start","messageID":"msg_123"}`)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 10})
	if len(events) != 0 {
		t.Fatalf("expected no visible event, got %#v", events)
	}
}

func TestOpenCodeNormalizeSuppressesRawJSONStdout(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	chunk := `{"type":"text","part":{"type":"text","text":"hello"}}`
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "output", Stream: "stdout", Chunk: chunk, Seq: 11})
	if len(events) != 1 {
		t.Fatalf("expected one suppression event, got %d", len(events))
	}
	if events[0].Type != "runner_output" || events[0].Text != "" || events[0].Stream != "stdout" {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestOpenCodeBuildChatCommandNativeUsesSessionAndUserMessage(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildChatCommand(RunnerChatCommandRequest{
		ReplayPrompt:     "full replay prompt",
		UserMessage:      "continue from here",
		NativeSessionRef: "session_123",
		ContinuationMode: ContinuationNative,
		Model:            "gpt-5",
		Mode:             "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildChatCommand: %v", err)
	}
	want := []string{"run", "--format", "json", "--session", "session_123", "--model", "gpt-5", "--dangerously-skip-permissions", "continue from here"}
	assertArgsEqual(t, want, cmd.Args)
}

func TestOpenCodeExtractNativeSessionRef(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload, err := json.Marshal(map[string]any{
		"type":      "message.part.updated",
		"sessionID": "session_abc123",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ref, ok := adapter.ExtractNativeSessionRef(AgentRunEvent{Type: "structured", Payload: payload})
	if !ok {
		t.Fatalf("expected native session ref to be extracted")
	}
	if ref != "session_abc123" {
		t.Fatalf("expected session_abc123, got %q", ref)
	}
}
