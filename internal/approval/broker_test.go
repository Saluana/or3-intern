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

func TestBroker_EvaluateExecStoresRequesterContext(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	ctx := ContextWithRequesterContext(context.Background(), RequesterContext{
		Channel:     "slack",
		SessionKey:  "slack:C1:U1",
		From:        "U1",
		ReplyTarget: "C1",
		ReplyMeta:   map[string]any{"thread_ts": "123.45"},
	})
	decision, err := broker.EvaluateExec(ctx, ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
		SessionID:      "slack:C1:U1",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if !decision.RequiresApproval || decision.RequestID == 0 {
		t.Fatalf("expected approval request, got %#v", decision)
	}
	rec, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	requester := RequesterContextFromJSON(rec.RequesterContextJSON)
	if requester.Channel != "slack" || requester.SessionKey != "slack:C1:U1" || requester.From != "U1" || requester.ReplyTarget != "C1" {
		t.Fatalf("unexpected requester context: %#v", requester)
	}
	if requester.ReplyMeta["thread_ts"] != "123.45" {
		t.Fatalf("expected thread metadata, got %#v", requester.ReplyMeta)
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

	reuse, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: input.ExecutablePath,
		Argv:           append([]string{}, input.Argv...),
		WorkingDir:     input.WorkingDir,
		ToolName:       input.ToolName,
		AgentID:        input.AgentID,
		SessionID:      input.SessionID,
		ApprovalToken:  issued.Token,
	})
	if err != nil {
		t.Fatalf("EvaluateExec reused token: %v", err)
	}
	if reuse.Allowed || !reuse.RequiresApproval || reuse.RequestID == first.RequestID {
		t.Fatalf("expected consumed token to require a fresh approval, got %#v", reuse)
	}
}

