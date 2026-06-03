package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/tools"
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
	Capability    tools.CapabilityLevel
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
	cfg        config.Config
	chat       *agentcli.ChatManager
	bootstrap  RunnerBootstrapContext
	context    *RunnerContextBuilder
	contextMax int
}

// NewRunnerTurnOrchestrator constructs an orchestrator when runner chat is enabled.
func NewRunnerTurnOrchestrator(cfg config.Config, chat *agentcli.ChatManager, bootstrap RunnerBootstrapContext, deps RunnerContextDeps) *RunnerTurnOrchestrator {
	if chat == nil || !cfg.AgentCLI.Enabled {
		return nil
	}
	return &RunnerTurnOrchestrator{
		cfg:        cfg,
		chat:       chat,
		bootstrap:  bootstrap,
		context:    NewRunnerContextBuilder(cfg, deps),
		contextMax: 48 * 1024,
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
	bootstrap := o.bootstrapForTrigger(triggerKind)
	var contextBlocks []string
	if o.context != nil {
		contextBlocks = o.context.BuildContextBlocks(ctx, sessionKey, userMessage, triggerKind, bootstrap)
	} else {
		contextBlocks = bootstrap.contextBlocks(triggerKind)
	}
	prompt := agentcli.BuildRunnerPrompt(agentcli.RunnerPromptContext{
		TrustedSystemInstructions: bootstrap.trustedBlocks(),
		ContextBlocks:             contextBlocks,
		UserMessage:               userMessage,
		TriggerKind:               triggerKind,
		MaxBytes:                  o.contextMax,
	})
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
		PromptMessage:    prompt,
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
	runCtx := tools.ContextWithApprovalToken(ctx, req.ApprovalToken)
	runCtx = tools.ContextWithRequesterIdentity(runCtx, req.Actor, req.Role)
	runCtx = tools.ContextWithCapabilityCeiling(runCtx, req.Capability)
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
