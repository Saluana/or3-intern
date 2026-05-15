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
		if _, _, ok := resolveEffectiveProfile(cfg, "service", "service"); !ok && (opts.Mode == ModeStartupService || config.IsHostedProfile(cfg.RuntimeProfile)) {
			findings = append(findings, Finding{
				ID:       "service.effective_profile_missing",
				Area:     "service",
				Severity: severityFor(opts.Mode, SeverityWarn, true),
				Summary:  "exposed service ingress has no effective access profile",
				Detail:   "Non-CLI ingress needs an access profile in hardened modes so browser, device, and service requests have a clear capability boundary.",
				FixMode:  FixModeInteractive,
				FixHint:  "Enable access profiles and map the service trigger or service channel to a restricted profile.",
			})
		}
	}
	if cfg.Service.AllowUnauthenticatedPairing && !isLoopbackAddr(cfg.Service.Listen) && !cfg.Service.AllowRemoteUnauthenticatedPairing {
		findings = append(findings, Finding{
			ID:       "service.unauthenticated_pairing_remote",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "unauthenticated pairing requires a loopback listen address",
			Detail:   "Remote unauthenticated pairing can let an untrusted device request access before you approve a secure pairing flow.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Disable unauthenticated pairing or bind the service to loopback.",
		})
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Service.SharedSecretRole)) {
	case "", "viewer", "service-client":
	default:
		findings = append(findings, Finding{
			ID:       "service.shared_secret_role_unsafe",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service.sharedSecretRole must be viewer or service-client",
			Detail:   "Shared-secret bearer tokens are broad credentials, so they should not grant operator or admin access.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Set service.sharedSecretRole to service-client and use paired devices for stronger access.",
		})
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Service.MaxCapability)) {
	case "", "safe":
	default:
		findings = append(findings, Finding{
			ID:       "service.max_capability_unsafe",
			Area:     "service",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile)),
			Summary:  "service.maxCapability must remain safe",
			Detail:   "The shared service entry point should default to safe capabilities and require explicit profiles for higher-risk actions.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Set service.maxCapability to safe.",
		})
	}
	return findings
}
