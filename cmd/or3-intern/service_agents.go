package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

func (s *serviceServer) handleTurns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, serviceTurnsBodyLimit)
	req, err := decodeServiceTurnRequest(r.Body, s.runtime.Tools)
	if err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	writeServiceRequestWarnings(w, req.Warnings)
	if err := validateServiceToolCapabilities(s.runtime.Tools, req.AllowedTools, s.config.Service.MaxCapability); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "requested tools exceed service capability ceiling", err)
		return
	}
	audit := serviceAuditHeadersFromRequest(r)
	req.Meta = mergeServiceAuditMeta(req.Meta, audit)
	if traceID := serviceTraceIDFromContext(r.Context()); traceID != "" {
		if req.Meta == nil {
			req.Meta = map[string]any{}
		}
		if serviceMetaText(req.Meta, "trace_id") == "" {
			req.Meta["trace_id"] = traceID
		}
	}
	req.ApprovalToken = serviceFirstNonEmpty(req.ApprovalToken, serviceApprovalTokenFromRequest(r))
	if req.SessionKey == "" || req.Message == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key and message are required"})
		return
	}
	job := s.jobs.Register("turn")
	log.Printf("service_turn: registered job=%s session=%s trace=%s", job.ID, req.SessionKey, serviceMetaText(req.Meta, "trace_id"))
	w.Header().Set("X-Or3-Job-Id", job.ID)
	s.jobs.Publish(job.ID, "queued", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"status": "queued"}))
	s.persistServiceJobSummary(context.Background(), job.ID)

	ctx, cancel := context.WithCancel(withDetachedContext(r.Context()))
	s.jobs.AttachCancel(job.ID, cancel)
	go s.runTurnJob(ctx, job.ID, req, serviceAuthIdentityFromContext(r.Context()))

	if acceptsSSE(r) {
		s.streamJob(w, r, job.ID)
		return
	}
	snapshot, ok := s.jobs.Wait(r.Context(), job.ID)
	if !ok {
		writeServiceJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "job timed out", "job_id": job.ID})
		return
	}
	statusCode := http.StatusOK
	if snapshot.Status == "failed" {
		statusCode = http.StatusBadGateway
	}
	writeServiceValue(w, statusCode, controlplane.BuildJobResponse(snapshot))
}

func (s *serviceServer) runTurnJob(ctx context.Context, jobID string, req serviceTurnRequest, identity serviceAuthIdentity) {
	defer s.persistServiceJobSummary(context.Background(), jobID)
	log.Printf("service_turn: started job=%s session=%s trace=%s replay=%t", jobID, req.SessionKey, serviceMetaText(req.Meta, "trace_id"), req.ReplayToolCall != nil)
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	profileName := s.effectiveServiceProfileName(req.ProfileName)
	var err error
	if req.ReplayToolCall != nil {
		_, err = s.app().ReplayToolCall(ctx, app.ReplayToolCallRequest{
			SessionKey:    req.SessionKey,
			ToolName:      req.ReplayToolCall.Name,
			ArgumentsJSON: req.ReplayToolCall.ArgumentsJSON,
			AllowedTools:  req.AllowedTools,
			RestrictTools: req.RestrictTools,
			ProfileName:   profileName,
			Capability:    tools.CapabilityLevel(s.config.Service.MaxCapability),
			ApprovalToken: req.ApprovalToken,
			Actor:         identity.Actor,
			Role:          identity.Role,
			Observer:      observer,
		})
	} else {
		err = s.app().RunTurn(ctx, app.TurnRequest{
			SessionKey:    req.SessionKey,
			Message:       req.Message,
			Attachments:   req.Attachments,
			Meta:          req.Meta,
			AllowedTools:  req.AllowedTools,
			RestrictTools: req.RestrictTools,
			ProfileName:   profileName,
			Capability:    tools.CapabilityLevel(s.config.Service.MaxCapability),
			ApprovalToken: req.ApprovalToken,
			Actor:         identity.Actor,
			Role:          identity.Role,
			Observer:      observer,
			Streamer:      agent.NullStreamer{},
		})
	}
	if err != nil {
		log.Printf("service_turn: error job=%s session=%s trace=%s public_code=%s", jobID, req.SessionKey, serviceMetaText(req.Meta, "trace_id"), agent.PublicErrorCode(err))
		s.completeTurnJobWithError(ctx, jobID, err, observer, req.SessionKey, req.Meta)
		return
	}
	finalText, recoveredEmpty := observer.finalTextForCompletion("or3-intern completed without a final response.")
	if recoveredEmpty {
		log.Printf("service_turn: completed_empty_final job=%s session=%s trace=%s saw_tool=%t last_tool=%s last_tool_status=%s", jobID, req.SessionKey, serviceMetaText(req.Meta, "trace_id"), observer.sawToolActivity(), observer.lastToolName, observer.lastToolStatus)
	}
	log.Printf("service_turn: completed job=%s session=%s trace=%s recovered_empty=%t final_preview=%q", jobID, req.SessionKey, serviceMetaText(req.Meta, "trace_id"), recoveredEmpty, boundedServiceLogPreview(finalText, 160))
	payload := map[string]any{"final_text": finalText}
	if recoveredEmpty {
		payload["degraded"] = true
		payload["empty_final_text_recovered"] = true
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(req.SessionKey, req.Meta, payload))
}

