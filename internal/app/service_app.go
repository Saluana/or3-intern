package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type ServiceApp struct {
	runtime         *agent.Runtime
	jobs            *agent.JobRegistry
	subagentManager *agent.SubagentManager
	control         *controlplane.Service
	auth            *auth.Service
}

func NewServiceApp(cfg config.Config, runtime *agent.Runtime, jobs *agent.JobRegistry, subagentManager *agent.SubagentManager, control *controlplane.Service) *ServiceApp {
	app := &ServiceApp{runtime: runtime, jobs: jobs, subagentManager: subagentManager, control: control}
	if control != nil {
		if authSvc, err := auth.NewService(cfg, control.DB, control.Audit); err == nil {
			app.auth = authSvc
		}
	}
	return app
}

type TurnRequest struct {
	SessionKey    string
	Message       string
	Meta          map[string]any
	AllowedTools  []string
	RestrictTools bool
	ProfileName   string
	Capability    tools.CapabilityLevel
	ApprovalToken string
	Actor         string
	Role          string
	Observer      agent.ConversationObserver
	Streamer      channels.StreamingChannel
}

func (a *ServiceApp) serviceRunContext(ctx context.Context, sessionKey, profileName, approvalToken, actor, role string, capability tools.CapabilityLevel, observer agent.ConversationObserver, streamer channels.StreamingChannel) context.Context {
	runCtx := tools.ContextWithRequestSource(ctx, tools.RequestSourceService)
	runCtx = tools.ContextWithSession(runCtx, strings.TrimSpace(sessionKey))
	runCtx = tools.ContextWithApprovalToken(runCtx, approvalToken)
	runCtx = tools.ContextWithRequesterIdentity(runCtx, actor, role)
	runCtx = tools.ContextWithCapabilityCeiling(runCtx, capability)
	if a != nil && a.runtime != nil {
		runCtx = a.runtime.ContextWithProfileName(runCtx, profileName)
		runCtx = tools.ContextWithToolGuard(runCtx, a.runtime.GuardToolExecution)
	}
	if observer != nil {
		runCtx = agent.ContextWithConversationObserver(runCtx, observer)
	}
	if streamer != nil {
		runCtx = agent.ContextWithStreamingChannel(runCtx, streamer)
	}
	return runCtx
}

func (a *ServiceApp) serviceToolRegistry(allowedTools []string, restrictTools bool) *tools.Registry {
	if a == nil || a.runtime == nil {
		return nil
	}
	if !restrictTools {
		return a.runtime.Tools
	}
	filtered := tools.NewRegistry()
	if len(allowedTools) > 0 {
		filtered = a.runtime.Tools.CloneFiltered(allowedTools)
	}
	return filtered
}

func (a *ServiceApp) RunTurn(ctx context.Context, req TurnRequest) error {
	if a == nil || a.runtime == nil {
		return errors.New("runtime unavailable")
	}
	runCtx := a.serviceRunContext(ctx, req.SessionKey, req.ProfileName, req.ApprovalToken, req.Actor, req.Role, req.Capability, req.Observer, req.Streamer)
	if req.RestrictTools {
		filtered := a.serviceToolRegistry(req.AllowedTools, req.RestrictTools)
		runCtx = agent.ContextWithToolRegistry(runCtx, filtered)
	}
	meta := cloneMap(req.Meta)
	if strings.TrimSpace(req.ProfileName) != "" {
		meta["profile_name"] = strings.TrimSpace(req.ProfileName)
	}
	return a.runtime.Handle(runCtx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: strings.TrimSpace(req.SessionKey),
		Channel:    "service",
		From:       "or3-net",
		Message:    strings.TrimSpace(req.Message),
		Meta:       meta,
	})
}

type ReplayToolCallRequest struct {
	SessionKey    string
	ToolName      string
	ArgumentsJSON string
	AllowedTools  []string
	RestrictTools bool
	ProfileName   string
	Capability    tools.CapabilityLevel
	ApprovalToken string
	Actor         string
	Role          string
	Observer      agent.ConversationObserver
}

