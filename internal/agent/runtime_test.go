package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

// mockDeliverer captures delivered messages
type mockDeliverer struct {
	messages []string
	channel  string
	to       string
	meta     map[string]any
	err      error
}

func (m *mockDeliverer) Deliver(ctx context.Context, channel, to, text string) error {
	m.messages = append(m.messages, text)
	m.channel = channel
	m.to = to
	return m.err
}

func (m *mockDeliverer) DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error {
	m.meta = channels.CloneMeta(meta)
	return m.Deliver(ctx, channel, to, text)
}

// buildChatServer creates a test HTTP server that responds to /chat/completions
func buildChatServer(t *testing.T, response providers.ChatCompletionResponse) (*httptest.Server, *providers.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	c := providers.New(srv.URL, "test-key", 10*time.Second)
	c.HTTP = srv.Client()
	return srv, c
}

func openRuntimeTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func buildSimpleRuntime(t *testing.T, provider *providers.Client, d *db.DB, deliver *mockDeliverer) *Runtime {
	t.Helper()
	reg := tools.NewRegistry()
	b := &Builder{
		DB:         d,
		HistoryMax: 10,
	}
	return &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      b,
		MaxToolLoops: 2,
		Deliver:      deliver,
	}
}

func TestRuntime_Handle_UserMessage(t *testing.T) {
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
			}{Role: "assistant", Content: "Hello there!"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) == 0 {
		t.Error("expected at least one delivered message")
	}
	if deliver.messages[0] != "Hello there!" {
		t.Errorf("expected 'Hello there!', got %q", deliver.messages[0])
	}
}

func TestRuntime_Handle_UsesChannelTargetFromEventMeta(t *testing.T) {
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
			}{Role: "assistant", Content: "Reply"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "discord",
		From:       "user-id",
		Message:    "hello",
		Meta:       map[string]any{"channel_id": "channel-1"},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if deliver.to != "channel-1" {
		t.Fatalf("expected delivery to channel target, got %q", deliver.to)
	}
}

func TestRuntime_Handle_PreservesReplyMetadataForDelivery(t *testing.T) {
	d := openRuntimeTestDB(t)
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
				}{Role: "assistant", Content: "Reply"},
			},
		},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "slack",
		From:       "user-id",
		Message:    "hello",
		Meta: map[string]any{
			"channel_id":            "channel-1",
			channels.MetaThreadTS:   "123.45",
			"attachments":           []string{"artifact"},
			channels.MetaMediaPaths: []string{"/tmp/file.txt"},
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := deliver.meta[channels.MetaThreadTS]; got != "123.45" {
		t.Fatalf("expected thread metadata to be delivered, got %#v", deliver.meta)
	}
	if _, ok := deliver.meta["attachments"]; ok {
		t.Fatalf("expected only reply metadata to be delivered, got %#v", deliver.meta)
	}
}

func TestRuntime_Handle_StreamPreservesReplyMetadata(t *testing.T) {
	d := openRuntimeTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":" there"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()
	streamer := &replyMetaStreamer{}
	rt := &Runtime{DB: d, Provider: provider, Model: "gpt-4", Tools: tools.NewRegistry(), Builder: &Builder{DB: d, HistoryMax: 10}, MaxToolLoops: 2, Streamer: streamer}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-stream",
		Channel:    "slack",
		From:       "user-id",
		Message:    "hello",
		Meta:       map[string]any{"channel_id": "channel-1", channels.MetaThreadTS: "123.45"},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if streamer.to != "channel-1" {
		t.Fatalf("expected stream target channel-1, got %q", streamer.to)
	}
	if got := streamer.meta[channels.MetaThreadTS]; got != "123.45" {
		t.Fatalf("expected streamed thread metadata, got %#v", streamer.meta)
	}
	if got := streamer.meta["channel"]; got != "slack" {
		t.Fatalf("expected stream channel metadata, got %#v", streamer.meta)
	}
	if streamer.writer == nil || !streamer.writer.closed {
		t.Fatalf("expected stream final text, got %#v", streamer.writer)
	}
	if strings.Join(streamer.writer.deltas, "") != "Hello there" {
		t.Fatalf("expected streamed deltas to form final text, got %#v", streamer.writer.deltas)
	}
}

func TestRuntime_Handle_EmailAutoReplyDisabledDoesNotDeliver(t *testing.T) {
	d := openRuntimeTestDB(t)
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
				}{Role: "assistant", Content: "Reply"},
			},
		},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "email:alice@example.com",
		Channel:    "email",
		From:       "alice@example.com",
		Message:    "hello",
		Meta:       map[string]any{"auto_reply_enabled": false},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) != 0 {
		t.Fatalf("expected no delivered messages, got %#v", deliver.messages)
	}
	msgs, err := d.GetLastMessages(context.Background(), "email:alice@example.com", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected persisted user and assistant messages, got %#v", msgs)
	}
}

