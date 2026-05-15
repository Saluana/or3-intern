package main

import (
	"context"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

type runtimeSecurityComponents struct {
	Config  config.Config
	Secrets *security.SecretManager
	Audit   *security.AuditLogger
}

type runtimeApprovalComponents struct {
	ApprovalBroker *approval.Broker
}

func buildRuntimeSecurity(ctx context.Context, cfg config.Config, database *db.DB) (runtimeSecurityComponents, error) {
	securedCfg, secretManager, auditLogger, err := setupSecurity(ctx, cfg, database)
	if err != nil {
		return runtimeSecurityComponents{}, err
	}
	return runtimeSecurityComponents{Config: securedCfg, Secrets: secretManager, Audit: auditLogger}, nil
}

func buildRuntimeApprovalSecurity(cfg config.Config, database *db.DB, auditLogger *security.AuditLogger) (runtimeApprovalComponents, error) {
	approvalBroker, err := setupApprovalBroker(cfg, database, auditLogger)
	if err != nil {
		return runtimeApprovalComponents{}, err
	}
	return runtimeApprovalComponents{ApprovalBroker: approvalBroker}, nil
}
