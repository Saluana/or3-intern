package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
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
}

func NewServiceApp(runtime *agent.Runtime, jobs *agent.JobRegistry, subagentManager *agent.SubagentManager, control *controlplane.Service) *ServiceApp {
	return &ServiceApp{runtime: runtime, jobs: jobs, subagentManager: subagentManager, control: control}
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

func (a *ServiceApp) RunTurn(ctx context.Context, req TurnRequest) error {
	if a == nil || a.runtime == nil {
		return errors.New("runtime unavailable")
	}
	runCtx := tools.ContextWithRequestSource(ctx, tools.RequestSourceService)
	runCtx = tools.ContextWithApprovalToken(runCtx, req.ApprovalToken)
	runCtx = tools.ContextWithRequesterIdentity(runCtx, req.Actor, req.Role)
	runCtx = tools.ContextWithCapabilityCeiling(runCtx, req.Capability)
	if req.Observer != nil {
		runCtx = agent.ContextWithConversationObserver(runCtx, req.Observer)
	}
	if req.Streamer != nil {
		runCtx = agent.ContextWithStreamingChannel(runCtx, req.Streamer)
	}
	if req.RestrictTools {
		filtered := tools.NewRegistry()
		if len(req.AllowedTools) > 0 {
			filtered = a.runtime.Tools.CloneFiltered(req.AllowedTools)
		}
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
