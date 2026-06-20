package runners

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
	return openRunnerTestDB(t)
}

func testChatManager(database *db.DB) *ChatManager {
	jobs := jobs.NewRegistry(0, 0)
	return &ChatManager{
		DB: database,
		Manager: &Manager{
			DB:       database,
			Jobs:     jobs,
			Registry: NewDefaultRegistry(),
			Cfg: config.RunnersConfig{
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
		RunnerID:         string(RunnerOpenCode),
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
	run, ok, err := d.GetRunnerRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetRunnerRun: ok=%v err=%v", ok, err)
	}
	if run.MetaJSON == "" || run.MetaJSON == "{}" {
		t.Fatalf("expected max turns in meta, got %q", run.MetaJSON)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(run.MetaJSON), &meta); err != nil {
		t.Fatalf("decode meta json: %v", err)
	}
	if got := meta["_max_turns"]; got != float64(7) {
		t.Fatalf("expected _max_turns=7, got %#v in %s", got, run.MetaJSON)
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
	run, ok, err := d.GetRunnerRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetRunnerRun: ok=%v err=%v", ok, err)
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

func TestChatManagerWorkspaceChangeStartsFreshNativeSession(t *testing.T) {
	d := openChatManagerTestDB(t)
	cm := testChatManager(d)
	ctx := context.Background()
	sess, err := cm.EnsureSession(ctx, StartTurnRequest{
		AppSessionKey: "app-workspace", RunnerID: string(RunnerOpenCode),
		ContinuationMode: ContinuationNative, Cwd: "/old-workspace",
	})
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if err := d.UpdateRunnerChatSessionNativeRef(ctx, sess.ID, "native-old"); err != nil {
		t.Fatalf("UpdateRunnerChatSessionNativeRef: %v", err)
	}
	result, err := cm.StartTurn(ctx, sess.ID, StartTurnRequest{
		UserMessage: "continue here", ContinuationMode: ContinuationNative,
		Cwd: "/new-workspace", PromptMessage: "compiled bootstrap",
	})
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	defer func() { _ = cm.Manager.Abort(ctx, result.JobID) }()
	updated, err := d.GetRunnerChatSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetRunnerChatSession: %v", err)
	}
	if updated.Cwd != "/new-workspace" || updated.NativeSessionRef != "" {
		t.Fatalf("expected fresh native workspace session, got %#v", updated)
	}
	run, ok, err := d.GetRunnerRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetRunnerRun: ok=%v err=%v", ok, err)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(run.MetaJSON), &meta); err != nil {
		t.Fatalf("decode run meta: %v", err)
	}
	if meta["runner_chat_native_session_ref"] != "" || run.Cwd != "/new-workspace" {
		t.Fatalf("stale native workspace metadata: run=%#v meta=%#v", run, meta)
	}
	appMeta, err := d.GetChatSessionMeta(ctx, sess.AppSessionKey)
	if err != nil || appMeta.RunnerCwd != "/new-workspace" {
		t.Fatalf("app session cwd=%q err=%v", appMeta.RunnerCwd, err)
	}
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

func TestRunnerChatWrapperErrorMessageBeatsNestedCodexJSONL(t *testing.T) {
	raw := `{"assistant_message_id":2636,"error_message":"Reading additional input from stdin...\nfailed to load skill /Users/brendon/.agents/skills/waveapps-accounting/SKILL.md: invalid YAML: mapping values are not allowed in this context at line 2 column 81","final_text":"{\"type\":\"thread.started\",\"thread_id\":\"019eb6e7\"}\n{\"type\":\"turn.completed\",\"status\":\"failed\"}","status":"failed"}`
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
	errText := extractErrorFromSnapshot(snap)
	if !strings.Contains(errText, "failed to load skill") || !strings.Contains(errText, "waveapps-accounting") {
		t.Fatalf("error = %q, want skill load failure", errText)
	}
}

func TestRunnerStructuredNoiseIsNotFinalText(t *testing.T) {
	raw := `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.completed","status":"failed"}`
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
}

func TestRunnerChatWrapperPlainFinalTextStillDisplays(t *testing.T) {
	raw := `{"assistant_message_id":2637,"error_message":"","final_text":"Done from Codex.","status":"succeeded"}`
	snap := jobs.Snapshot{
		Status: "completed",
		Events: []jobs.Event{{
			Type: "completion",
			Data: map[string]any{"final_text": raw, "final_text_preview": raw},
		}},
	}
	if got := extractFinalTextFromSnapshot(snap); got != "Done from Codex." {
		t.Fatalf("final text = %q, want wrapped text", got)
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
	run, ok, err := d.GetRunnerRun(ctx, result.JobID)
	if err != nil || !ok {
		t.Fatalf("GetRunnerRun: ok=%v err=%v", ok, err)
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
