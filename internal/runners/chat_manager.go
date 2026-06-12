package runners

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

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
	"or3-intern/internal/tools"
	"or3-intern/internal/turns"
)

// ChatManager owns runner-backed chat turn lifecycle on top of the existing
// agent CLI Manager. It builds replay prompts, persists chat session/turn/
// event rows, and mirrors normalized user/assistant messages into the shared
// `messages` table.
type ChatManager struct {
	DB               *db.DB
	Manager          *Manager
	Jobs             *jobs.Registry
	Broker           *approval.Broker
	OnSuccessfulTurn func(sessionKey string)

	mu          sync.Mutex
	activeTurns map[string]turnContext // turnID -> cancel/job binding
}

type turnContext struct {
	cancel context.CancelFunc
	jobID  string
}

// StartTurnRequest is the input for ChatManager.StartTurn.
type StartTurnRequest struct {
	AppSessionKey      string
	RunnerID           string
	UserMessage        string
	Attachments        []turns.Attachment
	PromptMessage      string
	PromptMessageFinal bool
	ContinuationMode   ContinuationMode
	Model              string
	Mode               string
	Isolation          string
	Cwd                string
	MaxTurns           int
	TimeoutSeconds     int
	Meta               map[string]any
	ApprovalToken      string
	RunnerPermission   *RunnerPermissionRequest
}

type turnMirrorState struct {
	permission *runnerApprovalState
}

type runnerApprovalState struct {
	Request       RunnerPermissionRequest
	Decision      approval.Decision
	Message       string
	NativeRequest NativeRequestRef
	HasNative     bool
}

