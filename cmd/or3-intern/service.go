package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type serviceServer struct {
	config          config.Config
	runtime         *agent.Runtime
	subagentManager *agent.SubagentManager
	jobs            *agent.JobRegistry
	broker          *approval.Broker
	controlOnce     sync.Once
	controlSvc      *controlplane.Service
	appOnce         sync.Once
	appSvc          *app.ServiceApp
	rateMu          sync.Mutex
	rateWindow      time.Time
	rateCounts      map[string]int
}

const (
	serviceTurnsBodyLimit      int64 = 1 << 20
	serviceSubagentsBodyLimit  int64 = 1 << 20
	servicePairingBodyLimit    int64 = 64 << 10
	serviceApprovalBodyLimit   int64 = 64 << 10
	serviceEmbeddingsBodyLimit int64 = 64 << 10
	serviceScopeBodyLimit      int64 = 64 << 10
)

func runServiceCommand(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, jobs *agent.JobRegistry) error {
	return runServiceCommandWithBroker(ctx, cfg, rt, subagentManager, jobs, nil)
}

func runServiceCommandWithBroker(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, jobs *agent.JobRegistry, broker *approval.Broker) error {
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		return fmt.Errorf("service secret is required")
	}
	if err := validateStartupCommand("service", cfg, false); err != nil {
		return err
	}
	if rt == nil {
		return fmt.Errorf("runtime not configured")
	}
	if jobs == nil {
		jobs = agent.NewJobRegistry(0, 0)
	}
	server := &serviceServer{config: cfg, runtime: rt, subagentManager: subagentManager, jobs: jobs, broker: broker}
	mux := newServiceMux(server)

	httpServer := &http.Server{
		Addr:              cfg.Service.Listen,
		Handler:           serviceAuthMiddlewareWithBroker(cfg, broker, serviceBoundaryMiddleware(server, mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
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

func newServiceMux(server *serviceServer) *http.ServeMux {
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
	mux.Handle("/internal/v1/health", http.HandlerFunc(server.handleHealth))
	mux.Handle("/internal/v1/readiness", http.HandlerFunc(server.handleReadiness))
	mux.Handle("/internal/v1/capabilities", http.HandlerFunc(server.handleCapabilities))
	mux.Handle("/internal/v1/embeddings", http.HandlerFunc(server.handleEmbeddings))
	mux.Handle("/internal/v1/embeddings/", http.HandlerFunc(server.handleEmbeddings))
	mux.Handle("/internal/v1/audit", http.HandlerFunc(server.handleAudit))
	mux.Handle("/internal/v1/audit/", http.HandlerFunc(server.handleAudit))
	mux.Handle("/internal/v1/scope", http.HandlerFunc(server.handleScope))
	mux.Handle("/internal/v1/scope/", http.HandlerFunc(server.handleScope))
	return mux
}

func (s *serviceServer) control() *controlplane.Service {
	s.controlOnce.Do(func() {
		s.controlSvc = controlplane.New(s.config, s.runtime, s.broker, s.jobs, s.subagentManager)
	})
	return s.controlSvc
}

func (s *serviceServer) app() *app.ServiceApp {
	s.appOnce.Do(func() {
		s.appSvc = app.NewServiceApp(s.runtime, s.jobs, s.subagentManager, s.control())
	})
	return s.appSvc
}

func (s *serviceServer) handlePairing(w http.ResponseWriter, r *http.Request) {
	appSvc := s.app()
	if s.broker == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "approval broker unavailable"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/pairing/")
	if path == "requests" {
		switch r.Method {
		case http.MethodPost:
			limitServiceRequestBody(w, r, servicePairingBodyLimit)
			var body struct {
				Role         string         `json:"role"`
				DisplayName  string         `json:"display_name"`
				DisplayName2 string         `json:"displayName"`
				Origin       string         `json:"origin"`
				Metadata     map[string]any `json:"metadata"`
				DeviceID     string         `json:"device_id"`
			}
			if err := decodeServiceRequestBody(r.Body, &body); err != nil {
				writeServiceRequestDecodeError(w, err)
				return
			}
			req, code, err := appSvc.CreatePairingRequest(r.Context(), approval.PairingRequestInput{Role: body.Role, DisplayName: serviceFirstNonEmpty(body.DisplayName, body.DisplayName2), Origin: body.Origin, Metadata: body.Metadata, DeviceID: body.DeviceID})
			if err != nil {
				writeServiceError(w, r, http.StatusBadRequest, "pairing request failed", err)
				return
			}
			writeServiceJSON(w, http.StatusAccepted, map[string]any{"id": req.ID, "device_id": req.DeviceID, "role": req.Role, "display_name": req.DisplayName, "expires_at": req.ExpiresAt, "code": code})
		case http.MethodGet:
			if !requireServiceRole(w, r, approval.RoleOperator) {
				return
			}
			items, err := appSvc.ListPairingRequests(r.Context(), r.URL.Query().Get("status"), 100)
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "pairing list unavailable", err)
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
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			RequestID int64  `json:"request_id"`
			Code      string `json:"code"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		device, token, err := appSvc.ExchangePairingCode(r.Context(), approval.PairingExchangeInput{RequestID: body.RequestID, Code: body.Code})
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "pairing exchange failed", err)
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
		req, err := appSvc.ApprovePairingRequest(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "pairing approval failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": req.ID, "status": req.Status})
	case "deny":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if err := appSvc.DenyPairingRequest(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "pairing denial failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "denied"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "pairing action not found"})
	}
}

func (s *serviceServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	appSvc := s.app()
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
		items, err := appSvc.ListDevices(r.Context(), 100)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "device list unavailable", err)
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
		if err := appSvc.RevokeDevice(r.Context(), deviceID, serviceAuthIdentityFromContext(r.Context()).Actor); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "device revoke failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "status": "revoked"})
	case "rotate":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		rotated, token, err := appSvc.RotateDevice(r.Context(), deviceID)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "device rotation failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": rotated.DeviceID, "token": token})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "device action not found"})
	}
}

func (s *serviceServer) handleApprovals(w http.ResponseWriter, r *http.Request) {
	appSvc := s.app()
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
		items, err := appSvc.ListApprovalRequests(r.Context(), controlplane.ApprovalFilter{
			Status: r.URL.Query().Get("status"),
			Type:   r.URL.Query().Get("type"),
			Limit:  100,
		})
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "approval list unavailable", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	trimmedPath := strings.Trim(path, "/")
	if trimmedPath == "expire" {
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		expired, err := appSvc.ExpireApprovals(r.Context(), serviceAuthIdentityFromContext(r.Context()).Actor)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "approval expiration failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"expired": expired})
		return
	}
	if trimmedPath == "allowlists" {
		switch r.Method {
		case http.MethodGet:
			items, err := appSvc.ListAllowlists(r.Context(), r.URL.Query().Get("domain"), 100)
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "allowlist list unavailable", err)
				return
			}
			writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodPost:
			limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
			body, err := decodeServiceAllowlistRequest(r.Body)
			if err != nil {
				writeServiceRequestDecodeError(w, err)
				return
			}
			rec, err := appSvc.AddAllowlist(r.Context(), body.Domain, body.Scope, body.Matcher, serviceAuthIdentityFromContext(r.Context()).Actor, body.ExpiresAt)
			if err != nil {
				writeServiceError(w, r, http.StatusBadRequest, "allowlist add failed", err)
				return
			}
			writeServiceJSON(w, http.StatusAccepted, map[string]any{"item": rec})
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	if strings.HasPrefix(trimmedPath, "allowlists/") {
		parts := strings.Split(trimmedPath, "/")
		if len(parts) != 3 || parts[2] != "remove" {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval route not found"})
			return
		}
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		id, err := parseServiceInt64(parts[1])
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid allowlist ID"})
			return
		}
		if err := appSvc.RemoveAllowlist(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "allowlist remove failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"allowlist_id": id, "status": "removed"})
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
		item, err := appSvc.GetApproval(r.Context(), id)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "approval lookup failed", err)
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
		limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
		var body struct {
			Allowlist bool   `json:"allowlist"`
			Note      string `json:"note"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		issued, err := appSvc.ApproveApproval(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Allowlist, body.Note)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "approval failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "token": issued.Token, "allowlist_id": issued.AllowlistID})
	case "deny":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
		var body struct {
			Note string `json:"note"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := appSvc.DenyApproval(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Note); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "approval denial failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "status": "denied"})
	case "cancel":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
		var body struct {
			Note string `json:"note"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := appSvc.CancelApproval(r.Context(), id, serviceAuthIdentityFromContext(r.Context()).Actor, body.Note); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "approval cancel failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"request_id": id, "status": "canceled"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "approval action not found"})
	}
}

func parseServiceInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}

func statusCodeForControlplaneError(err error, defaultCode int) int {
	switch {
	case errors.Is(err, controlplane.ErrDatabaseUnavailable), errors.Is(err, controlplane.ErrProviderUnavailable), errors.Is(err, controlplane.ErrAuditUnavailable), errors.Is(err, controlplane.ErrJobRegistryUnavailable):
		return http.StatusServiceUnavailable
	default:
		return defaultCode
	}
}

type serviceAllowlistRequest struct {
	Domain    string
	Scope     approval.AllowlistScope
	Matcher   any
	ExpiresAt int64
}

func decodeServiceAllowlistRequest(body io.Reader) (serviceAllowlistRequest, error) {
	var payload struct {
		Domain         string                  `json:"domain"`
		Scope          approval.AllowlistScope `json:"scope"`
		Matcher        json.RawMessage         `json:"matcher"`
		ExpiresAt      int64                   `json:"expires_at"`
		ExpiresAtCamel int64                   `json:"expiresAt"`
	}
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceAllowlistRequest{}, err
	}
	domain := strings.TrimSpace(payload.Domain)
	var matcher any
	switch domain {
	case string(approval.SubjectExec):
		var item approval.ExecAllowlistMatcher
		if len(payload.Matcher) > 0 {
			if err := json.Unmarshal(payload.Matcher, &item); err != nil {
				return serviceAllowlistRequest{}, fmt.Errorf("invalid request body")
			}
		}
		matcher = item
	case string(approval.SubjectSkillExec):
		var item approval.SkillAllowlistMatcher
		if len(payload.Matcher) > 0 {
			if err := json.Unmarshal(payload.Matcher, &item); err != nil {
				return serviceAllowlistRequest{}, fmt.Errorf("invalid request body")
			}
		}
		matcher = item
	default:
		return serviceAllowlistRequest{}, fmt.Errorf("unsupported allowlist domain")
	}
	return serviceAllowlistRequest{
		Domain:    domain,
		Scope:     payload.Scope,
		Matcher:   matcher,
		ExpiresAt: firstPositiveInt64(payload.ExpiresAt, payload.ExpiresAtCamel),
	}, nil
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
	limitServiceRequestBody(w, r, serviceTurnsBodyLimit)
	req, err := decodeServiceTurnRequest(r.Body, s.runtime.Tools)
	if err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
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
	writeServiceValue(w, statusCode, controlplane.BuildJobResponse(snapshot))
}

func (s *serviceServer) runTurnJob(ctx context.Context, jobID string, req serviceTurnRequest, identity serviceAuthIdentity) {
	s.jobs.Publish(jobID, "started", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"status": "running"}))
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	err := s.app().RunTurn(ctx, app.TurnRequest{
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
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			s.jobs.Complete(jobID, "aborted", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"message": "job aborted"}))
			return
		}
		s.jobs.Fail(jobID, servicePublicJobError(err), serviceLifecyclePayload(req.SessionKey, req.Meta, nil))
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
	limitServiceRequestBody(w, r, serviceSubagentsBodyLimit)
	req, err := decodeServiceSubagentRequest(r.Body, backgroundToolsRegistry(s.subagentManager))
	if err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
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
		if err == db.ErrSubagentQueueFull {
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

func (s *serviceServer) handleJobs(w http.ResponseWriter, r *http.Request) {
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/jobs/")
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	if len(parts) == 1 && strings.TrimSpace(parts[0]) != "" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		snapshot, err := s.app().GetJob(parts[0])
		if errors.Is(err, controlplane.ErrJobNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
			return
		}
		if err != nil {
			writeServiceError(w, r, http.StatusServiceUnavailable, "job lookup unavailable", err)
			return
		}
		writeServiceValue(w, http.StatusOK, controlplane.BuildJobSnapshotResponse(snapshot))
		return
	}
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
	snapshot, events, unsubscribe, ok := s.app().SubscribeJob(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	defer unsubscribe()
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

func (s *serviceServer) abortJob(w http.ResponseWriter, r *http.Request, jobID string) {
	ok, status, err := s.app().AbortJob(r.Context(), jobID)
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "job abort unavailable", err)
		return
	}
	if ok && status == "" {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
		return
	}
	if ok && status != "" {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID, "status": status})
		return
	}
	if status == "not_found" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "job is not abortable", "job_id": jobID})
}