func (s *serviceServer) effectiveServiceProfileName(requested string) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested
	}
	if s == nil || !s.config.Security.Profiles.Enabled {
		return ""
	}
	if profileName := strings.TrimSpace(s.config.Security.Profiles.Channels["service"]); profileName != "" {
		return profileName
	}
	return strings.TrimSpace(s.config.Security.Profiles.Default)
}

func (s *serviceServer) startApprovedResumeJob(ctx context.Context, issued approval.IssuedApproval, identity serviceAuthIdentity) (string, error) {
	if s == nil || s.jobs == nil || s.app() == nil {
		return "", nil
	}
	sessionKey := strings.TrimSpace(issued.Request.RequesterSessionID)
	if sessionKey == "" {
		return "", nil
	}
	switch strings.TrimSpace(issued.Request.Type) {
	case string(approval.SubjectExec), string(approval.SubjectSkillExec), string(approval.SubjectToolQuota):
	default:
		return "", nil
	}
	job := s.jobs.Register("turn")
	meta := map[string]any{
		"approval_request_id": issued.Request.ID,
		"approved_resume":     true,
	}
	log.Printf("service_approval: resume_registered approval=%d job=%s session=%s", issued.Request.ID, job.ID, sessionKey)
	s.jobs.Publish(job.ID, "queued", serviceLifecyclePayload(sessionKey, meta, map[string]any{"status": "queued"}))
	s.persistServiceJobSummary(context.Background(), job.ID)
	runCtx, cancel := context.WithCancel(withDetachedContext(ctx))
	s.jobs.AttachCancel(job.ID, cancel)
	go s.runApprovedResumeJob(runCtx, job.ID, issued, identity)
	return job.ID, nil
}

func (s *serviceServer) runApprovedResumeJob(ctx context.Context, jobID string, issued approval.IssuedApproval, identity serviceAuthIdentity) {
	defer s.persistServiceJobSummary(context.Background(), jobID)
	sessionKey := strings.TrimSpace(issued.Request.RequesterSessionID)
	if strings.HasPrefix(sessionKey, "doctor-app-") && strings.TrimSpace(issued.Request.Type) == string(approval.SubjectToolQuota) {
		s.runDoctorApprovedQuotaResumeJob(ctx, jobID, issued, identity)
		return
	}
	meta := map[string]any{
		"approval_request_id": issued.Request.ID,
		"approved_resume":     true,
	}
	log.Printf("service_approval: resume_started approval=%d job=%s session=%s", issued.Request.ID, jobID, sessionKey)
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(sessionKey, meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	finalText, err := s.app().ResumeApprovedRequest(ctx, app.ResumeApprovedRequest{
		IssuedApproval: issued,
		Capability:     tools.CapabilityLevel(s.config.Service.MaxCapability),
		Actor:          identity.Actor,
		Role:           identity.Role,
		Observer:       observer,
	})
	if err != nil {
		log.Printf("service_approval: resume_error approval=%d job=%s session=%s public_code=%s", issued.Request.ID, jobID, sessionKey, agent.PublicErrorCode(err))
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) && s.deliverApprovedResumeApprovalRequired(ctx, issued.Request, approvalErr) {
			s.completeTurnJobWithError(ctx, jobID, err, observer, sessionKey, meta)
			return
		}
		s.deliverApprovedResumeCompletion(ctx, issued.Request, approvalResumeFailureMessage(err))
		s.completeTurnJobWithError(ctx, jobID, err, observer, sessionKey, meta)
		return
	}
	if strings.TrimSpace(observer.finalText) == "" {
		observer.finalText = strings.TrimSpace(finalText)
	}
	completionText, recoveredEmpty := observer.finalTextForCompletion("The approval was accepted, but or3-intern did not return a final response after the resume job. Please retry the command if it still matters.")
	if recoveredEmpty {
		log.Printf("service_approval: resume_completed_empty_final approval=%d job=%s session=%s saw_tool=%t last_tool=%s last_tool_status=%s", issued.Request.ID, jobID, sessionKey, observer.sawToolActivity(), observer.lastToolName, observer.lastToolStatus)
	}
	payload := map[string]any{"final_text": completionText}
	if recoveredEmpty {
		payload["degraded"] = true
		payload["empty_final_text_recovered"] = true
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(sessionKey, meta, payload))
	s.deliverApprovedResumeCompletion(ctx, issued.Request, completionText)
	log.Printf("service_approval: resume_completed approval=%d job=%s session=%s recovered_empty=%t final_preview=%q", issued.Request.ID, jobID, sessionKey, recoveredEmpty, boundedServiceLogPreview(completionText, 160))
}

