package main

import (
	"or3-intern/internal/config"
	intdoctor "or3-intern/internal/doctor"
)

func approvalBrokerRequired(cfg config.Config) bool {
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeAdvisory})
	for _, finding := range report.Findings {
		if finding.ID == "approvals.key_missing" || finding.ID == "approvals.public_service_without_key" {
			return true
		}
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