const runnerChatMessageEventPayloadLimit = 300

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
		if req.PromptMessageFinal {
			prompt = promptMessage
		} else {
			// Read prior turn history to build the replay prompt.
			history, err := cm.DB.ListRunnerChatTurns(ctx, sess.ID, 0)
			if err != nil {
				return StartTurnResult{}, fmt.Errorf("list turns: %w", err)
			}
			prompt = BuildReplayPrompt(toAgentcliHistory(history), promptMessage)
		}
	}

	// Insert the new turn row (status=queued). UNIQUE partial index enforces
	// one active turn per session.
	model := firstNonEmptyStr(req.Model, sess.Model)
	if RunnerID(sess.RunnerID) == RunnerOpenCode && strings.TrimSpace(model) != "" && cm.Manager != nil {
		cfg := cm.Manager.configSnapshot()
		model = NormalizeOpenCodeModelID(ctx, cfg, nativeEnv(cfg), model)
	}
	turn := db.RunnerChatTurn{
		ID:               newRunnerChatID("rct"),
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusQueued,
		UserMessage:      userMessage,
		Model:            model,
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
	if len(req.Attachments) > 0 {
		userPayload["attachments"] = turns.AttachmentsForMeta(req.Attachments)
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
	nativeSessionRef := ""
	if req.ContinuationMode == ContinuationNative {
		nativeSessionRef = sess.NativeSessionRef
	}
	agentMeta["runner_chat_native_session_ref"] = nativeSessionRef
	if approvedPermission != nil {
		agentMeta["runner_permission"] = runnerPermissionToMap(*approvedPermission)
	}
	agentReq := RunnerRunRequest{
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
	turn.RunnerRunID = run.ID
	turn.RunnerJobID = run.JobID
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
	if cm.Manager != nil && turn.RunnerJobID != "" {
		_ = cm.Manager.Abort(ctx, turn.RunnerJobID)
	}
	return cm.DB.FinalizeRunnerChatTurn(ctx, turnID, db.RunnerChatTurnFinalize{
		Status:       db.RunnerChatTurnStatusAborted,
		ErrorMessage: "aborted by user",
		CompletedAt:  db.NowMS(),
	})
}

// RespondToTurnApprovalOpts captures the user decision for an outstanding
// approval attached to a runner chat turn.
type RespondToTurnApprovalOpts struct {
	Decision     string // approve | reject | cancel
	Note         string
	AllowSession bool
	Actor        string
}

// RespondToTurnApprovalResult summarises the path the chat manager took to
// resolve a pending approval. The app uses the route to render feedback
// ("runner resumed inline" vs "approval token issued").
type RespondToTurnApprovalResult struct {
	Route            string
	ApprovalID       int64
	NativeContinued  bool
	FallbackToToken  bool
	Token            string
	AllowlistID      int64
	AllowlistSession bool
}

// RespondToTurnApproval drives a pending approval attached to a turn. It
// first attempts the live continuation path via the native runtime's
// NativeRequestResponder. When the runtime is missing, dead, or refuses the
// decision, it falls back to the approval-token retry flow.
func (cm *ChatManager) RespondToTurnApproval(ctx context.Context, turnID string, opts RespondToTurnApprovalOpts) (RespondToTurnApprovalResult, error) {
	if cm == nil || cm.DB == nil {
		return RespondToTurnApprovalResult{}, errors.New("chat manager not configured")
	}
	turn, err := cm.DB.GetRunnerChatTurn(ctx, turnID)
	if err != nil {
		return RespondToTurnApprovalResult{}, err
	}
	if turn.Status != db.RunnerChatTurnStatusApprovalRequired {
		return RespondToTurnApprovalResult{}, fmt.Errorf("turn %s is not waiting for approval (status=%s)", turnID, turn.Status)
	}
	sess, err := cm.DB.GetRunnerChatSession(ctx, turn.SessionID)
	if err != nil {
		return RespondToTurnApprovalResult{}, err
	}

	// Refresh the session so we observe the latest native_session_ref and
	// continuation state.
	if latest, err := cm.DB.GetRunnerChatSession(ctx, sess.ID); err == nil {
		sess = latest
	}

	approvalID, ref, hasRef := cm.lastApprovalRef(ctx, turn)
	if approvalID == 0 {
		return RespondToTurnApprovalResult{}, errors.New("no approval is attached to this turn")
	}

	decision := strings.ToLower(strings.TrimSpace(opts.Decision))
	if decision == "" {
		decision = "approve"
	}
	actor := firstNonEmptyStr(opts.Actor, "app:runner-chat")
	note := strings.TrimSpace(opts.Note)

	// Deny/cancel paths short-circuit the live responder.
	if decision == "reject" || decision == "deny" || decision == "cancel" {
		if cm.Broker == nil {
			return RespondToTurnApprovalResult{}, errors.New("approval broker unavailable")
		}
		if decision == "cancel" {
			if err := cm.Broker.CancelRequest(ctx, approvalID, actor, note); err != nil {
				return RespondToTurnApprovalResult{}, err
			}
		} else {
			if err := cm.Broker.DenyRequest(ctx, approvalID, actor, note); err != nil {
				return RespondToTurnApprovalResult{}, err
			}
		}
		cm.appendApprovalResponseEvent(ctx, turn, sess, decision, RespondToTurnApprovalResult{Route: "broker", ApprovalID: approvalID, NativeContinued: false})
		_ = cm.DB.FinalizeRunnerChatTurn(ctx, turn.ID, db.RunnerChatTurnFinalize{
			Status:       mapJobStatusToTurnStatus("failed"),
			ErrorMessage: "approval " + decision,
			CompletedAt:  db.NowMS(),
		})
		return RespondToTurnApprovalResult{Route: "broker", ApprovalID: approvalID}, nil
	}

	// Approval path: authorize with the broker first, then try the live
	// responder. If the responder is gone, the issued token remains the
	// fallback for the next submitted turn.
	responder, ok := cm.lookupNativeResponder(sess.RunnerID)
	issued, err := cm.Broker.ApproveRequest(ctx, approvalID, actor, opts.AllowSession, note)
	if err != nil {
		return RespondToTurnApprovalResult{}, err
	}
	if ok && hasRef {
		respErr := responder.RespondToNativeRequest(ctx, ref, NativeRequestDecision{
			Decision:    "approve",
			Message:     note,
			AlwaysAllow: opts.AllowSession,
		})
		if respErr == nil {
			cm.appendApprovalResponseEvent(ctx, turn, sess, "approve", RespondToTurnApprovalResult{Route: "native", ApprovalID: approvalID, NativeContinued: true, Token: issued.Token, AllowlistID: issued.AllowlistID, AllowlistSession: opts.AllowSession})
			_ = cm.DB.MarkRunnerChatTurnApprovalResumed(ctx, turn.ID, db.NowMS())
			cm.resumeTurnAfterNativeApproval(sess, turn)
			return RespondToTurnApprovalResult{Route: "native", ApprovalID: approvalID, NativeContinued: true, Token: issued.Token, AllowlistID: issued.AllowlistID, AllowlistSession: opts.AllowSession}, nil
		}
		log.Printf("chat manager: native responder failed turn=%s runner=%s ref=%s err=%v; falling back to approval token", turn.ID, sess.RunnerID, ref.RequestID, respErr)
	}

	// Fallback: issue an approval token. The next turn submission carries
	// it via ApprovalToken and the broker will accept it inline.
	cm.appendApprovalResponseEvent(ctx, turn, sess, "approve", RespondToTurnApprovalResult{Route: "broker", ApprovalID: approvalID, FallbackToToken: true, Token: issued.Token, AllowlistID: issued.AllowlistID, AllowlistSession: opts.AllowSession})
	_ = cm.DB.MarkRunnerChatTurnApprovalResumed(ctx, turn.ID, db.NowMS())
	return RespondToTurnApprovalResult{Route: "broker", ApprovalID: approvalID, FallbackToToken: true, Token: issued.Token, AllowlistID: issued.AllowlistID, AllowlistSession: opts.AllowSession}, nil
}

// lastApprovalRef walks the persisted chat events for a turn and returns
// the most recent approval id plus the native request ref attached to it.
func (cm *ChatManager) lastApprovalRef(ctx context.Context, turn db.RunnerChatTurn) (int64, NativeRequestRef, bool) {
	events, err := cm.DB.ListRunnerChatEvents(ctx, turn.ID, 0, 1000)
	if err != nil {
		return 0, NativeRequestRef{}, false
	}
	var approvalID int64
	var ref NativeRequestRef
	var hasRef bool
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type != "approval_required" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			continue
		}
		if raw, ok := payload["approval_id"].(float64); ok && approvalID == 0 {
			approvalID = int64(raw)
		}
		if raw, ok := payload["approval_request_id"].(float64); ok && approvalID == 0 {
			approvalID = int64(raw)
		}
		if !hasRef {
			if raw, ok := payload["native_request_ref"].(map[string]any); ok {
				ref = nativeRequestRefFromMap(raw)
				if ref.RequestID != "" {
					hasRef = true
				}
			}
		}
		if approvalID != 0 {
			return approvalID, ref, hasRef
		}
	}
	return approvalID, ref, hasRef
}