func TestRuntime_Handle_PersistsAttachmentMetadata(t *testing.T) {
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
			}{Role: "assistant", Content: "ok"},
		}},
	}
	_, provider := buildChatServer(t, response)
	rt := buildSimpleRuntime(t, provider, d, &mockDeliverer{})
	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "telegram",
		From:       "user",
		Message:    "see image\n[image: photo.png]",
		Meta: map[string]any{
			"attachments": []map[string]any{{
				"artifact_id": "artifact-1",
				"filename":    "photo.png",
				"mime":        "image/png",
				"kind":        "image",
				"size_bytes":  10,
			}},
		},
	}
	if err := rt.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected persisted messages")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msgs[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	meta, ok := payload["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta in payload, got %#v", payload)
	}
	attachments, ok := meta["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("expected attachments in payload meta, got %#v", meta["attachments"])
	}
}

func TestRuntime_Handle_CronEvent(t *testing.T) {
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
			}{Role: "assistant", Content: "Cron response"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventCron,
		SessionKey: "sess1",
		Message:    "cron task",
	})
	if err != nil {
		t.Fatalf("Handle cron: %v", err)
	}
}

func TestRuntime_Handle_HeartbeatEventSkipsDefaultDelivery(t *testing.T) {
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
			}{Role: "assistant", Content: "Heartbeat complete"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	var released atomic.Bool
	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: "heartbeat:test",
		Channel:    "system",
		From:       "heartbeat",
		Message:    "run heartbeat",
		Meta: map[string]any{
			heartbeat.MetaKeyDone: func() {
				released.Store(true)
			},
		},
	})
	if err != nil {
		t.Fatalf("Handle heartbeat: %v", err)
	}
	if len(deliver.messages) != 0 {
		t.Fatalf("expected no default delivery for heartbeat, got %#v", deliver.messages)
	}
	if !released.Load() {
		t.Fatal("expected heartbeat completion callback to run")
	}

	msgs, err := d.GetLastMessages(context.Background(), "heartbeat:test", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) < 2 || msgs[len(msgs)-1].Role != "assistant" || msgs[len(msgs)-1].Content != "Heartbeat complete" {
		t.Fatalf("expected assistant reply to persist, got %#v", msgs)
	}
}

func TestRuntime_Handle_UnknownEvent(t *testing.T) {
	d := openRuntimeTestDB(t)
	_, provider := buildChatServer(t, providers.ChatCompletionResponse{})
	rt := buildSimpleRuntime(t, provider, d, &mockDeliverer{})

	// Unknown event type should return nil without processing
	err := rt.Handle(context.Background(), bus.Event{
		Type:       "unknown_type",
		SessionKey: "sess1",
	})
	if err != nil {
		t.Fatalf("expected no error for unknown event type, got: %v", err)
	}
}

func TestRuntime_Handle_NoBuilder(t *testing.T) {
	d := openRuntimeTestDB(t)
	_, provider := buildChatServer(t, providers.ChatCompletionResponse{})
	rt := &Runtime{
		DB:       d,
		Provider: provider,
		Model:    "gpt-4",
		Tools:    tools.NewRegistry(),
		Builder:  nil, // no builder
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Message:    "hello",
	})
	if err == nil {
		t.Fatal("expected error when builder is nil")
	}
}

func TestRuntime_Handle_NoChoices(t *testing.T) {
	d := openRuntimeTestDB(t)
	// Return empty choices
	response := providers.ChatCompletionResponse{Choices: nil}
	_, provider := buildChatServer(t, response)
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      b,
		MaxToolLoops: 2,
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Message:    "hello",
	})
	if err == nil {
		t.Fatal("expected error when no choices returned")
	}
}

func TestRuntime_Handle_WithToolCall(t *testing.T) {
	d := openRuntimeTestDB(t)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			// First call returns tool call
			resp = providers.ChatCompletionResponse{
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
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "echo_tool", Arguments: `{}`},
						}},
					},
				}},
			}
		} else {
			// Second call returns final text
			resp = providers.ChatCompletionResponse{
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
					}{Role: "assistant", Content: "Final answer"},
				}},
			}
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	reg := tools.NewRegistry()
	// Register a simple echo tool
	reg.Register(&echoTool{})

	deliver := &mockDeliverer{}
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      b,
		MaxToolLoops: 6,
		Deliver:      deliver,
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-tool",
		Message:    "do tool call",
	})
	if err != nil {
		t.Fatalf("Handle with tool call: %v", err)
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "Final answer" {
		t.Errorf("expected 'Final answer', got %v", deliver.messages)
	}
}

