package config

import "strings"

const (
	AccessLevelReader   = "reader"
	AccessLevelOperator = "operator"
	AccessLevelAdmin    = "admin"

	AccessProfileWorkspaceDir = "${workspaceDir}"
)

// BuiltinAccessProfiles returns the simple product-level access profiles used
// by channel and device setup. Tool names use runner/service permission
// language (e.g. "shell_exec" instead of "exec") so configs stay portable
// across different runner backends. DeclaredTools is metadata only — the
// runner enforces its own tool policy, this list is for visibility.
func BuiltinAccessProfiles() map[string]AccessProfileConfig {
	return map[string]AccessProfileConfig{
		AccessLevelReader: {
			MaxCapability: "safe",
			DeclaredTools: []string{
				"read_files",
				"search_files",
				"list_dirs",
				"read_artifacts",
				"memory_search",
				"memory_recent",
				"memory_get_pinned",
			},
			AllowedHosts:  []string{},
			WritablePaths: []string{},
		},
		AccessLevelOperator: {
			MaxCapability: "guarded",
			DeclaredTools: []string{
				"read_files",
				"search_files",
				"list_dirs",
				"read_artifacts",
				"write_files",
				"edit_files",
				"delete_files",
				"memory_search",
				"memory_recent",
				"memory_get_pinned",
				"web_search",
				"web_fetch",
				"web_fetch_markdown",
				"shell_exec",
			},
			AllowedHosts:  []string{},
			WritablePaths: []string{AccessProfileWorkspaceDir},
		},
		AccessLevelAdmin: {
			MaxCapability: "privileged",
			DeclaredTools: []string{
				"read_files",
				"search_files",
				"list_dirs",
				"read_artifacts",
				"write_files",
				"edit_files",
				"delete_files",
				"memory_set_pinned",
				"memory_add_note",
				"memory_search",
				"memory_recent",
				"memory_get_pinned",
				"web_search",
				"web_fetch",
				"web_fetch_markdown",
				"shell_exec",
				"send_messages",
				"schedule_cron",
			},
			AllowedHosts:  []string{},
			WritablePaths: []string{AccessProfileWorkspaceDir},
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
