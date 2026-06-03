package agentcli

import (
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

// LegacyRunnerLabel is the display label for historical built-in OR3 sessions.
const LegacyRunnerLabel = "OR3 Intern (legacy)"

// IsLegacyRunnerID reports whether id refers to the removed built-in agent runner.
func IsLegacyRunnerID(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), string(RunnerOR3))
}

// ResolveRunnerIDForTurn maps legacy runner IDs to the configured default runner.
// When migration occurs, legacyID is the previous runner id.
func ResolveRunnerIDForTurn(cfg config.Config, runnerID string) (active string, legacyID string, migrated bool) {
	trimmed := strings.TrimSpace(runnerID)
	if trimmed == "" {
		return string(ResolveDefaultRunner(cfg)), "", false
	}
	if !IsLegacyRunnerID(trimmed) {
		return strings.ToLower(trimmed), "", false
	}
	if !cfg.AgentCLI.Enabled {
		return "", trimmed, false
	}
	return string(ResolveDefaultRunner(cfg)), trimmed, true
}

// LegacyRunnerMigrationError is returned when a turn cannot migrate a legacy session.
func LegacyRunnerMigrationError(legacyID string) error {
	return fmt.Errorf("runner %q is deprecated; install a supported runner (default opencode) and set agentCLI.defaultRunner", legacyID)
}
