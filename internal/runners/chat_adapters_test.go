package runners

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
		{"codex", &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}, []string{"--ask-for-approval", "never", "-c", "mcp_servers={}", "exec", "--json", "--color", "never", "--skip-git-repo-check", "--sandbox", "workspace-write", "replay prompt"}},
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
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "output", Stream: "stderr", Chunk: "warn", Seq: 7})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "runner_output" || events[0].Stream != "stderr" || events[0].Text != "warn" || events[0].Seq != 7 {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestNormalizeGenericChatEventMapsStdoutToTextDelta(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "output", Stream: "stdout", Chunk: "hello", Seq: 3})
	if len(events) != 1 || events[0].Type != "text_delta" || events[0].Text != "hello" {
		t.Fatalf("unexpected normalized event: %#v", events)
	}
}

func TestCodexNormalizeStructuredAgentMessage(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	payload := json.RawMessage(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I’m here. What’s going on?"}}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 12})
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
		events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 13})
		if len(events) != 0 {
			t.Fatalf("expected lifecycle event to be suppressed, got %#v", events)
		}
	}
}

func TestCodexNormalizeStructuredTurnCompleted(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	payload := json.RawMessage(`{"type":"turn.completed","usage":{"input_tokens":24776}}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 13})
	if len(events) != 1 || events[0].Type != runtimeEventTurnCompleted {
		t.Fatalf("expected canonical turn completed event, got %#v", events)
	}
	assertPayloadField(t, events[0].Payload, "type", runtimeEventTurnCompleted)
	assertPayloadField(t, events[0].Payload, "state", "completed")
}

func TestCodexNormalizeSuppressesRawJSONStdout(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	chunk := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"hello"}}`
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "output", Stream: "stdout", Chunk: chunk, Seq: 14})
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
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 9})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "text_delta" || events[0].Text != "I'd need to know your location first." || events[0].Seq != 9 {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestOpenCodeNormalizeStructuredReasoningText(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"text","part":{"type":"text","kind":"reasoning","text":"I should keep this concise."}}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 9})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "reasoning_delta" || events[0].Text != "I should keep this concise." || events[0].Seq != 9 {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
	assertPayloadField(t, events[0].Payload, "stream_kind", runtimeStreamReasoningText)
}

func TestOpenCodeNormalizeStructuredReasoningTypePart(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"text","part":{"type":"reasoning","text":"The user wants a brief overview."}}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 10})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "reasoning_delta" || events[0].Text != "The user wants a brief overview." {
		t.Fatalf("unexpected normalized event: %#v", events[0])
	}
}

func TestOpenCodeNormalizeSuppressesStructuredStepEvents(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"step_start","messageID":"msg_123"}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 10})
	if len(events) != 0 {
		t.Fatalf("expected no visible event, got %#v", events)
	}
}

func TestOpenCodeNormalizeToolUse(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"tool_use","sessionID":"ses_123","part":{"type":"tool","tool":"webfetch","callID":"call_123","state":{"status":"completed","input":{"url":"https://example.com"},"output":"fetched docs"}}}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 10})
	if len(events) != 1 || events[0].Type != runtimeEventItemCompleted {
		t.Fatalf("expected completed tool event, got %#v", events)
	}
	assertPayloadField(t, events[0].Payload, "type", runtimeEventItemCompleted)
	assertPayloadField(t, events[0].Payload, "item_type", runtimeItemWebSearch)
	assertPayloadField(t, events[0].Payload, "status", "completed")
	assertPayloadField(t, events[0].Payload, "title", "webfetch")
}

func TestOpenCodeNormalizeWriteToolKeepsArguments(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	payload := json.RawMessage(`{"type":"message.part.updated","part":{"type":"tool","tool":"write","callID":"call_write","state":{"status":"running","title":"Writing app/main.go","input":{}},"path":"/tmp/project/app/main.go"}}`)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Payload: payload, Seq: 10})
	if len(events) != 1 || events[0].Type != runtimeEventItemUpdated {
		t.Fatalf("expected running write tool event, got %#v", events)
	}
	var normalized map[string]any
	if err := json.Unmarshal(events[0].Payload, &normalized); err != nil {
		t.Fatalf("unmarshal normalized payload: %v", err)
	}
	if normalized["item_type"] != runtimeItemFileChange {
		t.Fatalf("item_type = %v, want file_change", normalized["item_type"])
	}
	data, _ := normalized["data"].(map[string]any)
	input, _ := data["input"].(map[string]any)
	if input["path"] != "/tmp/project/app/main.go" {
		t.Fatalf("input = %#v, want path fallback", data["input"])
	}
	if data["callID"] != "call_write" || data["name"] != "write" {
		t.Fatalf("unexpected canonical tool data: %#v", data)
	}
}

