package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/configmeta"
	"or3-intern/internal/db"
	"or3-intern/internal/diagnosticlog"
	"or3-intern/internal/doctor"
	"or3-intern/internal/skilldiag"
	"or3-intern/internal/tools"
	"or3-intern/internal/uxcopy"
)

const serviceDoctorBodyLimit = 256 * 1024

type serviceDoctorStatusRequest struct {
	ClientFindings    []doctor.Finding                 `json:"client_findings"`
	ClientDiagnostics *diagnosticlog.ClientDiagnostics `json:"client_diagnostics,omitempty"`
}

type serviceDoctorSessionCreateRequest struct {
	SessionKey string `json:"session_key"`
	Title      string `json:"title"`
	RunnerID   string `json:"runner_id"`
	Model      string `json:"model"`
}

type serviceDoctorSessionMessageRequest struct {
	Content       string `json:"content"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinking_level"`
	Stream        bool   `json:"stream"`
	RunnerID      string `json:"runner_id"`
}

type serviceDoctorPlanCreateRequest struct {
	ConversationID    string                       `json:"conversation_id"`
	AcceptedCardID    string                       `json:"accepted_card_id"`
	ApprovedAuthority configmeta.RiskLevel         `json:"approved_authority"`
	Plan              adminflow.SettingsChangePlan `json:"plan"`
}

type serviceDoctorPlanApplyRequest struct {
	Approval          adminflow.ApprovalContext `json:"approval"`
	ApprovedAuthority configmeta.RiskLevel      `json:"approved_authority"`
}

func (s *serviceServer) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	configmeta.EnsureFirstSliceFieldsRegistered()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/doctor"), "/")
	switch {
	case path == "" || path == "status":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorStatus(w, r, nil)
	case path == "run":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
		var req serviceDoctorStatusRequest
		if err := decodeServiceRequestBody(r.Body, &req); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		clientFindings := append([]doctor.Finding{}, req.ClientFindings...)
		if req.ClientDiagnostics != nil {
			clientFindings = append(clientFindings, diagnosticlog.FindingsFromClientDiagnostics(*req.ClientDiagnostics)...)
		}
		s.handleDoctorStatus(w, r, clientFindings)
	case path == "admin-brain":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceValue(w, http.StatusOK, s.detectAdminBrainProvider(r.Context()))
	case path == "config-metadata":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"fields": configmeta.List()})
	case path == "logs":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorLogs(w, r)
	case strings.HasPrefix(path, "skills/"):
		s.handleDoctorSkillRoutes(w, r, strings.TrimPrefix(path, "skills/"))
	case path == "plans/preview":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanPreview(w, r)
	case path == "plans":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanCreate(w, r)
	case strings.HasPrefix(path, "plans/"):
		s.handleDoctorPlans(w, r, strings.TrimPrefix(path, "plans/"))
	case path == "sessions":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorSessionCreate(w, r)
	case strings.HasPrefix(path, "sessions/"):
		s.handleDoctorSessions(w, r, strings.TrimPrefix(path, "sessions/"))
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor route not found"})
	}
}

func (s *serviceServer) handleDoctorSkillRoutes(w http.ResponseWriter, r *http.Request, rel string) {
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) != 2 || parts[1] != "diagnostics" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor skill route not found"})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(name) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid skill name"})
		return
	}
	inv := s.serviceSkillsInventory(r.Context(), s.config)
	skill, ok := inv.Get(name)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "skill not found"})
		return
	}
	entry := s.config.Skills.Entries[serviceSkillEntryKey(skill)]
	result, err := skilldiag.Evaluate(r.Context(), skill.Dir, skilldiag.Options{
		Entry: skilldiag.SkillEntryState{
			SkillKey: serviceSkillEntryKey(skill),
			Enabled:  entry.Enabled,
			APIKey:   entry.APIKey,
			Env:      cloneSkillEnv(entry.Env),
			Config:   cloneSkillConfig(entry.Config),
		},
		Runner: skilldiag.ExecRunner{},
	})
	if err != nil {
		writeServiceValue(w, http.StatusBadGateway, map[string]any{"skill": serviceSkillItemFromMeta(skill, s.config.Skills), "diagnostics": result, "error": err.Error(), "plans": serviceDoctorPlansFromSkillDiag(result.SuggestedPlans)})
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"skill": serviceSkillItemFromMeta(skill, s.config.Skills), "diagnostics": result, "plans": serviceDoctorPlansFromSkillDiag(result.SuggestedPlans)})
}

func (s *serviceServer) handleDoctorStatus(w http.ResponseWriter, r *http.Request, clientFindings []doctor.Finding) {
	report := doctor.Evaluate(s.config, doctor.Options{Mode: doctor.ModeAdvisory})
	if len(clientFindings) > 0 {
		combined := append(append([]doctor.Finding{}, report.Findings...), clientFindings...)
		report = doctor.NewReport(doctor.ModeAdvisory, combined)
	}
	writeServiceValue(w, http.StatusOK, s.buildDoctorStatusResponse(r, report))
}

func (s *serviceServer) handleDoctorLogs(w http.ResponseWriter, r *http.Request) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	requestedLimit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		requestedLimit = n
	}
	limit := clampDoctorDiagnosticLogLimit(requestedLimit)
	sinceMS, ok := parseOptionalInt64Query(w, r, "since_ms")
	if !ok {
		return
	}
	untilMS, ok := parseOptionalInt64Query(w, r, "until_ms")
	if !ok {
		return
	}
	if sinceMS > 0 && untilMS > 0 && sinceMS > untilMS {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "since_ms must be before until_ms"})
		return
	}
	items, err := store.QueryDiagnosticLogEvents(r.Context(), db.DiagnosticLogQuery{
		Source:        strings.TrimSpace(r.URL.Query().Get("source")),
		Level:         strings.TrimSpace(r.URL.Query().Get("level")),
		CorrelationID: strings.TrimSpace(r.URL.Query().Get("correlation_id")),
		EventType:     strings.TrimSpace(r.URL.Query().Get("event_type")),
		Pattern:       serviceFirstNonEmpty(strings.TrimSpace(r.URL.Query().Get("pattern")), strings.TrimSpace(r.URL.Query().Get("known_failure_pattern"))),
		SinceUnixMS:   sinceMS,
		UntilUnixMS:   untilMS,
		Limit:         limit,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor logs unavailable", err)
		return
	}
	_ = s.recordDoctorAudit(r.Context(), serviceAuthIdentityFromContext(r.Context()), "doctor.log.queried", map[string]any{
		"source":          strings.TrimSpace(r.URL.Query().Get("source")),
		"level":           strings.TrimSpace(r.URL.Query().Get("level")),
		"correlation_id":  strings.TrimSpace(r.URL.Query().Get("correlation_id")),
		"event_type":      strings.TrimSpace(r.URL.Query().Get("event_type")),
		"pattern":         serviceFirstNonEmpty(strings.TrimSpace(r.URL.Query().Get("pattern")), strings.TrimSpace(r.URL.Query().Get("known_failure_pattern"))),
		"since_ms":        sinceMS,
		"until_ms":        untilMS,
		"limit":           limit,
		"requested_limit": requestedLimit,
		"returned":        len(items),
		"queried_at":      db.NowMS(),
	})
	writeServiceValue(w, http.StatusOK, map[string]any{
		"items":           items,
		"limit":           limit,
		"requested_limit": requestedLimit,
		"returned":        len(items),
	})
}

