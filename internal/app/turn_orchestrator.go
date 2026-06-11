package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/capability"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/requestctx"
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
	AgentCLIRunID       string
	AgentCLIJobID       string
}

// RunnerTurnOrchestrator routes ingress work to agentcli.ChatManager instead of
// the built-in provider/tool-loop runtime.
type RunnerTurnOrchestrator struct {
	cfg            config.Config
	chat           *agentcli.ChatManager
	bootstrap      RunnerBootstrapContext
	context        *RunnerContextBuilder
	promptCompiler *RunnerPromptCompiler
}

// NewRunnerTurnOrchestrator constructs an orchestrator when runner chat is enabled.
func NewRunnerTurnOrchestrator(cfg config.Config, chat *agentcli.ChatManager, bootstrap RunnerBootstrapContext, deps RunnerContextDeps) *RunnerTurnOrchestrator {
	if chat == nil || !cfg.AgentCLI.Enabled {
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
	runnerID := agentcli.RunnerID(strings.TrimSpace(req.RunnerID))
	activeRunner, legacyRunner, migrated := agentcli.ResolveRunnerIDForTurn(o.cfg, string(runnerID))
	if legacyRunner != "" && activeRunner == "" {
		return RunnerTurnResult{}, agentcli.LegacyRunnerMigrationError(legacyRunner)
	}
	runnerID = agentcli.RunnerID(activeRunner)
	if err := agentcli.ValidateSelectableRunner(o.cfg, runnerID); err != nil {
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
		mode = strings.TrimSpace(o.cfg.AgentCLI.DefaultMode)
	}
	isolation := strings.TrimSpace(req.Isolation)
	if isolation == "" {
		isolation = strings.TrimSpace(o.cfg.AgentCLI.DefaultIsolation)
	}
	startReq := agentcli.StartTurnRequest{
		AppSessionKey:    sessionKey,
		RunnerID:         string(runnerID),
		UserMessage:      userMessage,
		PromptMessage:    userMessage,
		Attachments:      req.Attachments,
		ContinuationMode: agentcli.ContinuationReplay,
		Model:            strings.TrimSpace(req.Model),
		Mode:             mode,
		Isolation:        isolation,
		Cwd:              strings.TrimSpace(req.Cwd),
		Meta:             runnerTurnMeta(req.Meta, migrated, legacyRunner, triggerKind),
		ApprovalToken:    strings.TrimSpace(req.ApprovalToken),
	}
	sess, err := o.chat.EnsureSession(ctx, startReq)
	if err != nil {
		return RunnerTurnResult{}, err
	}
	compiled, err := o.compileRunnerChatPrompt(ctx, sess.ID, sessionKey, userMessage, triggerKind, req.Meta, agentcli.ContinuationReplay)
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
		AgentCLIRunID:       result.Turn.AgentCLIRunID,
		AgentCLIJobID:       result.JobID,
	}, nil
}

// CompileRunnerChatPrompt builds the OR3 runner envelope for runner-chat HTTP turns.
func (o *RunnerTurnOrchestrator) CompileRunnerChatPrompt(ctx context.Context, sessionKey, userMessage, triggerKind string, meta map[string]any) RunnerPromptCompileResult {
	out, _ := o.compileRunnerChatPrompt(ctx, "", sessionKey, userMessage, triggerKind, meta, agentcli.ContinuationReplay)
	return out
}

// CompileRunnerChatPromptForSession builds a final runner-chat execution prompt
// with prior completed turns included as volatile OR3 context.
func (o *RunnerTurnOrchestrator) CompileRunnerChatPromptForSession(ctx context.Context, runnerChatSessionID, appSessionKey, userMessage, triggerKind string, meta map[string]any, continuation agentcli.ContinuationMode) (RunnerPromptCompileResult, error) {
	return o.compileRunnerChatPrompt(ctx, runnerChatSessionID, appSessionKey, userMessage, triggerKind, meta, continuation)
}

func (o *RunnerTurnOrchestrator) compileRunnerChatPrompt(ctx context.Context, runnerChatSessionID, appSessionKey, userMessage, triggerKind string, meta map[string]any, continuation agentcli.ContinuationMode) (RunnerPromptCompileResult, error) {
	extra := []string(nil)
	if continuation != agentcli.ContinuationNative && strings.TrimSpace(runnerChatSessionID) != "" && o != nil && o.chat != nil && o.chat.DB != nil {
		history, err := o.chat.DB.ListRunnerChatTurns(ctx, runnerChatSessionID, 0)
		if err != nil {
			return RunnerPromptCompileResult{}, fmt.Errorf("list runner chat history: %w", err)
		}
		if block := agentcli.BuildReplayHistoryContextBlock(appAgentcliHistory(history)); block != "" {
			extra = append(extra, block)
		}
	}
	return o.compilePrompt(ctx, RunnerPromptCompileInput{
		SessionKey:         appSessionKey,
		UserTask:           userMessage,
		TriggerKind:        triggerKind,
		Meta:               meta,
		ExtraContextBlocks: extra,
	}), nil
}

func appAgentcliHistory(turns []db.RunnerChatTurn) []agentcli.RunnerChatTurn {
	out := make([]agentcli.RunnerChatTurn, 0, len(turns))
	for _, t := range turns {
		out = append(out, agentcli.RunnerChatTurn{
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

// PrepareAgentRunRequest applies OR3 context compilation to background agent runs.
func (o *RunnerTurnOrchestrator) PrepareAgentRunRequest(ctx context.Context, req agentcli.AgentRunRequest) agentcli.AgentRunRequest {
	if o == nil || o.promptCompiler == nil {
		return req
	}
	return o.promptCompiler.PrepareAgentRunRequest(ctx, req)
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
	if o == nil {
		return errors.New("runner turn orchestrator unavailable")
	}
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventHeartbeat, bus.EventWebhook, bus.EventFileChange, bus.EventSystem:
	default:
		return nil
	}
	req := RunnerTurnRequestFromBusEvent(o.cfg, ev)
	runCtx := requestctx.ContextWithApprovalToken(ctx, req.ApprovalToken)
	runCtx = requestctx.ContextWithRequesterIdentity(runCtx, req.Actor, req.Role)
	runCtx = requestctx.ContextWithCapabilityCeiling(runCtx, req.Capability)
	_, err := o.StartTurn(runCtx, req)
	return err
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
		if raw, ok := ev.Meta["approval_token"].(string); ok {
			req.ApprovalToken = strings.TrimSpace(raw)
		}
		req.Attachments = turns.DecodeAttachments(ev.Meta["attachments"])
	}
	if req.RunnerID == "" {
		req.RunnerID = string(agentcli.ResolveDefaultRunner(cfg))
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

// ErrRunnerTurnsDisabled is returned when agent CLI is disabled.
var ErrRunnerTurnsDisabled = errors.New("runner turns require agentCLI.enabled and a configured default runner")

// ErrLegacyToolReplayDisabled is returned when built-in tool replay is requested in runner-first mode.
var ErrLegacyToolReplayDisabled = errors.New("built-in tool replay is disabled in runner-first mode; approve runner permissions or retry the turn")

func runnerTurnMeta(in map[string]any, migrated bool, legacyRunner, triggerKind string) map[string]any {
	meta := cloneServiceMeta(in)
	if meta == nil {
		meta = map[string]any{}
	}
	if triggerKind != "" {
		meta["trigger_kind"] = triggerKind
	}
	if migrated && legacyRunner != "" {
		meta["legacy_runner_id"] = legacyRunner
		meta["runner_migrated"] = true
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
