package doctor

import (
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/config"
)

func profileFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Security.Profiles.Enabled {
		if hasPublicIngress(cfg) {
			findings = append(findings, Finding{
				ID:       "profiles.public_ingress_without_profiles",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "public ingress is enabled while access profiles are disabled",
				FixMode:  FixModeInteractive,
			})
		}
		if cfg.Triggers.Webhook.Enabled {
			findings = append(findings, Finding{
				ID:       "profiles.webhook_without_profiles",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook is enabled while access profiles are disabled",
				FixMode:  FixModeInteractive,
			})
		}
		return findings
	}
	if len(cfg.Security.Profiles.Profiles) == 0 {
		findings = append(findings, Finding{
			ID:       "profiles.empty",
			Area:     "profiles",
			Severity: SeverityWarn,
			Summary:  "access profiles are enabled but no profiles are defined",
			FixMode:  FixModeInteractive,
		})
		return findings
	}
	if strings.TrimSpace(cfg.Security.Profiles.Default) == "" && len(cfg.Security.Profiles.Channels) == 0 && len(cfg.Security.Profiles.Triggers) == 0 {
		findings = append(findings, Finding{
			ID:       "profiles.no_mapping",
			Area:     "profiles",
			Severity: SeverityWarn,
			Summary:  "access profiles are enabled but no default, channel, or trigger mapping is configured",
			FixMode:  FixModeInteractive,
		})
	}
	if hasPublicIngress(cfg) && strings.TrimSpace(cfg.Security.Profiles.Default) == "" {
		missing := false
		for _, channel := range openAccessChannelNames(cfg) {
			if _, _, ok := resolveEffectiveProfile(cfg, "", channel); !ok {
				missing = true
				break
			}
		}
		if missing {
			findings = append(findings, Finding{
				ID:       "profiles.open_ingress_profile_missing",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "one or more open-access channels have no effective access profile",
				FixMode:  FixModeInteractive,
			})
		}
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, _, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok {
			findings = append(findings, Finding{
				ID:       "profiles.webhook_effective_missing",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook has no effective access profile",
				FixMode:  FixModeInteractive,
			})
		}
	}
	profileNames := make([]string, 0, len(cfg.Security.Profiles.Profiles))
	for name := range cfg.Security.Profiles.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		profile := cfg.Security.Profiles.Profiles[name]
		if hostListContainsLiteralStar(profile.AllowedHosts) {
			findings = append(findings, Finding{
				ID:       "profiles.profile_literal_star",
				Area:     "profiles",
				Severity: SeverityWarn,
				Summary:  fmt.Sprintf("profile %q allowedHosts contains *", name),
			})
		}
		if hostListTooBroad(profile.AllowedHosts) {
			findings = append(findings, Finding{
				ID:       "profiles.profile_broad_hosts",
				Area:     "profiles",
				Severity: SeverityWarn,
				Summary:  fmt.Sprintf("profile %q has broad allowedHosts", name),
			})
		}
		if profileAllowsPrivileged(profile) && len(profile.AllowedTools) == 0 {
			findings = append(findings, Finding{
				ID:       "profiles.privileged_without_tools",
				Area:     "profiles",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  fmt.Sprintf("profile %q permits privileged capability without an explicit tool allowlist", name),
			})
		}
	}
	return findings
}

func resolveEffectiveProfile(cfg config.Config, trigger, channel string) (string, config.AccessProfileConfig, bool) {
	if !cfg.Security.Profiles.Enabled {
		return "", config.AccessProfileConfig{}, false
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Triggers[strings.ToLower(strings.TrimSpace(trigger))]); profileName != "" {
		profile, ok := cfg.Security.Profiles.Profiles[profileName]
		return profileName, profile, ok
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Channels[strings.ToLower(strings.TrimSpace(channel))]); profileName != "" {
		profile, ok := cfg.Security.Profiles.Profiles[profileName]
		return profileName, profile, ok
	}
	profileName := strings.TrimSpace(cfg.Security.Profiles.Default)
	if profileName == "" {
		return "", config.AccessProfileConfig{}, false
	}
	profile, ok := cfg.Security.Profiles.Profiles[profileName]
	return profileName, profile, ok
}

func profileAllowsPrivileged(profile config.AccessProfileConfig) bool {
	maxCapability := strings.ToLower(strings.TrimSpace(profile.MaxCapability))
	return maxCapability == "" || maxCapability == "privileged"
}

func profileAllowsGuarded(profile config.AccessProfileConfig) bool {
	maxCapability := strings.ToLower(strings.TrimSpace(profile.MaxCapability))
	return maxCapability == "" || maxCapability == "privileged" || maxCapability == "guarded"
}

func profileAllowsTool(profile config.AccessProfileConfig, toolName string) bool {
	if len(profile.AllowedTools) == 0 {
		return true
	}
	toolName = strings.TrimSpace(toolName)
	for _, allowed := range profile.AllowedTools {
		if strings.TrimSpace(allowed) == toolName {
			return true
		}
	}
	return false
}

func profileHasMeaningfulToolRestriction(profile config.AccessProfileConfig) bool {
	return !profileAllowsGuarded(profile) || len(profile.AllowedTools) > 0
}

func profileCanReachExec(profile config.AccessProfileConfig) bool {
	return profileAllowsPrivileged(profile) && profileAllowsTool(profile, "exec")
}