type serviceRequestContextKey struct{}

type serviceRequestContext struct {
	RequestID string
}

func serviceBoundaryMiddleware(server *serviceServer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = newServiceRequestID()
		}
		w.Header().Set("X-Request-Id", requestID)
		ctx := context.WithValue(r.Context(), serviceRequestContextKey{}, serviceRequestContext{RequestID: requestID})
		r = r.WithContext(ctx)
		if server != nil && server.isMutationRequest(r) && !server.allowMutationRequest(r) {
			writeServiceJSON(w, http.StatusTooManyRequests, serviceErrorPayload(r, "rate limit exceeded"))
			return
		}
		captured := &serviceStatusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(captured, r)
		server.recordServiceAudit(r, captured.statusCode)
	})
}

type serviceStatusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *serviceStatusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *serviceStatusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *serviceServer) isMutationRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *serviceServer) allowMutationRequest(r *http.Request) bool {
	if s == nil || r == nil {
		return true
	}
	limit := s.config.Service.MutationRateLimitPerMinute
	if limit <= 0 {
		return true
	}
	actor := serviceAuthIdentityFromContext(r.Context()).Actor
	if actor == "" {
		actor = remoteIPKey(r.RemoteAddr)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	key := actor + ":" + r.URL.Path
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if s.rateCounts == nil || !s.rateWindow.Equal(now) {
		s.rateWindow = now
		s.rateCounts = map[string]int{}
	}
	s.rateCounts[key]++
	return s.rateCounts[key] <= limit
}

func (s *serviceServer) recordServiceAudit(r *http.Request, statusCode int) {
	if s == nil || s.runtime == nil || s.runtime.Audit == nil || r == nil {
		return
	}
	identity := serviceAuthIdentityFromContext(r.Context())
	payload := map[string]any{
		"path":        r.URL.Path,
		"method":      r.Method,
		"status_code": statusCode,
		"request_id":  serviceRequestIDFromContext(r.Context()),
	}
	if remote := remoteIPKey(r.RemoteAddr); remote != "" {
		payload["remote_addr"] = remote
	}
	_ = s.runtime.Audit.Record(r.Context(), "service.request", "", identity.Actor, payload)
}

func serviceRequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestCtx, _ := ctx.Value(serviceRequestContextKey{}).(serviceRequestContext)
	return strings.TrimSpace(requestCtx.RequestID)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, statusCode int, public string, err error) {
	if err != nil {
		log.Printf("service %s %s: %v", r.Method, r.URL.Path, err)
	}
	writeServiceJSON(w, statusCode, serviceErrorPayload(r, public))
}

