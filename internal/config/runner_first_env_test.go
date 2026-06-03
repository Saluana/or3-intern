package config

import (
	"strings"
	"testing"
)

func TestApplyEnvOverrides_RunnerFirstSubagentsWarnings(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()
	cfg.AgentCLI.Enabled = true
	t.Setenv("OR3_SUBAGENTS_ENABLED", "true")
	ApplyEnvOverrides(&cfg)
	if len(cfg.CompatEnvWarnings) == 0 {
		t.Fatal("expected compat warnings for OR3_SUBAGENTS_ENABLED")
	}
	found := false
	for _, warning := range cfg.CompatEnvWarnings {
		if strings.Contains(warning, "OR3_SUBAGENTS_ENABLED") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v", cfg.CompatEnvWarnings)
	}
}
