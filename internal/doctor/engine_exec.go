package doctor

import "or3-intern/internal/config"

func execFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Hardening.PrivilegedTools && !cfg.Hardening.GuardedTools {
		return findings
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		findings = append(findings, Finding{
			ID:       "exec.allowlist_empty",
			Area:     "exec",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "exec is enabled without an exec allowlist",
			FixMode:  FixModeManual,
		})
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		findings = append(findings, Finding{
			ID:       "exec.child_env_empty",
			Area:     "exec",
			Severity: SeverityWarn,
			Summary:  "exec-capable configuration has an empty child environment allowlist",
		})
	}
	if cfg.Hardening.EnableExecShell {
		findings = append(findings, Finding{
			ID:       "exec.shell_mode_enabled",
			Area:     "exec",
			Severity: SeverityWarn,
			Summary:  "exec shell command mode is enabled; prefer program + args and keep shell mode off unless strictly required",
		})
	}
	if publicIngressCanReachExec(cfg) || webhookCanReachExec(cfg) {
		findings = append(findings, Finding{
			ID:       "exec.public_ingress_reachable",
			Area:     "exec",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "public or webhook-facing ingress can reach privileged exec posture unless profiles deny it",
		})
	}
	return findings
}

func publicIngressCanReachSkillExec(cfg config.Config) bool {
	for _, channel := range openAccessChannelNames(cfg) {
		_, profile, ok := resolveEffectiveProfile(cfg, "", channel)
		if !ok {
			return cfg.Hardening.PrivilegedTools
		}
		if profileAllowsPrivileged(profile) && (profileAllowsTool(profile, "run_skill") || profileAllowsTool(profile, "run_skill_script")) {
			return true
		}
	}
	return false
}

func publicIngressCanReachExec(cfg config.Config) bool {
	if !cfg.Hardening.EnableExecShell || !cfg.Hardening.PrivilegedTools {
		return false
	}
	for _, channel := range openAccessChannelNames(cfg) {
		_, profile, ok := resolveEffectiveProfile(cfg, "", channel)
		if !ok {
			return true
		}
		if profileCanReachExec(profile) {
			return true
		}
	}
	return false
}

func webhookCanReachExec(cfg config.Config) bool {
	if !cfg.Hardening.EnableExecShell || !cfg.Hardening.PrivilegedTools || !cfg.Triggers.Webhook.Enabled {
		return false
	}
	_, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		return true
	}
	return profileCanReachExec(profile)
}
