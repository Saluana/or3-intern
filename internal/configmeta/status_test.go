package configmeta

import (
	"testing"

	"or3-intern/internal/config"
)

func TestStatusForConfigureKey_RunnerFirstHidesRunnerFieldsFromUI(t *testing.T) {
	// The or3-intern built-in agent loop is gone; runner-first mode hides
	// every `agentCLI.*` field from the settings UI. The values are still
	// read at runtime by the runner host, but no UI control writes them.
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	if got := StatusForConfigureKey(cfg, "agentCLI_default_runner"); got != FieldStatusHidden {
		t.Fatalf("status = %q, want hidden", got)
	}
	if got := StatusForConfigureKey(cfg, "agentCLI_enabled"); got != FieldStatusHidden {
		t.Fatalf("status = %q, want hidden", got)
	}
}
