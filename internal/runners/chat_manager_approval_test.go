package runners

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

type fakeResponderRuntime struct {
	fakeRuntime
	responded atomic.Int32
	lastRef   atomic.Pointer[NativeRequestRef]
	failNext  atomic.Bool
}

func (r *fakeResponderRuntime) RespondToNativeRequest(ctx context.Context, ref NativeRequestRef, decision NativeRequestDecision) error {
	if r.failNext.Load() {
		return errors.New("responder offline")
	}
	refCopy := ref
	r.lastRef.Store(&refCopy)
	r.responded.Add(1)
	return nil
}

func setupChatManagerForApproval(t *testing.T) (*db.DB, *ChatManager, *approval.Broker, *fakeResponderRuntime) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "or3.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	jobsReg := jobs.NewRegistry(0, 0)
	runtime := &fakeResponderRuntime{fakeRuntime: fakeRuntime{id: RunnerCodex}}
	registry := &RunnerRuntimeRegistry{}
	registry.Register(runtime)
	apCfg := config.Default().Security.Approvals
	apCfg.Enabled = true
	apCfg.HostID = "test-host"
	apCfg.Exec.Mode = config.ApprovalModeAsk
	broker := &approval.Broker{
		DB:      d,
		Config:  apCfg,
		HostID:  "test-host",
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
		Now:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	cm := &ChatManager{
		DB: d,
		Manager: &Manager{
			DB:       d,
			Jobs:     jobsReg,
			Registry: NewDefaultRegistry(),
			Cfg: config.RunnersConfig{
				DefaultMode:           string(RunnerModeSafeEdit),
				DefaultIsolation:      string(IsolationHostWorkspaceWrite),
				DefaultTimeoutSeconds: 60,
				MaxTimeoutSeconds:     120,
			},
			Runtimes: registry,
		},
		Jobs:   jobsReg,
		Broker: broker,
	}
	return d, cm, broker, runtime
}

