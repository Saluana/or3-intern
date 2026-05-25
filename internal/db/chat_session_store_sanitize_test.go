package db

import (
	"encoding/json"
	"testing"
)

func TestSanitizeForkPayload_Recursive(t *testing.T) {
	raw := map[string]any{
		"headers": map[string]any{
			"authorization": "Bearer secret-token",
			"content-type":  "application/json",
		},
		"nested": []any{
			map[string]any{"api_key": "sk-test", "note": "safe"},
		},
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := sanitizeForkPayload(string(blob), "parent-session")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal sanitized payload: %v", err)
	}
	headers, ok := parsed["headers"].(map[string]any)
	if !ok {
		t.Fatalf("expected headers map, got %#v", parsed["headers"])
	}
	if _, exists := headers["authorization"]; exists {
		t.Fatalf("expected authorization stripped, got %#v", headers)
	}
	nested, ok := parsed["nested"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("expected nested array, got %#v", parsed["nested"])
	}
	item, ok := nested[0].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %#v", nested[0])
	}
	if _, exists := item["api_key"]; exists {
		t.Fatal("expected nested api_key stripped")
	}
	if parsed["forked_from_session_key"] != "parent-session" {
		t.Fatalf("expected fork marker, got %#v", parsed["forked_from_session_key"])
	}
}
