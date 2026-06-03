package turns

import (
	"strings"
	"testing"
)

func TestDecodeAttachmentsAcceptsWorkspaceRef(t *testing.T) {
	atts := DecodeAttachments([]any{
		map[string]any{
			"id":       "att-1",
			"source":   "workspace_ref",
			"path":     "README.md",
			"filename": "README.md",
		},
	})
	if len(atts) != 1 {
		t.Fatalf("expected one attachment, got %#v", atts)
	}
	if atts[0].Name != "README.md" || atts[0].Kind != "file" {
		t.Fatalf("expected normalized attachment, got %#v", atts[0])
	}
	if err := ValidateAttachments(atts); err != nil {
		t.Fatalf("ValidateAttachments: %v", err)
	}
	body := RenderUserAttachmentsBody(atts)
	if body == "" || !strings.Contains(body, `path="README.md"`) {
		t.Fatalf("expected rendered attachment body with path, got %q", body)
	}
}
