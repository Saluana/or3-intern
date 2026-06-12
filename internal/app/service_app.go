package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"or3-intern/internal/requestctx"
	"os"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/capability"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
	"or3-intern/internal/runners"
	"or3-intern/internal/streaming"
	"or3-intern/internal/turns"
)

type ServiceApp struct {
	cfg              config.Config
	jobs             *jobs.Registry
	runnerManager    *runners.Manager
	turnOrchestrator *RunnerTurnOrchestrator
	control          *controlplane.Service
	auth             *auth.Service
}

func NewServiceApp(cfg config.Config, jobs *jobs.Registry, control *controlplane.Service) *ServiceApp {
	return NewServiceAppWithRunner(cfg, jobs, nil, control)
}

func NewServiceAppWithRunner(cfg config.Config, jobs *jobs.Registry, runnerManager *runners.Manager, control *controlplane.Service) *ServiceApp {
	return NewServiceAppWithRunnerTurns(cfg, jobs, runnerManager, nil, control)
}

func NewServiceAppWithRunnerTurns(cfg config.Config, jobs *jobs.Registry, runnerManager *runners.Manager, turnOrchestrator *RunnerTurnOrchestrator, control *controlplane.Service) *ServiceApp {
	app := &ServiceApp{cfg: cfg, jobs: jobs, runnerManager: runnerManager, turnOrchestrator: turnOrchestrator, control: control}
	if control != nil {
		if authSvc, err := auth.NewService(cfg, control.DB, control.Audit); err == nil {
			app.auth = authSvc
		}
	}
	return app
}

func (a *ServiceApp) SetConfig(cfg config.Config) {
	if a == nil {
		return
	}
	a.cfg = cfg
	if a.control != nil {
		if authSvc, err := auth.NewService(cfg, a.control.DB, a.control.Audit); err == nil {
			a.auth = authSvc
		}
	}
}

func (a *ServiceApp) SetRunnerRuntime(runnerManager *runners.Manager, turnOrchestrator *RunnerTurnOrchestrator) {
	if a == nil {
		return
	}
	a.runnerManager = runnerManager
	a.turnOrchestrator = turnOrchestrator
}

type TurnRequest struct {
	SessionKey    string
	Message       string
	Model         string
	Attachments   []turns.Attachment
	SystemPrompt  string
	Meta          map[string]any
	ProfileName   string
	Capability    capability.Level
	ApprovalToken string
	Actor         string
	Role          string
	Observer      streaming.ConversationObserver
	Streamer      channels.StreamingChannel
}

type TurnResult struct {
	RunnerTurn *RunnerTurnResult
}

func (a *ServiceApp) serviceRunContext(ctx context.Context, sessionKey, profileName, approvalToken, actor, role string, level capability.Level, observer streaming.ConversationObserver, streamer channels.StreamingChannel) context.Context {
	runCtx := requestctx.ContextWithRequestSource(ctx, requestctx.RequestSourceService)
	runCtx = requestctx.ContextWithSession(runCtx, strings.TrimSpace(sessionKey))
	runCtx = requestctx.ContextWithApprovalToken(runCtx, approvalToken)
	runCtx = requestctx.ContextWithRequesterIdentity(runCtx, actor, role)
	runCtx = requestctx.ContextWithCapabilityCeiling(runCtx, level)
	if observer != nil {
		runCtx = streaming.ContextWithConversationObserver(runCtx, observer)
	}
	if streamer != nil {
		runCtx = streaming.ContextWithStreamingChannel(runCtx, streamer)
	}
	return runCtx
}

func (a *ServiceApp) RunTurn(ctx context.Context, req TurnRequest) (TurnResult, error) {
	if a == nil {
		return TurnResult{}, errors.New("service unavailable")
	}
	if a.turnOrchestrator == nil {
		if a.cfg.RunnerFirst() {
			return TurnResult{}, ErrRunnerRuntimeUnavailable
		}
		return TurnResult{}, errors.New("runner orchestrator unavailable")
	}
	runnerReq := RunnerTurnRequest{
		SessionKey:    strings.TrimSpace(req.SessionKey),
		Channel:       "service",
		From:          "or3-net",
		Message:       strings.TrimSpace(req.Message),
		TriggerKind:   "user_message",
		Model:         strings.TrimSpace(req.Model),
		Attachments:   req.Attachments,
		Meta:          cloneServiceMeta(req.Meta),
		ApprovalToken: strings.TrimSpace(req.ApprovalToken),
		Actor:         strings.TrimSpace(req.Actor),
		Role:          strings.TrimSpace(req.Role),
		ProfileName:   strings.TrimSpace(req.ProfileName),
		Capability:    req.Capability,
	}
	if req.Meta != nil {
		if raw, ok := req.Meta["runner_id"].(string); ok {
			runnerReq.RunnerID = strings.TrimSpace(raw)
		}
	}
	runCtx := a.serviceRunContext(ctx, req.SessionKey, req.ProfileName, req.ApprovalToken, req.Actor, req.Role, req.Capability, req.Observer, req.Streamer)
	result, err := a.turnOrchestrator.StartTurn(runCtx, runnerReq)
	if err != nil {
		return TurnResult{}, err
	}
	return TurnResult{RunnerTurn: &result}, nil
}

