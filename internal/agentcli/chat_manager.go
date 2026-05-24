package agentcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

// ChatManager owns runner-backed chat turn lifecycle on top of the existing
// agent CLI Manager. It builds replay prompts, persists chat session/turn/
// event rows, and mirrors normalized user/assistant messages into the shared
// `messages` table.
type ChatManager struct {
	DB      *db.DB
	Manager *Manager
	Jobs    *agent.JobRegistry
	Broker  *approval.Broker

	mu          sync.Mutex
	activeTurns map[string]turnContext // turnID -> cancel/job binding
}

type turnContext struct {
	cancel context.CancelFunc
	jobID  string
}

// StartTurnRequest is the input for ChatManager.StartTurn.
type StartTurnRequest struct {
	AppSessionKey    string
	RunnerID         string
	UserMessage      string
	PromptMessage    string
	ContinuationMode ContinuationMode
	Model            string
	Mode             string
	Isolation        string
	Cwd              string
	MaxTurns         int
	TimeoutSeconds   int
	Meta             map[string]any
	AllowedTools     []string
	RestrictTools    bool
	ApprovalToken    string
	RunnerPermission *RunnerPermissionRequest
}

type turnMirrorState struct {
	permission *runnerApprovalState
}

type runnerApprovalState struct {
	Request  RunnerPermissionRequest
	Decision approval.Decision
	Message  string
}

// StartTurnResult contains the durable identifiers for a started turn.
type StartTurnResult struct {
	Session db.RunnerChatSession
	Turn    db.RunnerChatTurn
	JobID   string
}

// EnsureSession upserts the runner_chat_sessions row for the given app
// session/runner pair.
func (cm *ChatManager) EnsureSession(ctx context.Context, req StartTurnRequest) (db.RunnerChatSession, error) {
	if cm == nil || cm.DB == nil {
		return db.RunnerChatSession{}, errors.New("chat manager not configured")
	}
	mode := string(req.ContinuationMode)
	if mode == "" {
		mode = string(cm.defaultContinuationMode(req.RunnerID))
	}
	sess, err := cm.DB.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{
		ID:               newRunnerChatID("rcs"),
		AppSessionKey:    req.AppSessionKey,
		RunnerID:         req.RunnerID,
		ContinuationMode: mode,
		Model:            req.Model,
		Mode:             req.Mode,
		Isolation:        req.Isolation,
		Cwd:              req.Cwd,
		MaxTurns:         req.MaxTurns,
	})
	return sess, err
}

func (cm *ChatManager) defaultContinuationMode(runnerID string) ContinuationMode {
	spec, adapter, err := cm.chatRunner(runnerID)
	if err != nil {
		return ContinuationReplay
	}
	caps := spec.Supports.Chat
	if !caps.ChatNativeSession || !caps.ChatResume || !caps.ChatSessionRefExtractable {
		return ContinuationReplay
	}
	if _, ok := adapter.(NativeRunnerChatAdapter); !ok {
		return ContinuationReplay
	}
	return ContinuationNative
}

