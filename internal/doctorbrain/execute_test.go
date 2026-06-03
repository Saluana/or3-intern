package doctorbrain

import (
	"strings"
	"testing"
)

func TestBoundOutputRespectsByteLimit(t *testing.T) {
	text := strings.Repeat("é", 20)
	got := boundOutput(text, 10)
	if len(got) > 13 { // 10 bytes + "..."
		t.Fatalf("expected byte-bounded output, got len=%d %q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected suffix, got %q", got)
	}
}
