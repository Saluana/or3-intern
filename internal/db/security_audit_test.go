package db

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalBoundedAuditPayloadRemainsValidJSON(t *testing.T) {
	payload := map[string]any{
		"tool":    "exec",
		"command": strings.Repeat("x", 4096),
		"nested": map[string]any{
			"blob": strings.Repeat("y", 4096),
		},
		"items": []any{strings.Repeat("z", 1024), strings.Repeat("w", 1024)},
	}
	out, err := marshalBoundedAuditPayload(payload, maxAuditPayloadBytes)
	if err != nil {
		t.Fatalf("marshalBoundedAuditPayload: %v", err)
	}
	if len(out) > maxAuditPayloadBytes {
		t.Fatalf("payload length %d exceeds cap %d", len(out), maxAuditPayloadBytes)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("stored audit payload is not valid JSON: %v\npayload=%q", err, out)
	}
}

func TestMarshalJSONMapRejectsNonSerializableMetadata(t *testing.T) {
	ch := make(chan int)
	_, err := marshalJSONMap(map[string]any{"bad": ch})
	if err == nil {
		t.Fatal("expected marshal error for channel value")
	}
}
