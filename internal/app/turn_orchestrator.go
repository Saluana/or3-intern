package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/capability"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/requestctx"
	"or3-intern/internal/runners"
	"or3-intern/internal/turns"
)

// RunnerTurnRequest is the normalized input for a runner-backed chat turn.
type RunnerTurnRequest struct {
	SessionKey    string
	Channel       string
	From          string
	Message       string
	TriggerKind   string
	RunnerID      string
	Model         string
	Mode          string
	Isolation     string
	Cwd           string
	Attachments   []turns.Attachment
	Meta          map[string]any
	ApprovalToken string
	Actor         string
	Role          string
	ProfileName   string
	Capability    capability.Level
}

// RunnerTurnResult contains durable identifiers for a started runner turn.
type RunnerTurnResult struct {
	RunnerChatSessionID string
	RunnerChatTurnID    string
	RunnerRunID         string
	RunnerJobID         string
}

// RunnerTurnFinalResult is the persisted terminal state for a runner-backed
// chat turn.
type RunnerTurnFinalResult struct {
	Status       string
	FinalText    string
	ErrorMessage string
}

// RunnerTurnOrchestrator routes ingress work to runners.ChatManager instead of
// the built-in provider/tool-loop runtime.
type RunnerTurnOrchestrator struct {
	cfg            config.Config
	chat           *runners.ChatManager
	bootstrap      RunnerBootstrapContext
	context        *RunnerContextBuilder
	promptCompiler *RunnerPromptCompiler
}

// NewRunnerTurnOrchestrator constructs an orchestrator when runner chat is enabled.
func NewRunnerTurnOrchestrator(cfg config.Config, chat *runners.ChatManager, bootstrap RunnerBootstrapContext, deps RunnerContextDeps) *RunnerTurnOrchestrator {
	if chat == nil {
		return nil
	}
	compiler := NewRunnerPromptCompiler(cfg, bootstrap, deps)
	return &RunnerTurnOrchestrator{
		cfg:            cfg,
		chat:           chat,
		bootstrap:      bootstrap,
		context:        compiler.context,
		promptCompiler: compiler,
	}
}

func (o *RunnerTurnOrchestrator) bootstrapForTrigger(triggerKind string) RunnerBootstrapContext {
	if o == nil {
		return RunnerBootstrapContext{}
	}
	if isAutonomousTrigger(triggerKind) {
		return LoadRunnerBootstrapContext(o.cfg)
	}
	return o.bootstrap
}

// StartTurn enqueues a runner chat turn for the given request.
func (o *RunnerTurnOrchestrator) StartTurn(ctx context.Context, req RunnerTurnRequest) (RunnerTurnResult, error) {
	if o == nil || o.chat == nil {
		return RunnerTurnResult{}, errors.New("runner turn orchestrator unavailable")
	}
	runnerID := runners.RunnerID(strings.TrimSpace(req.RunnerID))
	if runnerID == "" {
		runnerID = runners.ResolveDefaultRunner(o.cfg)
	}
	if err := runners.ValidateSelectableRunner(o.cfg, runnerID); err != nil {
		return RunnerTurnResult{}, err
	}
	userMessage := strings.TrimSpace(req.Message)
	if userMessage == "" {
		return RunnerTurnResult{}, errors.New("message is required")
	}
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		return RunnerTurnResult{}, errors.New("session_key is required")
	}
	triggerKind := strings.TrimSpace(req.TriggerKind)
	if triggerKind == "" {
		triggerKind = "user_message"
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = strings.TrimSpace(o.cfg.Runners.DefaultMode)
	}
	isolation := strings.TrimSpace(req.Isolation)
	if isolation == "" {
		isolation = strings.TrimSpace(o.cfg.Runners.DefaultIsolation)
	}
	startReq := runners.StartTurnRequest{
		AppSessionKey:    sessionKey,
		RunnerID:         string(runnerID),
		UserMessage:      userMessage,
		PromptMessage:    userMessage,
		Attachments:      req.Attachments,
		ContinuationMode: runners.ContinuationReplay,
		Model:            strings.TrimSpace(req.Model),
		Mode:             mode,
		Isolation:        isolation,
		Cwd:              strings.TrimSpace(req.Cwd),
		Meta:             runnerTurnMeta(req.Meta, triggerKind),
		ApprovalToken:    strings.TrimSpace(req.ApprovalToken),
	}
	sess, err := o.chat.EnsureSession(ctx, startReq)
	if err != nil {
		return RunnerTurnResult{}, err
	}
	compiled, err := o.compileRunnerChatPrompt(ctx, sess.ID, sessionKey, userMessage, triggerKind, req.Meta, runners.ContinuationReplay)
	if err != nil {
		return RunnerTurnResult{}, err
	}
	startReq.PromptMessage = compiled.CompiledPrompt
	startReq.PromptMessageFinal = true
	result, err := o.chat.StartTurn(ctx, sess.ID, startReq)
	if err != nil {
		return RunnerTurnResult{}, err
	}
	return RunnerTurnResult{
		RunnerChatSessionID: result.Session.ID,
		RunnerChatTurnID:    result.Turn.ID,
		RunnerRunID:         result.Turn.RunnerRunID,
		RunnerJobID:         result.JobID,
	}, nil
}