func nativeRequestRefFromMap(raw map[string]any) NativeRequestRef {
	ref := NativeRequestRef{
		RunnerID:  RunnerID(asString(raw["runner_id"])),
		Kind:      NativeRequestKind(asString(raw["kind"])),
		RequestID: asString(raw["request_id"]),
		SessionID: asString(raw["session_id"]),
		ThreadID:  asString(raw["thread_id"]),
		Method:    asString(raw["method"]),
		Summary:   asString(raw["summary"]),
	}
	if v, ok := raw["issued_at"].(float64); ok {
		ref.IssuedAt = int64(v)
	}
	return ref
}

// lookupNativeResponder returns the registered native runtime as a
// NativeRequestResponder if the runtime exposes that interface.
func (cm *ChatManager) lookupNativeResponder(runnerID string) (NativeRequestResponder, bool) {
	if cm == nil || cm.Manager == nil || cm.Manager.Runtimes == nil {
		return nil, false
	}
	runtime, ok := cm.Manager.Runtimes.Get(RunnerID(runnerID))
	if !ok || runtime == nil {
		return nil, false
	}
	responder, ok := runtime.(NativeRequestResponder)
	return responder, ok
}

func (cm *ChatManager) resumeTurnAfterNativeApproval(sess db.RunnerChatSession, turn db.RunnerChatTurn) {
	if cm == nil || cm.Manager == nil || cm.DB == nil {
		return
	}
	if strings.TrimSpace(turn.RunnerRunID) == "" || strings.TrimSpace(turn.RunnerJobID) == "" {
		return
	}
	go func() {
		ctx := context.Background()
		run, ok, err := cm.DB.GetRunnerRun(ctx, turn.RunnerRunID)
		if err != nil || !ok {
			log.Printf("chat manager: resume after approval load run failed: turn=%s err=%v ok=%v", turn.ID, err, ok)
			return
		}
		if run.Status != db.RunnerRunStatusRunning {
			log.Printf("chat manager: resume after approval skipped: turn=%s run_status=%s", turn.ID, run.Status)
			return
		}
		if cm.Jobs != nil {
			cm.Jobs.Reopen(turn.RunnerJobID)
			cm.Jobs.Publish(turn.RunnerJobID, "resumed", map[string]any{
				"status":                 db.RunnerRunStatusRunning,
				"runner_id":              sess.RunnerID,
				"runner_chat_turn_id":    turn.ID,
				"runner_chat_session_id": sess.ID,
				"resumed_after_approval": true,
			})
		}
		latestTurn, err := cm.DB.GetRunnerChatTurn(ctx, turn.ID)
		if err != nil {
			log.Printf("chat manager: resume after approval load turn failed: turn=%s err=%v", turn.ID, err)
			return
		}
		latestSess, err := cm.DB.GetRunnerChatSession(ctx, sess.ID)
		if err != nil {
			log.Printf("chat manager: resume after approval load session failed: turn=%s err=%v", turn.ID, err)
			return
		}
		go cm.mirrorJobEvents(latestSess, latestTurn, turn.RunnerJobID)
		if err := cm.Manager.ResumeNativeRunAfterApproval(ctx, run); err != nil {
			log.Printf("chat manager: resume after approval failed: turn=%s err=%v", turn.ID, err)
		}
	}()
}