func (s *serviceServer) handleDoctorPlanPreview(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
	var req serviceDoctorPlanCreateRequest
	if err := decodeServiceRequestBody(r.Body, &req); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	plan := req.Plan
	if strings.TrimSpace(plan.ID) == "" {
		plan.ID = newDoctorID("scp-preview")
	}
	state, err := (adminflow.PlanValidator{}).Stage(s.config, &plan, adminflow.ValidationOptions{ApprovedAuthority: serviceDoctorApprovedAuthority(r.Context())})
	if err != nil {
		status := http.StatusBadRequest
		if err == adminflow.ErrStalePlan {
			status = http.StatusConflict
		}
		writeServiceValue(w, status, map[string]any{"error": err.Error(), "plan": plan, "validation": state.Validation, "persisted": false})
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{
		"plan":          plan,
		"doctor_report": state.DoctorReport,
		"live_reloaded": state.LiveReloadKeys,
		"validation":    state.Validation,
		"persisted":     false,
	})
}

func (s *serviceServer) handleDoctorPlanCreate(w http.ResponseWriter, r *http.Request) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
	var req serviceDoctorPlanCreateRequest
	if err := decodeServiceRequestBody(r.Body, &req); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	plan := req.Plan
	if strings.TrimSpace(plan.ID) == "" {
		plan.ID = newDoctorID("scp")
	}
	if strings.TrimSpace(plan.CreatedBy) == "" {
		plan.CreatedBy = s.serviceDoctorActor(r)
	}
	state, err := (adminflow.PlanValidator{}).Stage(s.config, &plan, adminflow.ValidationOptions{ApprovedAuthority: serviceDoctorApprovedAuthority(r.Context())})
	if err != nil {
		status := http.StatusBadRequest
		if err == adminflow.ErrStalePlan {
			status = http.StatusConflict
		}
		writeServiceValue(w, status, map[string]any{"error": err.Error(), "plan": plan, "validation": state.Validation})
		return
	}
	if err := serviceDoctorPlanPersistAllowed(r.Context(), plan); err != nil {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": err.Error(), "code": "step_up_required"})
		return
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "plan encoding failed", err)
		return
	}
	approvalJSON, _ := json.Marshal(adminflow.ApprovalContext{PlanID: plan.ID})
	liveReloadJSON, _ := json.Marshal(state.LiveReloadKeys)
	if err := store.CreateSettingsChangePlan(r.Context(), db.SettingsChangePlanRecord{
		ID:             plan.ID,
		Status:         "validated",
		ConversationID: req.ConversationID,
		AcceptedCardID: req.AcceptedCardID,
		CreatedBy:      plan.CreatedBy,
		PlanJSON:       string(planJSON),
		ApprovalJSON:   string(approvalJSON),
		LiveReloadJSON: string(liveReloadJSON),
	}); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan persistence failed", err)
		return
	}
	_ = s.recordDoctorAudit(r.Context(), serviceAuthIdentityFromContext(r.Context()), "doctor.plan.created", serviceDoctorAuditPlanPayload(plan, serviceAuthIdentityFromContext(r.Context()), adminflow.ApprovalContext{}, map[string]any{
		"conversation_id":  req.ConversationID,
		"accepted_card_id": req.AcceptedCardID,
		"live_reloaded":    state.LiveReloadKeys,
		"validated_at":     db.NowMS(),
	}))
	_ = s.appendDoctorLog(r.Context(), db.DiagnosticLogEvent{Source: "doctor", Level: "info", CorrelationID: plan.ID, EventType: "doctor.plan.create", Payload: json.RawMessage(serviceDoctorMustJSON(serviceDoctorRedactedPlanForAudit(plan)))})
	writeServiceValue(w, http.StatusCreated, map[string]any{"plan": plan, "doctor_report": state.DoctorReport, "live_reloaded": state.LiveReloadKeys})
}

func (s *serviceServer) handleDoctorPlans(w http.ResponseWriter, r *http.Request, rel string) {
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
		return
	}
	planID := parts[0]
	tail := ""
	if len(parts) > 1 {
		tail = parts[1]
	}
	switch tail {
	case "":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanRead(w, r, planID)
	case "validate":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanValidate(w, r, planID)
	case "apply":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanApply(w, r, planID)
	case "rollback":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanRollback(w, r, planID)
	case "post-checks":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorPlanPostChecks(w, r, planID)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor plan route not found"})
	}
}

func (s *serviceServer) handleDoctorPlanRead(w http.ResponseWriter, r *http.Request, planID string) {
	record, plan, ok := s.loadDoctorPlan(r.Context(), planID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
		return
	}
	checkpoint, checkpointOK, err := s.doctorDB().GetLatestDoctorCheckpointForPlan(r.Context(), planID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor checkpoint lookup failed", err)
		return
	}
	response := map[string]any{
		"plan":               plan,
		"status":             record.Status,
		"approval":           json.RawMessage(record.ApprovalJSON),
		"live_reloaded":      json.RawMessage(record.LiveReloadJSON),
		"rollback_id":        record.RollbackID,
		"post_check_pending": record.PostCheckPending,
		"error":              record.ErrorText,
	}
	if checkpointOK {
		response["checkpoint"] = checkpoint
	}
	writeServiceValue(w, http.StatusOK, response)
}

func (s *serviceServer) handleDoctorPlanValidate(w http.ResponseWriter, r *http.Request, planID string) {
	record, plan, ok := s.loadDoctorPlan(r.Context(), planID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
		return
	}
	state, err := (adminflow.PlanValidator{}).Stage(s.config, &plan, adminflow.ValidationOptions{ApprovedAuthority: serviceDoctorApprovedAuthority(r.Context())})
	if err != nil {
		status := http.StatusBadRequest
		if err == adminflow.ErrStalePlan {
			status = http.StatusConflict
		}
		writeServiceValue(w, status, map[string]any{"error": err.Error(), "plan": plan, "validation": state.Validation})
		return
	}
	if err := serviceDoctorPlanPersistAllowed(r.Context(), plan); err != nil {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": err.Error(), "code": "step_up_required"})
		return
	}
	planJSON, _ := json.Marshal(plan)
	if err := s.doctorDB().UpdateSettingsChangePlanStatus(r.Context(), planID, "validated", record.RollbackID, "", false, record.ApprovalJSON, serviceDoctorMustJSON(state.LiveReloadKeys), 0); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan status update failed", err)
		return
	}
	if _, err := s.doctorDB().SQL.ExecContext(r.Context(), `UPDATE settings_change_plans SET plan_json=? WHERE id=?`, string(planJSON), planID); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan update failed", err)
		return
	}
	_ = s.recordDoctorAudit(r.Context(), serviceAuthIdentityFromContext(r.Context()), "doctor.plan.validated", serviceDoctorAuditPlanPayload(plan, serviceAuthIdentityFromContext(r.Context()), adminflow.ApprovalContext{}, map[string]any{
		"live_reloaded": state.LiveReloadKeys,
		"validated_at":  db.NowMS(),
	}))
	writeServiceValue(w, http.StatusOK, map[string]any{"plan": plan, "doctor_report": state.DoctorReport, "live_reloaded": state.LiveReloadKeys})
}

