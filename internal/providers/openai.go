package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	APIBase string
	APIKey  string
	HTTP    *http.Client
}

func New(apiBase, apiKey string, timeout time.Duration) *Client {
	return &Client{
		APIBase: apiBase,
		APIKey: apiKey,
		HTTP: &http.Client{Timeout: timeout},
	}
}

type ChatMessage struct {
	Role string `json:"role"`
	Content any `json:"content,omitempty"` // string|null
	Name string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDef struct {
	Type string `json:"type"`
	Function ToolFunc `json:"function"`
}
type ToolFunc struct {
	Name string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters any `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID string `json:"id"`
	Type string `json:"type"`
	Function struct{
		Name string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionRequest struct {
	Model string `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools []ToolDef `json:"tools,omitempty"`
	ToolChoice any `json:"tool_choice,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct{
		Message struct{
			Role string `json:"role"`
			Content any `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) Chat(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	var out ChatCompletionResponse
	b, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
	if err != nil { return out, err }
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" { r.Header.Set("Authorization", "Bearer "+c.APIKey) }

	resp, err := c.HTTP.Do(r)
	if err != nil { return out, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

type EmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}
type EmbeddingResponse struct {
	Data []struct{
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, model, input string) ([]float32, error) {
	var out EmbeddingResponse
	b, _ := json.Marshal(EmbeddingRequest{Model: model, Input: input})
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/embeddings", bytes.NewReader(b))
	if err != nil { return nil, err }
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" { r.Header.Set("Authorization", "Bearer "+c.APIKey) }

	resp, err := c.HTTP.Do(r)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil { return nil, err }
	if len(out.Data) == 0 { return nil, fmt.Errorf("no embedding returned") }
	return out.Data[0].Embedding, nil
}