func TestRuntime_Handle_WithMCPNamedTool_UsesNormalToolPath(t *testing.T) {
	d := openRuntimeTestDB(t)

	var firstTools []providers.ToolDef
	var secondMessages []providers.ChatMessage
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if callCount == 0 {
			firstTools = append([]providers.ToolDef(nil), req.Tools...)
		} else {
			secondMessages = append([]providers.ChatMessage(nil), req.Messages...)
		}
		callCount++

		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
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
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-mcp",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "mcp_demo_echo", Arguments: `{"text":"hi"}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{
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
					}{Role: "assistant", Content: "MCP done"},
				}},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	tool := &mcpEchoTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)

	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 6,
		Deliver:      deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-mcp-tool",
		Channel:    "cli",
		From:       "user",
		Message:    "use the MCP tool",
	}); err != nil {
		t.Fatalf("Handle with MCP tool: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected two provider requests, got %d", callCount)
	}
	if !tool.called || tool.gotText != "hi" {
		t.Fatalf("expected MCP tool to execute with text=hi, got called=%v text=%q", tool.called, tool.gotText)
	}
	if len(firstTools) != 1 {
		t.Fatalf("expected one advertised tool, got %#v", firstTools)
	}
	if firstTools[0].Function.Name != "mcp_demo_echo" {
		t.Fatalf("expected MCP tool name in tool defs, got %#v", firstTools[0].Function)
	}
	if firstTools[0].Function.Description != "MCP-backed echo" {
		t.Fatalf("expected MCP tool description in tool defs, got %#v", firstTools[0].Function.Description)
	}
	params, ok := firstTools[0].Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON schema parameters, got %#v", firstTools[0].Function.Parameters)
	}
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected schema properties, got %#v", params)
	}
	textField, ok := properties["text"].(map[string]any)
	if !ok || textField["type"] != "string" {
		t.Fatalf("expected text property schema, got %#v", properties["text"])
	}
	if len(secondMessages) == 0 {
		t.Fatal("expected second provider request to include tool result")
	}
	last := secondMessages[len(secondMessages)-1]
	if last.Role != "tool" || last.Content != "remote echo: hi" {
		t.Fatalf("expected tool message with MCP output, got %#v", last)
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "MCP done" {
		t.Fatalf("expected final delivery after MCP tool execution, got %#v", deliver.messages)
	}
}

// echoTool is a simple test tool for agent tests
type echoTool struct{ tools.Base }

func (e *echoTool) Name() string        { return "echo_tool" }
func (e *echoTool) Description() string { return "echoes input" }
func (e *echoTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (e *echoTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return "echo result", nil
}
func (e *echoTool) Schema() map[string]any {
	return e.SchemaFor(e.Name(), e.Description(), e.Parameters())
}

type guardedEchoTool struct{ echoTool }

func (e *guardedEchoTool) Capability() tools.CapabilityLevel { return tools.CapabilityGuarded }

type privilegedEchoTool struct{ echoTool }

func (e *privilegedEchoTool) Name() string                      { return "privileged_echo_tool" }
func (e *privilegedEchoTool) Capability() tools.CapabilityLevel { return tools.CapabilityPrivileged }

type mcpEchoTool struct {
	tools.Base
	called  bool
	gotText string
}

func (e *mcpEchoTool) Name() string        { return "mcp_demo_echo" }
func (e *mcpEchoTool) Description() string { return "MCP-backed echo" }
func (e *mcpEchoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}
}
func (e *mcpEchoTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	e.called = true
	e.gotText = fmt.Sprint(params["text"])
	return "remote echo: " + e.gotText, nil
}
func (e *mcpEchoTool) Schema() map[string]any {
	return e.SchemaFor(e.Name(), e.Description(), e.Parameters())
}

type deliveryContextTool struct {
	tools.Base
	channel string
	to      string
	meta    map[string]any
}

func (dct *deliveryContextTool) Name() string        { return "delivery_context_tool" }
func (dct *deliveryContextTool) Description() string { return "captures delivery context" }
func (dct *deliveryContextTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (dct *deliveryContextTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	dct.channel, dct.to = tools.DeliveryFromContext(ctx)
	dct.meta = tools.DeliveryMetaFromContext(ctx)
	return "captured", nil
}
func (dct *deliveryContextTool) Schema() map[string]any {
	return dct.SchemaFor(dct.Name(), dct.Description(), dct.Parameters())
}

type requiredTextTool struct {
	tools.Base
	called bool
}

func (rtt *requiredTextTool) Name() string        { return "required_text_tool" }
func (rtt *requiredTextTool) Description() string { return "requires string text input" }
func (rtt *requiredTextTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"text"},
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}
}
func (rtt *requiredTextTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	rtt.called = true
	return fmt.Sprint(params["text"]), nil
}
func (rtt *requiredTextTool) Schema() map[string]any {
	return rtt.SchemaFor(rtt.Name(), rtt.Description(), rtt.Parameters())
}

func TestFormatToolExecutionError_PreservesOutput(t *testing.T) {
	got := formatToolExecutionError("stdout:\nfailed test\n\nstderr:\nboom", fmt.Errorf("exec failed: exit status 1"))
	if !strings.Contains(got, "stdout:\nfailed test") {
		t.Fatalf("expected tool output to be preserved, got %q", got)
	}
	if !strings.Contains(got, "error: exec failed: exit status 1") {
		t.Fatalf("expected error text to be appended, got %q", got)
	}
}

type replyMetaStreamer struct {
	to     string
	meta   map[string]any
	writer *mockStreamWriter
}

func (m *replyMetaStreamer) BeginStream(ctx context.Context, to string, meta map[string]any) (channels.StreamWriter, error) {
	m.to = to
	m.meta = channels.CloneMeta(meta)
	m.writer = &mockStreamWriter{}
	return m.writer, nil
}

