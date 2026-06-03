package doctor

import (
	"strings"
	"testing"

	"or3-intern/internal/config"
)

func TestAgentCLIFindingsDisabledRunner(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = false
	report := Evaluate(cfg, Options{Mode: ModeAdvisory})
	found := false
	for _, f := range report.Findings {
		if f.ID == "agent_cli.disabled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected disabled runner finding, got %#v", report.Findings)
	}
}

func TestAgentCLIFindingsInvalidDefaultRunner(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.DefaultRunner = "or3-intern"
	report := Evaluate(cfg, Options{Mode: ModeConfigurePostSave})
	found := false
	for _, f := range report.Findings {
		if f.ID == "agent_cli.default_runner_invalid" {
			found = true
			if !strings.Contains(f.Detail, "or3-intern") {
				t.Fatalf("unexpected detail: %q", f.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("expected invalid default runner finding, got %#v", report.Findings)
	}
}