// CompileRunnerChatPrompt builds the OR3 runner envelope for runner-chat HTTP turns.
func (o *RunnerTurnOrchestrator) CompileRunnerChatPrompt(ctx context.Context, sessionKey, userMessage, triggerKind string, meta map[string]any) RunnerPromptCompileResult {
	out, _ := o.compileRunnerChatPrompt(ctx, "", sessionKey, userMessage, triggerKind, meta, runners.ContinuationReplay)
	return out
}

// CompileRunnerChatPromptForSession builds a final runner-chat execution prompt
// with prior completed turns included as volatile OR3 context.
func (o *RunnerTurnOrchestrator) CompileRunnerChatPromptForSession(ctx context.Context, runnerChatSessionID, appSessionKey, userMessage, triggerKind string, meta map[string]any, continuation runners.ContinuationMode) (RunnerPromptCompileResult, error) {
	return o.compileRunnerChatPrompt(ctx, runnerChatSessionID, appSessionKey, userMessage, triggerKind, meta, continuation)
}

func (o *RunnerTurnOrchestrator) compileRunnerChatPrompt(ctx context.Context, runnerChatSessionID, appSessionKey, userMessage, triggerKind string, meta map[string]any, continuation runners.ContinuationMode) (RunnerPromptCompileResult, error) {
	extra := []string(nil)
	nativeSessionRef := ""
	if continuation == runners.ContinuationNative && strings.TrimSpace(runnerChatSessionID) != "" && o != nil && o.chat != nil && o.chat.DB != nil {
		sess, err := o.chat.DB.GetRunnerChatSession(ctx, runnerChatSessionID)
		if err != nil {
			return RunnerPromptCompileResult{}, fmt.Errorf("get runner chat session: %w", err)
		}
		nativeSessionRef = strings.TrimSpace(sess.NativeSessionRef)
	}
	if continuation != runners.ContinuationNative && strings.TrimSpace(runnerChatSessionID) != "" && o != nil && o.chat != nil && o.chat.DB != nil {
		history, err := o.chat.DB.ListRunnerChatTurns(ctx, runnerChatSessionID, 0)
		if err != nil {
			return RunnerPromptCompileResult{}, fmt.Errorf("list runner chat history: %w", err)
		}
		if block := runners.BuildReplayHistoryContextBlock(appAgentcliHistory(history)); block != "" {
			extra = append(extra, block)
		}
	}
	if cwd, ok := meta["_cwd"].(string); ok && strings.TrimSpace(cwd) != "" {
		extra = append(extra, "working_directory: "+cwd)
	}
	result := o.compilePrompt(ctx, RunnerPromptCompileInput{
		SessionKey:         appSessionKey,
		UserTask:           userMessage,
		TriggerKind:        triggerKind,
		Meta:               meta,
		ExtraContextBlocks: extra,
	})
	if continuation == runners.ContinuationNative && nativeSessionRef != "" && o != nil && o.context != nil {
		bootstrap := o.bootstrapForTrigger(triggerKind)
		refresh, refreshDebug := o.context.BuildNativeMemoryRefresh(ctx, appSessionKey, userMessage, bootstrap)
		result.MemoryRefresh = refresh
		if strings.TrimSpace(refresh) != "" {
			result.MemoryDebug.NativeRefresh = true
		}
		result.MemoryDebug.PinnedNonEmpty = result.MemoryDebug.PinnedNonEmpty || refreshDebug.PinnedNonEmpty
		result.MemoryDebug.RetrievedNonEmpty = result.MemoryDebug.RetrievedNonEmpty || refreshDebug.RetrievedNonEmpty
		result.MemoryDebug.DigestNonEmpty = result.MemoryDebug.DigestNonEmpty || refreshDebug.DigestNonEmpty
	}
	return result, nil
}