func (s *serviceServer) handleDoctorPlanApply(w http.ResponseWriter, r *http.Request, planID string) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	record, plan, ok := s.loadDoctorPlan(r.Context(), planID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
		return
	}
	if !serviceDoctorPlanStatusAllowsApply(record.Status) {
		writeServiceJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("plan cannot be applied from status %q", record.Status), "status": record.Status})
		return
	}
	limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
	var req serviceDoctorPlanApplyRequest
	if err := decodeServiceRequestBody(r.Body, &req); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	state, err := (adminflow.PlanValidator{}).Stage(s.config, &plan, adminflow.ValidationOptions{ApprovedAuthority: serviceDoctorApprovedAuthority(r.Context())})
	if err != nil {
		status := http.StatusBadRequest
		if err == adminflow.ErrStalePlan {
			status = http.StatusConflict
		}
		writeServiceValue(w, status, map[string]any{"error": err.Error(), "plan": plan, "validation": state.Validation})
		return
	}
	if err := validateDoctorApprovalForPlan(r.Context(), plan, req.Approval); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	rollbackID := ""
	rollbackPlan, safeRollback := buildServiceDoctorRollbackPlan(plan)
	if rollbackPlan.Available {
		rollbackID = newDoctorID("scr")
		if err := store.CreateSettingsChangeRollback(r.Context(), db.SettingsChangeRollbackRecord{
			ID:           rollbackID,
			PlanID:       plan.ID,
			Status:       "available",
			RollbackJSON: serviceDoctorMustJSON(rollbackPlan),
			ChangesJSON:  serviceDoctorMustJSON(serviceDoctorRedactedRollbackChanges(plan)),
		}); err != nil {
			writeServiceError(w, r, http.StatusServiceUnavailable, "rollback persistence failed", err)
			return
		}
	}
	configPath := s.configPath
	if strings.TrimSpace(configPath) == "" {
		configPath = cfgPathOrDefault("")
	}
	if err := store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "applying", rollbackID, "", true, serviceDoctorMustJSON(req.Approval), serviceDoctorMustJSON(state.LiveReloadKeys), 0); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan status update failed", err)
		return
	}
	if err := config.Save(configPath, state.StagedConfig); err != nil {
		_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "validated", rollbackID, err.Error(), true, serviceDoctorMustJSON(req.Approval), serviceDoctorMustJSON(state.LiveReloadKeys), 0)
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	s.applyLiveConfig(state.StagedConfig)
	appliedAt := db.NowMS()
	postChecks := plan.PostApplyChecks
	if len(postChecks) == 0 {
		postChecks = []adminflow.PostApplyCheck{{ID: "doctor.configure_post_save", Description: "Re-run Doctor post-save checks", Timeout: 10}}
	}
	checkpointID := newDoctorID("dcp")
	if err := store.CreateDoctorCheckpoint(r.Context(), db.DoctorCheckpointRecord{
		ID:             checkpointID,
		PlanID:         plan.ID,
		ConversationID: record.ConversationID,
		AcceptedCardID: record.AcceptedCardID,
		Status:         "pending",
		ChecksJSON:     serviceDoctorMustJSON(postChecks),
		ResultsJSON:    serviceDoctorMustJSON(plan.ValidationResults),
	}); err != nil {
		_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "apply_state_unknown", rollbackID, err.Error(), true, serviceDoctorMustJSON(req.Approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor checkpoint persistence failed", err)
		return
	}
	approval := req.Approval
	if strings.TrimSpace(approval.PlanID) == "" {
		approval.PlanID = plan.ID
	}
	if strings.TrimSpace(approval.Approver) == "" {
		approval.Approver = s.serviceDoctorActor(r)
	}
	if approval.Approved && strings.TrimSpace(approval.AuthMethod) == "" {
		approval.AuthMethod = serviceAuthIdentityFromContext(r.Context()).Kind
	}
	if approval.Approved && approval.ApprovedAtUnixMs <= 0 {
		approval.ApprovedAtUnixMs = db.NowMS()
	}
	if err := store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "applied", rollbackID, "", true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt); err != nil {
		_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "apply_state_unknown", rollbackID, err.Error(), true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan status update failed", err)
		return
	}
	planJSON := serviceDoctorMustJSON(plan)
	if _, err := store.SQL.ExecContext(r.Context(), `UPDATE settings_change_plans SET plan_json=? WHERE id=?`, planJSON, plan.ID); err != nil {
		_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "apply_state_unknown", rollbackID, err.Error(), true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan update failed", err)
		return
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	_ = s.recordDoctorAudit(r.Context(), identity, "doctor.checkpoint.created", map[string]any{
		"checkpoint_id":    checkpointID,
		"plan_id":          plan.ID,
		"checks":           postChecks,
		"created_at":       appliedAt,
		"actor":            serviceFirstNonEmpty(identity.Actor, s.serviceDoctorActor(r)),
		"post_check_count": len(postChecks),
	})
	_ = s.recordDoctorAudit(r.Context(), identity, "doctor.plan.applied", serviceDoctorAuditPlanPayload(plan, identity, approval, map[string]any{
		"rollback_id":        rollbackID,
		"rollback_safe":      safeRollback,
		"post_check_pending": true,
		"live_reloaded":      state.LiveReloadKeys,
		"config_path":        configPath,
		"applied_at":         appliedAt,
	}))
	response := map[string]any{
		"ok":                 true,
		"plan_id":            plan.ID,
		"rollback_id":        rollbackID,
		"restart_required":   plan.RestartRequired,
		"post_check_pending": true,
		"post_check_ids":     []string{checkpointID},
		"live_reloaded":      state.LiveReloadKeys,
		"config_path":        configPath,
		"post_restart_recovery": map[string]any{
			"resume_endpoint": "/internal/v1/doctor/plans/" + plan.ID + "/post-checks",
			"readiness_hint":  "After reconnect, poll /internal/v1/readiness and then resume this plan's post-checks.",
		},
	}
	if plan.RestartRequired {
		restartResponse, restartStatus, restartErr := s.startRestartServiceAction(r.Context(), r)
		response["restart_preview"] = s.restartActionDescriptor()
		response["restart_status"] = restartResponse.Status
		response["restart_action"] = restartResponse
		if restartResponse.OperationID != "" {
			response["operation_id"] = restartResponse.OperationID
		}
		if restartResponse.LogPath != "" {
			response["log_path"] = restartResponse.LogPath
		}
		switch {
		case restartErr != nil:
			message := restartErr.Error()
			response["restart_error"] = message
			response["manual_recovery"] = "The config was saved, but the service restart did not start. Restart OR3 manually, then run post-checks from the Doctor plan card."
			_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "restart_start_failed", rollbackID, message, true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
			_ = s.recordDoctorAudit(r.Context(), identity, "doctor.restart.failed", map[string]any{"plan_id": plan.ID, "status": restartResponse.Status, "error": message, "failed_at": db.NowMS()})
		case restartStatus == http.StatusConflict && restartResponse.Status == "approval_required":
			response["approval_state"] = "restart_approval_required"
			response["manual_recovery"] = "Approve the restart request or restart OR3 manually, then run post-checks from the Doctor plan card."
			_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "restart_approval_required", rollbackID, "", true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
			_ = s.recordDoctorAudit(r.Context(), identity, "doctor.restart.approval_required", map[string]any{"plan_id": plan.ID, "approval_id": restartResponse.ApprovalID, "created_at": db.NowMS()})
		case restartStatus == http.StatusAccepted:
			response["restart_requested"] = true
			response["approval_state"] = "restart_requested"
			_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "restart_pending", rollbackID, "", true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
			_ = s.recordDoctorAudit(r.Context(), identity, "doctor.restart.requested", map[string]any{"plan_id": plan.ID, "operation_id": restartResponse.OperationID, "log_path": restartResponse.LogPath, "requested_at": db.NowMS()})
		default:
			response["manual_recovery"] = "The config was saved, but automatic restart is unavailable. Restart OR3 manually, then run post-checks from the Doctor plan card."
			_ = store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "restart_start_failed", rollbackID, restartResponse.Message, true, serviceDoctorMustJSON(approval), serviceDoctorMustJSON(state.LiveReloadKeys), appliedAt)
		}
	}
	writeServiceValue(w, http.StatusOK, response)
}

