package runners

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

// ResolveDefaultRunner returns the configured default runner.
func ResolveDefaultRunner(cfg config.Config) RunnerID {
	id := RunnerID(strings.ToLower(strings.TrimSpace(cfg.Runners.Default)))
	if id == "" {
		return RunnerOpenCode
	}
	return id
}

// ValidateSelectableRunner reports whether id may be used for a new chat turn.
func ValidateSelectableRunner(cfg config.Config, id RunnerID) error {
	trimmed := RunnerID(strings.ToLower(strings.TrimSpace(string(id))))
	if trimmed == "" {
		return errors.New("runner id is required")
	}
	spec, ok := NewRunnerRegistry(SelectableRunners(), nil).Spec(trimmed)
	if !ok {
		return fmt.Errorf("unknown runner %q", id)
	}
	if !spec.Supports.Chat.ChatSelectable {
		return fmt.Errorf("runner %q is not chat-selectable", id)
	}
	for _, disabled := range cfg.Runners.Disabled {
		if strings.EqualFold(strings.TrimSpace(disabled), string(trimmed)) {
			return fmt.Errorf("runner %q is disabled by config", id)
		}
	}
	return nil
}

// IsRunnerDisabledByConfig reports whether the runner id is listed in disabledRunners.
func IsRunnerDisabledByConfig(cfg config.Config, id RunnerID) bool {
	for _, disabled := range cfg.Runners.Disabled {
		if strings.EqualFold(strings.TrimSpace(disabled), string(id)) {
			return true
		}
	}
	return false
}