func (s *serviceServer) deliverApprovedResumeApprovalRequired(ctx context.Context, fallbackReq db.ApprovalRequestRecord, approvalErr *tools.ApprovalRequiredError) bool {
	if s == nil || s.channelDeliverer == nil || approvalErr == nil {
		return false
	}
	req, text := approvalRequiredContinuationPrompt(ctx, s.broker, fallbackReq, approvalErr)
	requester := approval.RequesterContextFromJSON(req.RequesterContextJSON)
	if !isApprovalExternalChannel(requester.Channel) {
		return false
	}
	to := strings.TrimSpace(requester.ReplyTarget)
	if to == "" {
		to = strings.TrimSpace(requester.From)
	}
	if to == "" || strings.TrimSpace(text) == "" {
		return false
	}
	if err := s.channelDeliverer.DeliverWithMeta(ctx, requester.Channel, to, text, approvalDeliveryMeta(requester)); err != nil {
		log.Printf("service_approval: channel_delivery_failed approval=%d channel=%s err=%v", approvalErr.RequestID, requester.Channel, err)
		return false
	}
	return true
}

func (s *serviceServer) deliverApprovedResumeCompletion(ctx context.Context, req db.ApprovalRequestRecord, text string) {
	if s == nil || s.channelDeliverer == nil || strings.TrimSpace(text) == "" {
		return
	}
	requester := approval.RequesterContextFromJSON(req.RequesterContextJSON)
	if !isApprovalExternalChannel(requester.Channel) {
		return
	}
	to := strings.TrimSpace(requester.ReplyTarget)
	if to == "" {
		to = strings.TrimSpace(requester.From)
	}
	if to == "" {
		return
	}
	if err := s.channelDeliverer.DeliverWithMeta(ctx, requester.Channel, to, text, approvalDeliveryMeta(requester)); err != nil {
		log.Printf("service_approval: channel_delivery_failed approval=%d channel=%s err=%v", req.ID, requester.Channel, err)
	}
}

func approvalResumeFailureMessage(err error) string {
	code := agent.PublicErrorCode(err)
	if code == "" {
		code = agent.PublicErrorUnknown
	}
	return "Approval was accepted, but continuing the request failed (" + code + "). Please retry or review it in the OR3 app."
}

func isApprovalExternalChannel(channel string) bool {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "telegram", "discord", "slack", "whatsapp", "email":
		return true
	default:
		return false
	}
}

func approvalDeliveryMeta(requester approval.RequesterContext) map[string]any {
	meta := map[string]any{}
	for key, value := range requester.ReplyMeta {
		meta[key] = value
	}
	if strings.TrimSpace(requester.ReplyTarget) != "" {
		switch strings.ToLower(strings.TrimSpace(requester.Channel)) {
		case "telegram", "whatsapp":
			meta["chat_id"] = requester.ReplyTarget
		case "slack", "discord":
			meta["channel_id"] = requester.ReplyTarget
		case "email":
			meta["sender_email"] = requester.ReplyTarget
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func (s *serviceServer) completeTurnJobWithError(ctx context.Context, jobID string, err error, observer *serviceObserver, sessionKey string, meta map[string]any) {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		s.jobs.Complete(jobID, "aborted", serviceLifecyclePayload(sessionKey, meta, map[string]any{"message": "job aborted"}))
		return
	}
	var approvalErr *tools.ApprovalRequiredError
	if errors.As(err, &approvalErr) {
		log.Printf("service_turn: approval_required job=%s session=%s approval=%d trace=%s", jobID, sessionKey, approvalErr.RequestID, serviceMetaText(meta, "trace_id"))
		s.jobs.Complete(jobID, "approval_required", serviceApprovalRequiredPayload(sessionKey, meta, approvalErr))
		return
	}
	if fallback, ok := serviceTurnFallbackText(err, observer); ok {
		observer.finalText = fallback
		s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(sessionKey, meta, map[string]any{"final_text": fallback, "degraded": true}))
		return
	}
	s.jobs.Fail(jobID, servicePublicJobError(err), serviceLifecyclePayload(sessionKey, meta, nil))
}

func approvalResumeWarning(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "Approved, but the automatic resume did not start."
	}
	return fmt.Sprintf("Approved, but the automatic resume did not start: %s", message)
}