func TestBroker_VerifyApprovalTokenConsumesToken(t *testing.T) {
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
	if err := broker.VerifyApprovalToken(context.Background(), issued.Token, decision.SubjectHash, broker.HostID); err != nil {
		t.Fatalf("VerifyApprovalToken first use: %v", err)
	}
	if err := broker.VerifyApprovalToken(context.Background(), issued.Token, decision.SubjectHash, broker.HostID); err == nil || !strings.Contains(err.Error(), "already used or revoked") {
		t.Fatalf("expected consumed token failure, got %v", err)
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

func TestBroker_ExchangePairingCodeAcceptsFormattedCode(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	pairingReq, code, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{
		Role:        RoleOperator,
		DisplayName: "Phone",
		Origin:      "secure-enrollment-test",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := broker.ApprovePairingRequest(context.Background(), pairingReq.ID, "cli:test"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	formattedCode := code[:3] + "-" + code[3:]
	if _, _, err := broker.ExchangePairingCode(context.Background(), PairingExchangeInput{RequestID: pairingReq.ID, Code: formattedCode}); err != nil {
		t.Fatalf("ExchangePairingCode formatted: %v", err)
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

func TestBroker_ApprovePairingRequest_RejectsExpiredWithoutResolving(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
		cfg.PairingCodeTTLSeconds = 1
	})
	defer cleanup()

	req, _, err := broker.CreatePairingRequest(context.Background(), PairingRequestInput{
		Role:        RoleOperator,
		DisplayName: "Ops Laptop",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	broker.Now = func() time.Time {
		return time.Unix(1700000002, 0).UTC()
	}

	if _, err := broker.ApprovePairingRequest(context.Background(), req.ID, "cli:test"); err == nil {
		t.Fatal("expected expired pairing approval to fail")
	}
	updated, err := broker.DB.GetPairingRequest(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("GetPairingRequest: %v", err)
	}
	if updated.Status != StatusExpired {
		t.Fatalf("expected expired pairing request to be marked expired, got %q", updated.Status)
	}
	if updated.ApprovedAt != 0 {
		t.Fatalf("expected no approved timestamp, got %d", updated.ApprovedAt)
	}
}

func TestBroker_DenyPairingRequest_RecordsDeniedTimestampOnly(t *testing.T) {
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
	if err := broker.DenyPairingRequest(context.Background(), req.ID, "cli:test"); err != nil {
		t.Fatalf("DenyPairingRequest: %v", err)
	}
	updated, err := broker.DB.GetPairingRequest(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("GetPairingRequest: %v", err)
	}
	if updated.Status != StatusDenied {
		t.Fatalf("expected denied status, got %q", updated.Status)
	}
	if updated.ApprovedAt != 0 {
		t.Fatalf("expected no approved timestamp, got %d", updated.ApprovedAt)
	}
	if updated.DeniedAt == 0 {
		t.Fatal("expected denied timestamp")
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

	if _, _, err := broker.RotateDeviceToken(context.Background(), "channel-device", RoleOperator, "Slack User", map[string]any{"channel": "slack", "identity": "U-old"}); err != nil {
		t.Fatalf("RotateDeviceToken channel device: %v", err)
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

func TestBroker_ApproveRequest_IsAtomic(t *testing.T) {
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
	issued, err := broker.ApproveRequest(context.Background(), decision.RequestID, "cli:test", false, "ok")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if strings.TrimSpace(issued.Token) == "" {
		t.Fatal("expected issued approval token")
	}
	req, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if req.Status != StatusApproved {
		t.Fatalf("expected approved status, got %q", req.Status)
	}
}

func TestBroker_AddAllowlist_IsIdempotent(t *testing.T) {
	broker, cleanup := newTestBroker(t, nil)
	defer cleanup()
	ctx := context.Background()

	first, err := broker.AddAllowlist(ctx, string(SubjectExec),
		AllowlistScope{HostID: "test-host", Tool: "exec"},
		ExecAllowlistMatcher{ExecutablePath: "/bin/echo", Argv: []string{"ok"}, WorkingDir: "/tmp"},
		"cli:test", 0)
	if err != nil {
		t.Fatalf("AddAllowlist first: %v", err)
	}
	second, err := broker.AddAllowlist(ctx, string(SubjectExec),
		AllowlistScope{HostID: "test-host", Tool: "exec"},
		ExecAllowlistMatcher{ExecutablePath: "/bin/echo", Argv: []string{"ok"}, WorkingDir: "/tmp"},
		"cli:test", 0)
	if err != nil {
		t.Fatalf("AddAllowlist second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected duplicate allowlist reuse, got %d and %d", first.ID, second.ID)
	}
}

func TestBroker_AskModeIgnoresAllowlist(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()
	ctx := context.Background()

	_, err := broker.AddAllowlist(ctx, string(SubjectExec),
		AllowlistScope{HostID: "test-host", Tool: "exec"},
		ExecAllowlistMatcher{ExecutablePath: "/bin/echo", Argv: []string{"allowed"}, WorkingDir: "/tmp"},
		"cli:test", 0)
	if err != nil {
		t.Fatalf("AddAllowlist: %v", err)
	}

	decision, err := broker.EvaluateExec(ctx, ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"allowed"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed || !decision.RequiresApproval {
		t.Fatalf("expected ask mode to ignore allowlist and require approval, got %#v", decision)
	}
}

func TestBroker_AllowlistMatchesBeyondFirstPage(t *testing.T) {
	broker, cleanup := newTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAllowlist
	})
	defer cleanup()
	ctx := context.Background()

	target, err := broker.AddAllowlist(ctx, string(SubjectExec),
		AllowlistScope{HostID: "test-host", Tool: "exec"},
		ExecAllowlistMatcher{ExecutablePath: "/bin/echo", Argv: []string{"target"}, WorkingDir: "/tmp"},
		"cli:test", 0)
	if err != nil {
		t.Fatalf("AddAllowlist target: %v", err)
	}
	for i := 0; i < 205; i++ {
		_, err := broker.AddAllowlist(ctx, string(SubjectExec),
			AllowlistScope{HostID: "test-host", Tool: "exec"},
			ExecAllowlistMatcher{ExecutablePath: "/bin/echo", Argv: []string{strconv.Itoa(i)}},
			"cli:test", 0)
		if err != nil {
			t.Fatalf("AddAllowlist noise %d: %v", i, err)
		}
	}

	decision, err := broker.EvaluateExec(ctx, ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"target"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if !decision.Allowed || decision.Reason != "allowlist" {
		t.Fatalf("expected allowlist match beyond first page, got %#v", decision)
	}
	if target.ID <= 0 {
		t.Fatal("expected target allowlist id")
	}
}
