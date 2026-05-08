package agentcli

import (
	"context"
	"testing"

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openChatManagerTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func testChatManager(database *db.DB) *ChatManager {
	jobs := agent.NewJobRegistry(0, 0)
	return &ChatManager{
		DB: database,
		Manager: &Manager{
			DB:       database,
			Jobs:     jobs,
			Registry: NewDefaultRegistry(),
			Cfg: config.AgentCLIConfig{
				Enabled:               true,
				DefaultMode:           string(RunnerModeSafeEdit),
				DefaultIsolation:      string(IsolationHostWorkspaceWrite),
				DefaultTimeoutSeconds: 60,
				MaxTimeoutSeconds:     120,
			},
			MaxQueued: 16,
		},
		Jobs: jobs,
	}
}

func TestChatManagerStartTurnDoesNotAppendUserMessageOnActiveConflict(t *testing.T) {
	d := openChatManagerTestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{
		AppSessionKey:    "app-session",
		RunnerID:         string(RunnerCodex),
		ContinuationMode: ContinuationReplay,
	})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if _, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "first"}); err != nil {
		t.Fatalf("StartTurn first: %v", err)
	}
	if _, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "second"}); err != db.ErrRunnerChatTurnActive {
		t.Fatalf("expected ErrRunnerChatTurnActive, got %v", err)
	}
	var count int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_key=?`, sess.AppSessionKey).Scan(&count); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected only accepted user message, got %d messages", count)
	}
}

func TestChatManagerUsesSessionMaxTurnsDefault(t *testing.T) {
	d := openChatManagerTestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{
		AppSessionKey:    "app-session",
		RunnerID:         string(RunnerClaude),
		ContinuationMode: ContinuationReplay,
		MaxTurns:         7,
	})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	result, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "hello"})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	run, ok, err := d.GetAgentCLIRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetAgentCLIRun: ok=%v err=%v", ok, err)
	}
	if run.MetaJSON == "" || run.MetaJSON == "{}" {
		t.Fatalf("expected max turns in meta, got %q", run.MetaJSON)
	}
	if got := run.MetaJSON; got != `{"_max_turns":7,"runner_chat_continuation_mode":"replay","runner_chat_native_session_ref":"","runner_chat_replay_prompt":"System: This conversation is being replayed for context. Previous turns are provided below in chronological order. Treat them as authoritative chat history.\n\nUser: hello\n","runner_chat_session_id":"`+sess.ID+`","runner_chat_turn_id":"`+result.Turn.ID+`","runner_chat_user_message":"hello"}` {
		t.Fatalf("unexpected meta json: %s", got)
	}
}

func TestChatManagerPersistsNormalizedRunnerEvents(t *testing.T) {
	d := openChatManagerTestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := d.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{
		ID:               "rcs-events",
		AppSessionKey:    "app-session",
		RunnerID:         string(RunnerCodex),
		ContinuationMode: string(ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := d.CreateRunnerChatTurn(ctx, db.RunnerChatTurn{
		ID:               "rct-events",
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusQueued,
		UserMessage:      "hello",
		ContinuationMode: string(ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	cm.persistJobEvent(turn, sess, "job-events", agent.JobEvent{
		Sequence: 3,
		Type:     "output",
		Data: map[string]any{
			"stream": "stdout",
			"chunk":  "hello",
		},
	})
	events, err := d.ListRunnerChatEvents(ctx, turn.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListRunnerChatEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "text_delta" || events[0].Text != "hello" {
		t.Fatalf("unexpected normalized events: %#v", events)
	}
}