func (a *ServiceApp) ReplayToolCall(ctx context.Context, req ReplayToolCallRequest) (string, error) {
	if a == nil || a.runtime == nil {
		return "", errors.New("runtime unavailable")
	}
	registry := a.serviceToolRegistry(req.AllowedTools, req.RestrictTools)
	if registry == nil {
		return "", errors.New("tool registry unavailable")
	}
	toolName := strings.TrimSpace(req.ToolName)
	if toolName == "" {
		return "", errors.New("tool name is required")
	}
	argsJSON := strings.TrimSpace(req.ArgumentsJSON)
	if argsJSON == "" {
		argsJSON = "{}"
	}
	runCtx := a.serviceRunContext(ctx, req.SessionKey, req.ProfileName, req.ApprovalToken, req.Actor, req.Role, req.Capability, req.Observer, nil)
	if req.RestrictTools {
		runCtx = agent.ContextWithToolRegistry(runCtx, registry)
	}
	if req.Observer != nil {
		req.Observer.OnToolCall(runCtx, toolName, argsJSON)
	}
	out, err := registry.Execute(runCtx, toolName, argsJSON)
	if err != nil {
		var params map[string]any
		_ = json.Unmarshal([]byte(argsJSON), &params)
		out = tools.EncodeToolFailure(toolName, params, out, err)
	}
	if req.Observer != nil {
		req.Observer.OnToolResult(runCtx, toolName, out, err)
	}
	if err != nil {
		return "", err
	}
	if a.runtime.DB == nil || a.runtime.Builder == nil || a.runtime.Provider == nil {
		finalText := summarizeReplayToolResult(toolName, out)
		if req.Observer != nil {
			req.Observer.OnCompletion(runCtx, finalText, false)
		}
		return finalText, nil
	}
	toolCallID, findErr := a.findReplayToolCallID(runCtx, req.SessionKey, toolName, argsJSON)
	if findErr != nil {
		return "", findErr
	}
	if a.runtime.DB != nil && strings.TrimSpace(req.SessionKey) != "" {
		payload := map[string]any{
			"name":      toolName,
			"replayed":  true,
			"args_json": argsJSON,
		}
		if strings.TrimSpace(toolCallID) != "" {
			payload["tool_call_id"] = toolCallID
		}
		if _, err := a.runtime.DB.AppendMessage(runCtx, req.SessionKey, "tool", out, payload); err != nil {
			return "", err
		}
	}
	if err := a.runtime.Handle(runCtx, bus.Event{
		Type:       bus.EventSystem,
		SessionKey: strings.TrimSpace(req.SessionKey),
		Channel:    "service",
		From:       "or3-net",
		Message:    replayContinuationPrompt(toolName),
		Meta: map[string]any{
			"approved_tool_replay": true,
			"tool_name":            toolName,
		},
	}); err != nil {
		return "", err
	}
	return "", nil
}

func summarizeReplayToolResult(toolName string, out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return fmt.Sprintf("%s completed.", strings.TrimSpace(toolName))
	}
	var result tools.ToolResult
	if err := json.Unmarshal([]byte(out), &result); err == nil {
		parts := make([]string, 0, 3)
		if preview := strings.TrimSpace(result.Preview); preview != "" {
			parts = append(parts, preview)
		}
		if len(parts) == 0 {
			if summary := strings.TrimSpace(result.Summary); summary != "" {
				parts = append(parts, summary)
			}
		}
		if artifactID := strings.TrimSpace(result.ArtifactID); artifactID != "" {
			parts = append(parts, fmt.Sprintf("artifact: %s", artifactID))
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n")
		}
	}
	return out
}

func (a *ServiceApp) findReplayToolCallID(ctx context.Context, sessionKey, toolName, argsJSON string) (string, error) {
	if a == nil || a.runtime == nil || a.runtime.Builder == nil {
		return "", nil
	}
	pp, _, err := a.runtime.Builder.BuildWithOptions(ctx, agent.BuildOptions{
		SessionKey: strings.TrimSpace(sessionKey),
	})
	if err != nil {
		return "", err
	}
	wantArgs := canonicalReplayArgs(argsJSON)
	for i := len(pp.History) - 1; i >= 0; i-- {
		msg := pp.History[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for j := len(msg.ToolCalls) - 1; j >= 0; j-- {
			tc := msg.ToolCalls[j]
			if strings.TrimSpace(tc.Function.Name) != strings.TrimSpace(toolName) {
				continue
			}
			if wantArgs != "" && canonicalReplayArgs(tc.Function.Arguments) != wantArgs {
				continue
			}
			if id := strings.TrimSpace(tc.ID); id != "" {
				return id, nil
			}
		}
	}
	return "", nil
}

func canonicalReplayArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return raw
	}
	return string(encoded)
}