// StartTurn creates a new runner_chat_turn for `sessionID`, builds the replay
// prompt, persists the user message into `messages`, enqueues the underlying
// agent CLI run, and wires event mirroring + finalization into the
// runner_chat_events / runner_chat_turns / messages tables.
//
// Native continuation mode is used for runners that advertise resumable native
// sessions; callers requesting it for other runners receive ErrUnsupportedNativeSession.
func (cm *ChatManager) StartTurn(ctx context.Context, sessionID string, req StartTurnRequest) (StartTurnResult, error) {
	if cm == nil || cm.DB == nil || cm.Manager == nil {
		return StartTurnResult{}, errors.New("chat manager not configured")
	}
	sess, err := cm.DB.GetRunnerChatSession(ctx, sessionID)
	if err != nil {
		return StartTurnResult{}, err
	}
	if req.ContinuationMode == "" {
		req.ContinuationMode = ContinuationMode(sess.ContinuationMode)
	}
	if req.ContinuationMode == "" {
		req.ContinuationMode = ContinuationReplay
	}
	runnerSpec, runnerAdapter, err := cm.chatRunner(sess.RunnerID)
	if err != nil {
		return StartTurnResult{}, err
	}
	if req.ContinuationMode == ContinuationNative {
		caps := runnerSpec.Supports.Chat
		if !caps.ChatNativeSession || !caps.ChatResume || !caps.ChatSessionRefExtractable {
			return StartTurnResult{}, ErrUnsupportedNativeSession
		}
		if _, ok := runnerAdapter.(NativeRunnerChatAdapter); !ok {
			return StartTurnResult{}, ErrUnsupportedNativeSession
		}
	}
	userMessage := strings.TrimSpace(req.UserMessage)
	if userMessage == "" {
		return StartTurnResult{}, errors.New("user_message required")
	}
	promptMessage := strings.TrimSpace(req.PromptMessage)
	if promptMessage == "" {
		promptMessage = userMessage
	}
	approvedPermission, err := cm.approvedRunnerPermission(ctx, sess, req)
	if err != nil {
		return StartTurnResult{}, err
	}

	prompt := ""
	if req.ContinuationMode != ContinuationNative {
		// Read prior turn history to build the replay prompt.
		history, err := cm.DB.ListRunnerChatTurns(ctx, sess.ID, 0)
		if err != nil {
			return StartTurnResult{}, fmt.Errorf("list turns: %w", err)
		}
		prompt = BuildReplayPrompt(toAgentcliHistory(history), promptMessage)
	}

	// Insert the new turn row (status=queued). UNIQUE partial index enforces
	// one active turn per session.
	turn := db.RunnerChatTurn{
		ID:               newRunnerChatID("rct"),
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusQueued,
		UserMessage:      userMessage,
		Model:            firstNonEmptyStr(req.Model, sess.Model),
		Mode:             firstNonEmptyStr(req.Mode, sess.Mode),
		Isolation:        firstNonEmptyStr(req.Isolation, sess.Isolation),
		Cwd:              firstNonEmptyStr(req.Cwd, sess.Cwd),
		ContinuationMode: string(req.ContinuationMode),
	}
	turn, err = cm.DB.CreateRunnerChatTurn(ctx, turn)
	if err != nil {
		return StartTurnResult{}, err
	}

	userPayload := map[string]any{
		"transport":              "runner_chat",
		"runner_id":              sess.RunnerID,
		"runner_chat_session_id": sess.ID,
		"runner_chat_turn_id":    turn.ID,
		"continuation_mode":      string(req.ContinuationMode),
	}
	userMsgID, err := cm.appendMessage(ctx, sess.AppSessionKey, "user", userMessage, userPayload)
	if err != nil {
		_ = cm.DB.FinalizeRunnerChatTurn(context.Background(), turn.ID, db.RunnerChatTurnFinalize{
			Status:       db.RunnerChatTurnStatusFailed,
			ErrorMessage: fmt.Sprintf("persist user message: %v", err),
			CompletedAt:  db.NowMS(),
		})
		return StartTurnResult{}, fmt.Errorf("persist user message: %w", err)
	}
	if err := cm.DB.SetRunnerChatTurnUserMessageID(ctx, turn.ID, userMsgID); err != nil {
		_ = cm.DB.FinalizeRunnerChatTurn(context.Background(), turn.ID, db.RunnerChatTurnFinalize{
			Status:       db.RunnerChatTurnStatusFailed,
			ErrorMessage: fmt.Sprintf("persist user message id: %v", err),
			CompletedAt:  db.NowMS(),
		})
		return StartTurnResult{}, fmt.Errorf("persist user message id: %w", err)
	}
	turn.UserMessageID = userMsgID

	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = sess.MaxTurns
	}

	// Enqueue the underlying agent CLI run.
	agentMeta := make(map[string]any, len(req.Meta)+6)
	for key, value := range req.Meta {
		agentMeta[key] = value
	}
	agentMeta["runner_chat_session_id"] = sess.ID
	agentMeta["runner_chat_turn_id"] = turn.ID
	agentMeta["runner_chat_continuation_mode"] = string(req.ContinuationMode)
	agentMeta["runner_chat_user_message"] = userMessage
	agentMeta["runner_chat_replay_prompt"] = prompt
	agentMeta["runner_chat_native_session_ref"] = sess.NativeSessionRef
	if approvedPermission != nil {
		agentMeta["runner_permission"] = runnerPermissionToMap(*approvedPermission)
	}
	if len(req.AllowedTools) > 0 {
		agentMeta["doctor_allowed_tools"] = append([]string{}, req.AllowedTools...)
	}
	if req.RestrictTools {
		agentMeta["doctor_restrict_tools"] = true
	}
	agentReq := AgentRunRequest{
		ParentSessionKey: sess.AppSessionKey,
		RunnerID:         sess.RunnerID,
		Task:             firstNonEmptyStr(prompt, promptMessage),
		Cwd:              turn.Cwd,
		Model:            turn.Model,
		Mode:             turn.Mode,
		Isolation:        turn.Isolation,
		MaxTurns:         maxTurns,
		TimeoutSeconds:   req.TimeoutSeconds,
		Meta:             agentMeta,
		AllowedTools:     append([]string{}, req.AllowedTools...),
		RestrictTools:    req.RestrictTools,
	}
	if err := ValidateDoctorAgentRunRequest(agentReq); err != nil {
		_ = cm.DB.FinalizeRunnerChatTurn(context.Background(), turn.ID, db.RunnerChatTurnFinalize{
			Status:       db.RunnerChatTurnStatusFailed,
			ErrorMessage: err.Error(),
			CompletedAt:  db.NowMS(),
		})
		return StartTurnResult{}, err
	}
	run, err := cm.Manager.Enqueue(ctx, agentReq)
	if err != nil {
		// Roll back: mark the turn failed and surface the error.
		_ = cm.DB.FinalizeRunnerChatTurn(context.Background(), turn.ID, db.RunnerChatTurnFinalize{
			Status:       db.RunnerChatTurnStatusFailed,
			ErrorMessage: err.Error(),
			CompletedAt:  db.NowMS(),
		})
		return StartTurnResult{}, err
	}
	if err := cm.DB.MarkRunnerChatTurnStarted(context.Background(), turn.ID, run.ID, run.JobID); err != nil {
		log.Printf("chat manager: mark turn started failed: turn=%s err=%v", turn.ID, err)
	}
	turn.AgentCLIRunID = run.ID
	turn.AgentCLIJobID = run.JobID
	turn.Status = db.RunnerChatTurnStatusRunning
	log.Printf("chat manager: started runner chat turn runner=%s session=%s turn=%s job=%s mode=%s isolation=%s", sess.RunnerID, sess.ID, turn.ID, run.JobID, turn.Mode, turn.Isolation)

	// Subscribe to job events to mirror them into runner_chat_events and
	// finalize the turn on terminal events.
	go cm.mirrorJobEvents(sess, turn, run.JobID)

	return StartTurnResult{Session: sess, Turn: turn, JobID: run.JobID}, nil
}

