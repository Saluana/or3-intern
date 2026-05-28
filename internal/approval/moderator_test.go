package approval

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func newModeratorTestBroker(t *testing.T, moderator Moderator, mutate func(*config.ApprovalConfig)) *Broker {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "moderator-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Enabled = true
	approvalCfg.HostID = "test-host"
	approvalCfg.Exec.Mode = config.ApprovalModeAsk
	approvalCfg.Moderator.Enabled = true
	approvalCfg.Moderator.Actions = config.ApprovalModeratorActionMap{
		Low: config.ApprovalModeratorActionApprove, Medium: config.ApprovalModeratorActionApprove,
		High: config.ApprovalModeratorActionEscalate, Extreme: config.ApprovalModeratorActionDeny,
	}
	if mutate != nil {
		mutate(&approvalCfg)
	}
	return &Broker{
		DB: database, Config: approvalCfg, HostID: approvalCfg.HostID,
		SignKey: []byte("0123456789abcdef0123456789abcdef"), Moderator: moderator,
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
}

func TestBroker_ModeratorAutoApprovesLowRiskExec(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskLow, Action: ModeratorApprove, Reason: "bounded test command",
	}}, nil)
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/usr/bin/go", Argv: []string{"test", "./..."}, WorkingDir: "/tmp/workspace", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if !decision.Allowed || decision.Reason != "moderator_approved" {
		t.Fatalf("expected moderator auto-approve, got %#v", decision)
	}
}

func TestBroker_ModeratorEscalatesHighRiskEvenWhenModelApproves(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskHigh, Action: ModeratorApprove, Reason: "model tried approve",
	}}, nil)
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/bin/bash", Argv: []string{"-c", "echo hi"}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed || !decision.RequiresApproval {
		t.Fatalf("expected escalation, got %#v", decision)
	}
}

func TestBroker_ModeratorDeniesWithUserPolicyGrepAlternative(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskLow, Action: ModeratorApprove, Reason: "ok",
	}}, func(cfg *config.ApprovalConfig) {
		cfg.Moderator.UserPolicy = "never use grep"
	})
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/usr/bin/grep", Argv: []string{"secret", "."}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected policy deny, got %#v", decision)
	}
	if !strings.Contains(decision.Reason, "rg") {
		t.Fatalf("expected rg alternative in reason, got %q", decision.Reason)
	}
}

func TestBroker_ModeratorFailureEscalates(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Err: errors.New("timeout")}, nil)
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/usr/bin/go", Argv: []string{"test"}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed || !decision.RequiresApproval {
		t.Fatalf("expected failure escalation, got %#v", decision)
	}
}

func TestBroker_ModeratorHardDenyDestructive(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskLow, Action: ModeratorApprove, Reason: "should not run",
	}}, nil)
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/bin/rm", Argv: []string{"-rf", "/"}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected hard deny, got %#v", decision)
	}
}

func TestBroker_ModeratorRequiresSigningKeyBeforeReview(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskLow, Action: ModeratorApprove, Reason: "ok",
	}}, nil)
	broker.SignKey = nil
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/usr/bin/go", Argv: []string{"test"}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed || decision.RequiresApproval {
		t.Fatalf("expected broker unavailable without signing key, got %#v", decision)
	}
	if decision.Reason != "approval broker unavailable" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestBroker_ApproveRequestRejectsWithoutSigningKey(t *testing.T) {
	broker := newModeratorTestBroker(t, nil, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
		cfg.Moderator.Enabled = false
	})
	ctx := context.Background()
	decision, err := broker.EvaluateExec(ctx, ExecEvaluation{
		ExecutablePath: "/usr/bin/go", Argv: []string{"test"}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if !decision.RequiresApproval || decision.RequestID == 0 {
		t.Fatalf("expected pending request, got %#v", decision)
	}
	broker.SignKey = nil
	if _, err := broker.ApproveRequest(ctx, decision.RequestID, "human", false, "ok"); err == nil {
		t.Fatal("expected ApproveRequest to fail without signing key")
	}
}

func TestBroker_ModeratorHardDenySecurityWeakening(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskLow, Action: ModeratorApprove, Reason: "should not approve",
	}}, nil)
	decision, err := broker.EvaluateExec(context.Background(), ExecEvaluation{
		ExecutablePath: "/usr/bin/jq",
		Argv:           []string{".security.approvals.enabled = false", "config.json"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected hard deny for security weakening, got %#v", decision)
	}
}

func TestParseModeratorResponseRejectsInvalid(t *testing.T) {
	if _, err := parseModeratorResponse(`{"risk":"low","action":"maybe","reason":"x"}`); err == nil {
		t.Fatal("expected invalid action to fail")
	}
	if _, err := parseModeratorResponse(`{"risk":"low","action":"approve"}`); err == nil {
		t.Fatal("expected missing reason to fail")
	}
}

func TestRedactModeratorText(t *testing.T) {
	text := "api_key=supersecret token=abc.def.ghi"
	redacted, stats := redactModeratorText(text, 200)
	if strings.Contains(redacted, "supersecret") {
		t.Fatalf("expected redacted output, got %q", redacted)
	}
	if stats.Secrets == 0 {
		t.Fatal("expected secret redaction count")
	}
}

func TestEvaluateToolQuotaModeratorApprove(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskLow, Action: ModeratorApprove, Reason: "small bump",
	}}, func(cfg *config.ApprovalConfig) {
		cfg.Moderator.Actions.Low = config.ApprovalModeratorActionApprove
	})
	decision, err := broker.EvaluateToolQuota(context.Background(), ToolQuotaEvaluation{
		Scope: "session", LimitName: "tool_calls", ToolName: "read_file", Current: 9, Limit: 10,
	}, config.ApprovalModeAsk)
	if err != nil {
		t.Fatalf("EvaluateToolQuota: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected auto-approve, got %#v", decision)
	}
}

func TestEvaluateMessageSendRequiresApprovalWhenAskMode(t *testing.T) {
	broker := newModeratorTestBroker(t, nil, func(cfg *config.ApprovalConfig) {
		cfg.Moderator.Enabled = false
		cfg.MessageSend.Mode = config.ApprovalModeAsk
	})
	decision, err := broker.EvaluateMessageSend(context.Background(), MessageSendEvaluation{
		Channel: "slack", To: "U123", Text: "hello", AgentID: "agent", SessionID: "sess",
	})
	if err != nil {
		t.Fatalf("EvaluateMessageSend: %v", err)
	}
	if decision.Allowed || !decision.RequiresApproval {
		t.Fatalf("expected pending approval, got %#v", decision)
	}
}

func TestEvaluateToolQuotaModeratorEscalatesHighIncrease(t *testing.T) {
	broker := newModeratorTestBroker(t, &FakeModerator{Result: ModeratorReviewResult{
		Risk: RiskHigh, Action: ModeratorApprove, Reason: "big bump",
	}}, nil)
	decision, err := broker.EvaluateToolQuota(context.Background(), ToolQuotaEvaluation{
		Scope: "session", LimitName: "tool_calls", ToolName: "exec", Current: 100, Limit: 500,
	}, config.ApprovalModeAsk)
	if err != nil {
		t.Fatalf("EvaluateToolQuota: %v", err)
	}
	if decision.Allowed || !decision.RequiresApproval {
		t.Fatalf("expected escalation, got %#v", decision)
	}
}
