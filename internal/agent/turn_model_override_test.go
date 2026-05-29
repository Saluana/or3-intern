package agent

import (
	"context"
	"testing"
)

func TestTurnModelOverrideFromContext(t *testing.T) {
	ctx := ContextWithTurnModelOverride(context.Background(), "gpt-4.1-mini")
	if got := turnModelOverrideFromContext(ctx); got != "gpt-4.1-mini" {
		t.Fatalf("expected override model, got %q", got)
	}
	if got := turnModelOverrideFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty override without context, got %q", got)
	}
}
