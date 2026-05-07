package doctor

import "or3-intern/internal/config"

func skillFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Skills.EnableExec {
		return findings
	}
	if !cfg.Skills.Policy.QuarantineByDefault {
		findings = append(findings, Finding{
			ID:       "skills.quarantine_disabled",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "skill execution is enabled while quarantineByDefault is false",
		})
	}
	if len(cfg.Skills.Policy.TrustedOwners) == 0 {
		findings = append(findings, Finding{
			ID:       "skills.trusted_owners_empty",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "skill execution is enabled without a trustedOwners policy for managed skills",
		})
	}
	if len(cfg.Skills.Policy.TrustedRegistries) == 0 {
		findings = append(findings, Finding{
			ID:       "skills.trusted_registries_empty",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "skill execution is enabled without a trustedRegistries policy for managed skills",
		})
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		findings = append(findings, Finding{
			ID:       "skills.child_env_empty",
			Area:     "skills",
			Severity: SeverityWarn,
			Summary:  "skill execution is enabled with an empty child environment allowlist",
		})
	}
	if hasPublicIngress(cfg) && publicIngressCanReachSkillExec(cfg) {
		findings = append(findings, Finding{
			ID:       "skills.public_ingress_reachable",
			Area:     "skills",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
			Summary:  "public ingress can reach skill execution through a permissive profile",
		})
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok || (profileAllowsPrivileged(profile) && (profileAllowsTool(profile, "run_skill") || profileAllowsTool(profile, "run_skill_script"))) {
			findings = append(findings, Finding{
				ID:       "skills.webhook_reachable",
				Area:     "skills",
				Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupServe),
				Summary:  "webhook can reach skill execution through a permissive profile",
			})
		}
	}
	return findings
}
