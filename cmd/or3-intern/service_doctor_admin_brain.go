package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/db"
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

func doctorAdminBrainAllowedTools(reg *tools.Registry) []string {
	if reg == nil {
		return nil
	}
	allowed := make([]string, 0, len(doctorAdminBrainAllowedToolNames))
	for _, name := range doctorAdminBrainAllowedToolNames {
		if reg.Get(name) == nil {
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
	if s == nil || s.runtime == nil || s.jobs == nil {
		return "", fmt.Errorf("runtime unavailable")
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
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
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
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	err := s.runDoctorInternalAdminBrainTurnWithObserver(ctx, doctorInternalAdminBrainTurnRequest{
		sessionKey:    sessionKey,
		content:       doctorApprovedQuotaContinuationPrompt(),
		approvalToken: strings.TrimSpace(issued.Token),
		identity:      identity,
	}, observer)
	if err != nil {
		log.Printf("service_approval: doctor_quota_resume_error approval=%d job=%s session=%s public_code=%s", issued.Request.ID, jobID, sessionKey, agent.PublicErrorCode(err))
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

func (s *serviceServer) runDoctorInternalAdminBrainTurnWithObserver(ctx context.Context, req doctorInternalAdminBrainTurnRequest, observer *serviceObserver) error {
	if s == nil || s.runtime == nil {
		return fmt.Errorf("runtime unavailable")
	}
	allowedTools := doctorAdminBrainAllowedTools(s.runtime.Tools)
	toolBudget := agent.DoctorAdminBrainToolBudget()
	return s.app().RunTurn(ctx, app.TurnRequest{
		SessionKey:          req.sessionKey,
		Message:             req.content,
		SystemPrompt:        s.buildDoctorAdminBrainContext(ctx),
		Meta:                doctorInternalAdminBrainTurnMeta(req.content),
		AllowedTools:        allowedTools,
		RestrictTools:       true,
		Capability:          tools.CapabilityLevel(s.config.Service.MaxCapability),
		ApprovalToken:       req.approvalToken,
		Actor:               strings.TrimSpace(req.identity.Actor),
		Role:                strings.TrimSpace(req.identity.Role),
		Observer:            observer,
		Streamer:            agent.NullStreamer{},
		ProfileName:         "",
		ToolBudgetOverrides: &toolBudget,
	})
}