func replayContinuationPrompt(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("Approval was granted for the previously requested %s call. The exact approved tool call has now been executed and its latest result is already in the conversation. Continue the same task from that result. Do not stop just because the approved tool call succeeded, and do not repeat the same %s call unless it is still necessary.", name, name)
}

type SubagentRequest struct {
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	AllowedTools     []string
	RestrictTools    bool
	ProfileName      string
	Capability       tools.CapabilityLevel
	Channel          string
	ReplyTo          string
	Meta             map[string]any
	Timeout          time.Duration
	ApprovalToken    string
	Actor            string
	Role             string
}

func (a *ServiceApp) StartSubagent(ctx context.Context, req SubagentRequest) (tools.SpawnJob, error) {
	if a == nil || a.subagentManager == nil {
		return tools.SpawnJob{}, errors.New("subagent manager unavailable")
	}
	jobCtx := tools.ContextWithRequestSource(ctx, tools.RequestSourceService)
	jobCtx = tools.ContextWithCapabilityCeiling(jobCtx, req.Capability)
	return a.subagentManager.EnqueueService(jobCtx, agent.ServiceSubagentRequest{
		ParentSessionKey: strings.TrimSpace(req.ParentSessionKey),
		Task:             strings.TrimSpace(req.Task),
		PromptSnapshot:   append([]providers.ChatMessage{}, req.PromptSnapshot...),
		AllowedTools:     append([]string{}, req.AllowedTools...),
		RestrictTools:    req.RestrictTools,
		ProfileName:      strings.TrimSpace(req.ProfileName),
		Channel:          strings.TrimSpace(req.Channel),
		ReplyTo:          strings.TrimSpace(req.ReplyTo),
		Meta:             cloneMap(req.Meta),
		Timeout:          req.Timeout,
		ApprovalToken:    strings.TrimSpace(req.ApprovalToken),
		RequesterActor:   strings.TrimSpace(req.Actor),
		RequesterRole:    strings.TrimSpace(req.Role),
	})
}

func (a *ServiceApp) GetJob(jobID string) (agent.JobSnapshot, error) {
	if a == nil || a.control == nil {
		return agent.JobSnapshot{}, controlplane.ErrJobRegistryUnavailable
	}
	return a.control.GetJob(jobID)
}

func (a *ServiceApp) AbortJob(ctx context.Context, jobID string) (bool, string, error) {
	if a == nil || a.jobs == nil {
		return false, "", controlplane.ErrJobRegistryUnavailable
	}
	if a.jobs.Cancel(jobID) {
		return true, "", nil
	}
	if a.subagentManager != nil {
		if err := a.subagentManager.Abort(ctx, jobID); err == nil {
			return true, "", nil
		} else if strings.Contains(strings.ToLower(err.Error()), "not abortable") {
			return false, "not_abortable", nil
		} else {
			if !strings.Contains(strings.ToLower(err.Error()), "not found") {
				return false, "", err
			}
		}
	}
	snapshot, ok := a.jobs.Snapshot(jobID)
	if !ok {
		return false, "not_found", nil
	}
	if isTerminalStatus(snapshot.Status) {
		return true, snapshot.Status, nil
	}
	return false, "not_abortable", nil
}

func (a *ServiceApp) WaitForJob(ctx context.Context, jobID string) (agent.JobSnapshot, bool) {
	if a == nil || a.jobs == nil {
		return agent.JobSnapshot{}, false
	}
	return a.jobs.Wait(ctx, jobID)
}

func (a *ServiceApp) SubscribeJob(jobID string) (agent.JobSnapshot, <-chan agent.JobEvent, func(), bool) {
	if a == nil || a.jobs == nil {
		return agent.JobSnapshot{}, nil, nil, false
	}
	return a.jobs.Subscribe(jobID)
}

