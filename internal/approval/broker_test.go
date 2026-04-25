package approval

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func newTestBroker(t *testing.T, mutate func(*config.ApprovalConfig)) (*Broker, func()) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "approval-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Enabled = true
	approvalCfg.HostID = "test-host"
	if mutate != nil {
		mutate(&approvalCfg)
	}
	broker := &Broker{
		DB:      database,
		Config:  approvalCfg,
		HostID:  approvalCfg.HostID,
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	}
	return broker, func() {
		database.Close()
	}
}

func TestBroker_EvaluateExecReusesPendingApprovalRequest(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	input := ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
		AgentID:        "agent-1",
		SessionID:      "session-1",
	}
	first, err := broker.EvaluateExec(context.Background(), input)
	if err != nil {
		t.Fatalf("EvaluateExec first: %v", err)
	}
	second, err := broker.EvaluateExec(context.Background(), input)
	if err != nil {
		t.Fatalf("EvaluateExec second: %v", err)
	}
	if !first.RequiresApproval || first.RequestID == 0 {
		t.Fatalf("expected first evaluation to require approval, got %#v", first)
	}
	if second.RequestID != first.RequestID {
		t.Fatalf("expected duplicate request reuse, got first=%d second=%d", first.RequestID, second.RequestID)
	}
	if second.SubjectHash != first.SubjectHash {
		t.Fatalf("expected stable subject hash, got %q and %q", first.SubjectHash, second.SubjectHash)
	}
}

func TestBroker_EvaluateExec_AllowsApprovedTokenAcrossRetries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()
	broker.Now = func() time.Time { return now }

	input := ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
		AgentID:        "agent-1",
		SessionID:      "session-1",
	}
	first, err := broker.EvaluateExec(context.Background(), input)
	if err != nil {
		t.Fatalf("EvaluateExec first: %v", err)
	}
	issued, err := broker.ApproveRequest(context.Background(), first.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}

	now = now.Add(2 * time.Minute)
	retry, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: input.ExecutablePath,
		Argv:           append([]string{}, input.Argv...),
		WorkingDir:     input.WorkingDir,
		ToolName:       input.ToolName,
		AgentID:        input.AgentID,
		SessionID:      input.SessionID,
		ApprovalToken:  issued.Token,
	})
	if err != nil {
		t.Fatalf("EvaluateExec retry: %v", err)
	}
	if !retry.Allowed {
		t.Fatalf("expected approved token to allow retry, got %#v", retry)
	}
}

func TestBroker_PairingAndApprovalTokenRoundTrip(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	pairingReq, code, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{
		Role:        RoleOperator,
		DisplayName: "Ops Laptop",
		Origin:      "service-test",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := broker.ApprovePairingRequest(context.Background(), pairingReq.ID, "cli:test"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	device, deviceToken, err := broker.ExchangePairingCode(context.Background(), PairingExchangeInput{RequestID: pairingReq.ID, Code: code})
	if err != nil {
		t.Fatalf("ExchangePairingCode: %v", err)
	}
	if device.DeviceID == "" || deviceToken == "" {
		t.Fatal("expected paired device and token")
	}
	if _, err := broker.AuthenticateDeviceToken(context.Background(), deviceToken, RoleOperator); err != nil {
		t.Fatalf("AuthenticateDeviceToken: %v", err)
	}

	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"approved"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	issued, err := broker.ApproveRequest(context.Background(), decision.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if err := broker.VerifyApprovalToken(context.Background(), issued.Token, decision.SubjectHash, broker.HostID); err != nil {
		t.Fatalf("VerifyApprovalToken: %v", err)
	}
}

func TestBroker_VerifyApprovalTokenRejectsMismatchTamperAndRevoked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()
	broker.Now = func() time.Time { return now }

	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{ExecutablePath: "/bin/echo", Argv: []string{"ok"}, WorkingDir: "/tmp", ToolName: "exec"})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	issued, err := broker.ApproveRequest(context.Background(), decision.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}

	for _, tc := range []struct {
		name        string
		token       string
		subjectHash string
		hostID      string
		want        string
	}{
		{name: "wrong host", token: issued.Token, subjectHash: decision.SubjectHash, hostID: "other-host", want: "host mismatch"},
		{name: "wrong subject", token: issued.Token, subjectHash: "wrong-subject", hostID: broker.HostID, want: "subject mismatch"},
		{name: "tampered signature", token: tamperApprovalTokenSignature(issued.Token), subjectHash: decision.SubjectHash, hostID: broker.HostID, want: "invalid approval token signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := broker.VerifyApprovalToken(context.Background(), tc.token, tc.subjectHash, tc.hostID)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}

	claims, err := broker.parseApprovalToken(issued.Token)
	if err != nil {
		t.Fatalf("parseApprovalToken: %v", err)
	}
	if err := broker.DB.RevokeApprovalToken(context.Background(), claims.TokenID, now.UnixMilli()); err != nil {
		t.Fatalf("RevokeApprovalToken: %v", err)
	}
	if err := broker.VerifyApprovalToken(context.Background(), issued.Token, decision.SubjectHash, broker.HostID); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked token failure, got %v", err)
	}
}

func tamperApprovalTokenSignature(token string) string {
	idx := strings.LastIndex(token, ".")
	if idx < 0 || idx == len(token)-1 {
		return token + "0"
	}
	if token[len(token)-1] == '0' {
		return token[:len(token)-1] + "1"
	}
	return token[:len(token)-1] + "0"
}

func TestBroker_ExchangePairingCodeRejectsWrongCodeAndExpiredRequest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
		cfg.PairingCodeTTLSeconds = 60
	})
	defer cleanup()
	broker.Now = func() time.Time { return now }

	req, code, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{Role: RoleOperator, DisplayName: "Ops Laptop"})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := broker.ApprovePairingRequest(context.Background(), req.ID, "cli:test"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	if _, _, err := broker.ExchangePairingCode(context.Background(), PairingExchangeInput{RequestID: req.ID, Code: code + "wrong"}); err == nil {
		t.Fatal("expected wrong pairing code to fail")
	}

	now = now.Add(2 * time.Minute)
	if _, _, err := broker.ExchangePairingCode(context.Background(), PairingExchangeInput{RequestID: req.ID, Code: code}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired pairing code failure, got %v", err)
	}
}

