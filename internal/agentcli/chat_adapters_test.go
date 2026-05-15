package agentcli

import (
	"encoding/json"
	"strings"
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
		{"gemini", &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}, []string{"--prompt", "replay prompt", "--output-format", "stream-json", "--approval-mode", "auto_edit"}},
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
	} {
		events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 13})
		if len(events) != 0 {
			t.Fatalf("expected lifecycle event to be suppressed, got %#v", events)
		}
	}
}

func TestCodexNormalizeStructuredTurnCompleted(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	payload := json.RawMessage(`{"type":"turn.completed","usage":{"input_tokens":24776}}`)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 13})
	if len(events) != 1 || events[0].Type != runtimeEventTurnCompleted {
		t.Fatalf("expected canonical turn completed event, got %#v", events)
	}
	assertPayloadField(t, events[0].Payload, "type", runtimeEventTurnCompleted)
	assertPayloadField(t, events[0].Payload, "state", "completed")
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

func TestOpenCodeNormalizeToolUse(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"tool_use","sessionID":"ses_123","part":{"type":"tool","tool":"webfetch","callID":"call_123","state":{"status":"completed","input":{"url":"https://example.com"},"output":"fetched docs"}}}`)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 10})
	if len(events) != 1 || events[0].Type != runtimeEventItemCompleted {
		t.Fatalf("expected completed tool event, got %#v", events)
	}
	assertPayloadField(t, events[0].Payload, "type", runtimeEventItemCompleted)
	assertPayloadField(t, events[0].Payload, "item_type", runtimeItemWebSearch)
	assertPayloadField(t, events[0].Payload, "status", "completed")
	assertPayloadField(t, events[0].Payload, "title", "webfetch")
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

func TestNativeChatCommandsUseUserMessageNotReplayPrompt(t *testing.T) {
	tests := []struct {
		name    string
		adapter RunnerChatAdapter
		ref     string
		want    []string
	}{
		{
			name:    "codex",
			adapter: &CodexAdapter{spec: RunnerSpec{Binary: "codex"}},
			ref:     "thread_123",
			want:    []string{"--ask-for-approval", "never", "--cd", "/workspace", "--sandbox", "workspace-write", "exec", "resume", "--json", "--skip-git-repo-check", "thread_123", "continue from here"},
		},
		{
			name:    "claude",
			adapter: &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}},
			ref:     "session_123",
			want:    []string{"--bare", "--resume", "session_123", "-p", "continue from here", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-mode", "acceptEdits"},
		},
		{
			name:    "gemini",
			adapter: &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}},
			ref:     "session_123",
			want:    []string{"--resume", "session_123", "--prompt", "continue from here", "--output-format", "stream-json", "--approval-mode", "auto_edit"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.adapter.BuildChatCommand(RunnerChatCommandRequest{
				ReplayPrompt:     "full replay prompt that must not be sent",
				UserMessage:      "continue from here",
				NativeSessionRef: tt.ref,
				ContinuationMode: ContinuationNative,
				Mode:             string(RunnerModeSafeEdit),
				Cwd:              "/workspace",
			})
			if err != nil {
				t.Fatalf("BuildChatCommand: %v", err)
			}
			assertArgsEqual(t, tt.want, cmd.Args)
			for _, arg := range cmd.Args {
				if arg == "full replay prompt that must not be sent" {
					t.Fatalf("native command leaked replay prompt: %v", cmd.Args)
				}
				if arg == "latest" {
					t.Fatalf("native command used process-global latest continuation: %v", cmd.Args)
				}
			}
		})
	}
}

func TestCodexNativeResumeSandboxAutoUsesSupportedArgs(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildChatCommand(RunnerChatCommandRequest{
		ReplayPrompt:     "full replay prompt that must not be sent",
		UserMessage:      "continue from here",
		NativeSessionRef: "thread_123",
		ContinuationMode: ContinuationNative,
		Mode:             string(RunnerModeSandboxAuto),
	})
	if err != nil {
		t.Fatalf("BuildChatCommand: %v", err)
	}
	want := []string{"--dangerously-bypass-approvals-and-sandbox", "exec", "resume", "--json", "--skip-git-repo-check", "thread_123", "continue from here"}
	assertArgsEqual(t, want, cmd.Args)
	for _, forbidden := range []string{"--color", "never", "latest", "full replay prompt that must not be sent"} {
		if contains(cmd.Args, forbidden) {
			t.Fatalf("codex native resume args contained forbidden %q: %v", forbidden, cmd.Args)
		}
	}
}

func TestNativeFirstTurnUsesUserMessageAndStreamOutput(t *testing.T) {
	gemini := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	cmd, err := gemini.BuildChatCommand(RunnerChatCommandRequest{
		ReplayPrompt:     "full replay prompt",
		UserMessage:      "start native session",
		ContinuationMode: ContinuationNative,
		Mode:             string(RunnerModeSafeEdit),
	})
	if err != nil {
		t.Fatalf("BuildChatCommand: %v", err)
	}
	assertArgsEqual(t, []string{"--prompt", "start native session", "--output-format", "stream-json", "--approval-mode", "auto_edit"}, cmd.Args)
	if cmd.OutputMode != OutputJSONL {
		t.Fatalf("expected native Gemini chat to use JSONL output, got %q", cmd.OutputMode)
	}
}

func TestGeminiNormalizeStructuredResultEnvelope(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	payload := json.RawMessage(`{"session_id":"session_outer","response":"{\n \"session_id\": \"session_inner\",\n \"response\": \"I'm fully operational and ready to assist.\",\n \"stats\": {\"models\": {}}\n}","stats":{"models":{"gemini-3-flash-preview":{"api":{"totalRequests":1}}}}}`)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 42})
	if len(events) != 1 {
		t.Fatalf("expected one normalized event, got %#v", events)
	}
	if events[0].Type != "text_delta" {
		t.Fatalf("expected text_delta, got %#v", events[0])
	}
	if events[0].Text != "I'm fully operational and ready to assist." {
		t.Fatalf("unexpected Gemini normalized text: %q", events[0].Text)
	}
}

func TestGeminiNormalizeSuppressesUserEchoAndSuccessResult(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	for _, payload := range []json.RawMessage{
		json.RawMessage(`{"type":"init","session_id":"session_gemini"}`),
		json.RawMessage(`{"type":"message","role":"user","content":"System: replay prompt"}`),
		json.RawMessage(`{"type":"result","status":"success","stats":{"total_tokens":12}}`),
	} {
		events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: payload, Seq: 43})
		if len(events) != 0 {
			t.Fatalf("expected Gemini metadata/user echo to be suppressed, got %#v", events)
		}
	}
}

func TestGeminiNormalizeAssistantMessageDelta(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: json.RawMessage(`{"type":"message","role":"assistant","content":"I'm listening."}`), Seq: 44})
	if len(events) != 1 || events[0].Type != "text_delta" || events[0].Text != "I'm listening." {
		t.Fatalf("expected assistant message delta only, got %#v", events)
	}
}

func TestGeminiNormalizeToolUseAndResultShareStableCardKey(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	toolID := "google_web_search_1778396831085_0"
	started := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: json.RawMessage(`{"type":"tool_use","tool_name":"google_web_search","tool_id":"` + toolID + `","parameters":{"query":"vancouver news"}}`), Seq: 45})
	completed := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Payload: json.RawMessage(`{"type":"tool_result","tool_id":"` + toolID + `","status":"success","output":"Search results returned."}`), Seq: 46})
	if len(started) != 1 || started[0].Type != runtimeEventItemStarted {
		t.Fatalf("expected Gemini tool_use to start one item, got %#v", started)
	}
	if len(completed) != 1 || completed[0].Type != runtimeEventItemCompleted {
		t.Fatalf("expected Gemini tool_result to complete one item, got %#v", completed)
	}
	for _, event := range []RunnerChatEvent{started[0], completed[0]} {
		assertPayloadField(t, event.Payload, "item_type", runtimeItemWebSearch)
		assertPayloadField(t, event.Payload, "title", "google_web_search")
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		data, ok := payload["data"].(map[string]any)
		if !ok || data["id"] != toolID || data["name"] != "google_web_search" {
			t.Fatalf("expected stable Gemini tool data, got %#v", payload["data"])
		}
	}
}

func TestNativeSessionRefExtractors(t *testing.T) {
	cases := []struct {
		name    string
		adapter NativeRunnerChatAdapter
		payload json.RawMessage
		want    string
	}{
		{"codex", &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}, json.RawMessage(`{"type":"thread.started","thread_id":"thread_abc"}`), "thread_abc"},
		{"claude", &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}, json.RawMessage(`{"type":"system","subtype":"init","session_id":"session_claude"}`), "session_claude"},
		{"gemini", &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}, json.RawMessage(`{"type":"init","session_id":"session_gemini"}`), "session_gemini"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.adapter.ExtractNativeSessionRef(AgentRunEvent{Type: "structured", Payload: tt.payload})
			if !ok || got != tt.want {
				t.Fatalf("ExtractNativeSessionRef got %q ok=%v want %q", got, ok, tt.want)
			}
		})
	}
}

func TestCanonicalToolAndContentPayloads(t *testing.T) {
	codex := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := codex.NormalizeChatEvent(AgentRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"method":"item/commandExecution/outputDelta","params":{"delta":"running tests\n"}}`)})
	if len(events) != 1 || events[0].Type != runtimeEventContentDelta || events[0].Text != "running tests\n" {
		t.Fatalf("unexpected command output event: %#v", events)
	}
	assertPayloadField(t, events[0].Payload, "type", runtimeEventContentDelta)
	assertPayloadField(t, events[0].Payload, "stream_kind", runtimeStreamCommandOutput)

	tool := codex.NormalizeChatEvent(AgentRunEvent{Type: "structured", Seq: 2, Payload: json.RawMessage(`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`)})
	if len(tool) != 1 || tool[0].Type != runtimeEventItemStarted {
		t.Fatalf("unexpected tool event: %#v", tool)
	}
	assertPayloadField(t, tool[0].Payload, "item_type", runtimeItemCommandExecution)
}

