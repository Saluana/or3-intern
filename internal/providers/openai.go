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
	Fallbacks       []Fallback
}

type Fallback struct {
	Client *Client
	Model  string
}

type ProviderError struct {
	StatusCode int
	Status     string
	Body       string
	Err        error
}

func (e ProviderError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Status != "" {
		return fmt.Sprintf("provider error %s: %s", e.Status, e.Body)
	}
	return "provider error"
}

func (e ProviderError) Unwrap() error { return e.Err }

func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode == http.StatusRequestTimeout ||
			providerErr.StatusCode == http.StatusTooManyRequests ||
			providerErr.StatusCode >= 500
	}
	var streamErr ProviderStreamError
	if errors.As(err, &streamErr) {
		return streamErr.Retryable
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "temporary") || strings.Contains(msg, "connection reset")
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
	resp, err := c.chatOnce(ctx, req)
	if err == nil || c == nil || !IsTransientError(err) {
		return resp, err
	}
	var lastErr error = err
	for _, fallback := range c.Fallbacks {
		if fallback.Client == nil {
			continue
		}
		nextReq := req
		if strings.TrimSpace(fallback.Model) != "" {
			nextReq.Model = strings.TrimSpace(fallback.Model)
		}
		resp, err = fallback.Client.chatOnce(ctx, nextReq)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !IsTransientError(err) {
			break
		}
	}
	return ChatCompletionResponse{}, lastErr
}

func (c *Client) chatOnce(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
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
			return out, ProviderError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(body), Err: fmt.Errorf("provider error %s: %s", resp.Status, string(body))}
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
	emitted := false
	resp, err := c.chatStreamOnce(ctx, req, func(text string) {
		if text != "" {
			emitted = true
		}
		if onDelta != nil {
			onDelta(text)
		}
	})
	if err == nil || emitted || c == nil || !IsTransientError(err) {
		return resp, err
	}
	var streamErr ProviderStreamError
	if errors.As(err, &streamErr) && c.ProviderProfile(req.Model).Retry.FallbackToNonStream {
		originalErr := err
		resp, err = c.Chat(ctx, req)
		if err == nil {
			if len(resp.Choices) > 0 && onDelta != nil {
				if text := contentToStreamString(resp.Choices[0].Message.Content); text != "" {
					onDelta(text)
				}
			}
			return resp, nil
		}
		err = originalErr
	}
	var lastErr error = err
	for _, fallback := range c.Fallbacks {
		if fallback.Client == nil {
			continue
		}
		nextReq := req
		if strings.TrimSpace(fallback.Model) != "" {
			nextReq.Model = strings.TrimSpace(fallback.Model)
		}
		resp, err = fallback.Client.chatStreamOnce(ctx, nextReq, onDelta)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !IsTransientError(err) {
			break
		}
	}
	return ChatCompletionResponse{}, lastErr
}

func (c *Client) chatStreamOnce(ctx context.Context, req ChatCompletionRequest, onDelta func(text string)) (ChatCompletionResponse, error) {
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
		return ChatCompletionResponse{}, ProviderError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(body), Err: fmt.Errorf("provider error %s: %s", resp.Status, string(body))}
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
	assembler := StreamAssembler{Profile: c.ProviderProfile(req.Model)}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			assembler.RecordMalformed(data)
			continue
		}
		for _, event := range assembler.ApplyChunk(chunk) {
			if event.TextDelta != "" && onDelta != nil {
				onDelta(event.TextDelta)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatCompletionResponse{}, err
	}
	assistant, err := assembler.Finalize()
	if err != nil {
		return ChatCompletionResponse{}, err
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
					Role:      assistant.Role,
					Content:   assistant.Content,
					ToolCalls: assistant.ToolCalls,
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
	acc := toolCallAccumulator{calls: existing}
	return acc.Apply(delta)
}

func contentToStreamString(v any) string {
	if v == nil {
		return ""
	}
	if text, ok := v.(string); ok {
		return text
	}
	b, _ := json.Marshal(v)
	return string(b)
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
	vec, err := c.embedOnce(ctx, model, input)
	if err == nil || c == nil || !IsTransientError(err) {
		return vec, err
	}
	var lastErr error = err
	for _, fallback := range c.Fallbacks {
		if fallback.Client == nil {
			continue
		}
		nextModel := model
		if strings.TrimSpace(fallback.Model) != "" {
			nextModel = strings.TrimSpace(fallback.Model)
		}
		vec, err = fallback.Client.embedOnce(ctx, nextModel, input)
		if err == nil {
			return vec, nil
		}
		lastErr = err
		if !IsTransientError(err) {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) embedOnce(ctx context.Context, model, input string) ([]float32, error) {
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
		return nil, ProviderError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(body), Err: fmt.Errorf("provider error %s: %s", resp.Status, string(body))}
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