func serviceErrorPayload(r *http.Request, public string) map[string]any {
	payload := map[string]any{"error": strings.TrimSpace(public)}
	if payload["error"] == "" {
		payload["error"] = "request failed"
	}
	if requestID := serviceRequestIDFromContext(r.Context()); requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}

func servicePublicJobError(err error) string {
	if err == nil {
		return "job failed"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "job canceled"
	}
	return "job failed"
}

func remoteIPKey(raw string) string {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return ""
	}
	host := addr
	if parsed, err := netip.ParseAddrPort(addr); err == nil {
		return parsed.Addr().String()
	}
	if hostPart, _, err := net.SplitHostPort(addr); err == nil {
		host = hostPart
	}
	return strings.Trim(host, "[]")
}

func newServiceRequestID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func validateServiceToolCapabilities(registry *tools.Registry, names []string, maxCapability string) error {
	ceiling := tools.CapabilityLevel(strings.ToLower(strings.TrimSpace(maxCapability)))
	if ceiling == "" || registry == nil || len(names) == 0 {
		return nil
	}
	for _, name := range names {
		toolName := strings.TrimSpace(name)
		if toolName == "" {
			continue
		}
		tool := registry.Get(toolName)
		if tool == nil {
			continue
		}
		if capabilityRank(tools.ToolCapability(tool, nil)) > capabilityRank(ceiling) {
			return fmt.Errorf("tool exceeds service capability ceiling: %s", toolName)
		}
	}
	return nil
}

