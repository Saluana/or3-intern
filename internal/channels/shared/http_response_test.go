package shared

import (
	"strings"
	"testing"
)

func TestDecodeJSONLimitedRejectsOversizedResponse(t *testing.T) {
	var out map[string]any
	body := `{"value":"` + strings.Repeat("x", int(MaxAPIResponseBytes)) + `"}`
	if err := DecodeJSONLimited(strings.NewReader(body), &out); err == nil {
		t.Fatal("expected oversized channel response to be rejected")
	}
}

func TestReadErrorPreviewIsBounded(t *testing.T) {
	preview := ReadErrorPreview(strings.NewReader(strings.Repeat("x", 128<<10)))
	if len(preview) > (64<<10)+len("...[truncated]") {
		t.Fatalf("error preview was not bounded: %d bytes", len(preview))
	}
	if !strings.HasSuffix(preview, "...[truncated]") {
		t.Fatal("expected truncation marker")
	}
}
