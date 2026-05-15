package doctor

import (
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

type ChannelSnapshot struct {
	Name          string
	DisplayName   string
	Enabled       bool
	OpenAccess    bool
	InboundPolicy config.InboundPolicy
	HasAllowlist  bool
}

func collectChannels(cfg config.Config) []ChannelSnapshot {
	return []ChannelSnapshot{
		{"telegram", "Telegram", cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.OpenAccess,
			cfg.Channels.Telegram.InboundPolicy, hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs)},
		{"slack", "Slack", cfg.Channels.Slack.Enabled, cfg.Channels.Slack.OpenAccess,
			cfg.Channels.Slack.InboundPolicy, hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs)},
		{"discord", "Discord", cfg.Channels.Discord.Enabled, cfg.Channels.Discord.OpenAccess,
			cfg.Channels.Discord.InboundPolicy, hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs)},
		{"whatsapp", "WhatsApp", cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.OpenAccess,
			cfg.Channels.WhatsApp.InboundPolicy, hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom)},
		{"email", "Email", cfg.Channels.Email.Enabled, cfg.Channels.Email.OpenAccess,
			cfg.Channels.Email.InboundPolicy, hasNonEmpty(cfg.Channels.Email.AllowedSenders)},
	}
}

func anyEnabledChannels(cfg config.Config) bool {
	for _, ch := range collectChannels(cfg) {
		if ch.Enabled {
			return true
		}
	}
	return false
}

func hasPublicIngress(cfg config.Config) bool {
	return len(openAccessChannelNames(cfg)) > 0
}

func openAccessChannelNames(cfg config.Config) []string {
	var channels []string
	for _, ch := range collectChannels(cfg) {
		if ch.Enabled && ch.OpenAccess {
			channels = append(channels, ch.Name)
		}
	}
	return channels
}

func channelExposureFindings(cfg config.Config, opts Options) []Finding {
	var findings []Finding
	for _, ch := range collectChannels(cfg) {
		if ch.Enabled && ch.OpenAccess {
			findings = append(findings, publicChannelExposureFindings(cfg, opts, ch)...)
		}
	}
	return findings
}

func publicChannelExposureFindings(cfg config.Config, opts Options, ch ChannelSnapshot) []Finding {
	findings := []Finding{{
		ID:       "channels.open_access",
		Area:     ch.Name,
		Severity: SeverityWarn,
		Summary:  "channel is open to any sender",
	}}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "", ch.Name)
	if !ok {
		if cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "channels.open_access_privileged_without_profile",
				Area:     ch.Name,
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "open-access channel can reach privileged tools because no access profile applies",
			})
		}
		if cfg.Hardening.GuardedTools {
			findings = append(findings, Finding{
				ID:       "channels.open_access_guarded_without_profile",
				Area:     ch.Name,
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "open-access channel can reach guarded tools because no access profile applies",
			})
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "channels.open_access_skills_without_profile",
				Area:     ch.Name,
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "open-access channel can reach skill execution because no access profile applies",
			})
		}
		return findings
	}
	if profileAllowsPrivileged(profile) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_privileged_profile",
			Area:     ch.Name,
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("open-access channel resolves to profile %q with privileged capability", profileName),
		})
	}
	if cfg.Hardening.GuardedTools && !profileHasMeaningfulToolRestriction(profile) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_no_tool_boundary",
			Area:     ch.Name,
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("open-access channel resolves to profile %q without a meaningful tool restriction", profileName),
		})
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_exec_shell",
			Area:     ch.Name,
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("open-access channel can reach exec shell mode via profile %q", profileName),
		})
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && (profileAllowsTool(profile, "run_skill") || profileAllowsTool(profile, "run_skill_script")) {
		findings = append(findings, Finding{
			ID:       "channels.open_access_skill_exec",
			Area:     ch.Name,
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("open-access channel can reach skill execution via profile %q", profileName),
		})
	}
	return findings
}

func channelIngressFindings(cfg config.Config, opts Options) []Finding {
	var findings []Finding
	for _, ch := range collectChannels(cfg) {
		if !ch.Enabled {
			continue
		}
		if !requiresChannelAllowlist(ch.InboundPolicy, ch.OpenAccess, ch.HasAllowlist) {
			continue
		}
		findings = append(findings, Finding{
			ID:       "channels.invalid_ingress",
			Area:     ch.Name,
			Severity: severityFor(opts.Mode, SeverityError, opts.Mode == ModeStartupServe || opts.Mode == ModeConfigurePostSave),
			Summary:  fmt.Sprintf("%s is enabled without pairing, allowlist, or open access policy", ch.DisplayName),
			Detail:   "Enabled channels must choose an inbound authorization model.",
			FixMode:  FixModeInteractive,
			FixHint:  "Choose pairing, allowlist, open access, or deny inbound.",
			Metadata: map[string]string{"channel": ch.Name},
		})
	}
	if cfg.Channels.Email.Enabled && !cfg.Channels.Email.ConsentGranted {
		findings = append(findings, Finding{
			ID:       "email.consent_missing",
			Area:     "email",
			Severity: severityFor(opts.Mode, SeverityError, opts.Mode == ModeStartupServe),
			Summary:  "email is enabled without explicit consentGranted=true",
			FixMode:  FixModeManual,
		})
	}
	return findings
}

func hasNonEmpty(items []string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return true
		}
	}
	return false
}

func requiresChannelAllowlist(policy config.InboundPolicy, openAccess bool, hasAllowlist bool) bool {
	switch strings.ToLower(strings.TrimSpace(string(policy))) {
	case string(config.InboundPolicyAllowlist):
		return !hasAllowlist
	case string(config.InboundPolicyPairing), string(config.InboundPolicyDeny):
		return false
	default:
		return !openAccess && !hasAllowlist
	}
}
