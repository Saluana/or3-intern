package runners

import (
	"testing"

	"or3-intern/internal/config"
)

func TestResolveDefaultRunner(t *testing.T) {
	cfg := config.Default()
	if got := ResolveDefaultRunner(cfg); got != RunnerOpenCode {
		t.Fatalf("expected opencode default, got %q", got)
	}
}

func TestValidateSelectableRunnerRejectsUnknownRunner(t *testing.T) {
	cfg := config.Default()
	if err := ValidateSelectableRunner(cfg, "or3-intern"); err == nil {
		t.Fatal("expected unknown runner rejection")
	}
}