func TestCodexDeltaNormalizationPreservesWhitespace(t *testing.T) {
	codex := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := codex.NormalizeChatEvent(AgentRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"method":"item/agentMessage/delta","params":{"delta":" hello "}}`)})
	if len(events) != 1 || events[0].Text != " hello " {
		t.Fatalf("expected assistant delta whitespace to be preserved, got %#v", events)
	}

	output := codex.NormalizeChatEvent(AgentRunEvent{Type: "structured", Seq: 2, Payload: json.RawMessage(`{"method":"item/commandExecution/outputDelta","params":{"delta":"line\n"}}`)})
	if len(output) != 1 || output[0].Text != "line\n" {
		t.Fatalf("expected command delta newline to be preserved, got %#v", output)
	}
}

func TestUnknownProviderEventsStayBoundedAndRedacted(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	secret := "sk-" + strings.Repeat("x", 64)
	large := strings.Repeat("a", maxRawDiagnosticString+256)
	events := adapter.NormalizeChatEvent(AgentRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"mystery.event","api_key":"` + secret + `","message":"` + large + `"}`)})
	if len(events) != 1 || events[0].Type != "runner_output" {
		t.Fatalf("expected bounded diagnostic runner_output, got %#v", events)
	}
	encoded := string(events[0].Payload)
	if strings.Contains(encoded, secret) {
		t.Fatalf("expected secret to be redacted, got %s", encoded)
	}
	if !strings.Contains(encoded, `"api_key":"[redacted]"`) {
		t.Fatalf("expected api_key redaction, got %s", encoded)
	}
	if !strings.Contains(encoded, "[truncated]") || len(encoded) > maxRawDiagnosticString+512 {
		t.Fatalf("expected bounded diagnostic payload, len=%d payload=%s", len(encoded), encoded)
	}
}

func assertPayloadField(t *testing.T, raw json.RawMessage, key, want string) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal payload: %v; raw=%s", err, string(raw))
	}
	if got, _ := obj[key].(string); got != want {
		t.Fatalf("payload[%q]=%q want %q in %s", key, got, want, string(raw))
	}
}