func TestBroker_ExchangePairingCode_IsSingleUse(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	pairingReq, code, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{
		Role:        RoleOperator,
		DisplayName: "Ops Laptop",
		Origin:      "service-test",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := broker.ApprovePairingRequest(context.Background(), pairingReq.ID, "cli:test"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	if _, _, err := broker.ExchangePairingCode(context.Background(), PairingExchangeInput{RequestID: pairingReq.ID, Code: code}); err != nil {
		t.Fatalf("ExchangePairingCode first: %v", err)
	}
	if _, _, err := broker.ExchangePairingCode(context.Background(), PairingExchangeInput{RequestID: pairingReq.ID, Code: code}); err == nil {
		t.Fatal("expected second exchange to fail")
	}
}

func TestBroker_CreatePairingRequest_HonorsPairingModes(t *testing.T) {
	t.Run("deny rejects new pairing requests", func(t *testing.T) {
		broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
			cfg.Pairing.Mode = config.ApprovalModeDeny
		})
		defer cleanup()

		if _, _, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{Role: RoleOperator, DisplayName: "Ops Laptop"}); err == nil {
			t.Fatal("expected deny mode to reject pairing requests")
		}
	})

	t.Run("trusted authenticated pairing auto-approves requests", func(t *testing.T) {
		broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
			cfg.Pairing.Mode = config.ApprovalModeTrusted
		})
		defer cleanup()

		req, _, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{Role: RoleOperator, DisplayName: "Ops Laptop"})
		if err != nil {
			t.Fatalf("CreatePairingRequest: %v", err)
		}
		if req.Status != StatusApproved {
			t.Fatalf("expected trusted pairing request to be auto-approved, got %#v", req)
		}
	})

	t.Run("trusted anonymous pairing stays pending", func(t *testing.T) {
		broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
			cfg.Pairing.Mode = config.ApprovalModeTrusted
		})
		defer cleanup()

		ctx := ContextWithAuditAuthKind(context.Background(), "unauthenticated")
		req, _, err := broker.CreatePairingRequest(ctx, PairingRequestInput{Role: RoleOperator, DisplayName: "Ops Laptop"})
		if err != nil {
			t.Fatalf("CreatePairingRequest: %v", err)
		}
		if req.Status != StatusPending {
			t.Fatalf("expected anonymous trusted pairing request to remain pending, got %#v", req)
		}
	})

	t.Run("allowlist only permits known active devices", func(t *testing.T) {
		broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
			cfg.Pairing.Mode = config.ApprovalModeAllowlist
		})
		defer cleanup()

		if _, _, err := broker.RotateDeviceToken(context.Background(), "device-1", RoleOperator, "Ops Laptop", nil); err != nil {
			t.Fatalf("RotateDeviceToken: %v", err)
		}
		req, _, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{Role: RoleOperator, DeviceID: "device-1", DisplayName: "Ops Laptop"})
		if err != nil {
			t.Fatalf("CreatePairingRequest allowlisted: %v", err)
		}
		if req.Status != StatusApproved {
			t.Fatalf("expected allowlisted device to be auto-approved, got %#v", req)
		}
		if _, _, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{Role: RoleOperator, DeviceID: "device-2", DisplayName: "Ops Laptop"}); err == nil {
			t.Fatal("expected unknown device to be rejected in allowlist mode")
		}
	})

	t.Run("allowlist anonymous existing device stays pending", func(t *testing.T) {
		broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
			cfg.Pairing.Mode = config.ApprovalModeAllowlist
		})
		defer cleanup()

		if _, _, err := broker.RotateDeviceToken(context.Background(), "device-1", RoleOperator, "Ops Laptop", nil); err != nil {
			t.Fatalf("RotateDeviceToken: %v", err)
		}
		ctx := ContextWithAuditAuthKind(context.Background(), "unauthenticated")
		req, _, err := broker.CreatePairingRequest(ctx, PairingRequestInput{Role: RoleOperator, DeviceID: "device-1", DisplayName: "Ops Laptop"})
		if err != nil {
			t.Fatalf("CreatePairingRequest anonymous allowlisted: %v", err)
		}
		if req.Status != StatusPending {
			t.Fatalf("expected anonymous allowlisted device to require approval, got %#v", req)
		}
	})
}