func (s *serviceServer) handleDoctorPlanRollback(w http.ResponseWriter, r *http.Request, planID string) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	planRecord, plan, ok := s.loadDoctorPlan(r.Context(), planID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
		return
	}
	if strings.TrimSpace(planRecord.Status) == "rolled_back" {
		writeServiceValue(w, http.StatusOK, map[string]any{"ok": true, "rolled_back": true, "rollback_id": planRecord.RollbackID, "plan_id": plan.ID, "already_rolled_back": true})
		return
	}
	if !serviceDoctorPlanStatusAllowsRollback(planRecord.Status) {
		writeServiceJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("plan cannot be rolled back from status %q", planRecord.Status), "status": planRecord.Status})
		return
	}
	rollbackID := strings.TrimSpace(planRecord.RollbackID)
	if rollbackID == "" {
		writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "rollback is not available for this plan"})
		return
	}
	rollbackRecord, ok, err := store.GetSettingsChangeRollback(r.Context(), rollbackID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "rollback lookup failed", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "rollback not found"})
		return
	}
	if strings.TrimSpace(rollbackRecord.Status) == "applied" {
		writeServiceValue(w, http.StatusOK, map[string]any{"ok": true, "rolled_back": true, "rollback_id": rollbackID, "plan_id": plan.ID, "already_rolled_back": true})
		return
	}
	if !serviceDoctorRollbackIsAutomatic(plan) {
		writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "rollback requires manual recovery", "rollback": json.RawMessage(rollbackRecord.RollbackJSON)})
		return
	}
	limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
	var req serviceDoctorPlanApplyRequest
	if err := decodeServiceRequestBody(r.Body, &req); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	reverse := reverseDoctorPlan(plan)
	state, err := (adminflow.PlanValidator{}).Stage(s.config, &reverse, adminflow.ValidationOptions{ApprovedAuthority: serviceDoctorApprovedAuthority(r.Context())})
	if err != nil {
		status := http.StatusBadRequest
		if err == adminflow.ErrStalePlan {
			status = http.StatusConflict
		}
		writeServiceValue(w, status, map[string]any{"error": err.Error(), "plan": reverse, "validation": state.Validation})
		return
	}
	if err := validateDoctorApprovalForPlan(r.Context(), reverse, req.Approval); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	configPath := s.configPath
	if strings.TrimSpace(configPath) == "" {
		configPath = cfgPathOrDefault("")
	}
	if err := config.Save(configPath, state.StagedConfig); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	s.applyLiveConfig(state.StagedConfig)
	appliedAt := db.NowMS()
	if err := store.UpdateSettingsChangeRollbackStatus(r.Context(), rollbackID, "applied", "", appliedAt); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "rollback status update failed", err)
		return
	}
	if err := store.UpdateSettingsChangePlanStatus(r.Context(), plan.ID, "rolled_back", rollbackID, "", false, planRecord.ApprovalJSON, planRecord.LiveReloadJSON, appliedAt); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan status update failed", err)
		return
	}
	_ = s.recordDoctorAudit(r.Context(), serviceAuthIdentityFromContext(r.Context()), "doctor.plan.rollback", serviceDoctorAuditPlanPayload(plan, serviceAuthIdentityFromContext(r.Context()), adminflow.ApprovalContext{}, map[string]any{"rollback_id": rollbackID, "rolled_back_at": appliedAt}))
	writeServiceValue(w, http.StatusOK, map[string]any{"ok": true, "rolled_back": true, "rollback_id": rollbackID, "plan_id": plan.ID, "restart_required": reverse.RestartRequired})
}

func (s *serviceServer) handleDoctorPlanPostChecks(w http.ResponseWriter, r *http.Request, planID string) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	record, _, ok := s.loadDoctorPlan(r.Context(), planID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "plan not found"})
		return
	}
	checkpoint, ok, err := store.GetLatestDoctorCheckpointForPlan(r.Context(), planID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor checkpoint lookup failed", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "post-check checkpoint not found"})
		return
	}
	checks := []adminflow.PostApplyCheck{}
	if err := json.Unmarshal([]byte(checkpoint.ChecksJSON), &checks); err != nil || len(checks) == 0 {
		checks = []adminflow.PostApplyCheck{{ID: "doctor.configure_post_save", Description: "Re-run Doctor post-save checks", Timeout: 10}}
	}
	results, status, report := s.executeDoctorPostChecks(r.Context(), checks)
	if err := store.UpdateDoctorCheckpoint(r.Context(), checkpoint.ID, status, serviceDoctorMustJSON(results)); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor checkpoint update failed", err)
		return
	}
	planStatus := "post_checked"
	if status == "failed" {
		planStatus = "post_check_failed"
	}
	if err := store.UpdateSettingsChangePlanStatus(r.Context(), planID, planStatus, record.RollbackID, "", false, record.ApprovalJSON, record.LiveReloadJSON, record.AppliedAt); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "plan status update failed", err)
		return
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	_ = s.recordDoctorAudit(r.Context(), identity, "doctor.checkpoint.completed", map[string]any{"plan_id": planID, "checkpoint_id": checkpoint.ID, "status": status, "results": results, "completed_at": db.NowMS()})
	_ = s.recordDoctorAudit(r.Context(), identity, "doctor.post_check.completed", map[string]any{"plan_id": planID, "checkpoint_id": checkpoint.ID, "status": status, "results": results, "completed_at": db.NowMS()})
	response := map[string]any{"checkpoint_id": checkpoint.ID, "status": status, "results": results}
	if report != nil {
		response["doctor_report"] = report
	}
	writeServiceValue(w, http.StatusOK, response)
}

