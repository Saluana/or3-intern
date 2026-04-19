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
	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type serviceServer struct {
	runtime         *agent.Runtime
	subagentManager *agent.SubagentManager
	jobs            *agent.JobRegistry
	broker          *approval.Broker
}

func runServiceCommand(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, jobs *agent.JobRegistry) error {
	return runServiceCommandWithBroker(ctx, cfg, rt, subagentManager, jobs, nil)
}

func runServiceCommandWithBroker(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, jobs *agent.JobRegistry, broker *approval.Broker) error {
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		return fmt.Errorf("service secret is required")
	}
	if err := validateStartupCommand("service", cfg); err != nil {
		return err
	}
	if rt == nil {
		return fmt.Errorf("runtime not configured")
	}
	if jobs == nil {
		jobs = agent.NewJobRegistry(0, 0)
	}
	server := &serviceServer{runtime: rt, subagentManager: subagentManager, jobs: jobs, broker: broker}
	mux := http.NewServeMux()
	mux.Handle("/internal/v1/turns", http.HandlerFunc(server.handleTurns))
	mux.Handle("/internal/v1/subagents", http.HandlerFunc(server.handleSubagents))
	mux.Handle("/internal/v1/jobs/", http.HandlerFunc(server.handleJobs))
	mux.Handle("/internal/v1/pairing/requests", http.HandlerFunc(server.handlePairing))
	mux.Handle("/internal/v1/pairing/requests/", http.HandlerFunc(server.handlePairing))
	mux.Handle("/internal/v1/pairing/exchange", http.HandlerFunc(server.handlePairing))
	mux.Handle("/internal/v1/devices", http.HandlerFunc(server.handleDevices))
	mux.Handle("/internal/v1/devices/", http.HandlerFunc(server.handleDevices))
	mux.Handle("/internal/v1/approvals", http.HandlerFunc(server.handleApprovals))
	mux.Handle("/internal/v1/approvals/", http.HandlerFunc(server.handleApprovals))

	httpServer := &http.Server{
		Addr:              cfg.Service.Listen,
		Handler:           serviceAuthMiddlewareWithBroker(cfg.Service.Secret, broker, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("or3-intern service listening on %s", cfg.Service.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *serviceServer) handlePairing(w http.ResponseWriter, r *http.Request) {
	if s.broker == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "approval broker unavailable"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/pairing/")
	if path == "requests" {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Role         string         `json:"role"`
				DisplayName  string         `json:"display_name"`
				DisplayName2 string         `json:"displayName"`
				Origin       string         `json:"origin"`
				Metadata     map[string]any `json:"metadata"`
				DeviceID     string         `json:"device_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
				return
			}
			req, code, err := s.broker.CreatePairingRequest(r.Context(), approval.PairingRequestInput{Role: body.Role, DisplayName: serviceFirstNonEmpty(body.DisplayName, body.DisplayName2), Origin: body.Origin, Metadata: body.Metadata, DeviceID: body.DeviceID})
			if err != nil {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			writeServiceJSON(w, http.StatusAccepted, map[string]any{"id": req.ID, "device_id": req.DeviceID, "role": req.Role, "display_name": req.DisplayName, "expires_at": req.ExpiresAt, "code": code})
		case http.MethodGet:
			if !requireServiceRole(w, r, approval.RoleOperator) {
				return
			}
			items, err := s.broker.ListPairingRequests(r.Context(), r.URL.Query().Get("status"), 100)
			if err != nil {
				writeServiceJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
				return
			}
			writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	if path == "exchange" {
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var body struct {
			RequestID int64  `json:"request_id"`
			Code      string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		device, token, err := s.broker.ExchangePairingCode(r.Context(), approval.PairingExchangeInput{RequestID: body.RequestID, Code: body.Code})
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": device.DeviceID, "role": device.Role, "token": token})
		return
	}
	if !strings.HasPrefix(path, "requests/") {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "pairing route not found"})
		return
	}
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "requests/"), "/")
	if len(parts) != 2 {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "pairing route not found"})
		return
	}
	id, err := parseServiceInt64(parts[0])
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid pairing request ID"})
		return
	}
	switch parts[1] {
	case "approve":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		req, err := s.broker.ApprovePairingRequest(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": req.ID, "status": req.Status})
	case "deny":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if err := s.broker.DenyPairingRequest(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor); err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "denied"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "pairing action not found"})
	}
}

func (s *serviceServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	if s.broker == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "approval broker unavailable"})
		return
	}
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/devices")
	if path == "" || path == "/" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		items, err := s.broker.ListDevices(r.Context(), 100)
		if err != nil {
			writeServiceJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "device route not found"})
		return
	}
	deviceID := parts[0]
	switch parts[1] {
	case "revoke":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if err := s.broker.RevokeDevice(r.Context(), deviceID, serviceAuthIdentityFromContext(r.Context()).Actor); err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "status": "revoked"})
	case "rotate":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		rotated, token, err := s.broker.RotatePairedDeviceToken(r.Context(), deviceID)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": rotated.DeviceID, "token": token})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "device action not found"})
	}
}

func (s *serviceServer) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if s.broker == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "approval broker unavailable"})
		return
	}
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/approvals")
	if path == "" || path == "/" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		items, err := s.broker.ListApprovalRequests(r.Context(), r.URL.Query().Get("status"), 100)
		if err != nil {
			writeServiceJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		id, err := parseServiceInt64(parts[0])
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid approval ID"})
			return
		}
		item, err := s.broker.DB.GetApprovalRequest(r.Context(), id)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"item": item})
		return
	}
	if len(parts) != 2 {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval route not found"})
		return
	}
	id, err := parseServiceInt64(parts[0])
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid approval ID"})
		return
	}
	switch parts[1] {
	case "approve":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var body struct {
			Allowlist bool   `json:"allowlist"`
			Note      string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		issued, err := s.broker.ApproveRequest(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Allowlist, body.Note)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "token": issued.Token, "allowlist_id": issued.AllowlistID})
	case "deny":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		if err := s.broker.DenyRequest(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Note); err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "status": "denied"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval action not found"})
	}
}

func parseServiceInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}

func serviceFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func serviceApprovalTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return serviceFirstNonEmpty(
		r.Header.Get("X-Approval-Token"),
		r.Header.Get("X-Or3-Approval-Token"),
	)
}

func (s *serviceServer) handleTurns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	req, err := decodeServiceTurnRequest(r.Body, s.runtime.Tools)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
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
	s.jobs.Publish(job.ID, "queued", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"status": "queued"}))

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
	writeServiceJSON(w, statusCode, buildJobResponse(snapshot))
}

func (s *serviceServer) runTurnJob(ctx context.Context, jobID string, req serviceTurnRequest, identity serviceAuthIdentity) {
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	runCtx := agent.ContextWithConversationObserver(ctx, observer)
	runCtx = agent.ContextWithStreamingChannel(runCtx, agent.NullStreamer{})
	runCtx = tools.ContextWithApprovalToken(runCtx, req.ApprovalToken)
	runCtx = tools.ContextWithRequesterIdentity(runCtx, identity.Actor, identity.Role)
	if req.RestrictTools {
		filtered := tools.NewRegistry()
		if len(req.AllowedTools) > 0 {
			filtered = s.runtime.Tools.CloneFiltered(req.AllowedTools)
		}
		runCtx = agent.ContextWithToolRegistry(runCtx, filtered)
	}
	meta := cloneServiceMeta(req.Meta)
	if req.ProfileName != "" {
		meta["profile_name"] = req.ProfileName
	}
	err := s.runtime.Handle(runCtx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: req.SessionKey,
		Channel:    "service",
		From:       "or3-net",
		Message:    req.Message,
		Meta:       meta,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			s.jobs.Complete(jobID, "aborted", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"message": "job aborted"}))
			return
		}
		s.jobs.Fail(jobID, err.Error(), serviceLifecyclePayload(req.SessionKey, req.Meta, nil))
		return
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"final_text": observer.finalText}))
}

func (s *serviceServer) handleSubagents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if s.subagentManager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "subagent manager is not enabled"})
		return
	}
	req, err := decodeServiceSubagentRequest(r.Body, backgroundToolsRegistry(s.subagentManager))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	audit := serviceAuditHeadersFromRequest(r)
	req.Meta = mergeServiceAuditMeta(req.Meta, audit)
	req.ApprovalToken = serviceFirstNonEmpty(req.ApprovalToken, serviceApprovalTokenFromRequest(r))
	if req.ParentSessionKey == "" || req.Task == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "parent_session_key and task are required"})
		return
	}
	job, err := s.subagentManager.EnqueueService(r.Context(), agent.ServiceSubagentRequest{
		ParentSessionKey: req.ParentSessionKey,
		Task:             req.Task,
		PromptSnapshot:   req.PromptSnapshot,
		AllowedTools:     req.AllowedTools,
		RestrictTools:    req.RestrictTools,
		ProfileName:      req.ProfileName,
		Channel:          req.Channel,
		ReplyTo:          req.ReplyTo,
		Meta:             req.Meta,
		Timeout:          time.Duration(req.TimeoutSeconds) * time.Second,
		ApprovalToken:    req.ApprovalToken,
		RequesterActor:   serviceAuthIdentityFromContext(r.Context()).Actor,
		RequesterRole:    serviceAuthIdentityFromContext(r.Context()).Role,
	})
	if err != nil {
		statusCode := http.StatusBadGateway
		if err == db.ErrSubagentQueueFull {
			statusCode = http.StatusTooManyRequests
		}
		writeServiceJSON(w, statusCode, map[string]any{"error": err.Error()})
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{
		"job_id":            job.ID,
		"child_session_key": job.ChildSessionKey,
		"status":            db.SubagentStatusQueued,
	})
}

func (s *serviceServer) handleJobs(w http.ResponseWriter, r *http.Request) {
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/jobs/")
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job route not found"})
		return
	}
	jobID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	switch action {
	case "stream":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.streamJob(w, r, jobID)
	case "abort":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.abortJob(w, r, jobID)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job action not found"})
	}
}

func (s *serviceServer) streamJob(w http.ResponseWriter, r *http.Request, jobID string) {
	snapshot, events, unsubscribe, ok := s.jobs.Subscribe(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	defer unsubscribe()
	if err := beginSSE(w); err != nil {
		writeServiceJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
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

func (s *serviceServer) abortJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if s.jobs.Cancel(jobID) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
		return
	}
	if s.subagentManager != nil {
		if err := s.subagentManager.Abort(r.Context(), jobID); err == nil {
			writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
			return
		}
	}
	snapshot, ok := s.jobs.Snapshot(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	if isTerminalStatus(snapshot.Status) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID, "status": snapshot.Status})
		return
	}
	writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "job is not abortable", "job_id": jobID})
}

func beginSSE(w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming is not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()
	return nil
}

func writeSSEEvent(w http.ResponseWriter, eventType string, payload map[string]any) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming is not supported")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, encoded); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeServiceJSON(w http.ResponseWriter, statusCode int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func acceptsSSE(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func buildJobResponse(snapshot agent.JobSnapshot) map[string]any {
	response := map[string]any{
		"job_id": snapshot.ID,
		"kind":   snapshot.Kind,
		"status": snapshot.Status,
	}
	for i := len(snapshot.Events) - 1; i >= 0; i-- {
		event := snapshot.Events[i]
		switch event.Type {
		case "completion":
			for key, value := range event.Data {
				response[key] = value
			}
			return response
		case "error":
			response["error"] = event.Data["message"]
			return response
		}
	}
	return response
}

func cloneServiceMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}

type serviceAuditHeaders struct {
	RequestID        string
	WorkspaceID      string
	NetworkSessionID string
}

func serviceAuditHeadersFromRequest(r *http.Request) serviceAuditHeaders {
	return serviceAuditHeaders{
		RequestID:        strings.TrimSpace(r.Header.Get("X-Request-Id")),
		WorkspaceID:      strings.TrimSpace(r.Header.Get("X-Workspace-Id")),
		NetworkSessionID: strings.TrimSpace(r.Header.Get("X-Network-Session-Id")),
	}
}

func mergeServiceAuditMeta(meta map[string]any, audit serviceAuditHeaders) map[string]any {
	out := cloneServiceMeta(meta)
	if audit.RequestID != "" {
		out["request_id"] = audit.RequestID
	}
	if audit.WorkspaceID != "" {
		out["workspace_id"] = audit.WorkspaceID
	}
	if audit.NetworkSessionID != "" {
		out["network_session_id"] = audit.NetworkSessionID
	}
	return out
}

func serviceLifecyclePayload(sessionKey string, meta map[string]any, extra map[string]any) map[string]any {
	payload := map[string]any{"session_key": sessionKey}
	for _, key := range []string{"request_id", "workspace_id", "network_session_id"} {
		if value, ok := meta[key]; ok {
			payload[key] = value
		}
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func isTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "aborted", db.SubagentStatusSucceeded, db.SubagentStatusInterrupted:
		return true
	default:
		return false
	}
}

type serviceObserver struct {
	agent.ConversationObserver
	finalText string
}

func (o *serviceObserver) OnCompletion(ctx context.Context, finalText string, streamed bool) {
	o.finalText = finalText
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnCompletion(ctx, finalText, streamed)
	}
}
