package agentcli

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/agent"
	"or3-intern/internal/db"
)

func TestChatManagerStartTurnValidatesEmptyUserMessage(t *testing.T) {
	d := openAgentCLITestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{AppSessionKey: "app-session", RunnerID: string(RunnerCodex), ContinuationMode: ContinuationReplay})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if _, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "   "}); err == nil || !strings.Contains(err.Error(), "user_message required") {
		t.Fatalf("expected user message error, got %v", err)
	}
}

func TestChatManagerEnsureSessionDefaultsNativeForCapableRunners(t *testing.T) {
	d := openAgentCLITestDB(t)
	cm := testChatManager(d)
	sess, err := cm.EnsureSession(context.Background(), StartTurnRequest{AppSessionKey: "app-session", RunnerID: string(RunnerCodex)})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if sess.ContinuationMode != string(ContinuationNative) {
		t.Fatalf("expected native default continuation, got %q", sess.ContinuationMode)
	}
}

func TestChatManagerPersistsDistinctNativeRefsPerSession(t *testing.T) {
	d := openAgentCLITestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	first, err := d.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{ID: "session-a", AppSessionKey: "app-a", RunnerID: string(RunnerCodex), ContinuationMode: string(ContinuationNative)})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession first: %v", err)
	}
	second, err := d.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{ID: "session-b", AppSessionKey: "app-b", RunnerID: string(RunnerCodex), ContinuationMode: string(ContinuationNative)})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession second: %v", err)
	}
	cm.maybePersistNativeSessionRef(first, "job-a", agent.JobEvent{Type: "structured", Sequence: 1, Data: map[string]any{"payload": map[string]any{"type": "thread.started", "thread_id": "thread-a"}}})
	cm.maybePersistNativeSessionRef(second, "job-b", agent.JobEvent{Type: "structured", Sequence: 1, Data: map[string]any{"payload": map[string]any{"type": "thread.started", "thread_id": "thread-b"}}})

	gotFirst, err := d.GetRunnerChatSession(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetRunnerChatSession first: %v", err)
	}
	gotSecond, err := d.GetRunnerChatSession(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetRunnerChatSession second: %v", err)
	}
	if gotFirst.NativeSessionRef != "thread-a" || gotSecond.NativeSessionRef != "thread-b" {
		t.Fatalf("expected distinct native refs, got first=%q second=%q", gotFirst.NativeSessionRef, gotSecond.NativeSessionRef)
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