func (s *serviceServer) handleDoctorSessionCreate(w http.ResponseWriter, r *http.Request) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
	var req serviceDoctorSessionCreateRequest
	if err := decodeServiceRequestBody(r.Body, &req); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		sessionKey = newDoctorID("doctor-session")
	} else if !isStrongDoctorSessionKey(sessionKey) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is too weak; omit it to create a new session or use a server-issued key"})
		return
	} else {
		doctorLogWeakSessionKeyWarning(sessionKey)
	}
	adminBrain := s.detectAdminBrainProvider(r.Context())
	runnerID := strings.TrimSpace(req.RunnerID)
	if adminBrain.Kind == adminflow.AdminBrainAPIKeyProvider && doctorUsesRunnerChat(runnerID) {
		runnerID = string(agentcli.RunnerOR3)
	}
	if runnerID == "" {
		runnerID = adminBrain.RunnerID
	}
	if runnerID == "" && adminBrain.Kind == adminflow.AdminBrainAPIKeyProvider {
		runnerID = string(agentcli.RunnerOR3)
	}
	meta := db.ChatSessionMeta{
		SessionKey:  sessionKey,
		Title:       serviceFirstNonEmpty(req.Title, "Doctor Session"),
		RunnerID:    runnerID,
		RunnerLabel: serviceFirstNonEmpty(adminBrain.DisplayName, runnerID),
		RunnerModel: strings.TrimSpace(req.Model),
	}
	if strings.TrimSpace(meta.RunnerLabel) == "" {
		meta.RunnerLabel = "Admin Brain"
	}
	if doctorUsesRunnerChat(meta.RunnerID) && adminBrain.Kind == adminflow.AdminBrainRunner {
		var syncErr error
		meta, syncErr = s.syncDoctorSessionRunnerMeta(r.Context(), meta, meta.RunnerID, meta.RunnerModel, adminBrain)
		if syncErr != nil {
			writeServiceError(w, r, http.StatusBadGateway, "admin brain runner session setup failed", syncErr)
			return
		}
	} else {
		var err error
		meta, err = store.UpsertChatSessionMeta(r.Context(), meta)
		if err != nil {
			writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session create failed", err)
			return
		}
	}
	writeServiceValue(w, http.StatusCreated, map[string]any{"session": meta, "admin_brain": adminBrain})
}

func (s *serviceServer) handleDoctorSessions(w http.ResponseWriter, r *http.Request, rel string) {
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor session not found"})
		return
	}
	sessionKey := parts[0]
	tail := ""
	if len(parts) > 1 {
		tail = parts[1]
	}
	switch tail {
	case "":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorSessionRead(w, r, sessionKey)
	case "events":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorSessionEvents(w, r, sessionKey)
	case "messages":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorSessionMessage(w, r, sessionKey)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor session route not found"})
	}
}

func (s *serviceServer) handleDoctorSessionRead(w http.ResponseWriter, r *http.Request, sessionKey string) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	meta, err := store.GetChatSessionMeta(r.Context(), sessionKey)
	if err != nil {
		if err == db.ErrChatSessionNotFound {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor session not found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session lookup failed", err)
		return
	}
	messages, err := s.listDoctorSessionMessages(r.Context(), sessionKey)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session messages unavailable", err)
		return
	}
	writeDoctorSessionPayload(w, http.StatusOK, doctorSessionMessageResponse(messages, s.detectAdminBrainProvider(r.Context()), "sync", map[string]any{"session": meta}))
}

func (s *serviceServer) handleDoctorSessionEvents(w http.ResponseWriter, r *http.Request, sessionKey string) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	afterID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_id")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid after_id"})
			return
		}
		afterID = n
	}
	page, err := store.ListChatMessages(r.Context(), sessionKey, afterID, 100)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session events unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"events": page.Messages, "next_cursor": page.NextCursor})
}

func (s *serviceServer) handleDoctorSessionMessage(w http.ResponseWriter, r *http.Request, sessionKey string) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
	var req serviceDoctorSessionMessageRequest
	if err := decodeServiceRequestBody(r.Body, &req); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "content required"})
		return
	}
	if containsDoctorAdminBrainEnvelope(content) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "message must not include admin brain prompt context",
			"code":    "doctor_prompt_leak",
			"message": "Send only your question, not the internal admin prompt envelope.",
		})
		return
	}
	meta, err := store.GetChatSessionMeta(r.Context(), sessionKey)
	if err != nil {
		if err == db.ErrChatSessionNotFound {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor session not found"})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session lookup failed", err)
		return
	}
	adminBrain := s.detectAdminBrainProvider(r.Context())
	if runnerID := strings.TrimSpace(req.RunnerID); runnerID != "" || strings.TrimSpace(req.Model) != "" {
		meta, err = s.syncDoctorSessionRunnerMeta(r.Context(), meta, runnerID, strings.TrimSpace(req.Model), adminBrain)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "doctor session runner update failed", err)
			return
		}
	}
	useInternalAdminBrain := doctorShouldUseInternalAdminBrain(meta, adminBrain)
	if !useInternalAdminBrain && strings.TrimSpace(meta.RunnerChatSessionID) != "" && s.chatManager != nil && s.chatManager.DB != nil && s.chatManager.Manager != nil {
		releaseTurn, turnErr := s.claimDoctorSessionTurn(sessionKey, "runner_chat", meta.RunnerChatSessionID)
		if turnErr != nil {
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": turnErr.Error(), "code": "doctor_turn_active"})
			return
		}
		defer releaseTurn()
		turnMeta := map[string]any{
			"doctor_session":       true,
			"doctor_user_message":  content,
			"doctor_untrusted":     true,
			"doctor_tool_policy":   doctorAdminBrainToolPolicyName,
			"doctor_allowed_tools": doctorAdminBrainAllowedTools(s.runtime.Tools),
		}
		if thinking := strings.ToLower(strings.TrimSpace(req.ThinkingLevel)); thinking != "" {
			turnMeta["runner_thinking_level"] = thinking
		}
		allowedTools := doctorAdminBrainAllowedTools(s.runtime.Tools)
		result, err := s.chatManager.StartTurn(r.Context(), meta.RunnerChatSessionID, agentcli.StartTurnRequest{
			UserMessage:    content,
			PromptMessage:  s.buildDoctorAdminBrainEnvelope(r.Context(), content),
			Mode:           string(agentcli.RunnerModeReview),
			Isolation:      string(agentcli.IsolationHostReadOnly),
			Model:          strings.TrimSpace(req.Model),
			MaxTurns:       4,
			TimeoutSeconds: 120,
			AllowedTools:   allowedTools,
			RestrictTools:  true,
			ApprovalToken:  serviceApprovalTokenFromRequest(r),
			Meta:           turnMeta,
		})
		if err != nil {
			writeDoctorAdminBrainTurnError(w, r, err)
			return
		}
		messages, err := s.listDoctorSessionMessages(r.Context(), sessionKey)
		if err != nil {
			writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session messages unavailable", err)
			return
		}
		writeDoctorSessionPayload(w, http.StatusAccepted, doctorSessionMessageResponse(messages, adminBrain, "runner_chat", map[string]any{
			"runner_chat": map[string]any{"session_id": result.Session.ID, "turn_id": result.Turn.ID, "job_id": result.JobID},
		}))
		return
	}
	if useInternalAdminBrain {
		userSeq := s.nextDoctorMessageSequence(r.Context(), sessionKey)
		if _, err := store.AppendMessage(r.Context(), sessionKey, "user", content, doctorMessagePayload("doctor", userSeq, nil)); err != nil {
			writeServiceError(w, r, http.StatusServiceUnavailable, "doctor user message persistence failed", err)
			return
		}
		if req.Stream {
			jobID, err := s.startDoctorInternalAdminBrainTurn(r.Context(), sessionKey, content, strings.TrimSpace(req.Model), serviceApprovalTokenFromRequest(r), serviceAuthIdentityFromContext(r.Context()))
			if err != nil {
				writeDoctorAdminBrainTurnError(w, r, err)
				return
			}
			messages, err := s.listDoctorSessionMessages(r.Context(), sessionKey)
			if err != nil {
				writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session messages unavailable", err)
				return
			}
			writeDoctorSessionPayload(w, http.StatusAccepted, doctorSessionMessageResponse(messages, adminBrain, "job", map[string]any{"job_id": jobID}))
			return
		}
		releaseTurn, turnErr := s.claimDoctorSessionTurn(sessionKey, "sync", "internal")
		if turnErr != nil {
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": turnErr.Error(), "code": "doctor_turn_active"})
			return
		}
		defer releaseTurn()
		if err := s.runDoctorInternalAdminBrainTurn(r.Context(), sessionKey, content, strings.TrimSpace(req.Model), serviceApprovalTokenFromRequest(r), serviceAuthIdentityFromContext(r.Context())); err != nil {
			writeDoctorAdminBrainTurnError(w, r, err)
			return
		}
		messages, err := s.listDoctorSessionMessages(r.Context(), sessionKey)
		if err != nil {
			writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session messages unavailable", err)
			return
		}
		writeDoctorSessionPayload(w, http.StatusAccepted, doctorSessionMessageResponse(messages, adminBrain, "sync", nil))
		return
	}
	userSeq := s.nextDoctorMessageSequence(r.Context(), sessionKey)
	if _, err := store.AppendMessage(r.Context(), sessionKey, "user", content, doctorMessagePayload("doctor", userSeq, nil)); err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor user message persistence failed", err)
		return
	}
	messages, err := s.listDoctorSessionMessages(r.Context(), sessionKey)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor session messages unavailable", err)
		return
	}
	reason := strings.TrimSpace(adminBrain.Reason)
	if reason == "" {
		reason = "Admin Brain is not available. Basic Doctor checks still work from the health snapshot."
	}
	writeDoctorSessionPayload(w, http.StatusServiceUnavailable, doctorSessionMessageResponse(messages, adminBrain, "unavailable", map[string]any{
		"error":   reason,
		"message": reason,
		"code":    "admin_brain_unavailable",
	}))
}

