package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/providers"
)

// mockStreamWriter records all deltas and close/abort calls.
type mockStreamWriter struct {
	deltas  []string
	closed  bool
	aborted bool
}

func (w *mockStreamWriter) WriteDelta(_ context.Context, text string) error {
	w.deltas = append(w.deltas, text)
	return nil
}

func (w *mockStreamWriter) Close(_ context.Context, _ string) error {
	w.closed = true
	return nil
}

func (w *mockStreamWriter) Abort(_ context.Context) error {
	w.aborted = true
	return nil
}

// mockStreamer implements channels.StreamingChannel using mockStreamWriter.
type mockStreamer struct {
	writer *mockStreamWriter
}

func (s *mockStreamer) BeginStream(_ context.Context, _ string, _ map[string]any) (channels.StreamWriter, error) {
	return s.writer, nil
}

// buildSSEServer creates a test server that returns an SSE stream.
func buildSSEServer(t *testing.T, sseLines []string) (*httptest.Server, *providers.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range sseLines {
			fmt.Fprintln(w, l)
		}
	}))
	t.Cleanup(srv.Close)
	c := providers.New(srv.URL, "test-key", 10*time.Second)
	c.HTTP = srv.Client()
	return srv, c
}

func TestRuntime_Streaming_FinalAnswer(t *testing.T) {
	d := openRuntimeTestDB(t)
	sseLines := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":" streamed"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}
	_, provider := buildSSEServer(t, sseLines)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	writer := &mockStreamWriter{}
	streamer := &mockStreamer{writer: writer}
	rt.Streamer = streamer

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-stream",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Streamer was used: deltas should have been written
	if len(writer.deltas) == 0 {
		t.Error("expected deltas written to stream writer")
	}
	combined := ""
	for _, d := range writer.deltas {
		combined += d
	}
	if combined != "Hello streamed" {
		t.Errorf("expected 'Hello streamed', got %q", combined)
	}
	if !writer.closed {
		t.Error("expected stream writer to be closed")
	}

	// When streaming, the Deliver method should NOT be called (to avoid double output)
	if len(deliver.messages) != 0 {
		t.Errorf("expected no delivered messages (already streamed), got %v", deliver.messages)
	}
}

func TestRuntime_Streaming_FallbackWhenNoStreamer(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "No streaming here"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)
	// rt.Streamer is nil - should fall back to normal Deliver

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-fallback",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) == 0 {
		t.Error("expected message delivered via Deliver (no streamer)")
	}
	if deliver.messages[0] != "No streaming here" {
		t.Errorf("expected 'No streaming here', got %q", deliver.messages[0])
	}
}

func TestRuntime_Streaming_AbortOnToolCalls(t *testing.T) {
	d := openRuntimeTestDB(t)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: SSE response with a tool call (no text content)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"exec","arguments":"{\"cmd\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		} else {
			// Second call: SSE final answer
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"id":"2","choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		}
	}))
	defer srv.Close()

	prov := providers.New(srv.URL, "key", 10*time.Second)
	prov.HTTP = srv.Client()

	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, prov, d, deliver)

	writers := []*mockStreamWriter{}
	rt.Streamer = &funcStreamer{fn: func() (channels.StreamWriter, error) {
		w := &mockStreamWriter{}
		writers = append(writers, w)
		return w, nil
	}}

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-tool",
		Channel:    "cli",
		From:       "user",
		Message:    "run something",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// With lazy stream init, BeginStream is only called when there are text
	// deltas. The first turn has only tool calls (no text), so no writer is
	// created for it. Only the final text-only turn creates a writer.
	if len(writers) != 1 {
		t.Fatalf("expected exactly 1 stream writer for the final answer, got %d", len(writers))
	}
	if writers[0].aborted {
		t.Error("did not expect aborted stream output on final answer")
	}
	if !writers[0].closed {
		t.Error("expected final stream writer to be closed")
	}
}

// funcStreamer allows a custom function to create writers.
type funcStreamer struct {
	fn func() (channels.StreamWriter, error)
}

func (s *funcStreamer) BeginStream(_ context.Context, _ string, _ map[string]any) (channels.StreamWriter, error) {
	return s.fn()
}