func TestBroker_AddAllowlist_RejectsEmptyMatchers(t *testing.T) {
	broker, cleanup := newTestBroker(t, nil)
	defer cleanup()

	if _, err := broker.AddAllowlist(context.Background(), string(SubjectExec), AllowlistScope{}, ExecAllowlistMatcher{}, "cli", 0); err == nil {
		t.Fatal("expected empty exec matcher to be rejected")
	}
	if _, err := broker.AddAllowlist(context.Background(), string(SubjectSkillExec), AllowlistScope{}, SkillAllowlistMatcher{}, "cli", 0); err == nil {
		t.Fatal("expected empty skill matcher to be rejected")
	}
}

func TestBroker_DenyRequest_RejectsAlreadyApprovedRequest(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if _, err := broker.ApproveRequest(context.Background(), decision.RequestID, "cli:test", false, "ok"); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if err := broker.DenyRequest(context.Background(), decision.RequestID, "cli:test", "late deny"); err == nil {
		t.Fatal("expected deny after approval to fail")
	}
}

func TestBroker_DenyPairingRequest_RejectsAlreadyApprovedRequest(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	req, _, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{
		Role:        RoleOperator,
		DisplayName: "Ops Laptop",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := broker.ApprovePairingRequest(context.Background(), req.ID, "cli:test"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	if err := broker.DenyPairingRequest(context.Background(), req.ID, "cli:test"); err == nil {
		t.Fatal("expected deny after pairing approval to fail")
	}
}

func TestBroker_RotatePairedDeviceToken_RejectsRevokedDevice(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {})
	defer cleanup()

	device, _, err := broker.RotateDeviceToken(context.Background(), "device-1", RoleOperator, "Ops Laptop", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken seed: %v", err)
	}
	if err := broker.RevokeDevice(context.Background(), device.DeviceID, "cli:test"); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	if _, _, err := broker.RotatePairedDeviceToken(context.Background(), device.DeviceID); err == nil {
		t.Fatal("expected revoked device rotation to fail")
	}
}

func TestBroker_IsPairedChannelIdentity_FindsMatchBeyond200Rows(t *testing.T) {
	broker, cleanup := newTestBroker(t, nil)
	defer cleanup()

	if _, _, err := broker.RotateDeviceToken(context.Background(), "legacy-device", RoleOperator, "Legacy Slack User", map[string]any{"channel": "slack", "identity": "U-old"}); err != nil {
		t.Fatalf("RotateDeviceToken legacy: %v", err)
	}
	for i := 0; i < 205; i++ {
		if _, _, err := broker.RotateDeviceToken(context.Background(), filepath.Join("device", strconv.Itoa(i)), RoleOperator, "Noise", map[string]any{"channel": "slack", "identity": "noise"}); err != nil {
			t.Fatalf("RotateDeviceToken noise %d: %v", i, err)
		}
	}
	allowed, err := broker.IsPairedChannelIdentity(context.Background(), "slack", "U-old")
	if err != nil {
		t.Fatalf("IsPairedChannelIdentity: %v", err)
	}
	if !allowed {
		t.Fatal("expected legacy paired identity to be found beyond first page")
	}
}