func writeDoctorAdminBrainTurnError(w http.ResponseWriter, r *http.Request, err error) {
	var approvalErr *tools.ApprovalRequiredError
	switch {
	case errors.As(err, &approvalErr):
		writeServiceJSON(w, http.StatusConflict, map[string]any{
			"error":       err.Error(),
			"message":     err.Error(),
			"code":        "approval_required",
			"status":      "approval_required",
			"approval_id": approvalErr.RequestID,
			"request_id":  approvalErr.RequestID,
		})
	case errors.Is(err, db.ErrRunnerChatTurnActive):
		writeServiceJSON(w, http.StatusConflict, map[string]any{
			"error":   "another Admin Brain reply is already running for this session",
			"message": "another Admin Brain reply is already running for this session",
			"code":    "runner_chat_turn_active",
		})
	case errors.Is(err, agentcli.ErrUnsupportedNativeSession):
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "Admin Brain native continuation is not supported by this runner",
			"message": "Admin Brain native continuation is not supported by this runner",
			"code":    "unsupported_native_session",
		})
	case errors.Is(err, db.ErrRunnerChatSessionNotFound):
		writeServiceJSON(w, http.StatusNotFound, map[string]any{
			"error":   "Admin Brain chat session not found; clear the conversation and try again",
			"message": "Admin Brain chat session not found; clear the conversation and try again",
			"code":    "runner_chat_session_not_found",
		})
	default:
		copy := uxcopy.TranslateError(err)
		payload := serviceErrorPayload(r, "doctor admin brain turn failed")
		if message := strings.TrimSpace(copy.WhatHappened); message != "" {
			payload["message"] = message
		} else if title := strings.TrimSpace(copy.Title); title != "" {
			payload["message"] = title
		}
		if title := strings.TrimSpace(copy.Title); title != "" {
			payload["title"] = title
		}
		if fix := strings.TrimSpace(copy.Fix); fix != "" {
			payload["fix"] = fix
		}
		if command := strings.TrimSpace(copy.Command); command != "" {
			payload["command"] = command
		}
		if code := agent.PublicErrorCode(err); code != "" {
			payload["code"] = code
		}
		writeServiceJSON(w, http.StatusBadGateway, payload)
	}
}

func (s *serviceServer) buildDoctorStatusResponse(r *http.Request, report doctor.Report) map[string]any {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	inventory := s.serviceSkillsInventory(ctx, s.config)
	recentLogs := []db.DiagnosticLogEvent{}
	if store := s.doctorDB(); store != nil {
		if items, err := store.QueryDiagnosticLogEvents(ctx, db.DiagnosticLogQuery{Limit: 25}); err == nil {
			recentLogs = items
		}
	}
	health := s.control().GetHealth()
	readiness := s.control().GetReadiness()
	var bootstrap any
	if r != nil {
		bootstrap = s.buildAppBootstrap(r)
	}
	return map[string]any{
		"basic_doctor_available": true,
		"admin_brain":            s.detectAdminBrainProvider(ctx),
		"health":                 health,
		"readiness":              readiness,
		"app_bootstrap":          bootstrap,
		"report":                 report,
		"finding_cards":          serviceDoctorFindingCards(report.Findings),
		"skills": map[string]any{
			"count": len(inventory.Skills),
			"items": serviceSkillItems(inventory, s.config.Skills),
		},
		"recent_logs":      recentLogs,
		"pending_recovery": s.buildDoctorPendingRecovery(ctx),
	}
}

func serviceDoctorFindingCards(findings []doctor.Finding) []map[string]any {
	items := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		risk := serviceDoctorRiskFromSeverity(finding.Severity)
		items = append(items, map[string]any{
			"id":               finding.ID,
			"what_i_found":     finding.Summary,
			"what_this_means":  serviceFirstNonEmpty(strings.TrimSpace(finding.Detail), finding.Summary),
			"recommended_fix":  strings.TrimSpace(finding.FixHint),
			"risk_level":       risk,
			"approval_needed":  risk == configmeta.RiskWarning || risk == configmeta.RiskDanger,
			"restart_needed":   false,
			"advanced_details": finding,
		})
	}
	return items
}

func serviceDoctorRiskFromSeverity(severity doctor.Severity) configmeta.RiskLevel {
	switch severity {
	case doctor.SeverityInfo:
		return configmeta.RiskSafe
	case doctor.SeverityWarn:
		return configmeta.RiskNotice
	case doctor.SeverityError:
		return configmeta.RiskWarning
	case doctor.SeverityBlock:
		return configmeta.RiskDanger
	default:
		return configmeta.RiskNotice
	}
}

func (s *serviceServer) detectAdminBrainProvider(ctx context.Context) adminflow.AdminBrainProvider {
	runners := []agentcli.RunnerInfo{}
	if appSvc := s.app(); appSvc != nil {
		if detected, err := appSvc.DetectAgentCLIRunners(ctx); err == nil {
			runners = detected
		}
	}
	provider := adminflow.DetectAdminBrainProvider(s.config, runners)
	return provider
}