func cloneServiceMeta(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *ServiceApp) GetJob(jobID string) (jobs.Snapshot, error) {
	if a == nil || a.control == nil {
		return jobs.Snapshot{}, controlplane.ErrJobRegistryUnavailable
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
	if a.runnerManager != nil {
		if err := a.runnerManager.Abort(ctx, jobID); err == nil {
			return true, "", nil
		} else if strings.Contains(strings.ToLower(err.Error()), "not abortable") {
			return false, "not_abortable", nil
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

// DetectRunnerRunners returns runner info for all registered external runners.
func (a *ServiceApp) DetectRunnerRunners(ctx context.Context) ([]runners.RunnerInfo, error) {
	if a == nil {
		return nil, fmt.Errorf("service app is not available")
	}
	if a.runnerManager != nil {
		if a.runnerManager.Registry == nil {
			return nil, fmt.Errorf("runner registry is not configured")
		}
		detected := a.runnerManager.Registry.DetectAll(ctx, a.runnerManager.DetectOptions())
		return a.decorateRunnerRuntimeInfo(ctx, detected), nil
	}
	detectManager := &runners.Manager{Cfg: a.cfg.Runners}
	detected := runners.NewDefaultRegistry().DetectAll(ctx, detectManager.DetectOptions())
	return a.decorateRunnerRuntimeInfo(ctx, detected), nil
}

func (a *ServiceApp) decorateRunnerRuntimeInfo(ctx context.Context, detected []runners.RunnerInfo) []runners.RunnerInfo {
	if len(detected) == 0 {
		return detected
	}
	cfg := a.cfg.Runners
	var runtimes *runners.RunnerRuntimeRegistry
	if a.runnerManager != nil && a.runnerManager.Runtimes != nil {
		runtimes = a.runnerManager.Runtimes
	} else {
		runtimes = runners.NewDefaultRuntimeRegistry()
	}
	env := runners.BuildRunnerEnv(os.Environ(), cfg.ChildEnvAllowlist, nil)
	for i := range detected {
		id := runners.RunnerID(detected[i].ID)
		if runtime, ok := runtimes.Get(id); ok {
			detected[i].Runtime = runtime.Info(ctx, cfg, env)
		} else {
			detected[i].Runtime = runners.RunnerRuntimeInfo{Kind: runners.RuntimeCLI, Mode: runners.RuntimeModeCLI, State: runners.RuntimeStateUnavailable, Ownership: runners.RuntimeOwnershipNone, Fallback: true, FallbackReason: "using CLI adapter"}
		}
		if model := strings.TrimSpace(cfg.DefaultModels[detected[i].ID]); model != "" && detected[i].Runtime.DefaultModel == "" {
			detected[i].Runtime.DefaultModel = model
		}
	}
	return detected
}

// StartRunnerRun enqueues a new runner run.
func (a *ServiceApp) StartRunnerRun(ctx context.Context, req runners.RunnerRunRequest) (db.RunnerRun, error) {
	if a == nil || a.runnerManager == nil {
		return db.RunnerRun{}, fmt.Errorf("runner manager is not available")
	}
	if a.turnOrchestrator != nil {
		req = a.turnOrchestrator.PrepareRunnerRunRequest(ctx, req)
	}
	return a.runnerManager.Enqueue(ctx, req)
}

// GetRunnerRun reads a persisted runner run by run ID or job ID.
func (a *ServiceApp) GetRunnerRun(ctx context.Context, id string) (db.RunnerRun, bool, error) {
	if a == nil || a.runnerManager == nil || a.runnerManager.DB == nil {
		return db.RunnerRun{}, false, fmt.Errorf("runner manager is not available")
	}
	return a.runnerManager.DB.GetRunnerRun(ctx, id)
}

// ListRunnerRunEvents lists persisted events for a job.
func (a *ServiceApp) ListRunnerRunEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]db.RunnerRunEvent, error) {
	if a == nil || a.runnerManager == nil || a.runnerManager.DB == nil {
		return nil, fmt.Errorf("runner manager is not available")
	}
	return a.runnerManager.DB.ListRunnerRunEvents(ctx, jobID, afterSeq, limit)
}

// AbortRunnerRun cancels a runner run.
func (a *ServiceApp) AbortRunnerRun(ctx context.Context, jobID string) error {
	if a == nil || a.runnerManager == nil {
		return fmt.Errorf("runner manager is not available")
	}
	return a.runnerManager.Abort(ctx, jobID)
}

func (a *ServiceApp) WaitForJob(ctx context.Context, jobID string) (jobs.Snapshot, bool) {
	if a == nil || a.jobs == nil {
		return jobs.Snapshot{}, false
	}
	return a.jobs.Wait(ctx, jobID)
}

func (a *ServiceApp) SubscribeJob(jobID string) (jobs.Snapshot, <-chan jobs.Event, func(), bool) {
	if a == nil || a.jobs == nil {
		return jobs.Snapshot{}, nil, nil, false
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
	case "completed", "failed", "aborted":
		return true
	default:
		return false
	}
}