// AbortTurn cancels an in-flight turn. Best-effort: if the manager process
// restarted and lost the in-memory cancel, the row is still flipped to
// aborted directly.
func (cm *ChatManager) AbortTurn(ctx context.Context, turnID string) error {
	if cm == nil || cm.DB == nil {
		return errors.New("chat manager not configured")
	}
	turn, err := cm.DB.GetRunnerChatTurn(ctx, turnID)
	if err != nil {
		return err
	}
	switch turn.Status {
	case db.RunnerChatTurnStatusQueued, db.RunnerChatTurnStatusRunning:
	default:
		return nil
	}
	if cm.Manager != nil && turn.AgentCLIJobID != "" {
		_ = cm.Manager.Abort(ctx, turn.AgentCLIJobID)
	}
	return cm.DB.FinalizeRunnerChatTurn(ctx, turnID, db.RunnerChatTurnFinalize{
		Status:       db.RunnerChatTurnStatusAborted,
		ErrorMessage: "aborted by user",
		CompletedAt:  db.NowMS(),
	})
}

// ReconcileOnStartup marks any running/queued turns as aborted. Should be
// called once on service start, after the Manager has reconciled its own
// agent_cli_runs.
func (cm *ChatManager) ReconcileOnStartup(ctx context.Context) error {
	if cm == nil || cm.DB == nil {
		return nil
	}
	n, err := cm.DB.ReconcileRunnerChatTurnsOnStartup(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Printf("chat manager: reconciled %d in-flight turns to aborted on startup", n)
	}
	return nil
}

