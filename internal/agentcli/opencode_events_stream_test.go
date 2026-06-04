package agentcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func TestOpenCodeExecuteStreamsBusEvents(t *testing.T) {
	const sessionID = "sess_stream"
	var events []AgentRunEvent
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/config/providers":
			_, _ = w.Write([]byte(`{"providers":[{"id":"openrouter","models":{"mimo-v2.5":{}}}]}`))
		case "/event":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatalf("response writer is not flusher")
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{
						"type":      "text",
						"sessionID": sessionID,
						"id":        "part_1",
						"text":      "Hel",
					},
				},
			}))
			flusher.Flush()
			_, _ = fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
				"type": "message.part.updated",
				"properties": map[string]any{
					"part": map[string]any{
						"type":      "text",
						"sessionID": sessionID,
						"id":        "part_1",
						"text":      "Hello",
					},
				},
			}))
			flusher.Flush()
			<-r.Context().Done()
		case "/session":
			_, _ = w.Write([]byte(`{"id":"` + sessionID + `"}`))
		case "/session/" + sessionID + "/message":
			time.Sleep(40 * time.Millisecond)
			_, _ = w.Write([]byte(`{"type":"message","text":"Hello"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	_, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run:    db.AgentCLIRun{ID: "run_1", JobID: "job_1", Task: "hello", Model: "mimo-v2.5"},
		Chat:   RunnerChatCommandRequest{UserMessage: "hello"},
		Config: config.AgentCLIConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:    []string{"PATH="},
		OnEvent: func(e AgentRunEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var textChunks []string
	for _, ev := range events {
		if ev.Type != "structured" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if stringField(payload, "type") != "text" {
			continue
		}
		part := mapField(payload, "part")
		textChunks = append(textChunks, extractString(part["text"]))
	}
	if len(textChunks) < 2 {
		t.Fatalf("expected streamed text chunks before completion, got events=%d chunks=%v", len(events), textChunks)
	}
	if strings.Join(textChunks, "") != "Hello" {
		t.Fatalf("chunk text = %q, want Hello", strings.Join(textChunks, ""))
	}
}

func TestOpenCodeBusSuppressesUserTextParts(t *testing.T) {
	state := newOpenCodeStreamState()
	payload, ok := openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.updated",
		"properties": map[string]any{
			"message": map[string]any{
				"id":   "msg_user",
				"role": "user",
			},
		},
	}, state)
	if ok || payload != nil {
		t.Fatalf("message.updated should only update stream state, got ok=%v payload=%#v", ok, payload)
	}

	payload, ok = openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"part": map[string]any{
				"type":      "text",
				"messageID": "msg_user",
				"id":        "part_user",
				"text":      "Bro we need to talk",
			},
		},
	}, state)
	if ok || payload != nil {
		t.Fatalf("expected user text part to be suppressed, got ok=%v payload=%#v", ok, payload)
	}
	if state.streamedText.Load() {
		t.Fatal("suppressed user text must not count as streamed assistant text")
	}

	payload, ok = openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"part": map[string]any{
				"type":      "text",
				"messageID": "msg_assistant",
				"role":      "assistant",
				"id":        "part_assistant",
				"text":      "What's up?",
			},
		},
	}, state)
	if !ok || extractString(mapField(payload, "part")["text"]) != "What's up?" {
		t.Fatalf("expected assistant text payload, got ok=%v payload=%#v", ok, payload)
	}
}

func TestOpenCodeBusPreservesAssistantDeltaWhitespace(t *testing.T) {
	state := newOpenCodeStreamState()
	state.storePart(map[string]any{
		"type": "text",
		"role": "assistant",
		"id":   "part_assistant",
		"text": "Hello",
	})
	state.lastTextByPart["part_assistant"] = "Hello"

	payload, ok := openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.delta",
		"properties": map[string]any{
			"partID": "part_assistant",
			"delta":  " world ",
		},
	}, state)
	if !ok {
		t.Fatal("expected assistant delta payload")
	}
	part := mapField(payload, "part")
	if text, _ := part["text"].(string); text != " world " {
		t.Fatalf("expected delta whitespace to be preserved, got %#v", part)
	}
}

func TestOpenCodeBusPreservesCumulativeTextBoundaryWhitespace(t *testing.T) {
	state := newOpenCodeStreamState()
	payload, ok := openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"part": map[string]any{
				"type": "text",
				"role": "assistant",
				"id":   "part_assistant",
				"text": "Hello",
			},
		},
	}, state)
	if !ok || mapField(payload, "part")["text"] != "Hello" {
		t.Fatalf("expected initial assistant text payload, got ok=%v payload=%#v", ok, payload)
	}

	payload, ok = openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"part": map[string]any{
				"type": "text",
				"role": "assistant",
				"id":   "part_assistant",
				"text": "Hello ",
			},
		},
	}, state)
	if !ok {
		t.Fatal("expected cumulative assistant text delta")
	}
	part := mapField(payload, "part")
	if text, _ := part["text"].(string); text != " " {
		t.Fatalf("expected boundary space delta to be preserved, got %#v", part)
	}
}

func TestOpenCodeBusPreservesReasoningTextPartMetadata(t *testing.T) {
	state := newOpenCodeStreamState()
	payload, ok := openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"part": map[string]any{
				"type": "text",
				"role": "assistant",
				"id":   "part_reasoning",
				"kind": "thinking",
				"text": "I should answer casually.",
			},
		},
	}, state)
	if !ok {
		t.Fatal("expected reasoning text part payload")
	}
	part := mapField(payload, "part")
	if extractString(part["text"]) != "I should answer casually." || stringField(part, "kind") != "thinking" {
		t.Fatalf("expected delta text and reasoning metadata, got %#v", part)
	}
}

func TestOpenCodeBusStreamsReasoningTypePart(t *testing.T) {
	state := newOpenCodeStreamState()
	payload, ok := openCodeBusEventToStructuredPayload(map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"part": map[string]any{
				"type": "reasoning",
				"role": "assistant",
				"id":   "part_reasoning_type",
				"text": "The user is asking what I can help with.",
			},
		},
	}, state)
	if !ok {
		t.Fatal("expected reasoning type part payload")
	}
	part := mapField(payload, "part")
	if stringField(part, "type") != "reasoning" || extractString(part["text"]) != "The user is asking what I can help with." {
		t.Fatalf("expected reasoning part delta, got %#v", part)
	}
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
