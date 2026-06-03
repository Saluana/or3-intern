package agentcli

import (
	"testing"

	"or3-intern/internal/config"
)

func TestSelectableRunnersExcludesOR3(t *testing.T) {
	for _, spec := range SelectableRunners() {
		if spec.ID == RunnerOR3 {
			t.Fatalf("OR3 runner must not be chat-selectable, got %#v", spec)
		}
	}
}

func TestResolveDefaultRunner(t *testing.T) {
	cfg := config.Default()
	if got := ResolveDefaultRunner(cfg); got != RunnerOpenCode {
		t.Fatalf("expected opencode default, got %q", got)
	}
	cfg.AgentCLI.Enabled = false
	if got := ResolveDefaultRunner(cfg); got != "" {
		t.Fatalf("expected empty when disabled, got %q", got)
	}
}

func TestValidateSelectableRunnerRejectsOR3(t *testing.T) {
	cfg := config.Default()
	if err := ValidateSelectableRunner(cfg, RunnerOR3); err == nil {
		t.Fatal("expected OR3 rejection")
	}
}
