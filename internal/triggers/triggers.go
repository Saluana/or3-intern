// Package triggers defines shared metadata for webhook and filewatch events.
package triggers

import (
	"encoding/json"
	"strings"
)

// TriggerMeta carries metadata from trigger events.
type TriggerMeta struct {
	Source  string            // "webhook" or "filewatch"
	Path    string            // for file-change events
	Route   string            // for webhook events
	Headers map[string]string // for webhook events (limited subset)
}

// MetaKeyStructuredEvent stores normalized structured trigger metadata.
const MetaKeyStructuredEvent = "structured_event"

// StructuredEvent is the normalized envelope attached to structured trigger events.
type StructuredEvent struct {
	Type    string         `json:"type"`
	Source  string         `json:"source"`
	Trusted bool           `json:"trusted"`
	Details map[string]any `json:"details,omitempty"`
}

// StructuredEventMap converts event to a plain map for message metadata.
func StructuredEventMap(event StructuredEvent) map[string]any {
	details := map[string]any{}
	for key, value := range event.Details {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		details[trimmed] = value
	}
	return map[string]any{
		"type":    strings.TrimSpace(event.Type),
		"source":  strings.TrimSpace(event.Source),
		"trusted": event.Trusted,
		"details": details,
	}
}

// StructuredEventJSON pretty-prints raw structured trigger metadata.
func StructuredEventJSON(raw any) string {
	if raw == nil {
		return ""
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