func (a *ServiceApp) CreatePairingRequest(ctx context.Context, input approval.PairingRequestInput) (db.PairingRequestRecord, string, error) {
	if a == nil || a.control == nil {
		return db.PairingRequestRecord{}, "", controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.CreatePairingRequest(ctx, input)
}

func (a *ServiceApp) ListPairingRequests(ctx context.Context, status string, limit int) ([]db.PairingRequestRecord, error) {
	if a == nil || a.control == nil {
		return nil, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ListPairingRequests(ctx, status, limit)
}

func (a *ServiceApp) ApprovePairingRequest(ctx context.Context, requestID int64, actor string) (db.PairingRequestRecord, error) {
	if a == nil || a.control == nil {
		return db.PairingRequestRecord{}, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ApprovePairingRequest(ctx, requestID, actor)
}

func (a *ServiceApp) ApprovePairingRequestByCode(ctx context.Context, code string, actor string) (db.PairingRequestRecord, error) {
	if a == nil || a.control == nil {
		return db.PairingRequestRecord{}, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ApprovePairingRequestByCode(ctx, code, actor)
}

func (a *ServiceApp) DenyPairingRequest(ctx context.Context, requestID int64, actor string) error {
	if a == nil || a.control == nil {
		return controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.DenyPairingRequest(ctx, requestID, actor)
}

func (a *ServiceApp) ExchangePairingCode(ctx context.Context, input approval.PairingExchangeInput) (db.PairedDeviceRecord, string, error) {
	if a == nil || a.control == nil {
		return db.PairedDeviceRecord{}, "", controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ExchangePairingCode(ctx, input)
}

func (a *ServiceApp) ListDevices(ctx context.Context, limit int) ([]db.PairedDeviceRecord, error) {
	if a == nil || a.control == nil {
		return nil, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ListDevices(ctx, limit)
}

func (a *ServiceApp) RevokeDevice(ctx context.Context, deviceID, actor string) error {
	if a == nil || a.control == nil {
		return controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.RevokeDevice(ctx, deviceID, actor)
}

func (a *ServiceApp) RotateDevice(ctx context.Context, deviceID string) (db.PairedDeviceRecord, string, error) {
	if a == nil || a.control == nil {
		return db.PairedDeviceRecord{}, "", controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.RotateDevice(ctx, deviceID)
}

func (a *ServiceApp) ListApprovalRequests(ctx context.Context, filter controlplane.ApprovalFilter) ([]db.ApprovalRequestRecord, error) {
	if a == nil || a.control == nil {
		return nil, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ListApprovalRequests(ctx, filter)
}

func (a *ServiceApp) GetApproval(ctx context.Context, requestID int64) (db.ApprovalRequestRecord, error) {
	if a == nil || a.control == nil {
		return db.ApprovalRequestRecord{}, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.GetApproval(ctx, requestID)
}

func (a *ServiceApp) ApproveApproval(ctx context.Context, requestID int64, actor string, allowlist bool, note string) (approval.IssuedApproval, error) {
	if a == nil || a.control == nil {
		return approval.IssuedApproval{}, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ApproveApproval(ctx, requestID, actor, allowlist, note)
}

func (a *ServiceApp) DenyApproval(ctx context.Context, requestID int64, actor, note string) error {
	if a == nil || a.control == nil {
		return controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.DenyApproval(ctx, requestID, actor, note)
}

func (a *ServiceApp) CancelApproval(ctx context.Context, requestID int64, actor, note string) error {
	if a == nil || a.control == nil {
		return controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.CancelApproval(ctx, requestID, actor, note)
}

func (a *ServiceApp) ExpireApprovals(ctx context.Context, actor string) (int64, error) {
	if a == nil || a.control == nil {
		return 0, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ExpireApprovals(ctx, actor)
}

func (a *ServiceApp) ListAllowlists(ctx context.Context, domain string, limit int) ([]db.ApprovalAllowlistRecord, error) {
	if a == nil || a.control == nil {
		return nil, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.ListAllowlists(ctx, domain, limit)
}

func (a *ServiceApp) Auth() *auth.Service {
	if a == nil {
		return nil
	}
	return a.auth
}

func (a *ServiceApp) BeginPasskeyRegistration(ctx context.Context, req auth.BeginRegistrationRequest) (*auth.BeginCeremonyResponse, error) {
	if a == nil || a.auth == nil {
		return nil, auth.ErrAuthDisabled
	}
	return a.auth.BeginRegistration(ctx, req)
}

func (a *ServiceApp) FinishPasskeyRegistration(ctx context.Context, req auth.FinishRegistrationRequest) (db.PasskeyCredentialRecord, error) {
	if a == nil || a.auth == nil {
		return db.PasskeyCredentialRecord{}, auth.ErrAuthDisabled
	}
	return a.auth.FinishRegistration(ctx, req)
}

func (a *ServiceApp) BeginPasskeyLogin(ctx context.Context, req auth.BeginLoginRequest) (*auth.BeginCeremonyResponse, error) {
	if a == nil || a.auth == nil {
		return nil, auth.ErrAuthDisabled
	}
	return a.auth.BeginLogin(ctx, req)
}

func (a *ServiceApp) FinishPasskeyLogin(ctx context.Context, req auth.FinishLoginRequest) (auth.LoginResult, error) {
	if a == nil || a.auth == nil {
		return auth.LoginResult{}, auth.ErrAuthDisabled
	}
	return a.auth.FinishLogin(ctx, req)
}

func (a *ServiceApp) BeginStepUp(ctx context.Context, req auth.BeginStepUpRequest) (*auth.BeginCeremonyResponse, error) {
	if a == nil || a.auth == nil {
		return nil, auth.ErrAuthDisabled
	}
	return a.auth.BeginStepUp(ctx, req)
}

func (a *ServiceApp) FinishStepUp(ctx context.Context, req auth.FinishStepUpRequest) (db.AuthSessionRecord, error) {
	if a == nil || a.auth == nil {
		return db.AuthSessionRecord{}, auth.ErrAuthDisabled
	}
	return a.auth.FinishStepUp(ctx, req)
}

func (a *ServiceApp) ValidateAuthSession(ctx context.Context, token string) (auth.SessionClaims, error) {
	if a == nil || a.auth == nil {
		return auth.SessionClaims{}, auth.ErrAuthDisabled
	}
	return a.auth.ValidateSessionToken(ctx, token)
}

func (a *ServiceApp) RevokeAuthSession(ctx context.Context, token, reason string) error {
	if a == nil || a.auth == nil {
		return auth.ErrAuthDisabled
	}
	return a.auth.RevokeSessionToken(ctx, token, reason)
}

func (a *ServiceApp) ListPasskeys(ctx context.Context, userID string) ([]db.PasskeyCredentialRecord, error) {
	if a == nil || a.auth == nil {
		return nil, auth.ErrAuthDisabled
	}
	return a.auth.ListPasskeys(ctx, userID)
}

func (a *ServiceApp) RenamePasskey(ctx context.Context, passkeyID, nickname string) error {
	if a == nil || a.auth == nil {
		return auth.ErrAuthDisabled
	}
	return a.auth.RenamePasskey(ctx, passkeyID, nickname)
}

func (a *ServiceApp) RevokePasskey(ctx context.Context, sessionToken, passkeyID, reason string) error {
	if a == nil || a.auth == nil {
		return auth.ErrAuthDisabled
	}
	return a.auth.RevokePasskey(ctx, sessionToken, passkeyID, reason)
}

func (a *ServiceApp) AddAllowlist(ctx context.Context, domain string, scope approval.AllowlistScope, matcher any, actor string, expiresAt int64) (db.ApprovalAllowlistRecord, error) {
	if a == nil || a.control == nil {
		return db.ApprovalAllowlistRecord{}, controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.AddAllowlist(ctx, domain, scope, matcher, actor, expiresAt)
}

func (a *ServiceApp) RemoveAllowlist(ctx context.Context, id int64, actor string) error {
	if a == nil || a.control == nil {
		return controlplane.ErrApprovalBrokerUnavailable
	}
	return a.control.RemoveAllowlist(ctx, id, actor)
}

func ResolveToolPolicy(base *tools.Registry, policy *agent.ServiceToolPolicy, legacyAllowed []string) ([]string, bool, error) {
	return agent.ResolveServiceToolAllowlist(base, policy, legacyAllowed)
}

func DecodeServiceFilePayload(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("reader is required")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func isTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "aborted", db.SubagentStatusSucceeded, db.SubagentStatusInterrupted:
		return true
	default:
		return false
	}
}
