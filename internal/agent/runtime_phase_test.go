package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

func TestRuntime_RunBackground_ValidatesAndPersistsResults(t *testing.T) {
	d := openRuntimeTestDB(t)
	artifactsDir := t.TempDir()
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "abcdefghijklmnopqrstuvwxyz"},
			},
		},
	}
	_, provider := buildChatServer(t, response)
	rt := &Runtime{
		DB:               d,
		Provider:         provider,
		Model:            "gpt-4",
		Tools:            tools.NewRegistry(),
		Builder:          &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops:     2,
		MaxToolBytes:     10,
		ToolPreviewBytes: 5,
		Artifacts:        &artifacts.Store{Dir: artifactsDir, DB: d},
	}

	if _, err := rt.RunBackground(context.Background(), BackgroundRunInput{}); err == nil || !strings.Contains(err.Error(), "background session key required") {
		t.Fatalf("expected missing session key error, got %v", err)
	}
	if _, err := rt.RunBackground(context.Background(), BackgroundRunInput{SessionKey: "bg-missing-prompt"}); err == nil || !strings.Contains(err.Error(), "background prompt snapshot required") {
		t.Fatalf("expected missing prompt error, got %v", err)
	}

	meta := map[string]any{"custom": "value", "profile_name": "safe-mode"}
	result, err := rt.RunBackground(context.Background(), BackgroundRunInput{
		SessionKey:       "bg-success",
		ParentSessionKey: "parent",
		Task:             "background task",
		PromptSnapshot: []providers.ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Do the task."},
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("RunBackground: %v", err)
	}
	if result.FinalText != "abcdefghijklmnopqrstuvwxyz" || result.ArtifactID == "" || result.Preview == "" {
		t.Fatalf("expected persisted artifact-backed result, got %#v", result)
	}
	if _, ok := meta["artifact_id"]; ok {
		t.Fatalf("expected input metadata to remain unchanged, got %#v", meta)
	}

	msgs, err := d.GetLastMessages(context.Background(), "bg-success", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("expected persisted user and assistant messages, got %#v", msgs)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msgs[1].PayloadJSON), &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload["parent_session_key"] != "parent" || payload["artifact_id"] != result.ArtifactID || payload["preview"] != result.Preview || payload["custom"] != "value" {
		t.Fatalf("unexpected assistant payload: %#v", payload)
	}
}

func TestRuntime_RunBackground_SerializesConcurrentCallsPerSession(t *testing.T) {
	d := openRuntimeTestDB(t)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			seen := maxInFlight.Load()
			if current <= seen || maxInFlight.CompareAndSwap(seen, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "done"},
			}},
		})
	}))
	defer server.Close()
	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	rt := buildSimpleRuntime(t, provider, d, &mockDeliverer{})

	const workers = 8
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := rt.RunBackground(context.Background(), BackgroundRunInput{
				SessionKey: "bg-race",
				Task:       fmt.Sprintf("background task %d", i),
				PromptSnapshot: []providers.ChatMessage{
					{Role: "system", Content: "You are helpful."},
					{Role: "user", Content: "Do the task."},
				},
			})
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("RunBackground concurrent call failed: %v", err)
		}
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("expected session lock to serialize provider calls, got max in-flight %d", got)
	}
	msgs, err := d.GetLastMessages(context.Background(), "bg-race", 2*workers+1)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 2*workers {
		t.Fatalf("expected %d persisted messages, got %d", 2*workers, len(msgs))
	}
	rt.locksMu.Lock()
	defer rt.locksMu.Unlock()
	if len(rt.locks) != 0 {
		t.Fatalf("expected background session locks to be released, got %#v", rt.locks)
	}
}