func (cm *ChatManager) appendApprovalResponseEvent(ctx context.Context, turn db.RunnerChatTurn, sess db.RunnerChatSession, decision string, res RespondToTurnApprovalResult) {
	payload, _ := json.Marshal(map[string]any{
		"status":                 "approval_response",
		"code":                   "approval_response",
		"decision":               decision,
		"approval_id":            res.ApprovalID,
		"route":                  res.Route,
		"native_continued":       res.NativeContinued,
		"fallback_to_token":      res.FallbackToToken,
		"allowlist_session":      res.AllowlistSession,
		"runner_id":              sess.RunnerID,
		"runner_chat_session_id": sess.ID,
		"runner_chat_turn_id":    turn.ID,
	})
	seq := db.NowMS()
	if maxSeq, err := cm.DB.MaxRunnerChatEventSeq(ctx, turn.ID); err == nil && seq <= maxSeq {
		seq = maxSeq + 1
	}
	if err := cm.DB.AppendRunnerChatEvent(ctx, db.RunnerChatEvent{
		TurnID:      turn.ID,
		SessionID:   sess.ID,
		JobID:       turn.RunnerJobID,
		Seq:         seq,
		TS:          db.NowMS(),
		Type:        "approval_response",
		PayloadJSON: string(payload),
	}); err != nil {
		log.Printf("chat manager: append approval response event failed: turn=%s err=%v", turn.ID, err)
	}
}

// ReconcileOnStartup marks any running/queued turns as aborted. Should be
// called once on service start, after the Manager has reconciled its own
// runner_runs.
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