// ErrUnsupportedNativeSession is returned when callers request native
// continuation mode but no adapter has verified support.
var ErrUnsupportedNativeSession = errors.New("native chat session is not supported by this runner")

func (cm *ChatManager) mirrorJobEvents(sess db.RunnerChatSession, turn db.RunnerChatTurn, jobID string) {
	if cm.Jobs == nil {
		return
	}
	snapshot, ch, cancel, ok := cm.Jobs.Subscribe(jobID)
	if !ok {
		return
	}
	defer cancel()

	// Replay any events that already arrived before subscription.
	state := &turnMirrorState{}
	for _, ev := range snapshot.Events {
		cm.persistJobEvent(turn, sess, jobID, state, ev)
	}
	if isTerminalJobStatus(snapshot.Status) {
		cm.finalizeFromSnapshot(sess, turn, snapshot, state)
		return
	}

	finalSnapshot := snapshot
	for ev := range ch {
		cm.persistJobEvent(turn, sess, jobID, state, ev)
		if isTerminalEventType(ev.Type) {
			// Pull a fresh snapshot to capture final status/data.
			if s, ok := cm.Jobs.Snapshot(jobID); ok {
				finalSnapshot = s
			}
		}
	}
	if !isTerminalJobStatus(finalSnapshot.Status) {
		if s, ok := cm.Jobs.Snapshot(jobID); ok {
			finalSnapshot = s
		}
	}
	cm.finalizeFromSnapshot(sess, turn, finalSnapshot, state)
}

func (cm *ChatManager) persistJobEvent(turn db.RunnerChatTurn, sess db.RunnerChatSession, jobID string, state *turnMirrorState, ev agent.JobEvent) {
	rawEvent := jobEventToAgentRunEvent(ev, jobID, sess.RunnerID)
	cm.maybeCaptureRunnerPermission(turn, sess, jobID, state, rawEvent)
	if state != nil && state.permission != nil && shouldSuppressRunnerFailureEvent(rawEvent) {
		cm.maybePersistNativeSessionRef(sess, jobID, ev)
		return
	}
	_, runnerAdapter, err := cm.chatRunner(sess.RunnerID)
	var normalized []RunnerChatEvent
	if err == nil {
		normalized = runnerAdapter.NormalizeChatEvent(rawEvent)
	}
	if len(normalized) == 0 && err != nil {
		normalized = normalizeGenericChatEvent(rawEvent)
	}
	if len(normalized) == 0 {
		cm.maybePersistNativeSessionRef(sess, jobID, ev)
		return
	}
	for _, normalizedEvent := range normalized {
		payload := string(normalizedEvent.Payload)
		if payload == "" {
			rawPayload, _ := json.Marshal(ev.Data)
			payload = string(rawPayload)
		}
		chatEv := db.RunnerChatEvent{
			TurnID:      turn.ID,
			SessionID:   sess.ID,
			JobID:       jobID,
			Seq:         firstNonZeroInt64(normalizedEvent.Seq, ev.Sequence),
			TS:          db.NowMS(),
			Type:        normalizedEvent.Type,
			Stream:      normalizedEvent.Stream,
			Text:        normalizedEvent.Text,
			PayloadJSON: payload,
		}
		if err := cm.DB.AppendRunnerChatEvent(context.Background(), chatEv); err != nil {
			// Duplicate seq is benign during reconnect/replay; log others.
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "unique") {
				log.Printf("chat manager: append event failed: turn=%s seq=%d err=%v", turn.ID, chatEv.Seq, err)
			}
		}
	}
	cm.maybePersistNativeSessionRef(sess, jobID, ev)
}

