package tools

import (
	"encoding/json"
	"strings"
)

// ToolResult is the bounded result envelope shared by high-volume tools.
type ToolResult struct {
	Kind       string         `json:"kind"`
	OK         bool           `json:"ok"`
	Summary    string         `json:"summary,omitempty"`
	Preview    string         `json:"preview,omitempty"`
	ArtifactID string         `json:"artifact_id,omitempty"`
	Stats      map[string]any `json:"stats,omitempty"`
}

func EncodeToolResult(result ToolResult) string {
	if strings.TrimSpace(result.Kind) == "" {
		result.Kind = "tool_result"
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return `{"kind":"tool_result","ok":false,"summary":"failed to encode tool result"}`
	}
	return string(b)
}

func PreviewString(s string, maxBytes int) (string, bool) {
	s = strings.TrimSpace(s)
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}
	return s[:maxBytes] + "\n...[preview truncated]", true
}