func TestRuntime_Handle_StructuredAutonomyExecutesDirectly(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &deliveryContextTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	rt := &Runtime{DB: d, Tools: reg}

	var released atomic.Bool
	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventWebhook,
		SessionKey: "sess-structured",
		Channel:    "slack",
		From:       "remote",
		Message:    "ignored prompt body",
		Meta: map[string]any{
			"channel_id":          "channel-1",
			channels.MetaThreadTS: "123.45",
			heartbeat.MetaKeyDone: func() { released.Store(true) },
			triggers.MetaKeyStructuredTasks: triggers.StructuredTasksMap(triggers.StructuredTaskEnvelope{
				Tasks: []triggers.StructuredToolCall{{Tool: tool.Name()}},
			}),
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !released.Load() {
		t.Fatal("expected completion callback to run")
	}
	if tool.channel != "slack" || tool.to != "channel-1" {
		t.Fatalf("expected delivery context slack/channel-1, got %q/%q", tool.channel, tool.to)
	}
	if got := tool.meta[channels.MetaThreadTS]; got != "123.45" {
		t.Fatalf("expected reply metadata in tool context, got %#v", tool.meta)
	}

	msgs, err := d.GetLastMessages(context.Background(), "sess-structured", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected user/tool/assistant messages, got %#v", msgs)
	}
	if msgs[1].Role != "tool" {
		t.Fatalf("expected tool message, got %#v", msgs[1])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msgs[1].PayloadJSON), &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload["tool"] != tool.Name() || payload["structured_task"] != true {
		t.Fatalf("expected structured task payload, got %#v", payload)
	}
	if msgs[2].Role != "assistant" || !strings.Contains(msgs[2].Content, "1/1 succeeded") {
		t.Fatalf("expected structured execution summary, got %#v", msgs[2])
	}
}

func TestRuntime_Handle_StructuredAutonomyValidatesParams(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &requiredTextTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	rt := &Runtime{DB: d, Tools: reg}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: "sess-structured-invalid",
		Channel:    "system",
		From:       "heartbeat",
		Message:    heartbeat.SeedMessage,
		Meta: map[string]any{
			triggers.MetaKeyStructuredTasks: triggers.StructuredTasksMap(triggers.StructuredTaskEnvelope{
				Tasks: []triggers.StructuredToolCall{{Tool: tool.Name(), Params: map[string]any{"text": 123}}},
			}),
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.called {
		t.Fatal("expected invalid structured task to be rejected before execution")
	}

	msgs, err := d.GetLastMessages(context.Background(), "sess-structured-invalid", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected user and assistant summary messages, got %#v", msgs)
	}
	if got := msgs[1].Content; !strings.Contains(got, "0/1 succeeded") || !strings.Contains(got, "params.text must be a string") {
		t.Fatalf("expected validation failure summary, got %q", got)
	}
}

func TestRuntime_Handle_ArtifactSave(t *testing.T) {
	d := openRuntimeTestDB(t)
	artifactsDir := t.TempDir()

	// First call: return tool call that generates large output
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
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
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-large",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "large_output_tool", Arguments: `{}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{
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
					}{Role: "assistant", Content: "done"},
				}},
			}
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	if err := d.EnsureSession(context.Background(), "sess-artifact"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	reg := tools.NewRegistry()
	reg.Register(&largeOutputTool{})

	deliver := &mockDeliverer{}
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:               d,
		Provider:         provider,
		Model:            "gpt-4",
		Tools:            reg,
		Builder:          b,
		MaxToolLoops:     6,
		Deliver:          deliver,
		MaxToolBytes:     10, // small limit to trigger artifact save
		ToolPreviewBytes: 5,
		Artifacts:        &artifacts.Store{Dir: artifactsDir, DB: d},
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-artifact",
		Message:    "large output",
	})
	if err != nil {
		t.Fatalf("Handle artifact: %v", err)
	}
}

func TestRuntime_GuardToolExecution_ProfileDeniesPrivilegedTool(t *testing.T) {
	rt := &Runtime{}
	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:          "safe-only",
		MaxCapability: tools.CapabilitySafe,
		AllowedTools:  map[string]struct{}{"read_file": {}},
	})
	err := rt.guardToolExecution(ctx, &privilegedEchoTool{}, tools.CapabilityPrivileged, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "profile capability") {
		t.Fatalf("expected profile denial, got %v", err)
	}
}

func TestRuntime_GuardToolExecution_ProfileDeniesSubagents(t *testing.T) {
	rt := &Runtime{}
	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:           "no-subagents",
		MaxCapability:  tools.CapabilityGuarded,
		AllowedTools:   map[string]struct{}{"spawn_subagent": {}},
		AllowSubagents: false,
	})
	err := rt.guardToolExecution(ctx, &tools.SpawnSubagent{}, tools.CapabilityGuarded, map[string]any{"task": "background job"})
	if err == nil || !strings.Contains(err.Error(), "subagents denied") {
		t.Fatalf("expected subagent denial, got %v", err)
	}
}

func TestRuntime_GuardToolExecution_ProfileWithEmptyHostsDeniesWebFetch(t *testing.T) {
	rt := &Runtime{}
	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:          "no-network",
		MaxCapability: tools.CapabilityGuarded,
		AllowedTools:  map[string]struct{}{"web_fetch": {}},
	})
	err := rt.guardToolExecution(ctx, &tools.WebFetch{}, tools.CapabilityGuarded, map[string]any{"url": "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "host denied by policy") {
		t.Fatalf("expected host denial, got %v", err)
	}
}