func (s *serviceServer) buildDoctorAssistantReply(ctx context.Context, message string) string {
	status := s.buildDoctorStatusResponse((&http.Request{}).WithContext(ctx), doctor.Evaluate(s.config, doctor.Options{Mode: doctor.ModeAdvisory}))
	adminBrain, _ := status["admin_brain"].(adminflow.AdminBrainProvider)
	report, _ := status["report"].(doctor.Report)
	var b strings.Builder
	b.WriteString("Basic Doctor reviewed the current system state.")
	if strings.TrimSpace(message) != "" {
		b.WriteString(" ")
		b.WriteString("Your note was recorded: ")
		b.WriteString(strings.TrimSpace(message))
		b.WriteString(".")
	}
	b.WriteString(" ")
	b.WriteString(fmt.Sprintf("Findings: %d warning/error/block items, %d informational items.", report.Summary.WarnCount+report.Summary.ErrorCount+report.Summary.BlockCount, report.Summary.InfoCount))
	if adminBrain.Available {
		b.WriteString(" Admin Brain is available for deeper repair planning.")
	} else if strings.TrimSpace(adminBrain.Reason) != "" {
		b.WriteString(" ")
		b.WriteString(adminBrain.Reason)
	}
	if len(report.Findings) > 0 {
		b.WriteString(" Top finding: ")
		b.WriteString(report.Findings[0].Summary)
		b.WriteString(".")
	}
	return b.String()
}

func (s *serviceServer) doctorDB() *db.DB {
	if s != nil && s.runtime != nil && s.runtime.DB != nil {
		return s.runtime.DB
	}
	if s != nil {
		if ctrl := s.control(); ctrl != nil {
			return ctrl.DB
		}
	}
	return nil
}

func (s *serviceServer) loadDoctorPlan(ctx context.Context, planID string) (db.SettingsChangePlanRecord, adminflow.SettingsChangePlan, bool) {
	store := s.doctorDB()
	if store == nil {
		return db.SettingsChangePlanRecord{}, adminflow.SettingsChangePlan{}, false
	}
	record, ok, err := store.GetSettingsChangePlan(ctx, planID)
	if err != nil || !ok {
		return db.SettingsChangePlanRecord{}, adminflow.SettingsChangePlan{}, false
	}
	var plan adminflow.SettingsChangePlan
	if jsonErr := json.Unmarshal([]byte(record.PlanJSON), &plan); jsonErr != nil {
		return db.SettingsChangePlanRecord{}, adminflow.SettingsChangePlan{}, false
	}
	return record, plan, true
}

func (s *serviceServer) serviceDoctorActor(r *http.Request) string {
	identity := serviceAuthIdentityFromContext(r.Context())
	if strings.TrimSpace(identity.Actor) != "" {
		return identity.Actor
	}
	return "doctor"
}

func (s *serviceServer) appendDoctorLog(ctx context.Context, event db.DiagnosticLogEvent) error {
	store := s.doctorDB()
	if store == nil {
		return nil
	}
	event = diagnosticlog.NewEvent(event.Source, event.Level, event.CorrelationID, event.EventType, event.Payload)
	if err := store.AppendDiagnosticLogEvent(ctx, event); err != nil {
		return err
	}
	_ = s.recordDoctorAudit(ctx, serviceAuthIdentity{}, "doctor.log.appended", map[string]any{
		"source":         strings.TrimSpace(event.Source),
		"level":          strings.TrimSpace(event.Level),
		"correlation_id": strings.TrimSpace(event.CorrelationID),
		"event_type":     strings.TrimSpace(event.EventType),
		"payload":        serviceDoctorAuditLogPayload(event.Payload),
		"created_at":     db.NowMS(),
	})
	return nil
}

func parseOptionalInt64Query(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid " + key})
		return 0, false
	}
	return n, true
}

func (s *serviceServer) buildDoctorPendingRecovery(ctx context.Context) map[string]any {
	store := s.doctorDB()
	if store == nil {
		return map[string]any{"plans": []any{}}
	}
	records, err := store.ListPendingSettingsChangePlans(ctx, 25)
	if err != nil {
		return map[string]any{"plans": []any{}, "error": "pending recovery unavailable"}
	}
	plans := make([]map[string]any, 0, len(records))
	for _, record := range records {
		item := map[string]any{
			"plan_id":            record.ID,
			"status":             record.Status,
			"rollback_id":        record.RollbackID,
			"post_check_pending": record.PostCheckPending,
			"error":              record.ErrorText,
			"updated_at":         record.UpdatedAt,
			"applied_at":         record.AppliedAt,
		}
		if checkpoint, ok, err := store.GetLatestDoctorCheckpointForPlan(ctx, record.ID); err == nil && ok {
			item["checkpoint_id"] = checkpoint.ID
			item["checkpoint_status"] = checkpoint.Status
		}
		plans = append(plans, item)
	}
	return map[string]any{"plans": plans}
}

func (s *serviceServer) recordDoctorAudit(ctx context.Context, identity serviceAuthIdentity, eventType string, payload any) error {
	if s == nil || s.runtime == nil || s.runtime.Audit == nil {
		return nil
	}
	actor := strings.TrimSpace(identity.Actor)
	if actor == "" {
		actor = "doctor"
	}
	return s.runtime.Audit.Record(ctx, eventType, strings.TrimSpace(identity.Session), actor, payload)
}

func (s *serviceServer) executeDoctorPostChecks(ctx context.Context, checks []adminflow.PostApplyCheck) ([]adminflow.PlanValidationResult, string, *doctor.Report) {
	results := make([]adminflow.PlanValidationResult, 0, len(checks))
	status := "passed"
	var report *doctor.Report
	for _, check := range checks {
		result, checkReport := s.executeDoctorPostCheck(ctx, check)
		results = append(results, result)
		if report == nil && checkReport != nil {
			copy := *checkReport
			report = &copy
		}
		switch result.Status {
		case "fail":
			status = "failed"
		case "warning":
			if status == "passed" {
				status = "warning"
			}
		}
	}
	if len(results) == 0 {
		return []adminflow.PlanValidationResult{{Check: "doctor.configure_post_save", Status: "warning", Message: "no post-apply checks configured"}}, "warning", nil
	}
	return results, status, report
}

func (s *serviceServer) executeDoctorPostCheck(ctx context.Context, check adminflow.PostApplyCheck) (adminflow.PlanValidationResult, *doctor.Report) {
	checkID := strings.TrimSpace(check.ID)
	if checkID == "" {
		checkID = "doctor.configure_post_save"
	}
	timeoutSeconds := check.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	result, report := s.executeDoctorPostCheckNow(checkCtx, checkID)
	if checkCtx.Err() == context.DeadlineExceeded {
		return adminflow.PlanValidationResult{Check: checkID, Status: "fail", Message: fmt.Sprintf("timed out after %d seconds", timeoutSeconds)}, nil
	}
	if err := checkCtx.Err(); err != nil {
		return adminflow.PlanValidationResult{Check: checkID, Status: "fail", Message: err.Error()}, nil
	}
	return result, report
}

