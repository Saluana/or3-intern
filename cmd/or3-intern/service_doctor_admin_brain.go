package main

import (
	"or3-intern/internal/requestctx"
	"context"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/doctoradmin"
	"or3-intern/internal/doctorbrain"
	"or3-intern/internal/jobs"
	"or3-intern/internal/serviceerrors"
	"or3-intern/internal/streaming"
	"or3-intern/internal/tools"
)

const doctorAdminBrainToolPolicyName = "settings_plan_proposals_and_safe_diagnostics_only"

var doctorAdminBrainAllowedToolNames = []string{
	doctorToolNameStatus,
	doctorToolNameLogs,
	doctorToolNameDocsIndex,
	doctorToolNameDocsSearch,
	doctorToolNameDocsSection,
	doctorToolNameConfigSearch,
	doctorToolNameConfigCatalog,
	doctorToolNameConfigMetadata,
	doctorToolNameSkillDiagnostics,
	doctorToolNameCreatePlan,
	doctorToolNameReadPlan,
	doctorToolNameRunPostChecks,
}

func doctorUsesRunnerChat(runnerID string) bool {
	runnerID = strings.TrimSpace(runnerID)
	return runnerID != "" && !strings.EqualFold(runnerID, string(agentcli.RunnerOR3))
}

func doctorShouldUseInternalAdminBrain(meta db.ChatSessionMeta, provider adminflow.AdminBrainProvider) bool {
	if provider.Available && provider.Kind == adminflow.AdminBrainAPIKeyProvider {
		return true
	}
	if strings.TrimSpace(meta.RunnerChatSessionID) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(meta.RunnerID), string(agentcli.RunnerOR3)) {
		return true
	}
	return false
}

func doctorAdminBrainAllowedTools(admin *doctoradmin.Registry) []string {
	if admin == nil {
		return nil
	}
	allowed := make([]string, 0, len(doctorAdminBrainAllowedToolNames))
	for _, name := range doctorAdminBrainAllowedToolNames {
		if !admin.Has(name) {
			continue
		}
		allowed = append(allowed, name)
	}
	return allowed
}

type doctorInternalAdminBrainTurnRequest struct {
	sessionKey    string
	content       string
	model         string
	approvalToken string
	identity      serviceAuthIdentity
}

func doctorInternalAdminBrainTurnMeta(content string) map[string]any {
	return map[string]any{
		"doctor_session":      true,
		"doctor_user_message": content,
		"doctor_untrusted":    true,
		"doctor_tool_policy":  doctorAdminBrainToolPolicyName,
		"doctor_admin_brain":  "internal",
	}
}

func (s *serviceServer) startDoctorInternalAdminBrainTurn(ctx context.Context, sessionKey, content, model, approvalToken string, identity serviceAuthIdentity) (string, error) {
	if s == nil || s.jobs == nil || s.doctorAdmin == nil {
		return "", fmt.Errorf("doctor admin brain unavailable")
	}
	req := doctorInternalAdminBrainTurnRequest{
		sessionKey:    strings.TrimSpace(sessionKey),
		content:       content,
		model:         strings.TrimSpace(model),
		approvalToken: strings.TrimSpace(approvalToken),
		identity:      identity,
	}
	if req.sessionKey == "" || strings.TrimSpace(req.content) == "" {
		return "", fmt.Errorf("session_key and message are required")
	}
	job := s.jobs.Register("doctor_admin_brain")
	releaseTurn, err := s.claimDoctorSessionTurn(req.sessionKey, "job", job.ID)
	if err != nil {
		s.jobs.Complete(job.ID, "failed", map[string]any{"error": err.Error()})
		return "", err
	}
	meta := doctorInternalAdminBrainTurnMeta(req.content)
	s.jobs.Publish(job.ID, "queued", serviceLifecyclePayload(req.sessionKey, meta, map[string]any{"status": "queued"}))
	s.persistServiceJobSummary(context.Background(), job.ID)
	runCtx, cancel := context.WithCancel(withDetachedContext(ctx))
	s.jobs.AttachCancel(job.ID, cancel)
	go func() {
		defer releaseTurn()
		s.runDoctorInternalAdminBrainJob(runCtx, job.ID, req)
	}()
	return job.ID, nil
}

func (s *serviceServer) runDoctorInternalAdminBrainJob(ctx context.Context, jobID string, req doctorInternalAdminBrainTurnRequest) {
	defer s.persistServiceJobSummary(context.Background(), jobID)
	meta := doctorInternalAdminBrainTurnMeta(req.content)
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(req.sessionKey, meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: jobs.ObserverForRegistry(s.jobs, jobID)}
	if err := s.runDoctorInternalAdminBrainTurnWithObserver(ctx, req, observer); err != nil {
		s.completeTurnJobWithError(ctx, jobID, err, observer, req.sessionKey, meta)
		return
	}
	finalText, recoveredEmpty := observer.finalTextForCompletion("Admin Brain completed without a final response.")
	payload := map[string]any{"final_text": finalText}
	if recoveredEmpty {
		payload["degraded"] = true
		payload["empty_final_text_recovered"] = true
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(req.sessionKey, meta, payload))
}

