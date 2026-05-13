package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	req.ApprovalToken = serviceFirstNonEmpty(req.ApprovalToken, serviceApprovalTokenFromRequest(r))
	if req.SessionKey == "" || req.Message == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key and message are required"})
		return
	}
	job := s.jobs.Register("turn")
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
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	var err error
	if req.ReplayToolCall != nil {
		_, err = s.app().ReplayToolCall(ctx, app.ReplayToolCallRequest{
			SessionKey:    req.SessionKey,
			ToolName:      req.ReplayToolCall.Name,
			ArgumentsJSON: req.ReplayToolCall.ArgumentsJSON,
			AllowedTools:  req.AllowedTools,
			RestrictTools: req.RestrictTools,
			ProfileName:   req.ProfileName,
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
			Meta:          req.Meta,
			AllowedTools:  req.AllowedTools,
			RestrictTools: req.RestrictTools,
			ProfileName:   req.ProfileName,
			Capability:    tools.CapabilityLevel(s.config.Service.MaxCapability),
			ApprovalToken: req.ApprovalToken,
			Actor:         identity.Actor,
			Role:          identity.Role,
			Observer:      observer,
			Streamer:      agent.NullStreamer{},
		})
	}
	if err != nil {
		s.completeTurnJobWithError(ctx, jobID, err, observer, req.SessionKey, req.Meta)
		return
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"final_text": observer.finalText}))
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
	meta := map[string]any{
		"approval_request_id": issued.Request.ID,
		"approved_resume":     true,
	}
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
		s.completeTurnJobWithError(ctx, jobID, err, observer, sessionKey, meta)
		return
	}
	if strings.TrimSpace(observer.finalText) == "" {
		observer.finalText = strings.TrimSpace(finalText)
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(sessionKey, meta, map[string]any{"final_text": observer.finalText}))
}

func (s *serviceServer) completeTurnJobWithError(ctx context.Context, jobID string, err error, observer *serviceObserver, sessionKey string, meta map[string]any) {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		s.jobs.Complete(jobID, "aborted", serviceLifecyclePayload(sessionKey, meta, map[string]any{"message": "job aborted"}))
		return
	}
	var approvalErr *tools.ApprovalRequiredError
	if errors.As(err, &approvalErr) {
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
	if r.Method != http.MethodGet {
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
		if err := writeSSEEvent(w, event.Type, event.Data); err != nil {
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
			if err := writeSSEEvent(w, event.Type, event.Data); err != nil {
				return
			}
		}
	}
}

type serviceObserver struct {
	agent.ConversationObserver
	finalText     string
	lastToolError string
}

func (o *serviceObserver) OnCompletion(ctx context.Context, finalText string, streamed bool) {
	o.finalText = finalText
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnCompletion(ctx, finalText, streamed)
	}
}

func (o *serviceObserver) OnToolResult(ctx context.Context, name string, out string, err error) {
	if err != nil {
		o.lastToolError = err.Error()
	}
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnToolResult(ctx, name, out, err)
	}
}

func (o *serviceObserver) OnToolLifecycle(ctx context.Context, event agent.ToolLifecycleEvent) {
	if event.PublicCode == "" && event.Status == "failed" {
		event.PublicCode = agent.PublicErrorToolExecution
	}
	if lifecycle, ok := o.ConversationObserver.(agent.ToolLifecycleObserver); ok {
		lifecycle.OnToolLifecycle(ctx, event)
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