func (s *serviceServer) handleSubagents(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/internal/v1/subagents/") {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		jobID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/internal/v1/subagents/"))
		if jobID == "" || strings.Contains(jobID, "/") {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "subagent job not found"})
			return
		}
		if !s.writePersistedSubagentJobSnapshot(w, r, jobID) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "subagent job not found"})
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleSubagentsList(w, r)
		return
	case http.MethodPost:
		// fall through
	default:
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if s.subagentManager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "subagent manager is not enabled"})
		return
	}
	limitServiceRequestBody(w, r, serviceSubagentsBodyLimit)
	req, err := decodeServiceSubagentRequest(r.Body, backgroundToolsRegistry(s.subagentManager))
	if err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	writeServiceRequestWarnings(w, req.Warnings)
	if err := validateServiceToolCapabilities(backgroundToolsRegistry(s.subagentManager), req.AllowedTools, s.config.Service.MaxCapability); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "requested tools exceed service capability ceiling", err)
		return
	}
	audit := serviceAuditHeadersFromRequest(r)
	req.Meta = mergeServiceAuditMeta(req.Meta, audit)
	req.ApprovalToken = serviceFirstNonEmpty(req.ApprovalToken, serviceApprovalTokenFromRequest(r))
	if req.ParentSessionKey == "" || req.Task == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "parent_session_key and task are required"})
		return
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	job, err := s.app().StartSubagent(r.Context(), app.SubagentRequest{
		ParentSessionKey: req.ParentSessionKey,
		Task:             req.Task,
		PromptSnapshot:   req.PromptSnapshot,
		AllowedTools:     req.AllowedTools,
		RestrictTools:    req.RestrictTools,
		ProfileName:      req.ProfileName,
		Capability:       tools.CapabilityLevel(s.config.Service.MaxCapability),
		Channel:          req.Channel,
		ReplyTo:          req.ReplyTo,
		Meta:             req.Meta,
		Timeout:          time.Duration(req.TimeoutSeconds) * time.Second,
		ApprovalToken:    req.ApprovalToken,
		Actor:            identity.Actor,
		Role:             identity.Role,
	})
	if err != nil {
		statusCode := http.StatusBadGateway
		if errors.Is(err, db.ErrSubagentQueueFull) {
			statusCode = http.StatusTooManyRequests
		}
		writeServiceError(w, r, statusCode, "subagent request failed", err)
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{
		"job_id":            job.ID,
		"child_session_key": job.ChildSessionKey,
		"status":            db.SubagentStatusQueued,
	})
}

func (s *serviceServer) handleSubagentsList(w http.ResponseWriter, r *http.Request) {
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "subagent history is not available"})
		return
	}
	query := r.URL.Query()
	filter := db.SubagentJobFilter{
		Status:           strings.TrimSpace(query.Get("status")),
		ParentSessionKey: strings.TrimSpace(query.Get("parent_session_key")),
	}
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be a positive integer"})
			return
		}
		filter.Limit = parsed
	}
	jobs, err := store.ListSubagentJobs(r.Context(), filter)
	if err != nil {
		if errors.Is(err, db.ErrInvalidSubagentStatusFilter) {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceError(w, r, http.StatusServiceUnavailable, "subagent history unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildSubagentJobListResponse(jobs))
}

func limitServiceRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	if r != nil && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
}

func writeServiceRequestDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeServiceJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request body too large"})
		return
	}
	writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
}

