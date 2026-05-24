package config

import (
	"path/filepath"
	"strings"
)

const (
	AccessLevelReader   = "reader"
	AccessLevelOperator = "operator"
	AccessLevelAdmin    = "admin"

	AccessProfileWorkspaceDir = "${workspaceDir}"
)

// BuiltinAccessProfiles returns the simple product-level access profiles used
// by channel and device setup. Paths intentionally use a workspace placeholder
// so configs stay portable when a workspace moves.
func BuiltinAccessProfiles() map[string]AccessProfileConfig {
	return map[string]AccessProfileConfig{
		AccessLevelReader: {
			MaxCapability: "safe",
			AllowedTools: []string{
				"read_file",
				"search_file",
				"list_dir",
				"read_artifact",
				"memory_search",
				"memory_recent",
				"memory_get_pinned",
			},
			AllowedHosts:   []string{},
			WritablePaths:  []string{},
			AllowSubagents: false,
		},
		AccessLevelOperator: {
			MaxCapability: "guarded",
			AllowedTools: []string{
				"read_file",
				"search_file",
				"list_dir",
				"read_artifact",
				"write_file",
				"edit_file",
				"delete_file",
				"memory_search",
				"memory_recent",
				"memory_get_pinned",
				"web_search",
				"web_fetch",
				"web_fetch_markdown",
				"exec",
			},
			AllowedHosts:   []string{},
			WritablePaths:  []string{AccessProfileWorkspaceDir},
			AllowSubagents: false,
		},
		AccessLevelAdmin: {
			MaxCapability: "privileged",
			AllowedTools: []string{
				"read_file",
				"search_file",
				"list_dir",
				"read_artifact",
				"write_file",
				"edit_file",
				"delete_file",
				"memory_set_pinned",
				"memory_add_note",
				"memory_search",
				"memory_recent",
				"memory_get_pinned",
				"web_search",
				"web_fetch",
				"web_fetch_markdown",
				"exec",
				"run_skill",
				"run_skill_script",
				"spawn_subagent",
				"send_message",
				"cron",
			},
			AllowedHosts:   []string{},
			WritablePaths:  []string{AccessProfileWorkspaceDir},
			AllowSubagents: true,
		},
	}
}

func NormalizeAccessLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case AccessLevelReader, "viewer", "read", "readonly", "read-only":
		return AccessLevelReader
	case AccessLevelOperator, "moderator", "files", "control":
		return AccessLevelOperator
	case AccessLevelAdmin, "owner", "administrator":
		return AccessLevelAdmin
	default:
		return ""
	}
}

func AccessLevelToDeviceRole(level string) string {
	switch NormalizeAccessLevel(level) {
	case AccessLevelReader:
		return "viewer"
	case AccessLevelAdmin:
		return "admin"
	case AccessLevelOperator:
		return "operator"
	default:
		return ""
	}
}

func EnsureBuiltinAccessProfiles(profiles *AccessProfilesConfig) {
	if profiles == nil {
		return
	}
	if profiles.Channels == nil {
		profiles.Channels = map[string]string{}
	}
	if profiles.Triggers == nil {
		profiles.Triggers = map[string]string{}
	}
	if profiles.Profiles == nil {
		profiles.Profiles = map[string]AccessProfileConfig{}
	}
	for name, profile := range BuiltinAccessProfiles() {
		if _, exists := profiles.Profiles[name]; !exists {
			profiles.Profiles[name] = profile
		}
	}
}

func SetChannelAccessLevel(profiles *AccessProfilesConfig, channel, level string) bool {
	normalized := NormalizeAccessLevel(level)
	channel = strings.ToLower(strings.TrimSpace(channel))
	if profiles == nil || channel == "" || normalized == "" {
		return false
	}
	EnsureBuiltinAccessProfiles(profiles)
	profiles.Enabled = true
	profiles.Channels[channel] = normalized
	return true
}

func SetDefaultAccessLevel(profiles *AccessProfilesConfig, level string) bool {
	normalized := NormalizeAccessLevel(level)
	if profiles == nil || normalized == "" {
		return false
	}
	EnsureBuiltinAccessProfiles(profiles)
	profiles.Enabled = true
	profiles.Default = normalized
	return true
}

const LegacyElectronServiceProfile = "electron_local_service"

// MigrateLegacyServiceAccessChannel remaps the old Electron bootstrap profile to a
// builtin access level. That legacy profile was safe/read-only and hid write tools
// even when service.maxCapability was privileged.
func MigrateLegacyServiceAccessChannel(cfg *Config) {
	if cfg == nil || !cfg.Security.Profiles.Enabled {
		return
	}
	channel := strings.TrimSpace(cfg.Security.Profiles.Channels["service"])
	if channel != LegacyElectronServiceProfile {
		return
	}
	level := AccessLevelAdmin
	switch strings.ToLower(strings.TrimSpace(cfg.Service.MaxCapability)) {
	case "guarded":
		level = AccessLevelOperator
	case "safe":
		level = AccessLevelReader
	}
	SetChannelAccessLevel(&cfg.Security.Profiles, "service", level)
}

func ExpandAccessProfile(profile AccessProfileConfig, workspaceDir string) AccessProfileConfig {
	workspaceDir = strings.TrimSpace(workspaceDir)
	expanded := profile
	expanded.AllowedTools = append([]string{}, profile.AllowedTools...)
	expanded.AllowedHosts = append([]string{}, profile.AllowedHosts...)
	expanded.WritablePaths = make([]string, 0, len(profile.WritablePaths))
	for _, raw := range profile.WritablePaths {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if workspaceDir != "" {
			value = strings.ReplaceAll(value, AccessProfileWorkspaceDir, workspaceDir)
		}
		if strings.Contains(value, AccessProfileWorkspaceDir) {
			continue
		}
		if abs, err := filepath.Abs(value); err == nil {
			value = abs
		}
		expanded.WritablePaths = append(expanded.WritablePaths, value)
	}
	return expanded
}