func appAgentcliHistory(turns []db.RunnerChatTurn) []runners.RunnerChatTurn {
	out := make([]runners.RunnerChatTurn, 0, len(turns))
	for _, t := range turns {
		out = append(out, runners.RunnerChatTurn{
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

// PrepareRunnerRunRequest applies OR3 context compilation to background agent runs.
func (o *RunnerTurnOrchestrator) PrepareRunnerRunRequest(ctx context.Context, req runners.RunnerRunRequest) runners.RunnerRunRequest {
	if o == nil || o.promptCompiler == nil {
		return req
	}
	return o.promptCompiler.PrepareRunnerRunRequest(ctx, req)
}

func (o *RunnerTurnOrchestrator) compilePrompt(ctx context.Context, in RunnerPromptCompileInput) RunnerPromptCompileResult {
	if o == nil || o.promptCompiler == nil {
		userTask := strings.TrimSpace(in.UserTask)
		return RunnerPromptCompileResult{
			Mode:           OR3ContextAuto,
			UserTask:       userTask,
			CompiledPrompt: userTask,
			RawTask:        userTask,
			TriggerKind:    normalizeTriggerKind(in.TriggerKind),
		}
	}
	return o.promptCompiler.Compile(ctx, in)
}

// HandleBusEvent converts a bus event into a runner chat turn.
func (o *RunnerTurnOrchestrator) HandleBusEvent(ctx context.Context, ev bus.Event) error {
	_, err := o.StartBusEventTurn(ctx, ev)
	return err
}

// StartBusEventTurn converts a bus event into a runner chat turn and returns
// durable ids callers can use to wait for final delivery.
func (o *RunnerTurnOrchestrator) StartBusEventTurn(ctx context.Context, ev bus.Event) (RunnerTurnResult, error) {
	if o == nil {
		return RunnerTurnResult{}, errors.New("runner turn orchestrator unavailable")
	}
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventHeartbeat, bus.EventWebhook, bus.EventFileChange, bus.EventSystem:
	default:
		return RunnerTurnResult{}, nil
	}
	req := RunnerTurnRequestFromBusEvent(o.cfg, ev)
	runCtx := requestctx.ContextWithApprovalToken(ctx, req.ApprovalToken)
	runCtx = requestctx.ContextWithRequesterIdentity(runCtx, req.Actor, req.Role)
	runCtx = requestctx.ContextWithCapabilityCeiling(runCtx, req.Capability)
	return o.StartTurn(runCtx, req)
}

// WaitForTurnResult waits until the underlying runner job and chat turn have
// reached a terminal state, then returns the persisted text/error that the app
// timeline uses.
func (o *RunnerTurnOrchestrator) WaitForTurnResult(ctx context.Context, result RunnerTurnResult) (RunnerTurnFinalResult, bool) {
	if o == nil || o.chat == nil || o.chat.DB == nil || strings.TrimSpace(result.RunnerChatTurnID) == "" {
		return RunnerTurnFinalResult{}, false
	}
	if o.chat.Jobs != nil && strings.TrimSpace(result.RunnerJobID) != "" {
		_, _ = o.chat.Jobs.Wait(ctx, result.RunnerJobID)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		turn, err := o.chat.DB.GetRunnerChatTurn(ctx, result.RunnerChatTurnID)
		if err == nil && runnerChatTurnTerminal(turn.Status) {
			return RunnerTurnFinalResult{
				Status:       turn.Status,
				FinalText:    strings.TrimSpace(turn.FinalText),
				ErrorMessage: strings.TrimSpace(turn.ErrorMessage),
			}, true
		}
		select {
		case <-ctx.Done():
			return RunnerTurnFinalResult{}, false
		case <-ticker.C:
		}
	}
}

func runnerChatTurnTerminal(status string) bool {
	switch status {
	case db.RunnerChatTurnStatusSucceeded,
		db.RunnerChatTurnStatusApprovalRequired,
		db.RunnerChatTurnStatusFailed,
		db.RunnerChatTurnStatusAborted,
		db.RunnerChatTurnStatusTimedOut:
		return true
	default:
		return false
	}
}

// RunnerTurnRequestFromBusEvent maps a bus event to a runner turn request.
func RunnerTurnRequestFromBusEvent(cfg config.Config, ev bus.Event) RunnerTurnRequest {
	req := RunnerTurnRequest{
		SessionKey:  strings.TrimSpace(ev.SessionKey),
		Channel:     strings.TrimSpace(ev.Channel),
		From:        strings.TrimSpace(ev.From),
		Message:     strings.TrimSpace(ev.Message),
		TriggerKind: busEventTriggerKind(ev),
		Meta:        cloneServiceMeta(ev.Meta),
	}
	if ev.Meta != nil {
		if raw, ok := ev.Meta["runner_id"].(string); ok {
			req.RunnerID = strings.TrimSpace(raw)
		}
		if raw, ok := ev.Meta["model"].(string); ok {
			req.Model = strings.TrimSpace(raw)
		}
		if raw, ok := ev.Meta["cwd"].(string); ok {
			req.Cwd = strings.TrimSpace(raw)
		}
		if raw, ok := ev.Meta["approval_token"].(string); ok {
			req.ApprovalToken = strings.TrimSpace(raw)
		}
		req.Attachments = turns.DecodeAttachments(ev.Meta["attachments"])
	}
	if req.RunnerID == "" {
		req.RunnerID = string(runners.ResolveDefaultRunner(cfg))
	}
	if req.Channel == "cli" {
		req.Actor = "cli"
		req.Role = approval.RoleOperator
	} else if req.From != "" {
		req.Actor = req.From
	}
	return req
}

func busEventTriggerKind(ev bus.Event) string {
	switch ev.Type {
	case bus.EventHeartbeat:
		return "heartbeat"
	case bus.EventCron:
		return "cron"
	case bus.EventWebhook:
		return "webhook"
	case bus.EventFileChange:
		return "file_watch"
	case bus.EventSystem:
		return "system"
	default:
		return "user_message"
	}
}

// ErrRunnerRuntimeUnavailable is returned when the runner runtime is not
// wired up — the turn orchestrator is missing and the default runner is not
// configured. In a runner-first architecture there is no runner toggle to
// flip; the operator must configure a default runner.
var ErrRunnerRuntimeUnavailable = errors.New("runner runtime unavailable: default runner not configured")

func runnerTurnMeta(in map[string]any, triggerKind string) map[string]any {
	meta := cloneServiceMeta(in)
	if meta == nil {
		meta = map[string]any{}
	}
	if triggerKind != "" {
		meta["trigger_kind"] = triggerKind
	}
	return meta
}

// FormatRunnerTurnError exposes a stable user-facing message for runner failures.
func FormatRunnerTurnError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("runner turn failed: %v", err)
}
