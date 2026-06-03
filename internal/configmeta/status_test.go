package configmeta

import (
	"testing"

	"or3-intern/internal/config"
)

func TestStatusForConfigureKey_RunnerFirstDeprecatesSubagents(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	if got := StatusForConfigureKey(cfg, "runtime_subagents_enabled"); got != FieldStatusDeprecated {
		t.Fatalf("status = %q, want deprecated", got)
	}
	if got := StatusForConfigureKey(cfg, "agentCLI_default_runner"); got != FieldStatusActive {
		t.Fatalf("status = %q, want active", got)
	}
}

func TestListForConfig_HidesDeprecatedPaths(t *testing.T) {
	Clear()
	Register(ConfigFieldMetadata{
		Section: "subagents",
		Key:     "enabled",
		Path:    "subagents.enabled",
		Label:   "Enable subagents",
	})
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	fields := ListForConfig(cfg)
	if len(fields) != 0 {
		t.Fatalf("expected hidden subagents field, got %d fields", len(fields))
	}
}
