package doctor

import (
	"os/exec"
	"strings"

	"or3-intern/internal/config"
)

func hardeningFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		findings = append(findings, Finding{
			ID:       "env.child_allowlist_empty",
			Area:     "env",
			Severity: SeverityWarn,
			Summary:  "child process environment allowlist is empty",
			Detail:   "Subprocesses may inherit less predictable environment state.",
			FixMode:  FixModeManual,
		})
	}
	if !cfg.Hardening.Quotas.Enabled {
		findings = append(findings, Finding{
			ID:       "quotas.disabled",
			Area:     "quotas",
			Severity: SeverityWarn,
			Summary:  "tool quotas are disabled",
			Detail:   "Per-turn safety limits are disabled.",
			FixMode:  FixModeManual,
		})
	}
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 || cfg.Hardening.Quotas.MaxExecCalls <= 0 || cfg.Hardening.Quotas.MaxWebCalls <= 0 || cfg.Hardening.Quotas.MaxSubagentCalls <= 0 ||
		cfg.Hardening.Quotas.MaxSessionToolCalls <= 0 || cfg.Hardening.Quotas.MaxSessionExecCalls <= 0 || cfg.Hardening.Quotas.MaxSessionWebCalls <= 0 || cfg.Hardening.Quotas.MaxSessionSubagentCalls <= 0 {
		findings = append(findings, Finding{
			ID:       "quotas.unset",
			Area:     "quotas",
			Severity: SeverityWarn,
			Summary:  "one or more quota limits are unset",
			Detail:   "Quota values should be positive when quotas are enabled.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Restore the default hardening quotas.",
		})
	}
	if cfg.Hardening.PrivilegedTools && !cfg.Hardening.Sandbox.Enabled {
		findings = append(findings, Finding{
			ID:       "privileged-exec.sandbox_disabled",
			Area:     "privileged-exec",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "privileged tools are enabled without Bubblewrap sandboxing",
			Detail:   "Privileged exec-capable tools should run under Bubblewrap in hardened setups.",
			FixMode:  FixModeInteractive,
			FixHint:  "Run `or3-intern doctor --fix --interactive` to disable privileged tools or enable sandboxing.",
		})
	}
	bubblewrapPath := strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath)
	if cfg.Hardening.Sandbox.Enabled && bubblewrapPath == "" {
		findings = append(findings, Finding{
			ID:       "privileged-exec.bubblewrap_path_empty",
			Area:     "privileged-exec",
			Severity: SeverityError,
			Summary:  "Bubblewrap sandbox is enabled without a bubblewrapPath",
			Detail:   "Sandboxing cannot start without a bubblewrap binary path.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Set hardening.sandbox.bubblewrapPath to `bwrap`.",
		})
	}
	if cfg.Hardening.Sandbox.Enabled && bubblewrapPath != "" {
		if _, err := exec.LookPath(bubblewrapPath); err != nil {
			findings = append(findings, Finding{
				ID:       "privileged-exec.bubblewrap_missing",
				Area:     "privileged-exec",
				Severity: severityFor(opts.Mode, SeverityError, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "configured Bubblewrap binary is not available",
				Detail:   bubblewrapPath,
				FixMode:  FixModeInteractive,
				FixHint:  "Run `or3-intern doctor --fix --interactive` to disable privileged tools or update the Bubblewrap path.",
			})
		}
	}
	return findings
}

func isHostedOrStartupMode(cfg config.Config, mode Mode) bool {
	if config.IsHostedProfile(cfg.RuntimeProfile) {
		return true
	}
	return mode == ModeStartupChat || mode == ModeStartupServe || mode == ModeStartupService
}

func fixModeForBind(profile config.RuntimeProfile) FixMode {
	if config.IsHostedProfile(profile) {
		return FixModeInteractive
	}
	return FixModeAutomatic
}
