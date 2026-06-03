package agentcli

import (
	"errors"
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

// SelectableRunners returns runner specs that may be chosen for new chat turns.
func SelectableRunners() []RunnerSpec {
	all := AllRunners()
	out := make([]RunnerSpec, 0, len(all))
	for _, spec := range all {
		if spec.Supports.Chat.ChatSelectable {
			out = append(out, spec)
		}
	}
	return out
}

// ResolveDefaultRunner returns the configured default runner when agent CLI is enabled.
func ResolveDefaultRunner(cfg config.Config) RunnerID {
	if !cfg.AgentCLI.Enabled {
		return ""
	}
	id := RunnerID(strings.ToLower(strings.TrimSpace(cfg.AgentCLI.DefaultRunner)))
	if id == "" {
		return RunnerOpenCode
	}
	return id
}

// ValidateSelectableRunner reports whether id may be used for a new chat turn.
func ValidateSelectableRunner(cfg config.Config, id RunnerID) error {
	if !cfg.AgentCLI.Enabled {
		return errors.New("agent CLI is disabled; enable agentCLI.enabled and configure a default runner")
	}
	trimmed := RunnerID(strings.ToLower(strings.TrimSpace(string(id))))
	if trimmed == "" {
		return errors.New("runner id is required")
	}
	if trimmed == RunnerOR3 {
		return errors.New("built-in OR3 agent runner is deprecated; choose opencode or another external runner")
	}
	spec, ok := NewRunnerRegistry(SelectableRunners(), nil).Spec(trimmed)
	if !ok {
		return fmt.Errorf("unknown runner %q", id)
	}
	if !spec.Supports.Chat.ChatSelectable {
		return fmt.Errorf("runner %q is not chat-selectable", id)
	}
	for _, disabled := range cfg.AgentCLI.DisabledRunners {
		if strings.EqualFold(strings.TrimSpace(disabled), string(trimmed)) {
			return fmt.Errorf("runner %q is disabled by config", id)
		}
	}
	return nil
}

// IsRunnerDisabledByConfig reports whether the runner id is listed in disabledRunners.
func IsRunnerDisabledByConfig(cfg config.Config, id RunnerID) bool {
	for _, disabled := range cfg.AgentCLI.DisabledRunners {
		if strings.EqualFold(strings.TrimSpace(disabled), string(id)) {
			return true
		}
	}
	return false
}
