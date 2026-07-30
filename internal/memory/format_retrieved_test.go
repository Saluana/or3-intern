package memory

import "testing"

func TestFormatRetrievedBrief(t *testing.T) {
	out := FormatRetrievedBrief([]Retrieved{{ID: 1, Kind: "note", Text: "hello world", Score: 0.9, Source: "hybrid"}}, 500)
	if out == "(none)" || out == "" {
		t.Fatalf("expected formatted memory, got %q", out)
	}
}
