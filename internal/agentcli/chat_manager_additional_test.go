package agentcli

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/db"
)

func TestChatManagerStartTurnRejectsUnsupportedNativeAndEmptyUserMessage(t *testing.T) {
	d := openAgentCLITestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{AppSessionKey: "app-session", RunnerID: string(RunnerCodex), ContinuationMode: ContinuationReplay})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if _, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{ContinuationMode: ContinuationNative, UserMessage: "continue"}); err != ErrUnsupportedNativeSession {
		t.Fatalf("expected ErrUnsupportedNativeSession, got %v", err)
	}
	if _, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "   "}); err == nil || !strings.Contains(err.Error(), "user_message required") {
		t.Fatalf("expected user message error, got %v", err)
	}
}

func TestChatManagerStartTurnAppendMessageFailureMarksTurnFailed(t *testing.T) {
	d := openAgentCLITestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{AppSessionKey: "app-session", RunnerID: string(RunnerCodex), ContinuationMode: ContinuationReplay})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if _, err := d.SQL.ExecContext(ctx, `DROP TABLE messages`); err != nil {
		t.Fatalf("DROP TABLE messages: %v", err)
	}
	if _, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "hello"}); err == nil || !strings.Contains(err.Error(), "persist user message") {
		t.Fatalf("expected append failure, got %v", err)
	}
	turns, err := d.ListRunnerChatTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("ListRunnerChatTurns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %d", len(turns))
	}
	if turns[0].Status != db.RunnerChatTurnStatusFailed || !strings.Contains(turns[0].ErrorMessage, "persist user message") {
		t.Fatalf("unexpected failed turn: %#v", turns[0])
	}
}

func TestChatManagerReconcileOnStartupPaths(t *testing.T) {
	d := openAgentCLITestDB(t)
	cm := testChatManager(d)
	if err := cm.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("expected zero-row reconcile to succeed, got %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cm.ReconcileOnStartup(context.Background()); err == nil {
		t.Fatal("expected reconcile error after closing DB")
	}
}

func TestChatManagerHelperGuards(t *testing.T) {
	cm := &ChatManager{}
	cm.bumpChatSessionMeta("session", "codex", "rcs-1", "assistant output")
	if _, _, err := cm.chatRunner(string(RunnerCodex)); err == nil || !strings.Contains(err.Error(), "runner registry unavailable") {
		t.Fatalf("expected nil manager/registry error, got %v", err)
	}
	cm = &ChatManager{Manager: &Manager{}}
	if _, _, err := cm.chatRunner(string(RunnerCodex)); err == nil || !strings.Contains(err.Error(), "runner registry unavailable") {
		t.Fatalf("expected nil registry error, got %v", err)
	}
}