func TestRuntime_HandleTurnPostCleanup_DisableRollingConsolidationSkipsScheduler(t *testing.T) {
	triggered := make(chan struct{}, 1)
	rt := &Runtime{
		DisableRollingConsolidation: true,
		Consolidator:                &memory.Consolidator{Provider: &providers.Client{}},
		Builder:                     &Builder{HistoryMax: 10},
		ConsolidationScheduler: memory.NewScheduler(time.Second, func(context.Context, string) {
			triggered <- struct{}{}
		}),
	}
	rt.handleTurnPostCleanup(context.Background(), bus.Event{SessionKey: "sess-cleanup"})
	select {
	case <-triggered:
		t.Fatal("expected rolling consolidation to stay disabled")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRuntime_BoundTextResult_ExactMaxToolBytesKeepsInlineText(t *testing.T) {
	d := openRuntimeTestDB(t)
	rt := &Runtime{
		DB:               d,
		MaxToolBytes:     10,
		ToolPreviewBytes: 10,
		Artifacts:        &artifacts.Store{Dir: t.TempDir(), DB: d},
	}
	text := "1234567890"
	stored, preview, artifactID := rt.boundTextResult(context.Background(), "sess-inline", text)
	if stored != text || preview != text || artifactID != "" {
		t.Fatalf("expected exact-boundary text to stay inline, got stored=%q preview=%q artifact=%q", stored, preview, artifactID)
	}
}

func TestIsNewSessionCommand_TrimsWhitespace(t *testing.T) {
	cases := map[string]bool{
		"/new":       true,
		" /new  ":    true,
		"\n/clear\t": true,
		"/new now":   false,
		"":           false,
	}
	for input, want := range cases {
		if got := isNewSessionCommand(input); got != want {
			t.Fatalf("isNewSessionCommand(%q)=%v want %v", input, got, want)
		}
	}
}

func TestDeliveryTarget_FallsBackToSenderWhenMetaMissing(t *testing.T) {
	if got := deliveryTarget(bus.Event{From: "fallback-user"}); got != "fallback-user" {
		t.Fatalf("expected sender fallback, got %q", got)
	}
	ev := bus.Event{From: "fallback-user", Meta: map[string]any{"channel_id": nil, "chat_id": "target-1"}}
	if got := deliveryTarget(ev); got != "target-1" {
		t.Fatalf("expected metadata target, got %q", got)
	}
}

func TestCloneMap_NilReturnsEmptyMap(t *testing.T) {
	cloned := cloneMap(nil)
	if cloned == nil || len(cloned) != 0 {
		t.Fatalf("expected non-nil empty clone, got %#v", cloned)
	}
	cloned["key"] = "value"
	if len(cloneMap(nil)) != 0 {
		t.Fatal("expected fresh empty clone for nil map input")
	}
}

func TestReleaseEvent_IgnoresNilDoneCallbacks(t *testing.T) {
	releaseEvent(bus.Event{})
	releaseEvent(bus.Event{Meta: map[string]any{heartbeat.MetaKeyDone: nil}})
	called := false
	releaseEvent(bus.Event{Meta: map[string]any{heartbeat.MetaKeyDone: func() { called = true }}})
	if !called {
		t.Fatal("expected done callback to run when present")
	}
}

func TestShouldAutoDeliver_EmailCoercesStringValues(t *testing.T) {
	cases := []struct {
		name string
		ev   bus.Event
		want bool
	}{
		{name: "no meta defaults true", ev: bus.Event{Channel: "email"}, want: true},
		{name: "bool false", ev: bus.Event{Channel: "email", Meta: map[string]any{"auto_reply_enabled": false}}, want: false},
		{name: "string true", ev: bus.Event{Channel: "email", Meta: map[string]any{"auto_reply_enabled": "true"}}, want: true},
		{name: "string yes rejected", ev: bus.Event{Channel: "email", Meta: map[string]any{"auto_reply_enabled": "yes"}}, want: false},
		{name: "string no rejected", ev: bus.Event{Channel: "email", Meta: map[string]any{"auto_reply_enabled": "no"}}, want: false},
	}
	for _, tc := range cases {
		if got := shouldAutoDeliver(tc.ev); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestRuntime_ExecuteConversation_ContextDeadlineExceededMidLoop(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNumber := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if callNumber == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"id":   "tc-timeout",
							"type": "function",
							"function": map[string]any{
								"name":      "echo_tool",
								"arguments": `{}`,
							},
						}},
					},
				}},
			})
			return
		}
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "too late"},
			}},
		})
	}))
	defer server.Close()
	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})
	rt := &Runtime{
		DB:           openRuntimeTestDB(t),
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      &Builder{HistoryMax: 10},
		MaxToolLoops: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := rt.executeConversation(ctx, bus.EventUserMessage, "sess-deadline", []providers.ChatMessage{{Role: "user", Content: "hello"}}, reg, "cli", "user", nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestRuntime_HandleToolLoopLimitExceeded_AskModeRequiresBroker(t *testing.T) {
	rt := &Runtime{MaxToolLoopsExceededAction: config.QuotaExceededActionAsk}
	err := rt.handleToolLoopLimitExceeded(context.Background(), "sess-loop", 2)
	if err == nil || !strings.Contains(err.Error(), "approval broker is unavailable") {
		t.Fatalf("expected approval broker error, got %v", err)
	}
}

func TestRuntime_ExecuteConversation_AllowsNilContentWithoutToolCalls(t *testing.T) {
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: nil},
			},
		},
	}
	_, provider := buildChatServer(t, response)
	rt := &Runtime{DB: openRuntimeTestDB(t), Provider: provider, Model: "gpt-4", Builder: &Builder{HistoryMax: 10}}
	finalText, streamed, err := rt.executeConversation(context.Background(), bus.EventUserMessage, "sess-empty", []providers.ChatMessage{{Role: "user", Content: "hello"}}, nil, "cli", "user", nil)
	if err != nil || streamed || finalText != "" {
		t.Fatalf("expected empty final text without error, got text=%q streamed=%v err=%v", finalText, streamed, err)
	}
}