func (cm *ChatManager) persistJobEvent(turn db.RunnerChatTurn, sess db.RunnerChatSession, jobID string, state *turnMirrorState, ev jobs.Event) {
	rawEvent := jobEventToRunnerRunEvent(ev, jobID, sess.RunnerID)
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

func (cm *ChatManager) finalizeFromSnapshot(sess db.RunnerChatSession, turn db.RunnerChatTurn, snap jobs.Snapshot, state *turnMirrorState) {
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
	if events := cm.runnerChatEventsPayload(turn.ID); len(events) > 0 {
		assistantPayload["runner_chat_events"] = events
	}
	assistantMsgID := turn.AssistantMessageID
	if status == db.RunnerChatTurnStatusApprovalRequired || assistantMsgID == 0 {
		var err error
		assistantMsgID, err = cm.appendMessage(context.Background(), sess.AppSessionKey, "assistant", assistantContent, assistantPayload)
		if err != nil {
			log.Printf("chat manager: persist assistant message failed: turn=%s err=%v", turn.ID, err)
		}
	} else {
		if err := cm.updateAssistantMessage(context.Background(), assistantMsgID, assistantContent, assistantPayload); err != nil {
			log.Printf("chat manager: update assistant message failed: turn=%s err=%v", turn.ID, err)
		}
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
	if status == db.RunnerChatTurnStatusSucceeded && cm.OnSuccessfulTurn != nil {
		cm.OnSuccessfulTurn(sess.AppSessionKey)
	}

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

func (cm *ChatManager) maybeCaptureRunnerPermission(turn db.RunnerChatTurn, sess db.RunnerChatSession, jobID string, state *turnMirrorState, raw RunnerRunEvent) {
	if state == nil || state.permission != nil {
		return
	}
	var permission RunnerPermissionRequest
	var ok bool
	var nativeRef NativeRequestRef
	var hasNative bool
	switch RunnerID(sess.RunnerID) {
	case RunnerOpenCode:
		permission, ok = detectOpenCodePermissionRequest(raw)
		if !ok {
			// Fallback: detect a structured permission request ref even when
			// the stderr heuristic does not match. This ensures the chat
			// manager can drive the live continuation flow whenever the
			// runner emits a structured event with an id.
			if ref, ok2 := detectOpenCodePermissionRequestRef(raw, sess.NativeSessionRef); ok2 {
				nativeRef = ref
				hasNative = true
			}
		}
	case RunnerCodex:
		permission, ok = detectCodexStructuredPermissionRequest(raw)
		if !ok {
			if ref, ok2 := detectCodexPermissionRequestRef(raw); ok2 {
				nativeRef = ref
				hasNative = true
			}
		}
	default:
		return
	}
	if !ok && !hasNative {
		return
	}
	if !ok {
		// Synthesise a minimal permission request from the native ref so the
		// approval pipeline stays consistent.
		permission = RunnerPermissionRequest{
			RunnerID:   string(nativeRef.RunnerID),
			Kind:       runnerPermissionKindFilesystem,
			Access:     runnerPermissionAccessRead,
			TargetPath: nativeRef.Summary,
		}
		if normalized, ok2 := NormalizeRunnerPermissionRequest(permission); ok2 {
			permission = normalized
		} else {
			permission.TargetPath = "(native request)"
			permission, _ = NormalizeRunnerPermissionRequest(permission)
		}
	}
	cm.appendRunnerApprovalRequired(turn, sess, jobID, state, permission, nativeRef, hasNative)
}

func (cm *ChatManager) maybeCaptureCodexRunnerPermission(turn db.RunnerChatTurn, sess db.RunnerChatSession, state *turnMirrorState, finalText string) {
	if state == nil || state.permission != nil || RunnerID(sess.RunnerID) != RunnerCodex {
		return
	}
	permission, ok := detectCodexPermissionRequest(finalText)
	if !ok {
		return
	}
	cm.appendRunnerApprovalRequired(turn, sess, turn.RunnerJobID, state, permission, NativeRequestRef{}, false)
}

func (cm *ChatManager) appendRunnerApprovalRequired(turn db.RunnerChatTurn, sess db.RunnerChatSession, jobID string, state *turnMirrorState, permission RunnerPermissionRequest, nativeRef NativeRequestRef, hasNative bool) {
	if cm == nil || state == nil {
		return
	}
	if cm.Broker == nil {
		return
	}
	decision, err := cm.Broker.EvaluateRunnerPermission(context.Background(), approval.RunnerPermissionEvaluation{
		RunnerID:       sess.RunnerID,
		PermissionKind: permission.Kind,
		Access:         permission.Access,
		TargetPath:     permission.TargetPath,
		SessionID:      sess.AppSessionKey,
	})
	if err != nil {
		log.Printf("chat manager: runner permission evaluation failed: turn=%s err=%v", turn.ID, err)
		return
	}
	// If the broker has nothing to ask for, the runner can continue inline;
	// skip emitting the approval_required event so the app doesn't render a
	// dead approval card. We still record the native request for replay.
	if !decision.RequiresApproval || decision.RequestID == 0 {
		if hasNative {
			state.permission = &runnerApprovalState{
				Request:       permission,
				Decision:      decision,
				Message:       runnerPermissionApprovalMessage(permission),
				NativeRequest: nativeRef,
				HasNative:     true,
			}
		}
		return
	}
	state.permission = &runnerApprovalState{
		Request:       permission,
		Decision:      decision,
		Message:       runnerPermissionApprovalMessage(permission),
		NativeRequest: nativeRef,
		HasNative:     hasNative,
	}
	payload, _ := json.Marshal(map[string]any{
		"status":              "approval_required",
		"code":                "approval_required",
		"approval_id":         decision.RequestID,
		"approval_request_id": decision.RequestID,
		"approval_state":      "pending",
		"message":             state.permission.Message,
		"runner_permission":   runnerPermissionToMap(permission),
		"native_request_ref":  nativeRequestRefToMap(nativeRef, hasNative),
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

func nativeRequestRefToMap(ref NativeRequestRef, hasNative bool) map[string]any {
	if !hasNative {
		return nil
	}
	out := map[string]any{
		"runner_id":  string(ref.RunnerID),
		"kind":       string(ref.Kind),
		"request_id": ref.RequestID,
	}
	if ref.SessionID != "" {
		out["session_id"] = ref.SessionID
	}
	if ref.ThreadID != "" {
		out["thread_id"] = ref.ThreadID
	}
	if ref.Method != "" {
		out["method"] = ref.Method
	}
	if ref.Summary != "" {
		out["summary"] = ref.Summary
	}
	if ref.IssuedAt > 0 {
		out["issued_at"] = ref.IssuedAt
	}
	return out
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

func shouldSuppressRunnerFailureEvent(raw RunnerRunEvent) bool {
	if raw.Type == "completion" && (raw.Status == db.RunnerRunStatusFailed || raw.Status == db.RunnerRunStatusApprovalRequired) {
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

func (cm *ChatManager) runnerChatEventsPayload(turnID string) []map[string]any {
	if cm == nil || cm.DB == nil || strings.TrimSpace(turnID) == "" {
		return nil
	}
	events, err := cm.DB.ListRunnerChatEvents(context.Background(), turnID, 0, runnerChatMessageEventPayloadLimit)
	if err != nil {
		log.Printf("chat manager: list events for assistant payload failed: turn=%s err=%v", turnID, err)
		return nil
	}
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		item := map[string]any{
			"type": ev.Type,
			"seq":  ev.Seq,
		}
		if strings.TrimSpace(ev.JobID) != "" {
			item["job_id"] = ev.JobID
		}
		if strings.TrimSpace(ev.Stream) != "" {
			item["stream"] = ev.Stream
		}
		if ev.Text != "" {
			item["text"] = ev.Text
		}
		if strings.TrimSpace(ev.PayloadJSON) != "" {
			var payload any
			if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err == nil {
				item["payload"] = payload
			} else {
				item["payload_json"] = ev.PayloadJSON
			}
		}
		out = append(out, item)
	}
	return out
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

func (cm *ChatManager) updateAssistantMessage(ctx context.Context, messageID int64, content string, payload map[string]any) error {
	if cm == nil || cm.DB == nil || messageID <= 0 {
		return errors.New("chat manager not configured")
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = cm.DB.SQL.ExecContext(ctx,
		`UPDATE messages SET content=?, payload_json=? WHERE id=?`,
		content, string(payloadJSON), messageID)
	return err
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

func (cm *ChatManager) maybePersistNativeSessionRef(sess db.RunnerChatSession, jobID string, ev jobs.Event) {
	if ContinuationMode(strings.TrimSpace(sess.ContinuationMode)) != ContinuationNative {
		return
	}
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
	rawEvent := jobEventToRunnerRunEvent(ev, jobID, sess.RunnerID)
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

func jobEventToRunnerRunEvent(ev jobs.Event, jobID, runnerID string) RunnerRunEvent {
	raw := RunnerRunEvent{
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

func extractFinalTextFromSnapshot(snap jobs.Snapshot) string {
	// Walk events in reverse and return the first completion/final_text we find.
	for i := len(snap.Events) - 1; i >= 0; i-- {
		ev := snap.Events[i]
		if ev.Data == nil {
			continue
		}
		if v, ok := ev.Data["final_text"].(string); ok && strings.TrimSpace(v) != "" {
			if runnerErrorEnvelopeMessage(v) != "" {
				continue
			}
			if text := runnerReadableFinalText(v); text != "" {
				return text
			}
			if looksStructuredRunnerPayload(v) {
				continue
			}
			return v
		}
		if v, ok := ev.Data["final_text_preview"].(string); ok && strings.TrimSpace(v) != "" {
			if runnerErrorEnvelopeMessage(v) != "" {
				continue
			}
			if text := runnerReadableFinalText(v); text != "" {
				return text
			}
			if looksStructuredRunnerPayload(v) {
				continue
			}
			return v
		}
	}
	return ""
}

func extractErrorFromSnapshot(snap jobs.Snapshot) string {
	if snap.Status != "failed" && snap.Status != "timed_out" {
		return ""
	}
	for i := len(snap.Events) - 1; i >= 0; i-- {
		ev := snap.Events[i]
		if ev.Data == nil {
			continue
		}
		if v, ok := ev.Data["final_text"].(string); ok {
			if msg := runnerErrorEnvelopeMessage(v); msg != "" {
				return msg
			}
		}
		if v, ok := ev.Data["final_text_preview"].(string); ok {
			if msg := runnerErrorEnvelopeMessage(v); msg != "" {
				return msg
			}
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

func runnerErrorEnvelopeMessage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return ""
	}
	for _, payload := range structuredPayloadCandidates(text) {
		if msg := runnerErrorEnvelopeMessageValue(payload); msg != "" {
			return msg
		}
	}
	return ""
}

func runnerErrorEnvelopeMessageValue(value any) string {
	obj, ok := value.(map[string]any)
	if !ok {
		return extractOpenCodeErrorMessage(value)
	}
	if msg := extractString(obj["error_message"]); msg != "" {
		return msg
	}
	if msg := extractOpenCodeErrorMessage(obj); msg != "" {
		return msg
	}
	if errText := extractString(obj["error"]); errText != "" && !looksMachineOriented(errText) {
		return errText
	}
	if nested := extractString(obj["final_text"]); nested != "" {
		if msg := runnerErrorEnvelopeMessage(nested); msg != "" {
			return msg
		}
	}
	return ""
}

func runnerReadableFinalText(text string) string {
	for _, payload := range structuredPayloadCandidates(text) {
		if msg := runnerErrorEnvelopeMessageValue(payload); msg != "" {
			continue
		}
		if obj, ok := payload.(map[string]any); ok {
			if nested := extractString(obj["final_text"]); nested != "" {
				if msg := runnerErrorEnvelopeMessage(nested); msg != "" {
					continue
				}
				if text := runnerReadableFinalText(nested); text != "" {
					return text
				}
				if !looksStructuredRunnerPayload(nested) {
					return nested
				}
			}
		}
		bestScore := 0
		bestText := ""
		for _, runnerID := range []RunnerID{RunnerCodex, RunnerOpenCode, RunnerClaude, RunnerGemini} {
			score, candidate := extractFinalTextCandidate(runnerID, payload)
			if strings.TrimSpace(candidate) != "" && score > bestScore {
				bestScore = score
				bestText = candidate
			}
		}
		if strings.TrimSpace(bestText) != "" {
			return strings.TrimSpace(bestText)
		}
	}
	return ""
}

func looksStructuredRunnerPayload(text string) bool {
	return len(structuredPayloadCandidates(text)) > 0
}

func structuredPayloadCandidates(text string) []any {
	text = strings.TrimSpace(text)
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return nil
	}
	payloads, _ := decodeStructuredPayloads(text)
	out := make([]any, 0, len(payloads))
	for _, raw := range payloads {
		var value any
		if err := json.Unmarshal(raw, &value); err == nil {
			out = append(out, value)
		}
	}
	return out
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