func TestOpenCodeNormalizeSuppressesRawJSONStdout(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	chunk := `{"type":"text","part":{"type":"text","text":"hello"}}`
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "output", Stream: "stdout", Chunk: chunk, Seq: 11})
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

func TestOpenCodeBuildChatCommandReplayIgnoresNativeSessionRef(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildChatCommand(RunnerChatCommandRequest{
		ReplayPrompt:     "full replay prompt",
		UserMessage:      "continue from here",
		NativeSessionRef: "session_123",
		ContinuationMode: ContinuationReplay,
		Model:            "gpt-5",
	})
	if err != nil {
		t.Fatalf("BuildChatCommand: %v", err)
	}
	want := []string{"run", "--format", "json", "--model", "gpt-5", "full replay prompt"}
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
	ref, ok := adapter.ExtractNativeSessionRef(RunnerRunEvent{Type: "structured", Payload: payload})
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
			want:    []string{"--ask-for-approval", "never", "-c", "mcp_servers={}", "--cd", "/workspace", "--sandbox", "workspace-write", "exec", "resume", "--json", "--skip-git-repo-check", "thread_123", "continue from here"},
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
	want := []string{"-c", "mcp_servers={}", "--dangerously-bypass-approvals-and-sandbox", "exec", "resume", "--json", "--skip-git-repo-check", "thread_123", "continue from here"}
	assertArgsEqual(t, want, cmd.Args)
	for _, forbidden := range []string{"--color", "never", "latest", "full replay prompt that must not be sent"} {
		if contains(cmd.Args, forbidden) {
			t.Fatalf("codex native resume args contained forbidden %q: %v", forbidden, cmd.Args)
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
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.adapter.ExtractNativeSessionRef(RunnerRunEvent{Type: "structured", Payload: tt.payload})
			if !ok || got != tt.want {
				t.Fatalf("ExtractNativeSessionRef got %q ok=%v want %q", got, ok, tt.want)
			}
		})
	}
}

func TestCanonicalToolAndContentPayloads(t *testing.T) {
	codex := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := codex.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"method":"item/commandExecution/outputDelta","params":{"delta":"running tests\n"}}`)})
	if len(events) != 1 || events[0].Type != runtimeEventContentDelta || events[0].Text != "running tests\n" {
		t.Fatalf("unexpected command output event: %#v", events)
	}
	assertPayloadField(t, events[0].Payload, "type", runtimeEventContentDelta)
	assertPayloadField(t, events[0].Payload, "stream_kind", runtimeStreamCommandOutput)

	tool := codex.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 2, Payload: json.RawMessage(`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`)})
	if len(tool) != 1 || tool[0].Type != runtimeEventItemStarted {
		t.Fatalf("unexpected tool event: %#v", tool)
	}
	assertPayloadField(t, tool[0].Payload, "item_type", runtimeItemCommandExecution)
}

func TestCodexDeltaNormalizationPreservesWhitespace(t *testing.T) {
	codex := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := codex.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"method":"item/agentMessage/delta","params":{"delta":" hello "}}`)})
	if len(events) != 1 || events[0].Text != " hello " {
		t.Fatalf("expected assistant delta whitespace to be preserved, got %#v", events)
	}

	output := codex.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 2, Payload: json.RawMessage(`{"method":"item/commandExecution/outputDelta","params":{"delta":"line\n"}}`)})
	if len(output) != 1 || output[0].Text != "line\n" {
		t.Fatalf("expected command delta newline to be preserved, got %#v", output)
	}
}