func TestRuntime_GuardToolExecution_ProfileWithEmptyHostsDeniesWebSearch(t *testing.T) {
	rt := &Runtime{}
	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:          "no-network",
		MaxCapability: tools.CapabilitySafe,
		AllowedTools:  map[string]struct{}{"web_search": {}},
	})
	err := rt.guardToolExecution(ctx, &tools.WebSearch{}, tools.CapabilitySafe, map[string]any{"query": "hello"})
	if err == nil || !strings.Contains(err.Error(), "host denied by policy") {
		t.Fatalf("expected host denial, got %v", err)
	}
}

func TestRuntime_GuardToolExecution_ProfileWithEmptyWritablePathsDeniesWriteFile(t *testing.T) {
	rt := &Runtime{}
	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:          "read-only",
		MaxCapability: tools.CapabilityGuarded,
		AllowedTools:  map[string]struct{}{"write_file": {}},
	})
	err := rt.guardToolExecution(ctx, &tools.WriteFile{}, tools.CapabilityGuarded, map[string]any{"path": "/tmp/out.txt"})
	if err == nil || !strings.Contains(err.Error(), "path denied by profile") {
		t.Fatalf("expected path denial, got %v", err)
	}
}

func TestRuntime_GuardToolExecution_ProfileWithEmptyWritablePathsDeniesExecCWD(t *testing.T) {
	rt := &Runtime{}
	ctx := tools.ContextWithActiveProfile(context.Background(), tools.ActiveProfile{
		Name:          "read-only",
		MaxCapability: tools.CapabilityPrivileged,
		AllowedTools:  map[string]struct{}{"exec": {}},
	})
	err := rt.guardToolExecution(ctx, &tools.ExecTool{}, tools.CapabilityPrivileged, map[string]any{"cwd": "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "path denied by profile") {
		t.Fatalf("expected path denial, got %v", err)
	}
}

func TestRuntime_Handle_GuardedToolDeniedByDefault(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "tc1", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "echo_tool", Arguments: `{}`}}}}}}}
		} else {
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "done"}}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()
	reg := tools.NewRegistry()
	reg.Register(&guardedEchoTool{})
	rt := &Runtime{DB: d, Provider: provider, Model: "gpt-4", Tools: reg, Builder: &Builder{DB: d, HistoryMax: 10}, MaxToolLoops: 4, Deliver: &mockDeliverer{}, Hardening: config.Default().Hardening}
	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-guarded", Message: "try guarded"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess-guarded", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "tool requires guarded access") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected guarded access denial in tool history, got %#v", msgs)
	}
}

func TestRuntime_Handle_ToolQuotaExceeded(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		switch callCount {
		case 1, 2:
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("tc%d", callCount), Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "echo_tool", Arguments: `{}`}}}}}}}
		default:
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "done"}}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})
	hardening := config.Default().Hardening
	hardening.Quotas.MaxToolCalls = 1
	rt := &Runtime{DB: d, Provider: provider, Model: "gpt-4", Tools: reg, Builder: &Builder{DB: d, HistoryMax: 10}, MaxToolLoops: 4, Deliver: &mockDeliverer{}, Hardening: hardening}
	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-quota", Message: "hit quota"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess-quota", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "quota exceeded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected quota denial in tool history, got %#v", msgs)
	}
}

func TestRuntime_Handle_ToolContextIncludesDelivery(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
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
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-delivery",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "delivery_context_tool", Arguments: `{}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "done"}}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()
	tool := &deliveryContextTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	rt := &Runtime{DB: d, Provider: provider, Model: "gpt-4", Tools: reg, Builder: &Builder{DB: d, HistoryMax: 10}, MaxToolLoops: 4}
	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess",
		Channel:    "discord",
		From:       "user-1",
		Message:    "hello",
		Meta:       map[string]any{"channel_id": "channel-1", channels.MetaMessageReference: "m-1"},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.channel != "discord" || tool.to != "channel-1" {
		t.Fatalf("expected delivery context discord/channel-1, got %q/%q", tool.channel, tool.to)
	}
	if got := tool.meta[channels.MetaMessageReference]; got != "m-1" {
		t.Fatalf("expected message reference in tool delivery context, got %#v", tool.meta)
	}
}

func TestRuntime_Handle_HeartbeatSendMessageToolStillWorks(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
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
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-send",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "send_message", Arguments: `{"channel":"cli","to":"ops","text":"heartbeat ping"}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{
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
					}{Role: "assistant", Content: "sent"},
				}},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	var sentChannel, sentTo, sentText string
	reg := tools.NewRegistry()
	reg.Register(&tools.SendMessage{
		Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
			sentChannel = channel
			sentTo = to
			sentText = text
			return nil
		},
	})

	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 4,
		Deliver:      deliver,
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: "heartbeat:send",
		Channel:    "system",
		From:       "heartbeat",
		Message:    "notify",
	})
	if err != nil {
		t.Fatalf("Handle heartbeat send_message: %v", err)
	}
	if sentChannel != "cli" || sentTo != "ops" || sentText != "heartbeat ping" {
		t.Fatalf("expected explicit send_message delivery to work, got %q/%q/%q", sentChannel, sentTo, sentText)
	}
	if len(deliver.messages) != 0 {
		t.Fatalf("expected no default delivery for heartbeat final response, got %#v", deliver.messages)
	}
}

