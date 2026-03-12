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
		allowed := normalizeToolNames(legacyAllowed)
		if len(allowed) == 0 {
			return nil, false, nil
		}
		return allowed, true, nil
	}

	mode := strings.ToLower(strings.TrimSpace(policy.Mode))
	allowed := normalizeToolNames(policy.AllowedTools)
	blocked := normalizeToolNames(policy.BlockedTools)
	if mode == "" {
		return nil, false, fmt.Errorf("tool_policy mode is required")
	}

	switch mode {
	case "allow_all":
		return nil, false, nil
	case "deny_all":
		return []string{}, true, nil
	case "allow_list":
		if len(allowed) == 0 {
			return nil, false, fmt.Errorf("tool_policy allow_list requires allowed_tools")
		}
		return allowed, true, nil
	case "deny_list":
		if len(blocked) == 0 {
			return nil, false, fmt.Errorf("tool_policy deny_list requires blocked_tools")
		}
		if base == nil {
			return []string{}, true, nil
		}
		blockedSet := make(map[string]struct{}, len(blocked))
		for _, name := range blocked {
			blockedSet[name] = struct{}{}
		}
		resolved := make([]string, 0, len(base.Names()))
		for _, name := range base.Names() {
			if _, blocked := blockedSet[name]; blocked {
				continue
			}
			resolved = append(resolved, name)
		}
		return resolved, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported tool_policy mode: %s", policy.Mode)
	}
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