func (cm *ChatManager) finalizeFromSnapshot(sess db.RunnerChatSession, turn db.RunnerChatTurn, snap agent.JobSnapshot, state *turnMirrorState) {
	if latest, err := cm.DB.GetRunnerChatSession(context.Background(), sess.ID); err == nil {
		sess = latest
	}
	finalText := extractFinalTextFromSnapshot(snap)
	cm.maybeCaptureCodexRunnerPermission(turn, sess, state, finalText)
	errMessage := extractErrorFromSnapshot(snap)
	status := mapJobStatusToTurnStatus(snap.Status)
	if status == "" {
		status = db.RunnerChatTurnStatusFailed
	}
	if state != nil && state.permission != nil {
		status = db.RunnerChatTurnStatusApprovalRequired
		errMessage = ""
	}

	// Append assistant message into the shared timeline, even on failure
	// (so users see a placeholder "(no output)" or the error).
	assistantContent := finalText
	if assistantContent == "" {
		if errMessage != "" {
			assistantContent = "(error) " + errMessage
		} else {
			assistantContent = "(no output)"
		}
	}
	assistantPayload := map[string]any{
		"transport":              "runner_chat",
		"runner_id":              sess.RunnerID,
		"runner_chat_session_id": sess.ID,
		"runner_chat_turn_id":    turn.ID,
		"continuation_mode":      turn.ContinuationMode,
		"status":                 status,
		"user_message":           turn.UserMessage,
	}
	if strings.TrimSpace(turn.Model) != "" {
		assistantPayload["model"] = turn.Model
	}
	if strings.TrimSpace(turn.Mode) != "" {
		assistantPayload["mode"] = turn.Mode
	}
	if strings.TrimSpace(turn.Isolation) != "" {
		assistantPayload["isolation"] = turn.Isolation
	}
	if strings.TrimSpace(turn.Cwd) != "" {
		assistantPayload["cwd"] = turn.Cwd
	}
	if strings.TrimSpace(sess.NativeSessionRef) != "" {
		assistantPayload["native_session_ref"] = sess.NativeSessionRef
	}
	if state != nil && state.permission != nil {
		assistantPayload["approval_id"] = state.permission.Decision.RequestID
		assistantPayload["approval_request_id"] = state.permission.Decision.RequestID
		assistantPayload["approval_state"] = "pending"
		assistantPayload["runner_permission"] = runnerPermissionToMap(state.permission.Request)
	}
	if errMessage != "" {
		assistantPayload["error"] = errMessage
	}
	assistantMsgID, err := cm.appendMessage(context.Background(), sess.AppSessionKey, "assistant", assistantContent, assistantPayload)
	if err != nil {
		log.Printf("chat manager: persist assistant message failed: turn=%s err=%v", turn.ID, err)
	}

	if err := cm.DB.FinalizeRunnerChatTurn(context.Background(), turn.ID, db.RunnerChatTurnFinalize{
		Status:             status,
		FinalText:          finalText,
		ErrorMessage:       errMessage,
		AssistantMessageID: assistantMsgID,
		CompletedAt:        db.NowMS(),
	}); err != nil {
		log.Printf("chat manager: finalize turn failed: turn=%s err=%v", turn.ID, err)
	}
	duration := int64(0)
	if turn.RequestedAt > 0 {
		duration = db.NowMS() - turn.RequestedAt
	}
	log.Printf("chat manager: finalized runner chat turn runner=%s session=%s turn=%s status=%s duration_ms=%d", sess.RunnerID, sess.ID, turn.ID, status, duration)

	// Update chat_session_meta with the latest preview / counts.
	cm.bumpChatSessionMeta(sess.AppSessionKey, sess.RunnerID, sess.ID, finalText)
}

