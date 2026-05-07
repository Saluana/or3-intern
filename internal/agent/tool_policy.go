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
	case "ask", "work", "admin":
		return allowlistForMode(base, mode), true, nil
	default:
		return nil, false, fmt.Errorf("unsupported tool_policy mode: %s", policy.Mode)
	}
}

func allowlistForMode(base *tools.Registry, mode string) []string {
	if base == nil {
		return nil
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	out := make([]string, 0, len(base.Names()))
	for _, name := range base.Names() {
		tool := base.Get(name)
		if tool == nil {
			continue
		}
		meta := base.Metadata(name)
		if meta.Hidden || hasToolPolicyGroup(meta.Groups, tools.ToolGroupHidden) {
			continue
		}
		capability := tools.ToolCapability(tool, nil)
		switch mode {
		case "ask":
			if capabilityRankForPolicy(capability) > capabilityRankForPolicy(tools.CapabilitySafe) {
				continue
			}
			if hasAnyToolPolicyGroup(meta.Groups, []string{
				tools.ToolGroupWrite,
				tools.ToolGroupExec,
				tools.ToolGroupCron,
				tools.ToolGroupService,
				tools.ToolGroupChannels,
			}) {
				continue
			}
			if hasAnyToolPolicyGroup(meta.Groups, []string{
				tools.ToolGroupRead,
				tools.ToolGroupMemory,
				tools.ToolGroupWeb,
				tools.ToolGroupSkills,
				tools.ToolGroupMCP,
			}) {
				out = append(out, name)
			}
		case "work":
			if capabilityRankForPolicy(capability) > capabilityRankForPolicy(tools.CapabilityGuarded) {
				continue
			}
			if hasAnyToolPolicyGroup(meta.Groups, []string{
				tools.ToolGroupService,
				tools.ToolGroupCron,
				tools.ToolGroupChannels,
			}) {
				continue
			}
			out = append(out, name)
		case "admin":
			out = append(out, name)
		}
	}
	return out
}

func hasAnyToolPolicyGroup(groups []string, wanted []string) bool {
	for _, group := range wanted {
		if hasToolPolicyGroup(groups, group) {
			return true
		}
	}
	return false
}

func hasToolPolicyGroup(groups []string, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	for _, group := range groups {
		if strings.ToLower(strings.TrimSpace(group)) == wanted {
			return true
		}
	}
	return false
}

func capabilityRankForPolicy(level tools.CapabilityLevel) int {
	switch level {
	case tools.CapabilityPrivileged:
		return 3
	case tools.CapabilityGuarded:
		return 2
	case tools.CapabilitySafe:
		return 1
	default:
		return 0
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
