package doctor

import (
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

func webhookFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Triggers.Webhook.Enabled {
		return findings
	}
	secret := strings.TrimSpace(cfg.Triggers.Webhook.Secret)
	if secret == "" {
		findings = append(findings, Finding{
			ID:       "webhook.secret_missing",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "webhook is enabled without a secret",
			Detail:   "Webhook ingress should be authenticated with a shared secret.",
			FixMode:  FixModeInteractive,
			FixHint:  "Generate a strong webhook secret or disable the webhook.",
		})
	}
	addr := strings.TrimSpace(cfg.Triggers.Webhook.Addr)
	if !isLoopbackAddr(addr) {
		findings = append(findings, Finding{
			ID:       "webhook.public_bind",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "webhook bind address is not loopback-only",
			Detail:   addr,
			FixMode:  fixModeForBind(cfg.RuntimeProfile),
			FixHint:  "Bind the webhook listener to loopback unless you are intentionally exposing it.",
		})
	}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		findings = append(findings, webhookNoProfileFindings(cfg, opts)...)
		return findings
	}
	findings = append(findings, webhookProfileFindings(cfg, opts, profileName, profile)...)
	return findings
}

func webhookNoProfileFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{{
		ID:       "webhook.profile_missing",
		Area:     "webhook",
		Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
		Summary:  "webhook is enabled without an effective access profile",
		Detail:   "Webhook turns should resolve to a bounded access profile.",
		FixMode:  FixModeInteractive,
		FixHint:  "Create or map an access profile for the webhook.",
	}}
	if cfg.Hardening.PrivilegedTools {
		findings = append(findings, Finding{
			ID:       "webhook.privileged_without_profile",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "webhook can reach privileged tools because no access profile applies",
		})
	}
	if cfg.Hardening.GuardedTools {
		findings = append(findings, Finding{
			ID:       "webhook.guarded_without_profile",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "webhook can reach guarded tools because no access profile applies",
		})
	}
	if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
		findings = append(findings, Finding{
			ID:       "webhook.skills_without_profile",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "webhook can reach skill execution because no access profile applies",
		})
	}
	return findings
}

func webhookProfileFindings(cfg config.Config, opts Options, profileName string, profile config.AccessProfileConfig) []Finding {
	var findings []Finding
	if profileAllowsPrivileged(profile) {
		findings = append(findings, Finding{
			ID:       "webhook.profile_privileged",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("webhook resolves to profile %q with privileged capability", profileName),
		})
	}
	if profile.AllowSubagents {
		findings = append(findings, Finding{
			ID:       "webhook.profile_subagents",
			Area:     "webhook",
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("webhook resolves to profile %q with subagents enabled", profileName),
		})
	}
	if len(profile.WritablePaths) > 0 {
		findings = append(findings, Finding{
			ID:       "webhook.profile_writable_paths",
			Area:     "webhook",
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("webhook resolves to profile %q with writable paths", profileName),
		})
	}
	if hostListTooBroad(profile.AllowedHosts) {
		findings = append(findings, Finding{
			ID:       "webhook.profile_broad_hosts",
			Area:     "webhook",
			Severity: SeverityWarn,
			Summary:  fmt.Sprintf("webhook resolves to profile %q with broad allowedHosts", profileName),
		})
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		findings = append(findings, Finding{
			ID:       "webhook.exec_shell_exposure",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("webhook can reach exec shell mode via profile %q", profileName),
		})
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && (profileAllowsTool(profile, "run_skill") || profileAllowsTool(profile, "run_skill_script")) {
		findings = append(findings, Finding{
			ID:       "webhook.skill_exec_exposure",
			Area:     "webhook",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  fmt.Sprintf("webhook can reach skill execution via profile %q", profileName),
		})
	}
	return findings
}