func (cm *ChatManager) approvedRunnerPermission(ctx context.Context, sess db.RunnerChatSession, req StartTurnRequest) (*RunnerPermissionRequest, error) {
	if req.RunnerPermission == nil {
		return nil, nil
	}
	permission, ok := NormalizeRunnerPermissionRequest(*req.RunnerPermission)
	if !ok {
		return nil, nil
	}
	if cm == nil || cm.Broker == nil {
		return nil, fmt.Errorf("runner permission approvals unavailable")
	}
	decision, err := cm.Broker.EvaluateRunnerPermission(ctx, approval.RunnerPermissionEvaluation{
		RunnerID:       sess.RunnerID,
		PermissionKind: permission.Kind,
		Access:         permission.Access,
		TargetPath:     permission.TargetPath,
		SessionID:      sess.AppSessionKey,
		ApprovalToken:  strings.TrimSpace(req.ApprovalToken),
	})
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		if decision.RequiresApproval {
			return nil, &tools.ApprovalRequiredError{ToolName: "runner_chat", RequestID: decision.RequestID}
		}
		return nil, fmt.Errorf("runner permission blocked: %s", decision.Reason)
	}
	return &permission, nil
}

func (cm *ChatManager) maybeCaptureRunnerPermission(turn db.RunnerChatTurn, sess db.RunnerChatSession, jobID string, state *turnMirrorState, raw AgentRunEvent) {
	if state == nil || state.permission != nil {
		return
	}
	var permission RunnerPermissionRequest
	var ok bool
	switch RunnerID(sess.RunnerID) {
	case RunnerOpenCode:
		permission, ok = detectOpenCodePermissionRequest(raw)
	case RunnerCodex:
		permission, ok = detectCodexStructuredPermissionRequest(raw)
	default:
		return
	}
	if !ok {
		return
	}
	cm.appendRunnerApprovalRequired(turn, sess, jobID, state, permission)
}

func (cm *ChatManager) maybeCaptureCodexRunnerPermission(turn db.RunnerChatTurn, sess db.RunnerChatSession, state *turnMirrorState, finalText string) {
	if state == nil || state.permission != nil || RunnerID(sess.RunnerID) != RunnerCodex {
		return
	}
	permission, ok := detectCodexPermissionRequest(finalText)
	if !ok {
		return
	}
	cm.appendRunnerApprovalRequired(turn, sess, turn.AgentCLIJobID, state, permission)
}

func (cm *ChatManager) appendRunnerApprovalRequired(turn db.RunnerChatTurn, sess db.RunnerChatSession, jobID string, state *turnMirrorState, permission RunnerPermissionRequest) {
	if cm == nil || cm.Broker == nil || state == nil {
		return
	}
	decision, err := cm.Broker.EvaluateRunnerPermission(context.Background(), approval.RunnerPermissionEvaluation{
		RunnerID:       sess.RunnerID,
		PermissionKind: permission.Kind,
		Access:         permission.Access,
		TargetPath:     permission.TargetPath,
		SessionID:      sess.AppSessionKey,
	})
	if err != nil || !decision.RequiresApproval || decision.RequestID == 0 {
		return
	}
	state.permission = &runnerApprovalState{
		Request:  permission,
		Decision: decision,
		Message:  runnerPermissionApprovalMessage(permission),
	}
	payload, _ := json.Marshal(map[string]any{
		"status":              "approval_required",
		"code":                "approval_required",
		"approval_id":         decision.RequestID,
		"approval_request_id": decision.RequestID,
		"approval_state":      "pending",
		"message":             state.permission.Message,
		"runner_permission":   runnerPermissionToMap(permission),
	})
	if err := cm.DB.AppendRunnerChatEvent(context.Background(), db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		JobID:       jobID,
		Seq:         db.NowMS(),
		TS:          db.NowMS(),
		Type:        "approval_required",
		PayloadJSON: string(payload),
	}); err != nil {
		log.Printf("chat manager: append approval event failed: turn=%s err=%v", turn.ID, err)
	}
}

