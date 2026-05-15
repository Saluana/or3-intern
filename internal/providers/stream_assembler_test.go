package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamAssembler_SnapshotStyleTextEmitsOnlySuffix(t *testing.T) {
	assembler := StreamAssembler{Profile: OpenAICompatibleProfile()}
	chunks := []ChatStreamChunk{
		{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{Content: "Hello"}}}},
		{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{Content: "Hello, world"}}}},
		{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{Content: "Hello, world"}}}},
	}
	var got []string
	for _, chunk := range chunks {
		for _, event := range assembler.ApplyChunk(chunk) {
			if event.TextDelta != "" {
				got = append(got, event.TextDelta)
			}
		}
	}
	final, err := assembler.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if strings.Join(got, "") != "Hello, world" || final.Content != "Hello, world" {
		t.Fatalf("unexpected text deltas=%#v final=%q", got, final.Content)
	}
}

func TestStreamAssembler_FragmentedToolCallGetsGeneratedID(t *testing.T) {
	assembler := StreamAssembler{Profile: OpenAICompatibleProfile()}
	assembler.ApplyChunk(ChatStreamChunk{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{ToolCalls: []ToolCall{{
		Index: 0,
		Type:  "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "exec", Arguments: `{"command":`},
	}}}}}})
	assembler.ApplyChunk(ChatStreamChunk{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{ToolCalls: []ToolCall{{
		Index: 0,
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: `"echo hi"}`},
	}}}}}})
	final, err := assembler.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("expected one call, got %#v", final.ToolCalls)
	}
	if final.ToolCalls[0].ID == "" || final.ToolCalls[0].Function.Arguments != `{"command":"echo hi"}` {
		t.Fatalf("unexpected call: %#v", final.ToolCalls[0])
	}
}

func TestStreamAssembler_MalformedBeforeOutputIsRetryable(t *testing.T) {
	assembler := StreamAssembler{Profile: OpenAICompatibleProfile()}
	assembler.RecordMalformed(`{"choices":[`)
	_, err := assembler.Finalize()
	if err == nil {
		t.Fatal("expected error")
	}
	streamErr, ok := err.(ProviderStreamError)
	if !ok || !streamErr.Retryable || streamErr.Code != "malformed_stream_before_output" {
		t.Fatalf("expected retryable malformed error, got %#v", err)
	}
}

func TestStreamAssembler_MalformedAfterOutputFinishesWithWarning(t *testing.T) {
	assembler := StreamAssembler{Profile: OpenAICompatibleProfile()}
	assembler.ApplyChunk(ChatStreamChunk{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{Content: "visible"}}}})
	assembler.RecordMalformed(`{"choices":[`)
	final, err := assembler.Finalize()
	if err != nil {
		t.Fatalf("expected warning-only finish after visible output, got %v", err)
	}
	if final.Content != "visible" || len(final.Warnings) != 1 {
		t.Fatalf("unexpected final message: %#v", final)
	}
}

func TestStreamAssembler_IncompleteToolArgumentsBeforeTextIsRetryable(t *testing.T) {
	assembler := StreamAssembler{Profile: OpenAICompatibleProfile()}
	assembler.ApplyChunk(ChatStreamChunk{Choices: []ChatStreamChoice{{Delta: ChatStreamDelta{ToolCalls: []ToolCall{{
		Index: 0,
		ID:    "call_1",
		Type:  "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "exec", Arguments: `{"command":`},
	}}}}}})
	_, err := assembler.Finalize()
	if err == nil {
		t.Fatal("expected incomplete arguments error")
	}
	streamErr, ok := err.(ProviderStreamError)
	if !ok || streamErr.Code != "incomplete_tool_arguments" || !streamErr.Retryable {
		t.Fatalf("expected retryable incomplete args error, got %#v", err)
	}
}

func TestChatStream_FallbackToNonStreamOnMalformedBeforeOutput(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"choices":[`)
			fmt.Fprintln(w, `data: [DONE]`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"fallback text"}}]}`)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "key", 0)
	c.HTTP = srv.Client()
	var deltas []string
	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{Model: "gpt-4"}, func(text string) {
		deltas = append(deltas, text)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected stream then non-stream fallback, got %d calls", calls)
	}
	if resp.Choices[0].Message.Content != "fallback text" || strings.Join(deltas, "") != "fallback text" {
		t.Fatalf("unexpected fallback resp=%#v deltas=%#v", resp.Choices[0].Message.Content, deltas)
	}
}
