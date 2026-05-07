package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type typedArgsTool struct {
	tools.Base
	called bool
	params map[string]any
}

func (t *typedArgsTool) Name() string        { return "typed_args_tool" }
func (t *typedArgsTool) Description() string { return "typed args" }
func (t *typedArgsTool) Parameters() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"count", "enabled", "tags", "payload", "mode"},
		"properties": map[string]any{
			"count":   map[string]any{"type": "integer"},
			"enabled": map[string]any{"type": "boolean"},
			"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"payload": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
			"mode":    map[string]any{"type": "string", "enum": []string{"fast", "safe"}},
		},
	}
}
func (t *typedArgsTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	t.called = true
	t.params = params
	return "typed ok", nil
}
func (t *typedArgsTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func TestToolArgumentValidator_SafeCoercions(t *testing.T) {
	tool := &typedArgsTool{}
	result := ToolArgumentValidator{}.ValidateAndCoerce(tool, `{"count":"2","enabled":"true","tags":"one","payload":"{\"name\":\"x\"}","mode":"fast"}`)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %#v", result.Errors)
	}
	if len(result.Coercions) != 4 {
		t.Fatalf("expected four coercions, got %#v", result.Coercions)
	}
	if result.Params["count"] != float64(2) || result.Params["enabled"] != true {
		t.Fatalf("unexpected params: %#v", result.Params)
	}
	tags := result.Params["tags"].([]any)
	if len(tags) != 1 || tags[0] != "one" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}

func TestToolArgumentValidator_RejectsMalformedMissingEnumAndUnsafeTypes(t *testing.T) {
	tool := &typedArgsTool{}
	cases := []string{
		`{"count":`,
		`{"count":1}`,
		`{"count":"two","enabled":true,"tags":["one"],"payload":{},"mode":"fast"}`,
		`{"count":1,"enabled":true,"tags":["one"],"payload":{},"mode":"turbo"}`,
	}
	for _, raw := range cases {
		result := ToolArgumentValidator{}.ValidateAndCoerce(tool, raw)
		if len(result.Errors) == 0 {
			t.Fatalf("expected validation error for %s", raw)
		}
	}
}

func TestToolArgumentValidator_IntegerKeepsJSONNumberShape(t *testing.T) {
	tool := &typedArgsTool{}
	result := ToolArgumentValidator{}.ValidateAndCoerce(tool, `{"count":"2","enabled":true,"tags":["one"],"payload":{"name":"x"},"mode":"fast"}`)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %#v", result.Errors)
	}
	if _, ok := result.Params["count"].(float64); !ok {
		t.Fatalf("expected count to keep JSON number shape, got %T", result.Params["count"])
	}
}

func TestToolArgumentValidator_RejectsStringSliceEnum(t *testing.T) {
	tool := &typedArgsTool{}
	result := ToolArgumentValidator{}.ValidateAndCoerce(tool, `{"count":1,"enabled":true,"tags":["one"],"payload":{"name":"x"},"mode":"turbo"}`)
	if len(result.Errors) == 0 || result.Errors[0].Code != "enum" {
		t.Fatalf("expected enum validation error, got %#v", result.Errors)
	}
}

func TestNormalizeProviderToolCalls_DedupesAndGeneratesIDs(t *testing.T) {
	calls := []providers.ToolCall{
		{Index: 0, ID: "provider_1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "exec", Arguments: `{"command":"echo hi"}`}},
		{Index: 0, ID: "provider_1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "exec", Arguments: `{"command":"echo hi"}`}},
		{Index: 1, Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read_file", Arguments: `{"path":"README.md"}`}},
	}
	normalized := normalizeProviderToolCalls(calls, ToolCallSourceProvider, "turn")
	if len(normalized) != 2 {
		t.Fatalf("expected deduped calls, got %#v", normalized)
	}
	if normalized[1].ID == "" || normalized[1].ProviderID != "" {
		t.Fatalf("expected generated local ID for missing provider ID, got %#v", normalized[1])
	}
}

func TestAvailableNormalizedToolCalls_FiltersBlankAndUnknownNames(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&requiredTextTool{})
	calls := []NormalizedToolCall{
		{ID: "blank", Name: "", ArgumentsJSON: "{}"},
		{ID: "unknown", Name: "missing_tool", ArgumentsJSON: "{}"},
		{ID: "known", Name: "required_text_tool", ArgumentsJSON: `{"text":"ok"}`},
	}
	available := availableNormalizedToolCalls(calls, reg)
	if len(available) != 1 || available[0].ID != "known" {
		t.Fatalf("expected only known call, got %#v", available)
	}
}

func TestNormalizeProviderToolCalls_DedupesGeneratedCalls(t *testing.T) {
	calls := []providers.ToolCall{
		{Index: 0, Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read_file", Arguments: `{"path":"README.md"}`}},
		{Index: 0, Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read_file", Arguments: `{"path":"README.md"}`}},
	}
	normalized := normalizeProviderToolCalls(calls, ToolCallSourceMarkup, "markup")
	if len(normalized) != 1 {
		t.Fatalf("expected generated duplicate to collapse, got %#v", normalized)
	}
	if normalized[0].Source != ToolCallSourceMarkup || normalized[0].ID == "" {
		t.Fatalf("unexpected generated call: %#v", normalized[0])
	}
}

func TestRuntime_InvalidArgumentsBecomeModelVisibleToolResultThenRetry(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &requiredTextTool{}
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"required_text_tool","arguments":"{}"}}]}}]}`)
		case 2:
			var req providers.ChatCompletionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || !strings.Contains(contentToString(last.Content), "tool_argument_validation_failed") {
				t.Fatalf("expected validation tool result, got %#v", last)
			}
			fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_2","type":"function","function":{"name":"required_text_tool","arguments":"{\"text\":\"fixed\"}"}}]}}]}`)
		default:
			var req providers.ChatCompletionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || contentToString(last.Content) != "fixed" {
				t.Fatalf("expected successful tool result, got %#v", last)
			}
			fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","content":"done"}}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	provider := providers.New(srv.URL, "test-key", 10*time.Second)
	provider.HTTP = srv.Client()
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)
	rt.Tools.Register(tool)
	rt.MaxToolLoops = 4

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-validation-retry",
		Channel:    "cli",
		From:       "user",
		Message:    "call typed tool",
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !tool.called || len(deliver.messages) != 1 || deliver.messages[0] != "done" {
		t.Fatalf("expected fixed tool call and final response, called=%v messages=%#v", tool.called, deliver.messages)
	}
}

func TestRuntime_RepeatedInvalidArgumentsStopsWithValidationError(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &requiredTextTool{}
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"required_text_tool","arguments":"{}"}}]}}]}`)
	}))
	t.Cleanup(srv.Close)
	provider := providers.New(srv.URL, "test-key", 10*time.Second)
	provider.HTTP = srv.Client()
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)
	rt.Tools.Register(tool)
	rt.MaxToolLoops = 4

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-validation-repeated",
		Channel:    "cli",
		From:       "user",
		Message:    "call tool badly",
	})
	if err == nil || !strings.Contains(err.Error(), "validation failed repeatedly") {
		t.Fatalf("expected repeated validation error, got %v", err)
	}
	if tool.called {
		t.Fatal("invalid tool call should not execute")
	}
	if callCount != 2 {
		t.Fatalf("expected two provider attempts before stopping, got %d", callCount)
	}
}
