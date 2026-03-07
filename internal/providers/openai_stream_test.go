package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sseServer(t *testing.T, lines []string) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "key", 0)
	c.HTTP = srv.Client()
	return srv, c
}

func TestChatStream_TextOnly(t *testing.T) {
	lines := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":", world"},"finish_reason":""}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	var got []string
	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{Model: "m"}, func(text string) {
		got = append(got, text)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected choices")
	}
	content := resp.Choices[0].Message.Content
	if content != "Hello, world" {
		t.Errorf("expected 'Hello, world', got %q", content)
	}
	if strings.Join(got, "") != "Hello, world" {
		t.Errorf("onDelta got %v", got)
	}
}

func TestChatStream_NilOnDelta(t *testing.T) {
	lines := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"hi"},"finish_reason":""}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("ChatStream with nil onDelta: %v", err)
	}
	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("expected 'hi', got %q", resp.Choices[0].Message.Content)
	}
}

func TestChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()
	c := New(srv.URL, "bad", 0)
	c.HTTP = srv.Client()

	_, err := c.ChatStream(context.Background(), ChatCompletionRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got %q", err.Error())
	}
}

func TestChatStream_SkipsNonDataLines(t *testing.T) {
	lines := []string{
		`: keep-alive`,
		``,
		`data: {"id":"1","choices":[{"delta":{"content":"ok"},"finish_reason":""}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Choices[0].Message.Content)
	}
}

func TestChatStream_WithToolCalls(t *testing.T) {
	lines := []string{
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"exec","arguments":""}}]},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"echo\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	tcs := resp.Choices[0].Message.ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	if tcs[0].ID != "call_1" {
		t.Errorf("expected ID 'call_1', got %q", tcs[0].ID)
	}
	if tcs[0].Function.Name != "exec" {
		t.Errorf("expected name 'exec', got %q", tcs[0].Function.Name)
	}
	if tcs[0].Function.Arguments != `{"cmd":"echo"}` {
		t.Errorf("unexpected arguments: %q", tcs[0].Function.Arguments)
	}
}

func TestMergeStreamToolCalls_IndexBased(t *testing.T) {
	existing := []ToolCall{}
	// first chunk: sets ID, name, empty args
	existing = mergeStreamToolCalls(existing, []ToolCall{
		{Index: 0, ID: "call_1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "exec", Arguments: ""}},
	})
	// second chunk: no ID, partial args
	existing = mergeStreamToolCalls(existing, []ToolCall{
		{Index: 0, Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: `{"cmd":`}},
	})
	// third chunk: rest of args
	existing = mergeStreamToolCalls(existing, []ToolCall{
		{Index: 0, Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: `"hi"}`}},
	})

	if len(existing) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(existing))
	}
	if existing[0].ID != "call_1" {
		t.Errorf("ID mismatch: %q", existing[0].ID)
	}
	if existing[0].Function.Arguments != `{"cmd":"hi"}` {
		t.Errorf("args mismatch: %q", existing[0].Function.Arguments)
	}
}