func (s *serviceServer) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleArtifactUpload(w, r)
		return
	case http.MethodGet:
	default:
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/artifacts/")
	artifactID := strings.TrimSpace(strings.Trim(relative, "/"))
	if artifactID == "" || strings.Contains(artifactID, "/") {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
		return
	}
	if s.runtime == nil || s.runtime.Artifacts == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "artifacts unavailable"})
		return
	}
	q := r.URL.Query()
	sessionKey := serviceFirstNonEmpty(q.Get("session_key"), q.Get("sessionKey"))
	if sessionKey == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is required"})
		return
	}
	const defaultMaxBytes int64 = 200_000
	const hardCapBytes int64 = 2_000_000
	maxBytes := defaultMaxBytes
	if raw := strings.TrimSpace(q.Get("max_bytes")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			maxBytes = parsed
		}
	}
	if maxBytes > hardCapBytes {
		maxBytes = hardCapBytes
	}
	var offset int64
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			offset = parsed
		}
	}
	result, err := s.runtime.Artifacts.ReadCappedFrom(r.Context(), sessionKey, artifactID, offset, maxBytes)
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
		case errors.Is(err, artifacts.ErrNotAvailable):
			writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "artifact not available for session"})
		default:
			writeServiceError(w, r, http.StatusInternalServerError, "artifact read failed", err)
		}
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"id":         result.Artifact.ID,
		"mime":       result.Artifact.Mime,
		"size_bytes": result.Artifact.SizeBytes,
		"offset":     offset,
		"read_bytes": result.ReadBytes,
		"truncated":  result.Truncated,
		"content":    result.Content,
	})
}

func (s *serviceServer) handleArtifactUpload(w http.ResponseWriter, r *http.Request) {
	if s.runtime == nil || s.runtime.Artifacts == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "artifacts unavailable"})
		return
	}
	const maxUploadBytes = 8 << 20
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart form"})
		return
	}
	sessionKey := serviceFirstNonEmpty(r.FormValue("session_key"), r.FormValue("sessionKey"))
	if sessionKey == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is required"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "artifact upload read failed", err)
		return
	}
	if len(data) > maxUploadBytes {
		writeServiceJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "file too large"})
		return
	}
	filename := "attachment"
	mimeType := "application/octet-stream"
	if header != nil {
		if name := strings.TrimSpace(header.Filename); name != "" {
			filename = name
		}
		if header.Header != nil {
			if mt := strings.TrimSpace(header.Header.Get("Content-Type")); mt != "" {
				mimeType = mt
			}
		}
	}
	att, err := s.runtime.Artifacts.SaveNamed(r.Context(), sessionKey, filename, mimeType, data)
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "artifact upload failed", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{
		"id":          att.ArtifactID,
		"artifact_id": att.ArtifactID,
		"name":        att.Filename,
		"mime_type":   att.Mime,
		"size_bytes":  att.SizeBytes,
		"kind":        att.Kind,
	})
}

func (s *serviceServer) writePersistedSubagentJobSnapshot(w http.ResponseWriter, r *http.Request, jobID string) bool {
	store := s.control().DB
	if store == nil {
		return false
	}
	job, ok, err := store.GetSubagentJob(r.Context(), jobID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "subagent history unavailable", err)
		return true
	}
	if !ok {
		return false
	}
	response := controlplane.BuildSubagentJobResponse(job)
	if requestedAt, ok := response["requested_at"]; ok {
		response["created_at"] = requestedAt
	}
	if preview := strings.TrimSpace(job.ResultPreview); preview != "" {
		response["final_text"] = preview
	}
	if strings.TrimSpace(job.ArtifactID) == "" {
		if fullText := s.persistedSubagentFinalText(r.Context(), store, job); strings.TrimSpace(fullText) != "" {
			response["final_text"] = fullText
		}
	}
	response["events"] = s.persistedSubagentEvents(r.Context(), store, job)
	writeServiceValue(w, http.StatusOK, response)
	return true
}

func (s *serviceServer) writePersistedAgentCLIRunSnapshot(w http.ResponseWriter, r *http.Request, jobID string) bool {
	store := s.control().DB
	if store == nil {
		return false
	}
	run, ok, err := store.GetAgentCLIRun(r.Context(), jobID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent CLI run history unavailable", err)
		return true
	}
	if !ok {
		return false
	}
	response := controlplane.BuildAgentCLIRunResponse(run)
	if requestedAt, ok := response["requested_at"]; ok {
		response["created_at"] = requestedAt
	}
	events, err := store.ListAgentCLIEvents(r.Context(), run.JobID, 0, 100)
	if err != nil {
		log.Printf("load persisted agent CLI events failed: job=%s err=%v", run.JobID, err)
	}
	response["events"] = s.agentCLIEventsToJobEvents(events)
	writeServiceValue(w, http.StatusOK, response)
	return true
}

