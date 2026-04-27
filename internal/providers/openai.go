// Package providers wraps the OpenAI-compatible chat and embedding APIs used by or3-intern.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/security"
)

// Client talks to an OpenAI-compatible HTTP API.
type Client struct {
	APIBase         string
	APIKey          string
	HTTP            *http.Client
	EmbedDimensions int
	HostPolicy      security.HostPolicy
}

// EmbeddingFingerprint identifies the embedding space used for persisted
// vectors. It must change when either the provider endpoint or embedding model
// changes, even if the vector dimensionality stays the same.
func EmbeddingFingerprint(apiBase, model string, dimensions int) string {
	base := strings.ToLower(strings.TrimRight(strings.TrimSpace(apiBase), "/"))
	model = strings.TrimSpace(model)
	if base == "" && model == "" && dimensions <= 0 {
		return ""
	}
	if dimensions > 0 {
		return fmt.Sprintf("%s|%s|dims=%d", base, model, dimensions)
	}
	return base + "|" + model
}

// New constructs a Client for apiBase using timeout for all requests.
func New(apiBase, apiKey string, timeout time.Duration) *Client {
	return &Client{
		APIBase: apiBase,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// ChatMessage is one message sent to or returned from the provider.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"` // string|null
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// SupportsExplicitPromptCache reports whether the configured endpoint is known
// to accept Anthropic-style cache-control metadata on message content blocks.
// The default OpenAI-compatible path remains unchanged when this is false.
func (c *Client) SupportsExplicitPromptCache() bool {
	if c == nil {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(c.APIBase))
	return strings.Contains(base, "anthropic") || strings.Contains(base, "claude")
}

// BuildCacheAwareSystemContent returns a system-message content payload that
// preserves a stable prefix boundary for providers that understand explicit
// cache-control metadata. Callers should use this only when
// SupportsExplicitPromptCache returns true; otherwise a plain concatenated
// string should be sent for maximum compatibility.
func BuildCacheAwareSystemContent(stable, volatile string) any {
	stable = strings.TrimSpace(stable)
	volatile = strings.TrimSpace(volatile)
	parts := make([]map[string]any, 0, 2)
	if stable != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": stable,
			"cache_control": map[string]any{
				"type": "ephemeral",
			},
		})
	}
	if volatile != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": volatile,
		})
	}
	if len(parts) == 0 {
		return ""
	}
	return parts
}

// ToolDef declares a callable tool in provider request format.
type ToolDef struct {
	Type     string   `json:"type"`
	Function ToolFunc `json:"function"`
}
type ToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ToolCall is one tool invocation requested by the provider.
type ToolCall struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatCompletionRequest is the non-streaming chat completion payload.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// ChatCompletionResponse is the normalized response from a chat completion.
type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   any        `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// Chat performs a non-streaming chat completion request.
func (c *Client) Chat(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	var out ChatCompletionResponse
	b, _ := json.Marshal(req)
	const maxAttempts = 2
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
		if err != nil {
			return out, err
		}
		r.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			r.Header.Set("Authorization", "Bearer "+c.APIKey)
		}

		resp, err := c.do(r)
		if err != nil {
			return out, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return out, readErr
		}
		if resp.StatusCode >= 300 {
			return out, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
		}
		if err := json.Unmarshal(body, &out); err != nil {
			lastErr = formatProviderDecodeError(err, body)
			if attempt < maxAttempts && isRetryableProviderDecodeError(err, body) {
				continue
			}
			return out, lastErr
		}
		return out, nil
	}
	if lastErr != nil {
		return out, lastErr
	}
	return out, fmt.Errorf("provider decode failed")
}

func isRetryableProviderDecodeError(err error, body []byte) bool {
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "unexpected end of json input")
}

func formatProviderDecodeError(err error, body []byte) error {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Errorf("provider returned empty response body")
	}
	return fmt.Errorf("provider decode error: %w; body=%q", err, trimToRunes(trimmed, 240))
}

func trimToRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// ChatCompletionStreamRequest is sent when stream=true.
type ChatCompletionStreamRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream"`
}

// ChatStreamDelta is one incremental SSE delta from a streamed completion.
type ChatStreamDelta struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChatStreamChoice is one choice entry from a streamed completion chunk.
type ChatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        ChatStreamDelta `json:"delta"`
	FinishReason string          `json:"finish_reason"`
}

// ChatStreamChunk is one SSE payload from a streamed completion.
type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Choices []ChatStreamChoice `json:"choices"`
}

// ChatStream sends the request with stream=true, calls onDelta for each text
// delta, and returns the fully accumulated completion response.
func (c *Client) ChatStream(ctx context.Context, req ChatCompletionRequest, onDelta func(text string)) (ChatCompletionResponse, error) {
	streamReq := ChatCompletionStreamRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
		Temperature: req.Temperature,
		Stream:      true,
	}
	b, _ := json.Marshal(streamReq)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.do(r)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return ChatCompletionResponse{}, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if readErr != nil {
			return ChatCompletionResponse{}, readErr
		}
		var out ChatCompletionResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return ChatCompletionResponse{}, formatProviderDecodeError(err, body)
		}
		return out, nil
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var contentBuilder strings.Builder
	var finalToolCalls []ToolCall
	sawData := false

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		sawData = true
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		if len(delta.ToolCalls) > 0 {
			finalToolCalls = mergeStreamToolCalls(finalToolCalls, delta.ToolCalls)
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatCompletionResponse{}, err
	}
	if !sawData {
		return ChatCompletionResponse{}, fmt.Errorf("provider stream returned no data events")
	}

	out := ChatCompletionResponse{
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
					Role:      "assistant",
					Content:   contentBuilder.String(),
					ToolCalls: finalToolCalls,
				},
			},
		},
	}
	return out, nil
}

// mergeStreamToolCalls accumulates tool-call deltas arriving over SSE.
// OpenAI streaming sends each piece as {index, partial args}; we expand the
// slice to the required index and concatenate name/arguments incrementally.
func mergeStreamToolCalls(existing []ToolCall, delta []ToolCall) []ToolCall {
	for _, d := range delta {
		idx := d.Index
		for len(existing) <= idx {
			existing = append(existing, ToolCall{})
		}
		existing[idx].Function.Arguments += d.Function.Arguments
		if d.Function.Name != "" {
			existing[idx].Function.Name += d.Function.Name
		}
		if d.ID != "" {
			existing[idx].ID = d.ID
		}
		if d.Type != "" {
			existing[idx].Type = d.Type
		}
		existing[idx].Index = idx
	}
	return existing
}

type EmbeddingRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}
type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, model, input string) ([]float32, error) {
	var out EmbeddingResponse
	reqBody := EmbeddingRequest{Model: model, Input: input}
	if c != nil && c.EmbedDimensions > 0 {
		reqBody.Dimensions = c.EmbedDimensions
	}
	b, _ := json.Marshal(reqBody)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return out.Data[0].Embedding, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if c.HostPolicy.EnabledPolicy() {
		client = security.WrapHTTPClient(client, c.HostPolicy)
	}
	return client.Do(req)
}
