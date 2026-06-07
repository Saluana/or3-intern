package agentcli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

func openChatManagerTestDB(t *testing.T) *db.DB {
	t.Helper()
	return openAgentCLITestDB(t)
}

func testChatManager(database *db.DB) *ChatManager {
	jobs := jobs.NewRegistry(0, 0)
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
	result, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "first"})
	if err != nil {
		t.Fatalf("StartTurn first: %v", err)
	}
	defer func() { _ = cm.Manager.Abort(ctx, result.JobID) }()
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
	defer func() { _ = cm.Manager.Abort(ctx, result.JobID) }()
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

func TestChatManagerReplayDoesNotForwardNativeSessionRef(t *testing.T) {
	d := openChatManagerTestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{
		AppSessionKey:    "app-session",
		RunnerID:         string(RunnerOpenCode),
		ContinuationMode: ContinuationReplay,
	})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if err := d.UpdateRunnerChatSessionNativeRef(ctx, sess.ID, "stale_opencode_session"); err != nil {
		t.Fatalf("UpdateRunnerChatSessionNativeRef: %v", err)
	}
	result, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{UserMessage: "hello"})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	run, ok, err := d.GetAgentCLIRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetAgentCLIRun: ok=%v err=%v", ok, err)
	}
	meta := map[string]any{}
	if err := json.Unmarshal([]byte(run.MetaJSON), &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if got := meta["runner_chat_native_session_ref"]; got != "" {
		t.Fatalf("runner_chat_native_session_ref = %#v, want empty", got)
	}
	if err := cm.Manager.Abort(ctx, result.JobID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	for i := 0; i < 50; i++ {
		latest, err := d.GetRunnerChatTurn(ctx, result.Turn.ID)
		if err != nil {
			t.Fatalf("GetRunnerChatTurn: %v", err)
		}
		if latest.Status == db.RunnerChatTurnStatusAborted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("turn did not finalize after abort")
}

func TestRunnerErrorEnvelopeIsNotFinalText(t *testing.T) {
	raw := `{"type":"error","timestamp":1780727114335,"sessionID":"ses_1646495d3ffe7m5AzHnsNey91c","error":{"name":"UnknownError","data":{"message":"Unexpected server error. Check server logs for details.","ref":"err_c208f54a"}}}`
	snap := jobs.Snapshot{
		Status: "failed",
		Events: []jobs.Event{{
			Type: "completion",
			Data: map[string]any{"final_text": raw, "final_text_preview": raw},
		}},
	}
	if got := extractFinalTextFromSnapshot(snap); got != "" {
		t.Fatalf("final text = %q, want empty", got)
	}
	if got := extractErrorFromSnapshot(snap); got != "Unexpected server error. Check server logs for details." {
		t.Fatalf("error = %q", got)
	}
}

func TestChatManagerUsesFinalPromptWithoutReplayWrapping(t *testing.T) {
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
	finalPrompt := "<trusted_or3_system_instructions>\nSOUL\n</trusted_or3_system_instructions>\n\n<or3_context>\nreplay_history:\nUser: previous\nAssistant: answer\n</or3_context>\n\n<user_task>\nhello\n</user_task>\n"
	result, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{
		UserMessage:        "hello",
		PromptMessage:      finalPrompt,
		PromptMessageFinal: true,
	})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	defer func() { _ = cm.Manager.Abort(ctx, result.JobID) }()
	run, ok, err := d.GetAgentCLIRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetAgentCLIRun: ok=%v err=%v", ok, err)
	}
	if run.Task != strings.TrimSpace(finalPrompt) {
		t.Fatalf("expected final prompt as task, got %q", run.Task)
	}
	if strings.HasPrefix(run.Task, "System: This conversation is being replayed") {
		t.Fatalf("final prompt was replay-wrapped: %q", run.Task)
	}
	if !strings.HasPrefix(run.Task, "<trusted_or3_system_instructions>") {
		t.Fatalf("final prompt lost trusted prefix: %q", run.Task)
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
	cm.persistJobEvent(turn, sess, "job-events", &turnMirrorState{}, jobs.Event{
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

func TestChatManagerRunnerChatEventsPayloadPreservesCanonicalPayload(t *testing.T) {
	d := openChatManagerTestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := d.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{
		ID:               "rcs-payload",
		AppSessionKey:    "app-session",
		RunnerID:         string(RunnerCodex),
		ContinuationMode: string(ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := d.CreateRunnerChatTurn(ctx, db.RunnerChatTurn{
		ID:               "rct-payload",
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusQueued,
		UserMessage:      "hello",
		ContinuationMode: string(ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	payload := `{"type":"item.started","item_type":"command_execution","status":"inProgress","title":"Command run"}`
	if err := d.AppendRunnerChatEvent(ctx, db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		JobID:       "job-payload",
		Seq:         4,
		Type:        "item.started",
		Text:        "go test ./...",
		PayloadJSON: payload,
	}); err != nil {
		t.Fatalf("AppendRunnerChatEvent: %v", err)
	}
	events := cm.runnerChatEventsPayload(turn.ID)
	if len(events) != 1 {
		t.Fatalf("expected one event payload, got %#v", events)
	}
	if events[0]["type"] != "item.started" || events[0]["text"] != "go test ./..." || events[0]["job_id"] != "job-payload" {
		t.Fatalf("expected event fields preserved, got %#v", events[0])
	}
	encoded, err := json.Marshal(events[0]["payload"])
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	if !strings.Contains(string(encoded), `"item_type":"command_execution"`) {
		t.Fatalf("expected canonical payload object, got %s", string(encoded))
	}
}
