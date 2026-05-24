package controlplane

import (
	"strings"
	"testing"
)

func TestValidateScopeLinkInput(t *testing.T) {
	if err := ValidateScopeLinkInput("sess-a", "scope-1", nil); err != nil {
		t.Fatalf("expected valid input, got %v", err)
	}
	if err := ValidateScopeLinkInput("", "scope-1", nil); err == nil {
		t.Fatal("expected empty session_key error")
	}
	if err := ValidateScopeLinkInput("sess a", "scope-1", nil); err == nil {
		t.Fatal("expected invalid session_key error")
	}
	longReason := strings.Repeat("r", maxScopeReasonLen+1)
	if err := ValidateScopeLinkInput("sess-a", "scope-1", map[string]any{"reason": longReason}); err == nil {
		t.Fatal("expected reason length error")
	}
	if err := ValidateScopeLinkInput("sess-a", "scope-1", map[string]any{"reason": "secret:do-not"}); err == nil {
		t.Fatal("expected reserved reason prefix error")
	}
}