func TestRuntime_Handle_HeartbeatSendMessageDoesNotImplicitlyThread(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
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
						}{
							Role: "assistant",
							ToolCalls: []providers.ToolCall{{
								ID:   "tc-send",
								Type: "function",
								Function: struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								}{Name: "send_message", Arguments: `{"text":"heartbeat ping"}`},
							}},
						},
					},
				},
			}
		} else {
			resp = providers.ChatCompletionResponse{
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
						}{Role: "assistant", Content: "sent"},
					},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	var sentMeta map[string]any
	reg := tools.NewRegistry()
	reg.Register(&tools.SendMessage{
		Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
			sentMeta = channels.CloneMeta(meta)
			return nil
		},
	})

	rt := &Runtime{DB: d, Provider: provider, Model: "gpt-4", Tools: reg, Builder: &Builder{DB: d, HistoryMax: 10}, MaxToolLoops: 4}
	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: "heartbeat:thread",
		Channel:    "slack",
		From:       "heartbeat",
		Message:    "notify",
		Meta:       map[string]any{"channel_id": "C1", channels.MetaThreadTS: "123.45"},
	})
	if err != nil {
		t.Fatalf("Handle heartbeat send_message: %v", err)
	}
	if sentMeta != nil {
		if _, ok := sentMeta[channels.MetaThreadTS]; ok {
			t.Fatalf("expected proactive send_message not to inherit thread metadata, got %#v", sentMeta)
		}
	}
}

func TestRuntime_Handle_UserMessageSendMessageDeniedByDefault(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
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
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-send",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "send_message", Arguments: `{"channel":"cli","to":"ops","text":"hello"}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "done"}}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	called := false
	reg := tools.NewRegistry()
	reg.Register(&tools.SendMessage{
		Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
			called = true
			return nil
		},
	})

	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 4,
		Deliver:      &mockDeliverer{},
		Hardening:    config.Default().Hardening,
	}

	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-send-blocked", Message: "notify ops"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if called {
		t.Fatal("expected send_message delivery to be blocked for user-triggered events")
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess-send-blocked", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "tool requires guarded access: send_message") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected guarded access denial in tool history, got %#v", msgs)
	}
}

func TestRuntime_SessionQuotaStateEvictsOldestEntries(t *testing.T) {
	rt := &Runtime{}
	for i := 0; i < maxTrackedQuotaSessions+5; i++ {
		state := rt.sessionQuotaState(fmt.Sprintf("sess-%04d", i))
		if state == nil {
			t.Fatal("expected quota state")
		}
	}
	rt.quotaMu.Lock()
	defer rt.quotaMu.Unlock()
	if len(rt.quotas) > maxTrackedQuotaSessions {
		t.Fatalf("expected quota map to be bounded, got %d entries", len(rt.quotas))
	}
	if _, ok := rt.quotas["sess-0000"]; ok {
		t.Fatalf("expected oldest quota entry to be evicted")
	}
}

// largeOutputTool generates output larger than MaxToolBytes
type largeOutputTool struct{ tools.Base }

func (e *largeOutputTool) Name() string        { return "large_output_tool" }
func (e *largeOutputTool) Description() string { return "generates large output" }
func (e *largeOutputTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (e *largeOutputTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	// Generate output that exceeds MaxToolBytes
	return string(make([]byte, 100)), nil
}
func (e *largeOutputTool) Schema() map[string]any {
	return e.SchemaFor(e.Name(), e.Description(), e.Parameters())
}

func TestRuntime_Handle_NoMaxLoops_Defaults(t *testing.T) {
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
			}{Role: "assistant", Content: "response"},
		}},
	}
	_, provider := buildChatServer(t, response)
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      b,
		MaxToolLoops: 0, // should default to 6
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-default-loops",
		Message:    "hello",
	})
	if err != nil {
		t.Fatalf("Handle with default max loops: %v", err)
	}
}

func TestRuntime_Handle_SystemEvent(t *testing.T) {
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
			}{Role: "assistant", Content: "sys response"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventSystem,
		SessionKey: "sess-sys",
		Message:    "system message",
	})
	if err != nil {
		t.Fatalf("Handle system: %v", err)
	}
}

func TestToToolDefs_WithRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	defs := toToolDefs(reg)
	if len(defs) != 1 {
		t.Errorf("expected 1 tool def, got %d", len(defs))
	}
	if defs[0].Type != "function" {
		t.Errorf("expected type 'function', got %q", defs[0].Type)
	}
	if defs[0].Function.Name != "echo_tool" {
		t.Errorf("expected name 'echo_tool', got %q", defs[0].Function.Name)
	}
}

func TestRuntime_LockFor_SameKey(t *testing.T) {
	rt := &Runtime{}
	mu1 := rt.lockFor("key1")
	mu2 := rt.lockFor("key1")
	if mu1 != mu2 {
		t.Error("expected same mutex for same key")
	}
}

