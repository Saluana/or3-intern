package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func testApprovalBroker(t *testing.T) *approval.Broker {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "approvals-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg := config.Default().Security.Approvals
	cfg.Enabled = true
	cfg.HostID = "test-host"
	cfg.Exec.Mode = config.ApprovalModeAsk
	return &approval.Broker{
		DB:      database,
		Config:  cfg,
		HostID:  "test-host",
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
		Now:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
}

func TestRunApprovalsCommand_NilBroker(t *testing.T) {
	var out bytes.Buffer
	err := runApprovalsCommand(context.Background(), nil, []string{"list"}, &out, &out)
	if err == nil {
		t.Fatal("expected error for nil broker")
	}
}

func TestRunApprovalsCommand_NoArgs(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	err := runApprovalsCommand(context.Background(), broker, nil, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunApprovalsCommand_List_Empty(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	if err := runApprovalsCommand(context.Background(), broker, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	// Empty list should produce no output lines.
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected empty list output, got %q", out.String())
	}
}

func TestRunApprovalsCommand_List_ShowsPending(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	_, _ = broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"/bin/echo", "hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	var out bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.String(), "pending") {
		t.Errorf("expected 'pending' in list output, got %q", out.String())
	}
}

func TestRunApprovalsCommand_Show(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"/bin/echo", "test"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	idStr := strings.TrimSpace(sprint64(decision.RequestID))

	var out bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"show", idStr}, &out, &out); err != nil {
		t.Fatalf("show: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "id:") || !strings.Contains(text, "status:") {
		t.Errorf("expected id and status in show output, got %q", text)
	}
}

func TestRunApprovalsCommand_Show_InvalidID(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	if err := runApprovalsCommand(context.Background(), broker, []string{"show", "notanumber"}, &out, &out); err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestRunApprovalsCommand_Approve(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"/bin/echo", "approve-test"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil || decision.RequestID == 0 {
		t.Fatalf("EvaluateExec: %v (id=%d)", err, decision.RequestID)
	}
	idStr := sprint64(decision.RequestID)

	var out bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"approve", idStr}, &out, &out); err != nil {
		t.Fatalf("approve: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "approved") {
		t.Errorf("expected 'approved' in output, got %q", text)
	}
	if !strings.Contains(text, "token:") {
		t.Errorf("expected 'token:' in output, got %q", text)
	}
}

func TestRunApprovalsCommand_Approve_WithAllowlist(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"/bin/echo", "allowlist-test"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil || decision.RequestID == 0 {
		t.Fatalf("EvaluateExec: %v", err)
	}
	idStr := sprint64(decision.RequestID)

	var out bytes.Buffer
	var errBuf bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"approve", "--allowlist", idStr}, &out, &errBuf); err != nil {
		t.Fatalf("approve --allowlist: %v (stderr: %s)", err, errBuf.String())
	}
	text := out.String()
	if !strings.Contains(text, "allowlist_id:") {
		t.Errorf("expected 'allowlist_id:' in output, got %q", text)
	}
}

func TestRunApprovalsCommand_Deny(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()
	decision, err := broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"/bin/echo", "deny-test"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil || decision.RequestID == 0 {
		t.Fatalf("EvaluateExec: %v", err)
	}
	idStr := sprint64(decision.RequestID)

	var out bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"deny", idStr}, &out, &out); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if !strings.Contains(out.String(), "denied") {
		t.Errorf("expected 'denied' in output, got %q", out.String())
	}
}

func TestRunApprovalsCommand_Deny_InvalidID(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	if err := runApprovalsCommand(context.Background(), broker, []string{"deny", "bad"}, &out, &out); err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestRunApprovalsCommand_UnknownSubcommand(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	err := runApprovalsCommand(context.Background(), broker, []string{"frobnicate"}, &out, &out)
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunApprovalAllowlistCommand_AddAndList(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()

	var out bytes.Buffer
	var errBuf bytes.Buffer
	err := runApprovalsCommand(ctx, broker,
		[]string{"allowlist", "add", "--domain", "exec", "--program", "/bin/echo"},
		&out, &errBuf)
	if err != nil {
		t.Fatalf("allowlist add: %v (stderr: %s)", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "added allowlist") {
		t.Errorf("expected 'added allowlist' in output, got %q", out.String())
	}

	out.Reset()
	if err := runApprovalsCommand(ctx, broker, []string{"allowlist", "list"}, &out, &out); err != nil {
		t.Fatalf("allowlist list: %v", err)
	}
	if !strings.Contains(out.String(), "exec") {
		t.Errorf("expected 'exec' in allowlist list output, got %q", out.String())
	}
}

func TestRunApprovalAllowlistCommand_Remove(t *testing.T) {
	broker := testApprovalBroker(t)
	ctx := context.Background()

	// Add a rule directly via the broker to get a stable ID.
	rec, err := broker.AddAllowlist(ctx, string(approval.SubjectExec),
		approval.AllowlistScope{HostID: "test-host", Tool: "exec"},
		approval.ExecAllowlistMatcher{ExecutablePath: "/bin/ls"},
		"cli", 0)
	if err != nil {
		t.Fatalf("AddAllowlist: %v", err)
	}
	idStr := sprint64(rec.ID)

	var out bytes.Buffer
	if err := runApprovalsCommand(ctx, broker, []string{"allowlist", "remove", idStr}, &out, &out); err != nil {
		t.Fatalf("allowlist remove: %v", err)
	}
	if !strings.Contains(out.String(), "removed allowlist") {
		t.Errorf("expected 'removed allowlist' in output, got %q", out.String())
	}
}

func TestRunApprovalAllowlistCommand_UnknownSubcommand(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	err := runApprovalsCommand(context.Background(), broker, []string{"allowlist", "unknown"}, &out, &out)
	if err == nil {
		t.Fatal("expected error for unknown allowlist subcommand")
	}
}

func TestRunApprovalsCommand_RejectsExtraArgs(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	for _, args := range [][]string{{"show", "1", "extra"}, {"list", "pending", "extra"}, {"allowlist", "add", "extra"}} {
		if err := runApprovalsCommand(context.Background(), broker, args, &out, &out); err == nil {
			t.Fatalf("expected args %v to fail", args)
		}
	}
}

func TestRunApprovalAllowlistCommand_RejectsEmptyMatcher(t *testing.T) {
	broker := testApprovalBroker(t)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	err := runApprovalsCommand(context.Background(), broker, []string{"allowlist", "add", "--domain", "exec"}, &out, &errBuf)
	if err == nil {
		t.Fatal("expected empty exec allowlist matcher to fail")
	}
	if !strings.Contains(err.Error(), "must include at least one") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// sprint64 formats int64 as decimal string.
func sprint64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n >= 10 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	pos--
	buf[pos] = byte('0' + n)
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