func TestOpenCodeDeltaNormalizationPreservesWhitespace(t *testing.T) {
	opencode := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	events := opencode.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"message.part.delta","delta":" hello "}`)})
	if len(events) != 1 || events[0].Text != " hello " {
		t.Fatalf("expected assistant delta whitespace to be preserved, got %#v", events)
	}

	updated := opencode.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 2, Payload: json.RawMessage(`{"type":"message.part.updated","part":{"type":"text","text":" world "}}`)})
	if len(updated) != 1 || updated[0].Text != " world " {
		t.Fatalf("expected updated text whitespace to be preserved, got %#v", updated)
	}

	spaceOnly := opencode.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 3, Payload: json.RawMessage(`{"type":"message.part.delta","delta":" "}`)})
	if len(spaceOnly) != 1 || spaceOnly[0].Text != " " {
		t.Fatalf("expected whitespace-only assistant delta to be preserved, got %#v", spaceOnly)
	}
	if !strings.Contains(string(spaceOnly[0].Payload), `"delta":" "`) {
		t.Fatalf("expected canonical payload to preserve whitespace-only delta, got %s", string(spaceOnly[0].Payload))
	}
}

func TestUnknownProviderEventsStayBoundedAndRedacted(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	secret := "sk-" + strings.Repeat("x", 64)
	large := strings.Repeat("a", maxRawDiagnosticString+256)
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"mystery.event","api_key":"` + secret + `","message":"` + large + `"}`)})
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

func TestNormalizeOpenCodeTokenUsageEvent(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"token.usage","usage":{"input_tokens":120,"output_tokens":40,"cached_input_tokens":20,"total_tokens":160,"model":"claude-3-5-sonnet"}}`)})
	if len(events) != 1 || events[0].Type != runtimeEventTokenUsage {
		t.Fatalf("expected single token.usage event, got %#v", events)
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage map, got %#v", payload)
	}
	if got, _ := usage["input_tokens"].(float64); got != 120 {
		t.Fatalf("input_tokens = %v, want 120", usage["input_tokens"])
	}
	if got, _ := usage["output_tokens"].(float64); got != 40 {
		t.Fatalf("output_tokens = %v, want 40", usage["output_tokens"])
	}
	if got, _ := usage["cached_input_tokens"].(float64); got != 20 {
		t.Fatalf("cached_input_tokens = %v, want 20", usage["cached_input_tokens"])
	}
}

func TestNormalizeOpenCodeConfigWarningEvent(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"config.warning","code":"missing_api_key","message":"Anthropic key not set","kind":"missing"}`)})
	if len(events) != 1 || events[0].Type != runtimeEventConfigWarning {
		t.Fatalf("expected config.warning event, got %#v", events)
	}
	if !strings.Contains(string(events[0].Payload), `"code":"missing_api_key"`) {
		t.Fatalf("expected code in payload, got %s", string(events[0].Payload))
	}
}

func TestNormalizeOpenCodeModelRerouteEvent(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"model.reroute","from":"gpt-5","to":"gpt-5-mini","reason":"rate limit"}`)})
	if len(events) != 1 || events[0].Type != runtimeEventModelReroute {
		t.Fatalf("expected model.reroute event, got %#v", events)
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["from"] != "gpt-5" || payload["to"] != "gpt-5-mini" {
		t.Fatalf("expected from/to in payload, got %#v", payload)
	}
}

func TestNormalizeCodexTokenUsageEvent(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	events := adapter.NormalizeChatEvent(RunnerRunEvent{Type: "structured", Seq: 1, Payload: json.RawMessage(`{"type":"item/tokenCount/updated","usage":{"input_tokens":50,"output_tokens":30,"total_tokens":80}}`)})
	if len(events) != 1 || events[0].Type != runtimeEventTokenUsage {
		t.Fatalf("expected single token.usage event, got %#v", events)
	}
}

func TestExtractTokenUsageFromVariousShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]int64
	}{
		{"snake_case", `{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`, map[string]int64{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}},
		{"camelCase", `{"tokenUsage":{"inputTokens":10,"outputTokens":5,"totalTokens":15}}`, map[string]int64{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}},
		{"tokens", `{"tokens":{"input":1,"output":2,"total":3}}`, map[string]int64{"input_tokens": 1, "output_tokens": 2, "total_tokens": 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var obj map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &obj); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			usage := extractTokenUsage(obj)
			if usage == nil {
				t.Fatalf("expected usage, got nil")
			}
			for key, want := range tc.want {
				if got, ok := usage[key].(int64); !ok || got != want {
					t.Fatalf("usage[%q] = %v, want %d", key, usage[key], want)
				}
			}
		})
	}
}