func (s *serviceServer) agentCLIEventsToJobEvents(events []db.AgentCLIEvent) []agent.JobEvent {
	out := make([]agent.JobEvent, 0, len(events))
	for _, e := range events {
		payload := map[string]any{
			"type":   e.Type,
			"seq":    e.Seq,
			"stream": e.Stream,
			"chunk":  e.Chunk,
		}
		if e.PayloadJSON != "" {
			var raw map[string]any
			if err := json.Unmarshal([]byte(e.PayloadJSON), &raw); err == nil {
				for k, v := range raw {
					payload[k] = v
				}
			}
		}
		out = append(out, agent.JobEvent{
			Sequence: e.Seq,
			Type:     e.Type,
			Data:     payload,
		})
	}
	return out
}

func (s *serviceServer) persistedSubagentFinalText(ctx context.Context, store *db.DB, job db.SubagentJob) string {
	if store == nil || strings.TrimSpace(job.ChildSessionKey) == "" {
		return ""
	}
	messages, err := store.GetLastMessages(ctx, job.ChildSessionKey, 50)
	if err != nil {
		log.Printf("load persisted subagent final text failed: job=%s err=%v", job.ID, err)
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}

func (s *serviceServer) persistedSubagentEvents(ctx context.Context, store *db.DB, job db.SubagentJob) []agent.JobEvent {
	if store == nil || strings.TrimSpace(job.ChildSessionKey) == "" {
		return []agent.JobEvent{}
	}
	messages, err := store.GetLastMessages(ctx, job.ChildSessionKey, 100)
	if err != nil {
		log.Printf("load persisted subagent events failed: job=%s err=%v", job.ID, err)
		return []agent.JobEvent{}
	}
	events := make([]agent.JobEvent, 0)
	var sequence int64
	emit := func(eventType string, data map[string]any) {
		sequence++
		events = append(events, agent.JobEvent{Sequence: sequence, Type: eventType, Data: data})
	}
	for _, msg := range messages {
		payload := decodeServiceJSONMap(msg.PayloadJSON)
		switch msg.Role {
		case "assistant":
			rawCalls, ok := payload["tool_calls"].([]any)
			if !ok {
				continue
			}
			for _, raw := range rawCalls {
				call, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				function, _ := call["function"].(map[string]any)
				name := serviceJSONText(function, "name")
				if name == "" {
					name = serviceJSONText(call, "name")
				}
				arguments := serviceJSONText(function, "arguments")
				data := map[string]any{"name": name, "arguments": arguments}
				if id := serviceJSONText(call, "id"); id != "" {
					data["tool_call_id"] = id
				}
				emit("tool_call", data)
			}
		case "tool":
			name := serviceJSONText(payload, "tool")
			if name == "" {
				name = "tool"
			}
			result := strings.TrimSpace(msg.Content)
			if preview := serviceJSONText(payload, "preview"); preview != "" {
				result = preview
			}
			data := map[string]any{"name": name, "result": result}
			if id := serviceJSONText(payload, "tool_call_id"); id != "" {
				data["tool_call_id"] = id
			}
			if artifactID := serviceJSONText(payload, "artifact_id"); artifactID != "" {
				data["artifact_id"] = artifactID
			}
			emit("tool_result", data)
		}
	}
	return events
}

func serviceJSONText(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func serviceMetaText(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boundedServiceLogPreview(text string, limit int) string {
	text = redactServiceLogPreview(strings.TrimSpace(text))
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

var serviceLogSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ya29\.[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|id_token|approval_token|token)"?\s*[:=]\s*")([^"\s]+)("?)`),
}

func redactServiceLogPreview(text string) string {
	for _, pattern := range serviceLogSecretPatterns {
		text = pattern.ReplaceAllStringFunc(text, func(match string) string {
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [redacted]"
			}
			if strings.Contains(match, ":") || strings.Contains(match, "=") {
				return pattern.ReplaceAllString(match, `${1}[redacted]${3}`)
			}
			return "[redacted]"
		})
	}
	return text
}

func decodeServiceJSONMap(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func (s *serviceServer) streamJob(w http.ResponseWriter, r *http.Request, jobID string) {
	snapshot, events, unsubscribe, ok := s.app().SubscribeJob(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	defer unsubscribe()
	w.Header().Set("X-Or3-Job-Id", jobID)
	if err := beginSSE(w); err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "streaming is not supported", err)
		return
	}
	for _, event := range snapshot.Events {
		if err := writeSSEEvent(w, event.Type, serviceStreamEventPayload(event)); err != nil {
			return
		}
	}
	if isTerminalStatus(snapshot.Status) {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event.Type, serviceStreamEventPayload(event)); err != nil {
				return
			}
		}
	}
}

func serviceStreamEventPayload(event agent.JobEvent) map[string]any {
	payload := map[string]any{}
	for key, value := range event.Data {
		payload[key] = value
	}
	payload["sequence"] = event.Sequence
	payload["type"] = event.Type
	return payload
}

type serviceObserver struct {
	agent.ConversationObserver
	finalText             string
	sawToolCall           bool
	sawToolResult         bool
	lastToolName          string
	lastToolStatus        string
	lastToolError         string
	lastToolResultPreview string
	lastToolResult        string
	lastToolCallID        string
	lastApprovalID        int64
}

func (o *serviceObserver) OnCompletion(ctx context.Context, finalText string, streamed bool) {
	o.finalText = finalText
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnCompletion(ctx, finalText, streamed)
	}
}

func (o *serviceObserver) OnToolCall(ctx context.Context, name string, arguments string) {
	o.sawToolCall = true
	o.lastToolName = strings.TrimSpace(name)
	o.lastToolStatus = "running"
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnToolCall(ctx, name, arguments)
	}
}

func (o *serviceObserver) OnToolResult(ctx context.Context, name string, out string, err error) {
	o.sawToolResult = true
	o.lastToolName = strings.TrimSpace(name)
	o.lastToolStatus = "completed"
	o.lastToolResult = boundedServiceLogPreview(out, 16384)
	o.lastToolResultPreview = boundedServiceLogPreview(out, 180)
	if err != nil {
		o.lastToolError = err.Error()
		o.lastToolStatus = "failed"
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			o.lastToolStatus = "approval_required"
			o.lastApprovalID = approvalErr.RequestID
		}
	}
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnToolResult(ctx, name, out, err)
	}
}

func (o *serviceObserver) OnToolLifecycle(ctx context.Context, event agent.ToolLifecycleEvent) {
	o.sawToolCall = true
	o.lastToolName = strings.TrimSpace(event.Name)
	o.lastToolStatus = strings.TrimSpace(event.Status)
	o.lastToolCallID = strings.TrimSpace(event.ToolCallID)
	if event.Result != "" || event.ResultPreview != "" || event.Status == "completed" || event.Status == "failed" {
		o.sawToolResult = true
	}
	if event.ResultPreview != "" {
		o.lastToolResult = boundedServiceLogPreview(event.ResultPreview, 16384)
		o.lastToolResultPreview = boundedServiceLogPreview(event.ResultPreview, 180)
	} else if event.Result != "" {
		o.lastToolResult = boundedServiceLogPreview(event.Result, 16384)
		o.lastToolResultPreview = boundedServiceLogPreview(event.Result, 180)
	}
	if event.ApprovalID > 0 {
		o.lastApprovalID = event.ApprovalID
		o.lastToolStatus = "approval_required"
	}
	if event.PublicCode == "" && event.Status == "failed" {
		event.PublicCode = agent.PublicErrorToolExecution
	}
	if event.Status == "failed" && o.lastToolError == "" {
		o.lastToolError = firstNonEmptyString(event.ResultPreview, event.Result, event.PublicCode)
	}
	if lifecycle, ok := o.ConversationObserver.(agent.ToolLifecycleObserver); ok {
		lifecycle.OnToolLifecycle(ctx, event)
	}
}

func (o *serviceObserver) sawToolActivity() bool {
	return o != nil && (o.sawToolCall || o.sawToolResult)
}

func (o *serviceObserver) finalTextForCompletion(defaultMessage string) (string, bool) {
	if o == nil {
		return strings.TrimSpace(defaultMessage), strings.TrimSpace(defaultMessage) != ""
	}
	if finalText := strings.TrimSpace(o.finalText); finalText != "" {
		return finalText, false
	}
	if fallback, ok := o.emptyFinalTextFallback(); ok {
		o.finalText = fallback
		return fallback, true
	}
	defaultMessage = strings.TrimSpace(defaultMessage)
	if defaultMessage == "" {
		return "", false
	}
	o.finalText = defaultMessage
	return defaultMessage, true
}

func (o *serviceObserver) emptyFinalTextFallback() (string, bool) {
	if o == nil || !o.sawToolActivity() {
		return "", false
	}
	toolName := strings.TrimSpace(o.lastToolName)
	if toolName == "" {
		toolName = "tool"
	}
	switch strings.TrimSpace(o.lastToolStatus) {
	case "failed", "error":
		unavailableDetail := strings.ToLower(firstNonEmptyString(o.lastToolError, o.lastToolResultPreview))
		if tools.IsToolNotAvailableThisTurn(unavailableDetail) {
			if strings.EqualFold(toolName, tools.ToolNameExec) {
				return "I tried to run a shell command, but the Admin Assistant is intentionally limited to dedicated Doctor tools for safety. No command was run. Ask again and I will use Doctor status/config tools instead of exec.", true
			}
			if tools.IsWriteToolName(toolName) {
				return "I can't create or modify files in Ask mode (read-only). Switch to Work mode if you'd like me to write that file for you, or I can paste the content here for you to save manually.", true
			}
		}
		message := fmt.Sprintf("The tool failed, and the model did not return a final message. Last tool: %s.", toolName)
		if detail := strings.TrimSpace(firstNonEmptyString(o.lastToolError, o.lastToolResultPreview)); detail != "" {
			message += " " + boundedServiceLogPreview(detail, 220)
		}
		return message, true
	case "approval_required":
		if o.lastApprovalID > 0 {
			return fmt.Sprintf("The tool still needs approval before it can continue. Last tool: %s. Approval request: %d.", toolName, o.lastApprovalID), true
		}
		return fmt.Sprintf("The tool still needs approval before it can continue. Last tool: %s.", toolName), true
	default:
		if text, ok := doctorEmptyFinalSummaryFromToolResult(toolName, o.lastToolResult); ok {
			return text, true
		}
		return fmt.Sprintf("The tool finished, but the model did not return a final message. Last tool: %s.", toolName), true
	}
}

func (s *serviceServer) handleAgentRunners(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	appSvc := s.app()
	detected, err := appSvc.DetectAgentCLIRunners(r.Context())
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent runner detection unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"runners": detected})
}

func (s *serviceServer) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/internal/v1/agent-runs" || r.URL.Path == "/internal/v1/agent-runs/" {
		switch r.Method {
		case http.MethodGet:
			s.handleAgentRunsList(w, r)
		case http.MethodPost:
			s.handleAgentRunsStart(w, r)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/agent-runs/")
	parts := strings.SplitN(strings.Trim(relative, "/"), "/", 2)
	runID := strings.TrimSpace(parts[0])
	if runID == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		s.handleAgentRunEvents(w, r, runID)
		return
	}
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	s.handleAgentRunRead(w, r, runID)
}

func (s *serviceServer) handleAgentRunsList(w http.ResponseWriter, r *http.Request) {
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		limit = n
	}
	runs, err := store.ListAgentCLIRuns(r.Context(), db.AgentCLIRunFilter{
		Status:           strings.TrimSpace(r.URL.Query().Get("status")),
		ParentSessionKey: strings.TrimSpace(r.URL.Query().Get("parent_session_key")),
		Limit:            limit,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent runs list unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildAgentCLIRunListResponse(runs))
}

func (s *serviceServer) handleAgentRunsStart(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceAgentRunsBodyLimit)
	req, err := decodeServiceAgentRunRequest(r.Body)
	if err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	writeServiceRequestWarnings(w, req.Warnings)
	agentReq := agentcli.AgentRunRequest{
		ParentSessionKey: req.ParentSessionKey,
		RunnerID:         req.RunnerID,
		Task:             req.Task,
		TimeoutSeconds:   req.TimeoutSeconds,
		Cwd:              req.Cwd,
		Model:            req.Model,
		Mode:             req.Mode,
		Isolation:        req.Isolation,
		MaxTurns:         req.MaxTurns,
		Meta:             req.Meta,
	}
	run, err := s.app().StartAgentCLIRun(r.Context(), agentReq)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "agent run rejected", err)
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{
		"job_id": run.JobID,
		"run_id": run.ID,
		"status": run.Status,
	})
}

func (s *serviceServer) handleAgentRunRead(w http.ResponseWriter, r *http.Request, id string) {
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	run, ok, err := store.GetAgentCLIRun(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent run lookup unavailable", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "run not found"})
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildAgentCLIRunResponse(run))
}

func (s *serviceServer) handleAgentRunEvents(w http.ResponseWriter, r *http.Request, runID string) {
	afterSeq := int64(0)
	if afterStr := r.URL.Query().Get("after_seq"); afterStr != "" {
		if n, err := strconv.ParseInt(afterStr, 10, 64); err == nil {
			afterSeq = n
		}
	}
	store := s.control().DB
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	run, ok, err := store.GetAgentCLIRun(r.Context(), runID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent run lookup unavailable", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "run not found"})
		return
	}
	events, err := store.ListAgentCLIEvents(r.Context(), run.JobID, afterSeq, 200)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "agent events unavailable", err)
		return
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildAgentCLIEventListResponse(events))
}