func TestRuntime_NarrateApprovalRequired_HandlesNilRuntimeAndEmptyContent(t *testing.T) {
	var rt *Runtime
	if text, streamed := rt.narrateApprovalRequired(context.Background(), nil); text != "" || streamed {
		t.Fatalf("expected nil runtime to return empty narration, got text=%q streamed=%v", text, streamed)
	}

	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "   "},
			},
		},
	}
	_, provider := buildChatServer(t, response)
	runtime := &Runtime{Provider: provider, Model: "gpt-4"}
	if text, streamed := runtime.narrateApprovalRequired(context.Background(), nil); text != "" || streamed {
		t.Fatalf("expected empty provider content to suppress narration, got text=%q streamed=%v", text, streamed)
	}
}

func TestTerminalToolResultText_IgnoresInvalidJSONAndNilOK(t *testing.T) {
	if got := terminalToolResultText("read_skill", "{not-json"); got != "" {
		t.Fatalf("expected invalid JSON to be ignored, got %q", got)
	}
	if got := terminalToolResultText("read_skill", `{"kind":"skill_read","summary":"Skill is unavailable"}`); got != "" {
		t.Fatalf("expected nil ok field to be ignored, got %q", got)
	}
}

func TestRuntime_ScheduleIdlePrune_DefaultsForNonPositiveDelay(t *testing.T) {
	d := openRuntimeTestDB(t)
	rt := &Runtime{
		DB: d,
		ContextManager: config.ContextManagerConfig{
			Enabled:          true,
			IdlePruneSeconds: 0,
		},
		Consolidator: &memory.Consolidator{Provider: &providers.Client{}},
	}
	ev := bus.Event{SessionKey: "idle-zero", Channel: "cli", From: "user"}
	rt.scheduleIdlePrune(context.Background(), ev)
	assertIdleTimerScheduled(t, rt, "idle-zero")

	rt.ContextManager.IdlePruneSeconds = -5
	ev.SessionKey = "idle-negative"
	rt.scheduleIdlePrune(context.Background(), ev)
	assertIdleTimerScheduled(t, rt, "idle-negative")
}