func TestRuntime_LockFor_DifferentKeys(t *testing.T) {
	rt := &Runtime{}
	mu1 := rt.lockFor("key1")
	mu2 := rt.lockFor("key2")
	if mu1 == mu2 {
		t.Error("expected different mutexes for different keys")
	}
}

func TestRuntime_AcquireSessionLock_PreservesSingleEntry(t *testing.T) {
	rt := &Runtime{}
	entry1 := rt.acquireSessionLock("session-1")
	entry1.mu.Lock()

	entryCh := make(chan *sessionLock, 1)
	releaseEntry2 := make(chan struct{})
	done := make(chan struct{})
	go func() {
		entry2 := rt.acquireSessionLock("session-1")
		entryCh <- entry2
		<-releaseEntry2
		rt.releaseSessionLock("session-1", entry2)
		close(done)
	}()

	entry2 := <-entryCh
	if entry2 != entry1 {
		t.Fatal("expected second waiter to reuse the same session lock entry")
	}
	entry1.mu.Unlock()
	rt.releaseSessionLock("session-1", entry1)

	entry3 := rt.acquireSessionLock("session-1")
	if entry3 != entry1 {
		t.Fatal("expected lock entry to stay stable while another waiter exists")
	}
	rt.releaseSessionLock("session-1", entry3)
	close(releaseEntry2)
	<-done
}

func TestRuntime_ConsolidationScheduler_DoesNotBlockTurn(t *testing.T) {
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
			}{Role: "assistant", Content: "ok"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	rt.Consolidator = &memory.Consolidator{Provider: &providers.Client{}}
	rt.ConsolidationScheduler = memory.NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		started <- struct{}{}
		<-release
	})

	start := time.Now()
	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-non-blocking",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("expected non-blocking turn, elapsed=%v", elapsed)
	}
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("expected scheduler run")
	}
	close(release)
}