func capabilityRank(level tools.CapabilityLevel) int {
	switch level {
	case tools.CapabilityPrivileged:
		return 3
	case tools.CapabilityGuarded:
		return 2
	case tools.CapabilitySafe:
		return 1
	default:
		return 0
	}
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
	writeServiceValue(w, statusCode, payload)
}

func writeServiceValue(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func acceptsSSE(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
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

func (s *serviceServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeServiceValue(w, http.StatusOK, s.control().GetHealth())
}

func (s *serviceServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	report := s.control().GetReadiness()
	statusCode := http.StatusOK
	if !report.Ready {
		statusCode = http.StatusServiceUnavailable
	}
	writeServiceValue(w, statusCode, report)
}

func (s *serviceServer) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeServiceValue(w, http.StatusOK, s.control().GetCapabilities(r.URL.Query().Get("channel"), r.URL.Query().Get("trigger")))
}

func (s *serviceServer) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/embeddings"), "/")
	cp := s.control()
	switch path {
	case "status":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		report, err := cp.GetEmbeddingStatus(r.Context())
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, report)
	case "rebuild":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceEmbeddingsBodyLimit)
		var body struct {
			Target      string `json:"target"`
			TargetCamel string `json:"targetName"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		target := serviceFirstNonEmpty(body.Target, body.TargetCamel, r.URL.Query().Get("target"))
		result, err := cp.RebuildEmbeddings(r.Context(), target)
		if err != nil {
			status := statusCodeForControlplaneError(err, http.StatusBadRequest)
			writeServiceJSON(w, status, map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, result)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "embeddings route not found"})
	}
}

func (s *serviceServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/audit"), "/")
	cp := s.control()
	switch path {
	case "":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		report, err := cp.GetAuditStatus(r.Context())
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, report)
	case "verify":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		result, err := cp.VerifyAudit(r.Context())
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceValue(w, http.StatusOK, result)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "audit route not found"})
	}
}

func (s *serviceServer) handleScope(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/scope"), "/")
	cp := s.control()
	switch path {
	case "links":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceScopeBodyLimit)
		var body struct {
			SessionKey      string         `json:"session_key"`
			SessionKeyCamel string         `json:"sessionKey"`
			ScopeKey        string         `json:"scope_key"`
			ScopeKeyCamel   string         `json:"scopeKey"`
			Meta            map[string]any `json:"meta"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		linked, err := cp.LinkSessionScope(r.Context(), controlplane.ScopeLinkInput{
			SessionKey: serviceFirstNonEmpty(body.SessionKey, body.SessionKeyCamel),
			ScopeKey:   serviceFirstNonEmpty(body.ScopeKey, body.ScopeKeyCamel),
			Meta:       body.Meta,
		})
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadRequest), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"session_key": linked.SessionKey, "scope_key": linked.ScopeKey})
	case "sessions":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		scopeKey := serviceFirstNonEmpty(r.URL.Query().Get("scope_key"), r.URL.Query().Get("scopeKey"))
		if scopeKey == "" {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "scope_key is required"})
			return
		}
		sessions, err := cp.ListScopeSessions(r.Context(), scopeKey)
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"scope_key": scopeKey, "sessions": sessions})
	case "resolve":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		sessionKey := serviceFirstNonEmpty(r.URL.Query().Get("session_key"), r.URL.Query().Get("sessionKey"))
		if sessionKey == "" {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key is required"})
			return
		}
		scopeKey, err := cp.ResolveScopeKey(r.Context(), sessionKey)
		if err != nil {
			writeServiceJSON(w, statusCodeForControlplaneError(err, http.StatusBadGateway), map[string]any{"error": controlplane.DescribeUnavailable(err).Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"session_key": sessionKey, "scope_key": scopeKey})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "scope route not found"})
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
