package configmeta

import (
	"testing"

	"or3-intern/internal/config"
)

func TestStatusForConfigureKey_RunnerFirstKeepsRunnerFieldsActive(t *testing.T) {
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	if got := StatusForConfigureKey(cfg, "agentCLI_default_runner"); got != FieldStatusActive {
		t.Fatalf("status = %q, want active", got)
	}
}
