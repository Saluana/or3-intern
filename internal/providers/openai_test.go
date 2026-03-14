package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func mustEncodeResponse(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestNew(t *testing.T) {
	c := New("https://api.example.com", "my-key", 30*time.Second)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.APIBase != "https://api.example.com" {
		t.Errorf("expected APIBase='https://api.example.com', got %q", c.APIBase)
	}
	if c.APIKey != "my-key" {
		t.Errorf("expected APIKey='my-key', got %q", c.APIKey)
	}
	if c.HTTP == nil {
		t.Error("expected non-nil HTTP client")
	}
}

func TestChat_Success(t *testing.T) {
	response := ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   any        `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string     `json:"role"`
					Content   any        `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role:    "assistant",
					Content: "Hello! How can I help?",
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncodeResponse(t, w, response)
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		APIKey:  "test-key",
		HTTP:    srv.Client(),
	}

	req := ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}

	resp, err := c.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", resp.Choices[0].Message.Role)
	}
}

func TestChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		APIKey:  "bad-key",
		HTTP:    srv.Client(),
	}

	_, err := c.Chat(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
}

func TestChat_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		HTTP:    srv.Client(),
	}

	_, err := c.Chat(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestChat_NoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not have Authorization header
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncodeResponse(t, w, ChatCompletionResponse{})
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		APIKey:  "", // empty
		HTTP:    srv.Client(),
	}

	if _, err := c.Chat(context.Background(), ChatCompletionRequest{}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_WithToolCalls(t *testing.T) {
	response := ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   any        `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string     `json:"role"`
					Content   any        `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "exec",
								Arguments: `{"command":"echo hi"}`,
							},
						},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncodeResponse(t, w, response)
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	resp, err := c.Chat(context.Background(), ChatCompletionRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "exec" {
		t.Errorf("expected tool name 'exec', got %q", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}
}

func TestEmbed_Success(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	response := EmbeddingResponse{
		Data: []struct {
			Embedding []float32 `json:"embedding"`
		}{
			{Embedding: embedding},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncodeResponse(t, w, response)
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	vec, err := c.Embed(context.Background(), "text-embedding-3-small", "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != len(embedding) {
		t.Fatalf("expected %d elements, got %d", len(embedding), len(vec))
	}
	for i, v := range embedding {
		if vec[i] != v {
			t.Errorf("vec[%d] = %v, want %v", i, vec[i], v)
		}
	}
}

func TestEmbed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "server error")
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), "model", "text")
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
}

func TestEmbed_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), "model", "text")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEmbed_NoData(t *testing.T) {
	response := EmbeddingResponse{Data: []struct {
		Embedding []float32 `json:"embedding"`
	}{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustEncodeResponse(t, w, response)
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), "model", "text")
	if err == nil {
		t.Fatal("expected error when no embedding data returned")
	}
}

func TestChat_ContextCanceled(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until test signals done or timeout
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := &Client{APIBase: srv.URL, HTTP: &http.Client{Timeout: 200 * time.Millisecond}}
	_, err := c.Chat(ctx, ChatCompletionRequest{})

	close(done) // unblock server handlers
	srv.Close()

	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestEmbed_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		mustEncodeResponse(t, w, EmbeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
			}{{Embedding: []float32{0.1}}},
		})
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, APIKey: "my-embed-key", HTTP: srv.Client()}
	if _, err := c.Embed(context.Background(), "model", "text"); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if gotAuth != "Bearer my-embed-key" {
		t.Errorf("expected 'Bearer my-embed-key', got %q", gotAuth)
	}
}
