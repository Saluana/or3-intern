package agent

import (
	"fmt"
	"strings"

	"or3-intern/internal/tools"
)

type ServiceToolPolicy struct {
	Mode         string
	AllowedTools []string
	BlockedTools []string
}

func ResolveServiceToolAllowlist(base *tools.Registry, policy *ServiceToolPolicy, legacyAllowed []string) ([]string, bool, error) {
	if policy == nil {
		if len(normalizeToolNames(legacyAllowed)) > 0 {
			return nil, false, fmt.Errorf("tool_policy.mode is required")
		}
		return nil, false, nil
	}

	mode := strings.ToLower(strings.TrimSpace(policy.Mode))
	allowed := normalizeToolNames(policy.AllowedTools)
	if mode == "" {
		return nil, false, fmt.Errorf("tool_policy mode is required")
	}
	if err := validateServiceToolNames(base, allowed); err != nil {
		return nil, false, err
	}
	if err := validateServiceToolNames(base, normalizeToolNames(policy.BlockedTools)); err != nil {
		return nil, false, err
	}

	switch mode {
	case "allow_all":
		return nil, false, nil
	case "deny_all":
		return []string{}, true, nil
	case "allow_list":
		return allowed, true, nil
	case "deny_list":
		blocked := make(map[string]struct{}, len(policy.BlockedTools))
		for _, name := range normalizeToolNames(policy.BlockedTools) {
			blocked[name] = struct{}{}
		}
		if base == nil {
			return nil, false, nil
		}
		allowed := make([]string, 0, len(base.Names()))
		for _, name := range base.Names() {
			if _, ok := blocked[name]; ok {
				continue
			}
			allowed = append(allowed, name)
		}
		return allowed, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported tool_policy mode: %s", policy.Mode)
	}
}

func validateServiceToolNames(base *tools.Registry, names []string) error {
	if base == nil || len(names) == 0 {
		return nil
	}
	for _, name := range names {
		if base.Get(name) == nil {
			return fmt.Errorf("unknown tool in tool_policy: %s", name)
		}
	}
	return nil
}

func normalizeToolNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