func (s *serviceServer) executeDoctorPostCheckNow(ctx context.Context, checkID string) (adminflow.PlanValidationResult, *doctor.Report) {
	switch checkID {
	case "doctor.configure_post_save":
		report := doctor.Evaluate(s.config, doctor.Options{Mode: doctor.ModeConfigurePostSave})
		result := adminflow.PlanValidationResult{Check: checkID, Status: "pass"}
		if report.Summary.BlockCount > 0 {
			result.Status = "fail"
			result.Message = "doctor reported blocking findings"
		}
		return result, &report
	case "config.validate":
		if err := config.ValidateSnapshot(s.config); err != nil {
			return adminflow.PlanValidationResult{Check: checkID, Status: "fail", Message: err.Error()}, nil
		}
		return adminflow.PlanValidationResult{Check: checkID, Status: "pass"}, nil
	default:
		return adminflow.PlanValidationResult{Check: checkID, Status: "fail", Message: "post-apply check is not supported by the service"}, nil
	}
}

func serviceDoctorAuditPlanPayload(plan adminflow.SettingsChangePlan, identity serviceAuthIdentity, approval adminflow.ApprovalContext, extra map[string]any) map[string]any {
	redactedPlan := serviceDoctorRedactedPlanForAudit(plan)
	payload := map[string]any{
		"plan_id":               redactedPlan.ID,
		"title":                 redactedPlan.Title,
		"summary":               redactedPlan.Summary,
		"requester":             serviceFirstNonEmpty(redactedPlan.CreatedBy, identity.Actor, "doctor"),
		"approver":              strings.TrimSpace(approval.Approver),
		"auth_method":           serviceFirstNonEmpty(approval.AuthMethod, identity.Kind),
		"risk_level":            redactedPlan.RiskLevel,
		"restart_required":      redactedPlan.RestartRequired,
		"requires_approval":     redactedPlan.RequiresApproval,
		"requires_step_up_auth": redactedPlan.RequiresStepUpAuth,
		"rollback_available":    redactedPlan.RollbackPlan.Available,
		"rollback_safe":         redactedPlan.RollbackPlan.Safe,
		"post_check_count":      len(redactedPlan.PostApplyChecks),
		"changes":               redactedPlan.Changes,
		"timestamp_ms":          db.NowMS(),
	}
	if approval.Approved {
		payload["approved"] = true
		payload["approved_at"] = approval.ApprovedAtUnixMs
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func serviceDoctorRedactedPlanForAudit(plan adminflow.SettingsChangePlan) adminflow.SettingsChangePlan {
	redacted := plan
	redacted.Changes = append([]adminflow.SettingsPlanChange{}, plan.Changes...)
	for i := range redacted.Changes {
		change := &redacted.Changes[i]
		if serviceDoctorChangeLooksSecret(*change) {
			change.OldValue = serviceDoctorRedactPlanValue(change.OldValue)
			change.NewValue = serviceDoctorRedactPlanValue(change.NewValue)
		}
	}
	return redacted
}

func serviceDoctorRedactPlanValue(value adminflow.RedactedValue) adminflow.RedactedValue {
	if value.Redacted {
		return value
	}
	return adminflow.RedactValue(value.Value, true)
}

func serviceDoctorChangeLooksSecret(change adminflow.SettingsPlanChange) bool {
	if meta, ok := configmeta.GetByPath(change.ConfigPath); ok && meta.Secret {
		return true
	}
	if meta, ok := configmeta.Get(change.Section, change.Field); ok && meta.Secret {
		return true
	}
	field := strings.ToLower(strings.TrimSpace(change.Field + " " + change.ConfigPath))
	for _, hint := range []string{"api_key", "apikey", "token", "secret", "password", "credential"} {
		if strings.Contains(field, hint) {
			return true
		}
	}
	return false
}

func serviceDoctorAuditLogPayload(payload json.RawMessage) any {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err == nil {
		return adminflow.RedactJSON(decoded)
	}
	return adminflow.RedactString(trimmed)
}

func buildServiceDoctorRollbackPlan(plan adminflow.SettingsChangePlan) (adminflow.RollbackPlan, bool) {
	safe := serviceDoctorRollbackIsAutomatic(plan)
	rollback := adminflow.RollbackPlan{Available: len(plan.Changes) > 0, Safe: safe, RestartRequired: plan.RestartRequired}
	if safe {
		rollback.Instructions = "Use the rollback action to restore the previous config values."
		return rollback, true
	}
	rollback.ManualOnly = true
	rollback.Instructions = "This plan changed redacted values; rollback requires manual recovery."
	return rollback, false
}

func serviceDoctorRollbackIsAutomatic(plan adminflow.SettingsChangePlan) bool {
	if len(plan.Changes) == 0 {
		return false
	}
	for _, change := range plan.Changes {
		if change.OldValue.Redacted {
			return false
		}
	}
	return true
}

func reverseDoctorPlan(plan adminflow.SettingsChangePlan) adminflow.SettingsChangePlan {
	reversed := plan
	reversed.ID = newDoctorID("rollback-plan")
	reversed.Title = "Rollback: " + plan.Title
	for i := range reversed.Changes {
		reversed.Changes[i].OldValue, reversed.Changes[i].NewValue = reversed.Changes[i].NewValue, reversed.Changes[i].OldValue
	}
	reversed.ValidationResults = nil
	return reversed
}

func newDoctorID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("doctor: secure id generation failed: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

func serviceDoctorMustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func coalesceRisk(primary, fallback configmeta.RiskLevel) configmeta.RiskLevel {
	if primary != "" {
		return primary
	}
	return fallback
}

func serviceDoctorPlansFromSkillDiag(items []skilldiag.SuggestedPlan) []adminflow.SettingsChangePlan {
	plans := make([]adminflow.SettingsChangePlan, 0, len(items))
	for _, item := range items {
		plan := adminflow.SettingsChangePlan{
			ID:                    item.ID,
			Title:                 item.Title,
			Summary:               item.Summary,
			RiskLevel:             item.RiskLevel,
			RestartRequired:       item.RestartRequired,
			UserFacingExplanation: item.UserFacingSummary,
		}
		for _, change := range item.Changes {
			plan.Changes = append(plan.Changes, adminflow.SettingsPlanChange{
				ConfigPath: change.ConfigPath,
				Section:    change.Section,
				Channel:    change.Channel,
				Field:      change.Field,
				Operation:  change.Operation,
				OldValue:   serviceDoctorRedactedValueFromSuggested(change.OldValue),
				NewValue:   serviceDoctorRedactedValueFromSuggested(change.NewValue),
			})
		}
		for _, check := range item.PostApplyChecks {
			plan.PostApplyChecks = append(plan.PostApplyChecks, adminflow.PostApplyCheck{ID: check.ID, Description: check.Description, Timeout: check.Timeout})
		}
		plans = append(plans, plan)
	}
	return plans
}

func cloneSkillEnv(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func cloneSkillConfig(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func serviceDoctorRedactedValueFromSuggested(value skilldiag.SuggestedValue) adminflow.RedactedValue {
	return adminflow.RedactedValue{
		Value:    value.Value,
		Redacted: value.Redacted,
		Present:  value.Present,
		Summary:  value.Summary,
	}
}
