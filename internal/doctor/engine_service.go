package doctor

import (
	"strings"

	"or3-intern/internal/config"
)

func serviceFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Service.Enabled && opts.Mode != ModeStartupService {
		return findings
	}
	secret := strings.TrimSpace(cfg.Service.Secret)
	if secret == "" {
		findings = append(findings, Finding{
			ID:       "service.secret_missing",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service mode is enabled without a shared secret",
			Detail:   "The internal service API should not run without authentication.",
			FixMode:  FixModeInteractive,
			FixHint:  "Generate a strong service secret or disable service mode.",
		})
	} else if len(secret) < 24 {
		findings = append(findings, Finding{
			ID:       "service.secret_weak",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service mode is enabled with a weak shared secret",
			Detail:   "Use at least a 24-character random secret for service auth.",
			FixMode:  FixModeInteractive,
			FixHint:  "Generate a stronger service secret.",
		})
	}
	if !isLoopbackAddr(cfg.Service.Listen) {
		findings = append(findings, Finding{
			ID:       "service.public_bind",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service bind address is not loopback-only",
			Detail:   cfg.Service.Listen,
			FixMode:  fixModeForBind(cfg.RuntimeProfile),
			FixHint:  "Bind the service to loopback unless you intentionally expose it behind a hardened deployment.",
		})
	}
	return findings
}
