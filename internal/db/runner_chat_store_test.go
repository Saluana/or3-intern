package db

import (
	"context"
	"errors"
	"testing"
)

func TestRunnerChatStoreTurnLifecycleAndActiveUniqueness(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	sess, err := d.CreateOrGetRunnerChatSession(ctx, RunnerChatSession{
		ID:               "rcs-test",
		AppSessionKey:    "app-session",
		RunnerID:         "codex",
		ContinuationMode: "replay",
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := d.CreateRunnerChatTurn(ctx, RunnerChatTurn{
		ID:               "rct-test",
		SessionID:        sess.ID,
		Status:           RunnerChatTurnStatusQueued,
		UserMessage:      "hello",
		ContinuationMode: "replay",
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	if _, err := d.CreateRunnerChatTurn(ctx, RunnerChatTurn{
		ID:               "rct-conflict",
		SessionID:        sess.ID,
		Status:           RunnerChatTurnStatusQueued,
		UserMessage:      "again",
		ContinuationMode: "replay",
	}); !errors.Is(err, ErrRunnerChatTurnActive) {
		t.Fatalf("expected ErrRunnerChatTurnActive, got %v", err)
	}
	if err := d.MarkRunnerChatTurnStarted(ctx, turn.ID, "run-1", "job-1"); err != nil {
		t.Fatalf("MarkRunnerChatTurnStarted: %v", err)
	}
	if err := d.AppendRunnerChatEvent(ctx, RunnerChatEvent{TurnID: turn.ID, SessionID: sess.ID, JobID: "job-1", Seq: 1, Type: "text_delta", Text: "hi"}); err != nil {
		t.Fatalf("AppendRunnerChatEvent: %v", err)
	}
	events, err := d.ListRunnerChatEvents(ctx, turn.ID, 0, 10)
	if err != nil || len(events) != 1 || events[0].Text != "hi" {
		t.Fatalf("ListRunnerChatEvents got %#v err=%v", events, err)
	}
	if err := d.FinalizeRunnerChatTurn(ctx, turn.ID, RunnerChatTurnFinalize{Status: RunnerChatTurnStatusSucceeded, FinalText: "done", CompletedAt: NowMS()}); err != nil {
		t.Fatalf("FinalizeRunnerChatTurn: %v", err)
	}
	if _, err := d.CreateRunnerChatTurn(ctx, RunnerChatTurn{
		ID:               "rct-next",
		SessionID:        sess.ID,
		Status:           RunnerChatTurnStatusQueued,
		UserMessage:      "next",
		ContinuationMode: "replay",
	}); err != nil {
		t.Fatalf("CreateRunnerChatTurn after finalize: %v", err)
	}
}

func TestRunnerChatStoreReconcileOnStartup(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	sess, err := d.CreateOrGetRunnerChatSession(ctx, RunnerChatSession{ID: "rcs-reconcile", AppSessionKey: "app", RunnerID: "opencode", ContinuationMode: "replay"})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	if _, err := d.CreateRunnerChatTurn(ctx, RunnerChatTurn{ID: "rct-reconcile", SessionID: sess.ID, Status: RunnerChatTurnStatusRunning, UserMessage: "x", ContinuationMode: "replay"}); err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	n, err := d.ReconcileRunnerChatTurnsOnStartup(ctx)
	if err != nil {
		t.Fatalf("ReconcileRunnerChatTurnsOnStartup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reconciled turn, got %d", n)
	}
	turn, err := d.GetRunnerChatTurn(ctx, "rct-reconcile")
	if err != nil {
		t.Fatalf("GetRunnerChatTurn: %v", err)
	}
	if turn.Status != RunnerChatTurnStatusAborted || turn.ErrorMessage == "" {
		t.Fatalf("turn not reconciled: %#v", turn)
	}
}
