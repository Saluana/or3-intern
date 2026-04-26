package tools

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/artifacts"
)

type ReadArtifact struct {
	Base
	Store        *artifacts.Store
	MaxReadBytes int64
}

func (t *ReadArtifact) Name() string { return "read_artifact" }
func (t *ReadArtifact) Description() string {
	return "Fetch bounded artifact content by artifact ID for the current authorized session."
}
func (t *ReadArtifact) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"artifact_id": map[string]any{"type": "string"},
		"maxBytes":    map[string]any{"type": "integer", "description": "Max bytes to read (default bounded by runtime config)"},
		"offset":      map[string]any{"type": "integer", "description": "Byte offset to start reading from, useful for reading later chunks of large artifacts"},
	}, "required": []string{"artifact_id"}}
}
func (t *ReadArtifact) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *ReadArtifact) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Store == nil {
		return "", fmt.Errorf("artifact store not set")
	}
	artifactID := strings.TrimSpace(fmt.Sprint(params["artifact_id"]))
	if artifactID == "" || artifactID == "<nil>" {
		return "", fmt.Errorf("missing artifact_id")
	}
	maxBytes := t.MaxReadBytes
	if v, ok := params["maxBytes"].(float64); ok && int64(v) > 0 {
		maxBytes = int64(v)
	}
	offset := int64(0)
	if v, ok := params["offset"].(float64); ok && int64(v) > 0 {
		offset = int64(v)
	}
	result, err := t.Store.ReadCappedFrom(ctx, SessionFromContext(ctx), artifactID, offset, maxBytes)
	if err != nil {
		return "", err
	}
	marker := ""
	if result.Truncated {
		marker = "\n...[truncated]"
	}
	return fmt.Sprintf("artifact_id: %s\nsession_key: %s\nmime: %s\nsize_bytes: %d\noffset: %d\nread_bytes: %d\n\n%s%s",
		result.Artifact.ID, result.Artifact.SessionKey, result.Artifact.Mime, result.Artifact.SizeBytes, offset, result.ReadBytes, result.Content, marker), nil
}