func seedTurnWithApproval(t *testing.T, d *db.DB, approvalID int64) (db.RunnerChatSession, db.RunnerChatTurn) {
	t.Helper()
	sess, err := d.CreateOrGetRunnerChatSession(context.Background(), db.RunnerChatSession{
		ID:               "rcs-approval",
		AppSessionKey:    "app-session",
		RunnerID:         string(RunnerCodex),
		ContinuationMode: string(ContinuationNative),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := d.CreateRunnerChatTurn(context.Background(), db.RunnerChatTurn{
		ID:               "rct-approval",
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusApprovalRequired,
		UserMessage:      "hello",
		ContinuationMode: string(ContinuationNative),
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	return sess, turn
}

func insertApprovalRow(t *testing.T, d *db.DB, id int64, sessionID, subject string) {
	t.Helper()
	now := db.NowMS()
	_, err := d.SQL.ExecContext(context.Background(),
		`INSERT INTO approval_requests(id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, execution_host_id, status, policy_mode, requested_at, expires_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "runner_permission", subject, `{"path":"`+subject+`"}`, "codex", sessionID, "host-test", approval.StatusPending, "moderate", now, now+int64(60*60*1000))
	if err != nil {
		t.Fatalf("insert approval row: %v", err)
	}
}

func TestRespondToTurnApprovalLiveContinuation(t *testing.T) {
	d, cm, broker, runtime := setupChatManagerForApproval(t)
	ctx := context.Background()
	sess, turn := seedTurnWithApproval(t, d, 0)
	assistantMessageID, err := cm.appendMessage(ctx, sess.AppSessionKey, "assistant", "Waiting for approval", map[string]any{
		"status":              db.RunnerChatTurnStatusApprovalRequired,
		"approval_id":         4242,
		"approval_request_id": 4242,
		"approval_state":      "pending",
	})
	if err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	if err := d.SetRunnerChatTurnAssistantMessageID(ctx, turn.ID, assistantMessageID); err != nil {
		t.Fatalf("SetRunnerChatTurnAssistantMessageID: %v", err)
	}
	turn.AssistantMessageID = assistantMessageID
	// Record a fake approval_required event with a native ref.
	payload := []byte(`{"status":"approval_required","approval_id":4242,"approval_request_id":4242,"runner_permission":{"runner_id":"codex","kind":"filesystem","access":"read","target_path":"/tmp/x"},"native_request_ref":{"runner_id":"codex","kind":"approval","request_id":"7777","thread_id":"thread-1","method":"item/filechange/approval","summary":"Allow /tmp/x","issued_at":1700000000000}}`)
	if err := d.AppendRunnerChatEvent(ctx, db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		Seq:         db.NowMS(),
		TS:          db.NowMS(),
		Type:        "approval_required",
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("AppendRunnerChatEvent: %v", err)
	}
	// Insert the broker-side approval request so approve path resolves.
	insertApprovalRow(t, d, 4242, sess.AppSessionKey, "/tmp/x")

	res, err := cm.RespondToTurnApproval(ctx, turn.ID, RespondToTurnApprovalOpts{
		Decision:     "approve",
		Note:         "ok",
		AllowSession: false,
		Actor:        "test",
	})
	if err != nil {
		t.Fatalf("RespondToTurnApproval: %v", err)
	}
	if res.Route != "native" {
		t.Fatalf("expected route=native, got %s", res.Route)
	}
	if !res.NativeContinued {
		t.Fatalf("expected NativeContinued=true, got false")
	}
	if res.ApprovalID != 4242 {
		t.Fatalf("expected approval_id=4242, got %d", res.ApprovalID)
	}
	if runtime.responded.Load() != 1 {
		t.Fatalf("expected responder called once, got %d", runtime.responded.Load())
	}
	last := runtime.lastRef.Load()
	if last == nil || last.RequestID != "7777" {
		t.Fatalf("responder received wrong ref: %+v", last)
	}
	// After native continuation, the broker should still resolve the request.
	pending, _ := broker.CountApprovalRequests(ctx, approval.StatusPending, "")
	if pending != 0 {
		t.Fatalf("expected broker to have resolved the request, still pending=%d", pending)
	}
	// Turn should be flipped back to running.
	updated, err := d.GetRunnerChatTurn(ctx, turn.ID)
	if err != nil {
		t.Fatalf("GetRunnerChatTurn: %v", err)
	}
	if updated.Status != db.RunnerChatTurnStatusRunning {
		t.Fatalf("expected status=running, got %s", updated.Status)
	}
	var assistantPayloadJSON string
	if err := d.SQL.QueryRowContext(ctx, `SELECT payload_json FROM messages WHERE id=?`, assistantMessageID).Scan(&assistantPayloadJSON); err != nil {
		t.Fatalf("read resumed assistant payload: %v", err)
	}
	var assistantPayload map[string]any
	if err := json.Unmarshal([]byte(assistantPayloadJSON), &assistantPayload); err != nil {
		t.Fatalf("decode resumed assistant payload: %v", err)
	}
	if assistantPayload["status"] != db.RunnerChatTurnStatusRunning || assistantPayload["approval_state"] != nil {
		t.Fatalf("assistant payload did not become resumable: %#v", assistantPayload)
	}
}

func TestRespondToTurnApprovalDeadRuntimeFallback(t *testing.T) {
	d, cm, broker, runtime := setupChatManagerForApproval(t)
	ctx := context.Background()
	sess, turn := seedTurnWithApproval(t, d, 0)
	// Seed approval_required event with a native ref.
	payload := []byte(`{"status":"approval_required","approval_id":5151,"approval_request_id":5151,"runner_permission":{"runner_id":"codex","kind":"filesystem","access":"read","target_path":"/tmp/y"},"native_request_ref":{"runner_id":"codex","kind":"approval","request_id":"8888","thread_id":"thread-2","method":"item/filechange/approval","summary":"Allow /tmp/y","issued_at":1700000000000}}`)
	if err := d.AppendRunnerChatEvent(ctx, db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		Seq:         db.NowMS(),
		TS:          db.NowMS(),
		Type:        "approval_required",
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("AppendRunnerChatEvent: %v", err)
	}
	insertApprovalRow(t, d, 5151, sess.AppSessionKey, "/tmp/y")
	runtime.failNext.Store(true)

	res, err := cm.RespondToTurnApproval(ctx, turn.ID, RespondToTurnApprovalOpts{Decision: "approve", Actor: "test"})
	if err != nil {
		t.Fatalf("RespondToTurnApproval: %v", err)
	}
	if res.Route != "broker" {
		t.Fatalf("expected route=broker fallback, got %s", res.Route)
	}
	if !res.FallbackToToken {
		t.Fatalf("expected FallbackToToken=true")
	}
	if res.Token == "" {
		t.Fatalf("expected fallback token to be issued")
	}
	// Broker should have resolved the request.
	pending, _ := broker.CountApprovalRequests(ctx, approval.StatusPending, "")
	if pending != 0 {
		t.Fatalf("expected broker to resolve after fallback, pending=%d", pending)
	}
}

func TestRespondToTurnApprovalRejectFlow(t *testing.T) {
	d, cm, broker, runtime := setupChatManagerForApproval(t)
	ctx := context.Background()
	sess, turn := seedTurnWithApproval(t, d, 0)
	payload := []byte(`{"status":"approval_required","approval_id":6060,"approval_request_id":6060,"runner_permission":{"runner_id":"codex","kind":"filesystem","access":"read","target_path":"/tmp/z"},"native_request_ref":{"runner_id":"codex","kind":"approval","request_id":"9999","thread_id":"thread-3","method":"item/filechange/approval","summary":"Allow /tmp/z","issued_at":1700000000000}}`)
	if err := d.AppendRunnerChatEvent(ctx, db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		Seq:         db.NowMS(),
		TS:          db.NowMS(),
		Type:        "approval_required",
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("AppendRunnerChatEvent: %v", err)
	}
	insertApprovalRow(t, d, 6060, sess.AppSessionKey, "/tmp/z")
	res, err := cm.RespondToTurnApproval(ctx, turn.ID, RespondToTurnApprovalOpts{Decision: "reject", Actor: "test"})
	if err != nil {
		t.Fatalf("RespondToTurnApproval: %v", err)
	}
	if res.Route != "broker" {
		t.Fatalf("expected route=broker for reject, got %s", res.Route)
	}
	if runtime.responded.Load() != 0 {
		t.Fatalf("expected responder to NOT be called for reject, got %d", runtime.responded.Load())
	}
	pending, _ := broker.CountApprovalRequests(ctx, approval.StatusPending, "")
	if pending != 0 {
		t.Fatalf("expected broker to be denied, pending=%d", pending)
	}
	updated, _ := d.GetRunnerChatTurn(ctx, turn.ID)
	if updated.Status != db.RunnerChatTurnStatusFailed {
		t.Fatalf("expected turn to be failed after reject, got %s", updated.Status)
	}
}

func TestRespondToTurnApprovalApproveForSession(t *testing.T) {
	d, cm, _, runtime := setupChatManagerForApproval(t)
	ctx := context.Background()
	sess, turn := seedTurnWithApproval(t, d, 0)
	payload := []byte(`{"status":"approval_required","approval_id":7070,"approval_request_id":7070,"runner_permission":{"runner_id":"codex","kind":"filesystem","access":"read","target_path":"/tmp/sess"},"native_request_ref":{"runner_id":"codex","kind":"approval","request_id":"1111","thread_id":"thread-4","method":"item/filechange/approval","summary":"Allow /tmp/sess","issued_at":1700000000000}}`)
	if err := d.AppendRunnerChatEvent(ctx, db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		Seq:         db.NowMS(),
		TS:          db.NowMS(),
		Type:        "approval_required",
		PayloadJSON: string(payload),
	}); err != nil {
		t.Fatalf("AppendRunnerChatEvent: %v", err)
	}
	insertApprovalRow(t, d, 7070, sess.AppSessionKey, "/tmp/sess")
	res, err := cm.RespondToTurnApproval(ctx, turn.ID, RespondToTurnApprovalOpts{Decision: "approve", AllowSession: true, Actor: "test"})
	if err != nil {
		t.Fatalf("RespondToTurnApproval: %v", err)
	}
	if !res.AllowlistSession {
		t.Fatalf("expected AllowlistSession=true, got false")
	}
	if res.AllowlistID == 0 {
		t.Fatalf("expected allowlist id, got 0")
	}
	if runtime.responded.Load() != 1 {
		t.Fatalf("expected responder called once")
	}
	last := runtime.lastRef.Load()
	if last == nil {
		t.Fatal("expected responder to receive a ref")
	}
}