func runnerPermissionApprovalMessage(permission RunnerPermissionRequest) string {
	action := permission.Access
	if action == "" {
		action = runnerPermissionAccessRead
	}
	runner := strings.TrimSpace(permission.RunnerID)
	if runner == "" {
		runner = "runner"
	}
	target := strings.TrimSpace(permission.TargetPath)
	if target == "" {
		return "Approval is needed before the runner can continue."
	}
	return fmt.Sprintf("Approval is needed to let %s %s %s.", runner, action, target)
}

func shouldSuppressRunnerFailureEvent(raw AgentRunEvent) bool {
	if raw.Type == "completion" && raw.Status == db.AgentCLIStatusFailed {
		return true
	}
	return raw.Type == "error"
}

func (cm *ChatManager) bumpChatSessionMeta(appSessionKey, runnerID, runnerChatSessionID, lastFinalText string) {
	if cm.DB == nil {
		return
	}
	preview := previewSnippetClamped(lastFinalText, 160)
	count, _ := cm.countMessages(appSessionKey)
	now := db.NowMS()
	_, err := cm.DB.UpsertChatSessionMeta(context.Background(), db.ChatSessionMeta{
		SessionKey:          appSessionKey,
		RunnerID:            runnerID,
		RunnerChatSessionID: runnerChatSessionID,
		MessageCount:        count,
		LastMessagePreview:  preview,
		LastMessageAt:       now,
	})
	if err != nil {
		log.Printf("chat manager: upsert chat_session_meta failed: session=%s err=%v", appSessionKey, err)
	}
}

