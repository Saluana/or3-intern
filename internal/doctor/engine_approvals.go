package doctor

import (
	"strings"

	"or3-intern/internal/config"
)

func approvalFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Security.Approvals.Enabled {
		return findings
	}
	if approvalBrokerRequired(cfg) && strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
		findings = append(findings, Finding{
			ID:       "approvals.key_missing",
			Area:     "approvals",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService || opts.Mode == ModeStartupServe),
			Summary:  "approval broker keyFile is required when approvals use ask or allowlist mode",
			Detail:   "Approval tokens and pairing codes need a signing key.",
			FixMode:  FixModeAutomatic,
			FixHint:  "Generate the configured approvals key file.",
		})
	}
	if keyFinding := keyFileFinding("approvals.key_path_missing", "approvals", cfg.Security.Approvals.KeyFile, "approval key file is missing", FixModeAutomatic); keyFinding != nil && cfg.Security.Approvals.Enabled {
		findings = append(findings, *keyFinding)
	}
	if cfg.Service.Enabled && !isLoopbackAddr(cfg.Service.Listen) && approvalBrokerRequired(cfg) && strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
		findings = append(findings, Finding{
			ID:       "approvals.public_service_without_key",
			Area:     "approvals",
			Severity: severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService),
			Summary:  "service mode is exposed beyond loopback while approvals require a broker keyFile",
			Detail:   "Remote approval and pairing flows should not run without an approval signing key.",
			FixMode:  FixModeAutomatic,
		})
	}
	return findings
}

func approvalBrokerRequired(cfg config.Config) bool {
	if !cfg.Security.Approvals.Enabled {
		return false
	}
	for _, mode := range []config.ApprovalMode{
		cfg.Security.Approvals.Pairing.Mode,
		cfg.Security.Approvals.Exec.Mode,
		cfg.Security.Approvals.SkillExecution.Mode,
		cfg.Security.Approvals.SecretAccess.Mode,
		cfg.Security.Approvals.MessageSend.Mode,
	} {
		switch mode {
		case config.ApprovalModeAsk, config.ApprovalModeAllowlist:
			return true
		}
	}
	return false
}