func (s *serviceServer) runDoctorInternalAdminBrainTurn(ctx context.Context, sessionKey, content, model, approvalToken string, identity serviceAuthIdentity) error {
	req := doctorInternalAdminBrainTurnRequest{
		sessionKey:    strings.TrimSpace(sessionKey),
		content:       content,
		model:         strings.TrimSpace(model),
		approvalToken: strings.TrimSpace(approvalToken),
		identity:      identity,
	}
	return s.runDoctorInternalAdminBrainTurnWithObserver(ctx, req, &serviceObserver{})
}

func doctorApprovedQuotaContinuationPrompt() string {
	return "Approval was granted to continue this Admin Assistant turn. Continue the same task from the existing conversation state and tool results already present. Do not repeat the same documentation searches unless a new gap remains."
}

func (s *serviceServer) runDoctorApprovedQuotaResumeJob(ctx context.Context, jobID string, issued approval.IssuedApproval, identity serviceAuthIdentity) {
	sessionKey := strings.TrimSpace(issued.Request.RequesterSessionID)
	meta := map[string]any{
		"approval_request_id": issued.Request.ID,
		"approved_resume":     true,
		"doctor_quota_resume": true,
	}
	log.Printf("service_approval: doctor_quota_resume_started approval=%d job=%s session=%s", issued.Request.ID, jobID, sessionKey)
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(sessionKey, meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: jobs.ObserverForRegistry(s.jobs, jobID)}
	err := s.runDoctorInternalAdminBrainTurnWithObserver(ctx, doctorInternalAdminBrainTurnRequest{
		sessionKey:    sessionKey,
		content:       doctorApprovedQuotaContinuationPrompt(),
		approvalToken: strings.TrimSpace(issued.Token),
		identity:      identity,
	}, observer)
	if err != nil {
		log.Printf("service_approval: doctor_quota_resume_error approval=%d job=%s session=%s public_code=%s", issued.Request.ID, jobID, sessionKey, serviceerrors.PublicErrorCode(err))
		s.completeTurnJobWithError(ctx, jobID, err, observer, sessionKey, meta)
		return
	}
	finalText, recoveredEmpty := observer.finalTextForCompletion("Admin Brain resumed after approval but did not return a final response.")
	payload := map[string]any{"final_text": finalText}
	if recoveredEmpty {
		payload["degraded"] = true
		payload["empty_final_text_recovered"] = true
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(sessionKey, meta, payload))
	log.Printf("service_approval: doctor_quota_resume_completed approval=%d job=%s session=%s recovered_empty=%t", issued.Request.ID, jobID, sessionKey, recoveredEmpty)
}

func (s *serviceServer) doctorBrainModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	chatRole := s.config.ModelRole(config.ModelRoleChat)
	if trimmed := strings.TrimSpace(chatRole.Primary.Model); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(s.config.Provider.Model)
}

func (s *serviceServer) runDoctorInternalAdminBrainTurnWithObserver(ctx context.Context, req doctorInternalAdminBrainTurnRequest, observer *serviceObserver) error {
	if s == nil || s.doctorAdmin == nil {
		return fmt.Errorf("doctor admin brain unavailable")
	}
	prov := newProviderClient(s.config)
	if prov == nil {
		return fmt.Errorf("provider not configured")
	}
	runCtx := requestctx.ContextWithRequestSource(ctx, requestctx.RequestSourceService)
	runCtx = requestctx.ContextWithSession(runCtx, req.sessionKey)
	runCtx = requestctx.ContextWithApprovalToken(runCtx, req.approvalToken)
	runCtx = requestctx.ContextWithRequesterIdentity(runCtx, strings.TrimSpace(req.identity.Actor), strings.TrimSpace(req.identity.Role))
	runCtx = requestctx.ContextWithCapabilityCeiling(runCtx, tools.CapabilityLevel(s.config.Service.MaxCapability))
	var streamObserver streaming.ConversationObserver
	if observer != nil {
		streamObserver = observer
	}
	chatRole := s.config.ModelRole(config.ModelRoleChat)
	return doctorbrain.ExecuteTurn(runCtx, doctorbrain.Config{
		DB:           s.serviceDB(),
		Provider:     prov,
		Model:        s.doctorBrainModel(req.model),
		Temperature:  roleTemperatureOrDefault(chatRole, s.config.Provider.Temperature),
		Admin:        s.doctorAdmin,
		Allowed:      doctorAdminBrainAllowedTools(s.doctorAdmin),
		MaxToolBytes: s.config.MaxToolBytes,
	}, doctorbrain.TurnInput{
		SessionKey:   req.sessionKey,
		SystemPrompt: s.buildDoctorAdminBrainContext(ctx),
		UserMessage:  req.content,
	}, streamObserver)
}
