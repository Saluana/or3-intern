package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecServiceCommandPassesGuardedRegistryCeiling(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&ExecTool{
		Timeout:         time.Second,
		AllowedPrograms: []string{"echo"},
	})
	ctx := ContextWithToolGuard(serviceExecContext(), func(_ context.Context, _ Tool, capability CapabilityLevel, _ map[string]any) error {
		if capability != CapabilityGuarded {
			t.Fatalf("expected guarded capability, got %s", capability)
		}
		return nil
	})

	out, err := registry.ExecuteParams(ctx, "exec", map[string]any{"command": `echo "hello ceiling"`})
	if err != nil {
		t.Fatalf("ExecuteParams: %v", err)
	}
	if !strings.Contains(out, "hello ceiling") {
		t.Fatalf("expected echo output, got %q", out)
	}
}