func TestRuntime_HandleNewSession_Success(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if _, err := d.AppendMessage(ctx, "sess-new", "user", "hello", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	provServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"summary":"done","canonical_memory":"- fact"}`}}},
			})
		case "/embeddings":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": []float32{0.1}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provServer.Close()
	prov := providers.New(provServer.URL, "k", 5*time.Second)
	prov.HTTP = provServer.Client()

	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:       d,
		Provider: prov,
		Model:    "gpt-4.1-mini",
		Tools:    tools.NewRegistry(),
		Builder:  &Builder{DB: d, HistoryMax: 40},
		Deliver:  deliver,
		Consolidator: &memory.Consolidator{
			DB:                 d,
			Provider:           prov,
			WindowSize:         1,
			MaxMessages:        50,
			MaxInputChars:      4000,
			CanonicalPinnedKey: "long_term_memory",
		},
	}
	if err := rt.Handle(ctx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-new",
		Channel:    "cli",
		From:       "user",
		Message:    "/new",
	}); err != nil {
		t.Fatalf("Handle /new: %v", err)
	}
	msgs, err := d.GetLastMessages(ctx, "sess-new", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected messages cleared, got %d", len(msgs))
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "New session started." {
		t.Fatalf("unexpected deliver messages: %#v", deliver.messages)
	}
}

func TestRuntime_HandleNewSession_FailurePreservesHistory(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := d.AppendMessage(ctx, "sess-new-fail", "user", "hello", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	provServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer provServer.Close()
	prov := providers.New(provServer.URL, "k", 5*time.Second)
	prov.HTTP = provServer.Client()

	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:       d,
		Provider: prov,
		Model:    "gpt-4.1-mini",
		Tools:    tools.NewRegistry(),
		Builder:  &Builder{DB: d, HistoryMax: 40},
		Deliver:  deliver,
		Consolidator: &memory.Consolidator{
			DB:            d,
			Provider:      prov,
			WindowSize:    1,
			MaxMessages:   50,
			MaxInputChars: 4000,
		},
	}
	if err := rt.Handle(ctx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-new-fail",
		Channel:    "cli",
		From:       "user",
		Message:    "/new",
	}); err != nil {
		t.Fatalf("Handle /new: %v", err)
	}
	msgs, err := d.GetLastMessages(ctx, "sess-new-fail", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected history preserved on archive failure")
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "Memory archival failed") {
		t.Fatalf("expected archival failure message, got %#v", deliver.messages)
	}
}

func TestRuntime_ConsolidationScheduler_SingleFlightCoalesces(t *testing.T) {
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
			}{Role: "assistant", Content: "ok"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	var runs int32
	firstRelease := make(chan struct{})
	secondSeen := make(chan struct{}, 1)
	rt.Consolidator = &memory.Consolidator{Provider: &providers.Client{}}
	rt.ConsolidationScheduler = memory.NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		n := atomic.AddInt32(&runs, 1)
		if n == 1 {
			<-firstRelease
			return
		}
		if n == 2 {
			secondSeen <- struct{}{}
		}
	})

	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-c", Channel: "cli", From: "u", Message: "a"}); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-c", Channel: "cli", From: "u", Message: "b"}); err != nil {
		t.Fatalf("second handle: %v", err)
	}
	close(firstRelease)
	select {
	case <-secondSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("expected coalesced second scheduler pass")
	}
}

func TestRuntimeWithIndexedDocs(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()

	// Create a temp dir with a markdown file containing "important penguin behavior"
	tmpDir := t.TempDir()
	docContent := "# Penguin Facts\n\nImportant penguin behavior: penguins huddle together for warmth.\n"
	docPath := filepath.Join(tmpDir, "penguins.md")
	if err := os.WriteFile(docPath, []byte(docContent), 0o644); err != nil {
		t.Fatalf("write doc file: %v", err)
	}

	// Insert the doc directly via UpsertDoc (no real embedding server needed)
	if err := memory.UpsertDoc(ctx, d, scope.GlobalMemoryScope, docPath, "markdown", "Penguin Facts", "", docContent, nil, "testhash", 0, int64(len(docContent))); err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	docRetriever := &memory.DocRetriever{DB: d}

	// Build a fake provider response
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
			}{Role: "assistant", Content: "Penguins huddle for warmth."},
		}},
	}
	_, provider := buildChatServer(t, response)

	b := &Builder{
		DB:               d,
		HistoryMax:       10,
		DocRetriever:     docRetriever,
		DocRetrieveLimit: 5,
	}

	pp, _, err := b.BuildWithOptions(ctx, BuildOptions{SessionKey: "test-scope", UserMessage: "penguin behavior"})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}

	// Find system prompt content
	var sysText string
	for _, msg := range pp.System {
		if msg.Role == "system" {
			if s, ok := msg.Content.(string); ok {
				sysText += s
			}
		}
	}

	// The system prompt should include the doc excerpt about penguins
	if !strings.Contains(sysText, "penguin") && !strings.Contains(strings.ToLower(sysText), "penguin") {
		t.Errorf("expected system prompt to contain penguin doc context, got:\n%s", sysText)
	}
	_ = provider
}

func TestRuntimeLinkedSessionHistory(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()

	// Link two session keys to a shared scope
	if err := d.LinkSession(ctx, "session-a", "shared-scope", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "session-b", "shared-scope", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}

	// Append messages to both sessions
	if _, err := d.AppendMessage(ctx, "session-a", "user", "hello from session-a", nil); err != nil {
		t.Fatalf("AppendMessage a user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "session-a", "assistant", "reply to session-a", nil); err != nil {
		t.Fatalf("AppendMessage a assistant: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "session-b", "user", "hello from session-b", nil); err != nil {
		t.Fatalf("AppendMessage b user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "session-b", "assistant", "reply to session-b", nil); err != nil {
		t.Fatalf("AppendMessage b assistant: %v", err)
	}

	// GetLastMessagesScoped with either session key should return messages from both sessions
	msgs, err := d.GetLastMessagesScoped(ctx, "session-a", 20)
	if err != nil {
		t.Fatalf("GetLastMessagesScoped: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages across linked sessions, got %d", len(msgs))
	}

	contents := map[string]bool{}
	for _, m := range msgs {
		contents[m.Content] = true
	}
	if !contents["hello from session-a"] || !contents["hello from session-b"] {
		t.Fatalf("expected messages from both sessions, got %v", contents)
	}

	// Verify chronological order
	for i := 1; i < len(msgs); i++ {
		if msgs[i].ID < msgs[i-1].ID {
			t.Fatalf("messages not in chronological order at index %d", i)
		}
	}

}

func TestRuntimeEnsureSessionScope_AutoLinksDirectMessages(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
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
		t.Fatalf("expected auto-linked scope cli:default, got %q", scopeKey)
	}

	rt.ensureSessionScope(ctx, bus.Event{
		SessionKey: "whatsapp:555",
		Channel:    "whatsapp",
		Meta:       map[string]any{"is_group": false},
	})
	scopeKey, err = d.ResolveScopeKey(ctx, "whatsapp:555")
	if err != nil {
		t.Fatalf("ResolveScopeKey whatsapp: %v", err)
	}
	if scopeKey != "cli:default" {
		t.Fatalf("expected whatsapp direct message to share cli:default, got %q", scopeKey)
	}

	rt.ensureSessionScope(ctx, bus.Event{
		SessionKey: "whatsapp:group-1",
		Channel:    "whatsapp",
		Meta:       map[string]any{"is_group": true},
	})
	scopeKey, err = d.ResolveScopeKey(ctx, "whatsapp:group-1")
	if err != nil {
		t.Fatalf("ResolveScopeKey group: %v", err)
	}
	if scopeKey != "whatsapp:group-1" {
		t.Fatalf("expected group chat to stay isolated, got %q", scopeKey)
	}
}

func TestRuntimeTurn_NewSessionSkipsAutoLinking(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	rt := &Runtime{
		DB:                 d,
		DefaultScopeKey:    "cli:default",
		LinkDirectMessages: true,
		Deliver:            &mockDeliverer{},
	}
	if err := rt.Handle(ctx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "telegram:123",
		Channel:    "telegram",
		From:       "123",
		Message:    "/new",
		Meta:       map[string]any{"chat_type": "private"},
	}); err != nil {
		t.Fatalf("Handle /new: %v", err)
	}
	scopeKey, err := d.ResolveScopeKey(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("ResolveScopeKey: %v", err)
	}
	if scopeKey != "telegram:123" {
		t.Fatalf("expected /new to leave direct message unlinked, got %q", scopeKey)
	}
}
