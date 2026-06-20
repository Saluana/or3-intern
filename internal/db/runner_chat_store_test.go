package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestUpdateRunnerChatSessionCwdClearsNativeReference(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	sess, err := d.CreateOrGetRunnerChatSession(ctx, RunnerChatSession{
		ID: "rcs-workspace", AppSessionKey: "app-workspace", RunnerID: "opencode",
		ContinuationMode: "native", NativeSessionRef: "native-old", Cwd: "/old",
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	updated, err := d.UpdateRunnerChatSessionCwd(ctx, sess.ID, "/new")
	if err != nil {
		t.Fatalf("UpdateRunnerChatSessionCwd: %v", err)
	}
	if updated.Cwd != "/new" || updated.NativeSessionRef != "" {
		t.Fatalf("unexpected updated session: %#v", updated)
	}
}

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

func TestRunnerChatTurnColumnsMigration_ExistingDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy_runner_chat.db")

	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	legacyStmts := []string{
		`CREATE TABLE sessions(key TEXT PRIMARY KEY, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, metadata_json TEXT NOT NULL DEFAULT '{}', last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE runner_chat_sessions(
			id TEXT PRIMARY KEY,
			app_session_key TEXT NOT NULL,
			runner_id TEXT NOT NULL,
			continuation_mode TEXT NOT NULL DEFAULT 'replay',
			native_session_ref TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT '',
			isolation TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			max_turns INTEGER NOT NULL DEFAULT 0,
			meta_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(app_session_key, runner_id)
		);`,
		`CREATE TABLE runner_chat_turns(
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			status TEXT NOT NULL,
			user_message TEXT NOT NULL DEFAULT '',
			final_text TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			runner_job_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT '',
			isolation TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			continuation_mode TEXT NOT NULL DEFAULT 'replay',
			user_message_id INTEGER NOT NULL DEFAULT 0,
			assistant_message_id INTEGER NOT NULL DEFAULT 0,
			requested_at INTEGER NOT NULL,
			started_at INTEGER NOT NULL DEFAULT 0,
			completed_at INTEGER NOT NULL DEFAULT 0,
			meta_json TEXT NOT NULL DEFAULT '{}'
		);`,
	}
	for _, stmt := range legacyStmts {
		if _, err := rawDB.Exec(stmt); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	if _, err := rawDB.Exec(`INSERT INTO sessions(key, created_at, updated_at) VALUES('app-session', 1, 1)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO runner_chat_sessions(id, app_session_key, runner_id, continuation_mode, created_at, updated_at) VALUES('rcs-legacy', 'app-session', 'opencode', 'replay', 1, 1)`); err != nil {
		t.Fatalf("seed runner chat session: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO runner_chat_turns(id, session_id, sequence, status, user_message, continuation_mode, requested_at) VALUES('rct-legacy', 'rcs-legacy', 1, 'succeeded', 'hello', 'replay', 1)`); err != nil {
		t.Fatalf("seed runner chat turn: %v", err)
	}
	_ = rawDB.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open after legacy schema: %v", err)
	}
	defer d.Close()

	turns, err := d.ListRunnerChatTurns(context.Background(), "rcs-legacy", 0)
	if err != nil {
		t.Fatalf("ListRunnerChatTurns after migration: %v", err)
	}
	if len(turns) != 1 || turns[0].UserMessage != "hello" {
		t.Fatalf("unexpected turns after migration: %#v", turns)
	}
	if turns[0].RunnerRunID != "" {
		t.Fatalf("expected empty runner_run_id default, got %q", turns[0].RunnerRunID)
	}
}
