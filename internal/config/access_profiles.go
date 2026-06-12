package config

import "strings"

const (
	AccessLevelReader   = "reader"
	AccessLevelOperator = "operator"
	AccessLevelAdmin    = "admin"

	AccessProfileWorkspaceDir = "${workspaceDir}"
)

// BuiltinAccessProfiles returns the simple product-level access profiles used
// by channel and device setup.
func BuiltinAccessProfiles() map[string]AccessProfileConfig {
	return map[string]AccessProfileConfig{
		AccessLevelReader: {
			MaxCapability: "safe",
			AllowedHosts:  []string{},
			WritablePaths: []string{},
		},
		AccessLevelOperator: {
			MaxCapability: "guarded",
			AllowedHosts:  []string{},
			WritablePaths: []string{AccessProfileWorkspaceDir},
		},
		AccessLevelAdmin: {
			MaxCapability: "privileged",
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