func (cm *ChatManager) countMessages(sessionKey string) (int64, error) {
	if cm.DB == nil {
		return 0, nil
	}
	var n int64
	err := cm.DB.SQL.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM messages WHERE session_key=?`, sessionKey).Scan(&n)
	return n, err
}

// appendMessage writes into the shared `messages` table and returns the new id.
func (cm *ChatManager) appendMessage(ctx context.Context, sessionKey, role, content string, payload map[string]any) (int64, error) {
	tx, err := cm.DB.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	now := db.NowMS()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		sessionKey, now, now); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	if err := tx.Commit(); err != nil {
		return id, err
	}
	return id, nil
}

func newRunnerChatID(prefix string) string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}

func toAgentcliHistory(turns []db.RunnerChatTurn) []RunnerChatTurn {
	out := make([]RunnerChatTurn, 0, len(turns))
	for _, t := range turns {
		out = append(out, RunnerChatTurn{
			ID:          t.ID,
			Sequence:    t.Sequence,
			UserText:    t.UserMessage,
			FinalText:   t.FinalText,
			Status:      t.Status,
			RequestedAt: t.RequestedAt,
			CompletedAt: t.CompletedAt,
		})
	}
	return out
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func (cm *ChatManager) chatRunner(runnerID string) (RunnerSpec, RunnerChatAdapter, error) {
	if cm == nil || cm.Manager == nil || cm.Manager.Registry == nil {
		return RunnerSpec{}, nil, errors.New("runner registry unavailable")
	}
	spec, ok := cm.Manager.Registry.Spec(RunnerID(runnerID))
	if !ok {
		return RunnerSpec{}, nil, fmt.Errorf("unknown runner %q", runnerID)
	}
	adapter, ok := cm.Manager.Registry.Adapter(RunnerID(runnerID))
	if !ok {
		return RunnerSpec{}, nil, fmt.Errorf("no adapter for runner %q", runnerID)
	}
	chatAdapter, ok := adapter.(RunnerChatAdapter)
	if !ok {
		return RunnerSpec{}, nil, fmt.Errorf("runner %q does not support chat transport", runnerID)
	}
	return spec, chatAdapter, nil
}

func (cm *ChatManager) maybePersistNativeSessionRef(sess db.RunnerChatSession, jobID string, ev agent.JobEvent) {
	if sess.NativeSessionRef != "" {
		return
	}
	_, runnerAdapter, err := cm.chatRunner(sess.RunnerID)
	if err != nil {
		return
	}
	nativeAdapter, ok := runnerAdapter.(NativeRunnerChatAdapter)
	if !ok {
		return
	}
	rawEvent := jobEventToAgentRunEvent(ev, jobID, sess.RunnerID)
	ref, ok := nativeAdapter.ExtractNativeSessionRef(rawEvent)
	if !ok || strings.TrimSpace(ref) == "" {
		return
	}
	if err := cm.DB.UpdateRunnerChatSessionNativeRef(context.Background(), sess.ID, ref); err != nil {
		log.Printf("chat manager: persist native session ref failed: session=%s ref=%s err=%v", sess.ID, ref, err)
		return
	}
	log.Printf("chat manager: persisted native session ref runner=%s session=%s native_ref=%s", sess.RunnerID, sess.ID, ref)
}

func jobEventToAgentRunEvent(ev agent.JobEvent, jobID, runnerID string) AgentRunEvent {
	raw := AgentRunEvent{
		Type:     ev.Type,
		Seq:      ev.Sequence,
		JobID:    jobID,
		RunnerID: runnerID,
	}
	if ev.Data == nil {
		return raw
	}
	if stream, ok := ev.Data["stream"].(string); ok {
		raw.Stream = stream
	}
	if chunk, ok := ev.Data["chunk"].(string); ok {
		raw.Chunk = chunk
	}
	if status, ok := ev.Data["status"].(string); ok {
		raw.Status = status
	}
	if message, ok := ev.Data["message"].(string); ok {
		raw.Message = message
	}
	if duration, ok := ev.Data["duration_ms"].(float64); ok {
		raw.DurationMS = int64(duration)
	}
	if payload, ok := ev.Data["payload"]; ok {
		if b, err := json.Marshal(payload); err == nil {
			raw.Payload = b
		}
	} else if payloadJSON, ok := ev.Data["payload_json"].(string); ok {
		raw.Payload = json.RawMessage(payloadJSON)
	}
	return raw
}

func mapJobStatusToTurnStatus(status string) string {
	switch status {
	case "completed", "succeeded":
		return db.RunnerChatTurnStatusSucceeded
	case "approval_required":
		return db.RunnerChatTurnStatusApprovalRequired
	case "failed":
		return db.RunnerChatTurnStatusFailed
	case "aborted":
		return db.RunnerChatTurnStatusAborted
	case "timed_out":
		return db.RunnerChatTurnStatusTimedOut
	}
	return ""
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case "completed", "succeeded", "failed", "aborted", "timed_out":
		return true
	case "approval_required":
		return true
	}
	return false
}

func isTerminalEventType(t string) bool {
	switch t {
	case "completion", "completed", "failed", "aborted", "timed_out", "error":
		return true
	}
	return false
}

func extractFinalTextFromSnapshot(snap agent.JobSnapshot) string {
	// Walk events in reverse and return the first completion/final_text we find.
	for i := len(snap.Events) - 1; i >= 0; i-- {
		ev := snap.Events[i]
		if ev.Data == nil {
			continue
		}
		if v, ok := ev.Data["final_text"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
		if v, ok := ev.Data["final_text_preview"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func extractErrorFromSnapshot(snap agent.JobSnapshot) string {
	if snap.Status != "failed" && snap.Status != "timed_out" {
		return ""
	}
	for i := len(snap.Events) - 1; i >= 0; i-- {
		ev := snap.Events[i]
		if ev.Data == nil {
			continue
		}
		if v, ok := ev.Data["error"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
		if v, ok := ev.Data["message"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
		if v, ok := ev.Data["stderr_preview"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if snap.Status == "timed_out" {
		return "timed out"
	}
	return ""
}

func previewSnippetClamped(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Avoid unused linter warnings for time import on certain build configs.
var _ = time.Now
