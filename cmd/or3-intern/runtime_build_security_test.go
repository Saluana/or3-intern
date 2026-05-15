package main

import (
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func TestBuildRuntimeSecurityDisabledApprovals(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	cfg := config.Default()
	cfg.Security.Approvals.Enabled = false
	cfg.Security.Approvals.KeyFile = ""
	cfg.Security.SecretStore.Enabled = false
	cfg.Security.Audit.Enabled = false

	components, err := buildRuntimeSecurity(context.Background(), cfg, database)
	if err != nil {
		t.Fatalf("buildRuntimeSecurity: %v", err)
	}
	approvalComponents, err := buildRuntimeApprovalSecurity(components.Config, database, components.Audit)
	if err != nil {
		t.Fatalf("buildRuntimeApprovalSecurity: %v", err)
	}
	if approvalComponents.ApprovalBroker != nil {
		t.Fatalf("expected no approval broker when approvals disabled")
	}
}

func TestBuildRuntimeSecurityEnabledApprovals(t *testing.T) {
	tmp := t.TempDir()
	database, err := db.Open(filepath.Join(tmp, "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	cfg := config.Default()
	cfg.Security.Approvals.Enabled = true
	cfg.Security.Approvals.KeyFile = filepath.Join(tmp, "approval.key")
	cfg.Security.SecretStore.Enabled = false
	cfg.Security.Audit.Enabled = false

	components, err := buildRuntimeSecurity(context.Background(), cfg, database)
	if err != nil {
		t.Fatalf("buildRuntimeSecurity: %v", err)
	}
	approvalComponents, err := buildRuntimeApprovalSecurity(components.Config, database, components.Audit)
	if err != nil {
		t.Fatalf("buildRuntimeApprovalSecurity: %v", err)
	}
	if approvalComponents.ApprovalBroker == nil {
		t.Fatalf("expected approval broker when approvals enabled")
	}
}