func TestIsDirectMessageEvent_CoversAllChannels(t *testing.T) {
	cases := []struct {
		name string
		ev   bus.Event
		want bool
	}{
		{name: "telegram private", ev: bus.Event{Channel: "telegram", Meta: map[string]any{"chat_type": "private"}}, want: true},
		{name: "telegram group", ev: bus.Event{Channel: "telegram", Meta: map[string]any{"chat_type": "group"}}, want: false},
		{name: "slack im", ev: bus.Event{Channel: "slack", Meta: map[string]any{"channel_type": "im"}}, want: true},
		{name: "slack channel", ev: bus.Event{Channel: "slack", Meta: map[string]any{"channel_type": "channel"}}, want: false},
		{name: "discord private", ev: bus.Event{Channel: "discord", Meta: map[string]any{"is_private": true}}, want: true},
		{name: "discord guild fallback", ev: bus.Event{Channel: "discord", Meta: map[string]any{"guild_id": ""}}, want: true},
		{name: "discord guild channel", ev: bus.Event{Channel: "discord", Meta: map[string]any{"guild_id": "123"}}, want: false},
		{name: "whatsapp direct", ev: bus.Event{Channel: "whatsapp", Meta: map[string]any{"is_group": false}}, want: true},
		{name: "whatsapp group", ev: bus.Event{Channel: "whatsapp", Meta: map[string]any{"is_group": true}}, want: false},
		{name: "email", ev: bus.Event{Channel: "email", Meta: map[string]any{"from": "a@example.com"}}, want: true},
		{name: "unknown", ev: bus.Event{Channel: "cli", Meta: map[string]any{}}, want: false},
	}
	for _, tc := range cases {
		if got := isDirectMessageEvent(tc.ev); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestRuntime_HandleNewSession_WithBuilderAndNoConsolidatorStillResets(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	if _, err := d.AppendMessage(ctx, "sess-reset", "user", "hello", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "sess-reset", "assistant", "world", nil); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	deliver := &mockDeliverer{}
	rt := &Runtime{DB: d, Builder: &Builder{DB: d, HistoryMax: 10}, Deliver: deliver}
	if err := rt.handleNewSession(ctx, bus.Event{SessionKey: "sess-reset", Channel: "cli", From: "user"}); err != nil {
		t.Fatalf("handleNewSession: %v", err)
	}
	msgs, err := d.GetLastMessages(ctx, "sess-reset", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected session history to reset, got %#v", msgs)
	}
	if len(deliver.messages) != 1 || deliver.messages[0] != "New session started." {
		t.Fatalf("expected reset confirmation delivery, got %#v", deliver.messages)
	}
}

func TestRuntime_ReleaseSessionLock_IgnoresNilRuntime(t *testing.T) {
	var rt *Runtime
	rt.releaseSessionLock("sess", nil)
	rt.releaseSessionLock("sess", &sessionLock{refs: 1})
}

func TestRuntime_EnsureSessionScope_PreservesExistingScopeLink(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	if err := d.LinkSession(ctx, "telegram:123", "cli:default", map[string]any{"existing": true}); err != nil {
		t.Fatalf("LinkSession: %v", err)
	}
	rt := &Runtime{
		DB:                 d,
		DefaultScopeKey:    "cli:default",
		LinkDirectMessages: true,
	}
	rt.ensureSessionScope(ctx, bus.Event{
		SessionKey: "telegram:123",
		Channel:    "telegram",
		Meta:       map[string]any{"chat_type": "private"},
	})
	scopeKey, err := d.ResolveScopeKey(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("ResolveScopeKey: %v", err)
	}
	if scopeKey != "cli:default" {
		t.Fatalf("expected existing scope link to remain unchanged, got %q", scopeKey)
	}
}

func assertIdleTimerScheduled(t *testing.T, rt *Runtime, sessionKey string) {
	t.Helper()
	rt.idleMu.Lock()
	defer rt.idleMu.Unlock()
	timer := rt.idleTimers[sessionKey]
	if timer == nil {
		t.Fatalf("expected idle timer for session %s", sessionKey)
	}
	timer.Stop()
	delete(rt.idleTimers, sessionKey)
}
