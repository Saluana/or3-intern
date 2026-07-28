package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

func TestListRunnerChatSessionsOrdersFiltersAndBounds(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for _, sess := range []RunnerChatSession{
		{ID: "rcs-old", AppSessionKey: "or3-chat:workspace-a:thread-1", RunnerID: "codex", ContinuationMode: "replay", CreatedAt: 100, UpdatedAt: 100},
		{ID: "rcs-new", AppSessionKey: "or3-chat:workspace-a:thread-2", RunnerID: "opencode", ContinuationMode: "replay", CreatedAt: 200, UpdatedAt: 300},
		{ID: "rcs-other", AppSessionKey: "or3-chat:workspace-b:thread-1", RunnerID: "codex", ContinuationMode: "replay", CreatedAt: 150, UpdatedAt: 200},
		{ID: "rcs-wildcard", AppSessionKey: "literalXprefix:thread", RunnerID: "codex", ContinuationMode: "replay", CreatedAt: 400, UpdatedAt: 400},
	} {
		if _, err := d.CreateOrGetRunnerChatSession(ctx, sess); err != nil {
			t.Fatalf("CreateOrGetRunnerChatSession(%s): %v", sess.ID, err)
		}
	}

	filtered, err := d.ListRunnerChatSessions(ctx, RunnerChatSessionListFilter{
		AppSessionKeyPrefix: "or3-chat:workspace-a:",
		Limit:               10,
	})
	if err != nil {
		t.Fatalf("ListRunnerChatSessions filtered: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != "rcs-new" || filtered[1].ID != "rcs-old" {
		t.Fatalf("unexpected filtered order: %#v", filtered)
	}

	limited, err := d.ListRunnerChatSessions(ctx, RunnerChatSessionListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListRunnerChatSessions limited: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "rcs-wildcard" {
		t.Fatalf("unexpected limited sessions: %#v", limited)
	}

	literalWildcard, err := d.ListRunnerChatSessions(ctx, RunnerChatSessionListFilter{
		AppSessionKeyPrefix: "literal%prefix:",
		Limit:               10,
	})
	if err != nil {
		t.Fatalf("ListRunnerChatSessions literal wildcard: %v", err)
	}
	if len(literalWildcard) != 0 {
		t.Fatalf("prefix must not use SQL wildcard semantics: %#v", literalWildcard)
	}

	whitespacePrefix, err := d.ListRunnerChatSessions(ctx, RunnerChatSessionListFilter{
		AppSessionKeyPrefix: "   ",
		Limit:               10,
	})
	if err != nil {
		t.Fatalf("ListRunnerChatSessions whitespace prefix: %v", err)
	}
	if len(whitespacePrefix) != 0 {
		t.Fatalf("a non-empty prefix must never widen to an unfiltered list: %#v", whitespacePrefix)
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

func TestListRunnerChatTurnsLimitReturnsNewestInChronologicalOrder(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	sess, err := d.CreateOrGetRunnerChatSession(ctx, RunnerChatSession{
		ID:               "rcs-turn-window",
		AppSessionKey:    "or3-chat:workspace:thread",
		RunnerID:         "codex",
		ContinuationMode: "replay",
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := d.CreateRunnerChatTurn(ctx, RunnerChatTurn{
			ID:               fmt.Sprintf("rct-window-%d", i),
			SessionID:        sess.ID,
			Status:           RunnerChatTurnStatusSucceeded,
			UserMessage:      fmt.Sprintf("turn %d", i),
			ContinuationMode: "replay",
			RequestedAt:      int64(i),
			CompletedAt:      int64(i),
		}); err != nil {
			t.Fatalf("CreateRunnerChatTurn(%d): %v", i, err)
		}
	}

	recent, err := d.ListRunnerChatTurns(ctx, sess.ID, 2)
	if err != nil {
		t.Fatalf("ListRunnerChatTurns limited: %v", err)
	}
	if len(recent) != 2 ||
		recent[0].Sequence != 4 ||
		recent[1].Sequence != 5 {
		t.Fatalf("expected newest two turns in chronological order, got %#v", recent)
	}

	all, err := d.ListRunnerChatTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("ListRunnerChatTurns unbounded: %v", err)
	}
	if len(all) != 5 || all[0].Sequence != 1 || all[4].Sequence != 5 {
		t.Fatalf("expected full chronological history, got %#v", all)
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
