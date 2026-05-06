package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type serviceServer struct {
	config             config.Config
	configPath         string
	runtime            *agent.Runtime
	cronSvc            *cron.Service
	subagentManager    *agent.SubagentManager
	agentCLIManager    *agentcli.Manager
	jobs               *agent.JobRegistry
	broker             *approval.Broker
	unsafeDev          bool
	controlOnce        sync.Once
	controlSvc         *controlplane.Service
	appOnce            sync.Once
	appSvc             *app.ServiceApp
	terminalMu         sync.Mutex
	terminalSessions   map[string]*serviceTerminalSession
	terminalWSTicketMu sync.Mutex
	terminalWSTickets  map[string]serviceTerminalWebSocketTicket
	rateMu             sync.Mutex
	rateWindow         time.Time
	rateCounts         map[string]int
	authFailureMu      sync.Mutex
	authFailures       map[string]serviceAuthFailureState
	modelCatalogMu     sync.Mutex
	modelCatalogCache  map[string]serviceModelCatalogCacheEntry
}

type serviceAuthFailureState struct {
	Count        int
	FirstAttempt time.Time
	BlockedUntil time.Time
}

type serviceModelCatalogCacheEntry struct {
	FetchedAt time.Time
	Items     []serviceModelCatalogItem
}

type serviceModelCatalogItem struct {
	ID               string         `json:"id"`
	Name             string         `json:"name,omitempty"`
	Description      string         `json:"description,omitempty"`
	Provider         string         `json:"provider,omitempty"`
	ContextLength    int            `json:"contextLength,omitempty"`
	InputModalities  []string       `json:"inputModalities,omitempty"`
	OutputModalities []string       `json:"outputModalities,omitempty"`
	Pricing          map[string]any `json:"pricing,omitempty"`
	RawProvider      string         `json:"rawProvider,omitempty"`
}

const (
	serviceTurnsBodyLimit                    int64 = 1 << 20
	serviceSubagentsBodyLimit                int64 = 1 << 20
	servicePairingBodyLimit                  int64 = 64 << 10
	serviceApprovalBodyLimit                 int64 = 64 << 10
	serviceEmbeddingsBodyLimit               int64 = 64 << 10
	serviceScopeBodyLimit                    int64 = 64 << 10
	serviceConfigureBodyLimit                int64 = 256 << 10
	serviceFileUploadBodyLimit               int64 = 128 << 20
	serviceFileTextReadLimit                 int64 = 1 << 20
	serviceFileTextWriteLimit                int64 = 1 << 20
	serviceTerminalBodyLimit                 int64 = 64 << 10
	serviceAgentRunsBodyLimit                int64 = 256 << 10
	serviceCronBodyLimit                     int64 = 64 << 10
	serviceTerminalSessionTTL                      = 10 * time.Minute
	serviceTerminalMaxSessions                     = 4
	serviceTerminalWebSocketTicketTTL              = 30 * time.Second
	serviceTerminalWebSocketPingInterval           = 25 * time.Second
	serviceTerminalWebSocketWriteTimeout           = 10 * time.Second
	serviceTerminalWebSocketHandshakeTimeout       = 5 * time.Second
	serviceTerminalWebSocketProtocol               = "or3.terminal.v1"
	serviceTerminalWebSocketTicketPrefix           = "or3.ticket."
)

type serviceTerminalEvent struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

type serviceTerminalWebSocketTicket struct {
	SessionID string
	ExpiresAt time.Time
}

var (
	errTerminalSessionNotFound    = errors.New("terminal session not found")
	errTerminalInputRequired      = errors.New("input is required")
	errTerminalSessionNotWritable = errors.New("terminal session is not writable")
	errTerminalResizeRequired     = errors.New("terminal resize rows or cols are required")
)

type serviceTerminalSession struct {
	ID            string
	RootID        string
	RelativePath  string
	WorkingDir    string
	Shell         string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	LastActiveAt  time.Time
	Status        string
	Rows          int
	Cols          int
	ApprovalMode  string
	ApprovalID    int64
	ApprovalState string
	cmd           *exec.Cmd
	ptyFile       *os.File
	stdin         io.WriteCloser
	cancel        context.CancelFunc
	mu            sync.Mutex
	events        []serviceTerminalEvent
	subscribers   map[chan serviceTerminalEvent]struct{}
}

func (s *serviceTerminalSession) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"session_id":     s.ID,
		"root_id":        s.RootID,
		"path":           s.RelativePath,
		"cwd":            s.WorkingDir,
		"shell":          s.Shell,
		"created_at":     s.CreatedAt.UTC().Format(time.RFC3339),
		"expires_at":     s.ExpiresAt.UTC().Format(time.RFC3339),
		"last_active_at": s.LastActiveAt.UTC().Format(time.RFC3339),
		"status":         s.Status,
		"rows":           s.Rows,
		"cols":           s.Cols,
		"approval_mode":  s.ApprovalMode,
		"approval_id":    s.ApprovalID,
		"approval_state": s.ApprovalState,
		"event_count":    len(s.events),
	}
}

func (s *serviceTerminalSession) appendEvent(eventType string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.LastActiveAt = now
	s.ExpiresAt = now.Add(serviceTerminalSessionTTL)
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["session_id"]; !ok {
		data["session_id"] = s.ID
	}
	event := serviceTerminalEvent{Type: eventType, Data: data}
	s.events = append(s.events, event)
	for subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (s *serviceTerminalSession) subscribe() ([]serviceTerminalEvent, chan serviceTerminalEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := append([]serviceTerminalEvent(nil), s.events...)
	ch := make(chan serviceTerminalEvent, 32)
	if s.subscribers == nil {
		s.subscribers = map[chan serviceTerminalEvent]struct{}{}
	}
	s.subscribers[ch] = struct{}{}
	return history, ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subscribers, ch)
		close(ch)
	}
}

func (s *serviceTerminalSession) close(status string) {
	s.mu.Lock()
	if s.Status == "closed" || s.Status == "failed" || s.Status == "exited" {
		s.mu.Unlock()
		return
	}
	s.Status = status
	stdin := s.stdin
	cancel := s.cancel
	ptyFile := s.ptyFile
	s.stdin = nil
	s.cancel = nil
	s.ptyFile = nil
	s.mu.Unlock()
	if stdin != nil && stdin != ptyFile {
		_ = stdin.Close()
	}
	if ptyFile != nil {
		_ = ptyFile.Close()
	}
	if cancel != nil {
		cancel()
	}
	s.appendEvent("status", map[string]any{"status": status})
}

func clampTerminalRows(rows int) int {
	rows = max(rows, 24)
	if rows > 200 {
		return 200
	}
	return rows
}

func clampTerminalCols(cols int) int {
	cols = max(cols, 80)
	if cols > 400 {
		return 400
	}
	return cols
}

func runServiceCommand(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, agentCLIManager *agentcli.Manager, jobs *agent.JobRegistry) error {
	return runServiceCommandWithBroker(ctx, cfg, rt, subagentManager, agentCLIManager, jobs, nil)
}

func runServiceCommandWithBroker(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, agentCLIManager *agentcli.Manager, jobs *agent.JobRegistry, broker *approval.Broker) error {
	return runServiceCommandWithBrokerOptions(ctx, cfg, rt, subagentManager, agentCLIManager, jobs, broker, false)
}

func runServiceCommandWithBrokerOptions(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, agentCLIManager *agentcli.Manager, jobs *agent.JobRegistry, broker *approval.Broker, unsafeDev bool) error {
	return runServiceCommandWithBrokerOptionsAndCron(ctx, cfg, rt, subagentManager, agentCLIManager, jobs, broker, unsafeDev, nil)
}

func runServiceCommandWithBrokerOptionsAndCron(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, agentCLIManager *agentcli.Manager, jobs *agent.JobRegistry, broker *approval.Broker, unsafeDev bool, cronSvc *cron.Service) error {
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		return fmt.Errorf("service secret is required")
	}
	if err := validateStartupCommandWithOptions("service", cfg, unsafeDev, false); err != nil {
		return err
	}
	if rt == nil {
		return fmt.Errorf("runtime not configured")
	}
	if jobs == nil {
		jobs = agent.NewJobRegistry(0, 0)
	}
	server := &serviceServer{config: cfg, configPath: cfgPathOrDefault(""), runtime: rt, cronSvc: cronSvc, subagentManager: subagentManager, agentCLIManager: agentCLIManager, jobs: jobs, broker: broker, unsafeDev: unsafeDev}
	authSvc := server.app().Auth()
	mux := newServiceMux(server)

	httpServer := &http.Server{
		Addr:              cfg.Service.Listen,
		Handler:           serviceBrowserMiddleware(cfg, serviceAuthMiddlewareWithBrokerAndLimiter(cfg, broker, authSvc, server, serviceBoundaryMiddleware(server, mux))),
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
	mux.Handle("/internal/v1/subagents/", http.HandlerFunc(server.handleSubagents))
	mux.Handle("/internal/v1/jobs/", http.HandlerFunc(server.handleJobs))
	mux.Handle("/internal/v1/artifacts/", http.HandlerFunc(server.handleArtifacts))
	mux.Handle("/internal/v1/pairing/requests", http.HandlerFunc(server.handlePairing))
	mux.Handle("/internal/v1/pairing/requests/", http.HandlerFunc(server.handlePairing))
	mux.Handle("/internal/v1/pairing/exchange", http.HandlerFunc(server.handlePairing))
	mux.Handle("/internal/v1/devices", http.HandlerFunc(server.handleDevices))
	mux.Handle("/internal/v1/devices/", http.HandlerFunc(server.handleDevices))
	mux.Handle("/internal/v1/approvals", http.HandlerFunc(server.handleApprovals))
	mux.Handle("/internal/v1/approvals/", http.HandlerFunc(server.handleApprovals))
	mux.Handle("/internal/v1/auth/capabilities", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/session", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/session/", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/passkeys", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/passkeys/", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/passkeys/registration/", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/passkeys/login/", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/auth/step-up/", http.HandlerFunc(server.handleAuth))
	mux.Handle("/internal/v1/health", http.HandlerFunc(server.handleHealth))
	mux.Handle("/internal/v1/readiness", http.HandlerFunc(server.handleReadiness))
	mux.Handle("/internal/v1/capabilities", http.HandlerFunc(server.handleCapabilities))
	mux.Handle("/internal/v1/app/bootstrap", http.HandlerFunc(server.handleApp))
	mux.Handle("/internal/v1/actions/", http.HandlerFunc(server.handleActions))
	mux.Handle("/internal/v1/cron", http.HandlerFunc(server.handleCron))
	mux.Handle("/internal/v1/cron/", http.HandlerFunc(server.handleCron))
	mux.Handle("/internal/v1/embeddings", http.HandlerFunc(server.handleEmbeddings))
	mux.Handle("/internal/v1/embeddings/", http.HandlerFunc(server.handleEmbeddings))
	mux.Handle("/internal/v1/audit", http.HandlerFunc(server.handleAudit))
	mux.Handle("/internal/v1/audit/", http.HandlerFunc(server.handleAudit))
	mux.Handle("/internal/v1/scope", http.HandlerFunc(server.handleScope))
	mux.Handle("/internal/v1/scope/", http.HandlerFunc(server.handleScope))
	mux.Handle("/internal/v1/configure", http.HandlerFunc(server.handleConfigure))
	mux.Handle("/internal/v1/configure/", http.HandlerFunc(server.handleConfigure))
	mux.Handle("/internal/v1/skills", http.HandlerFunc(server.handleSkills))
	mux.Handle("/internal/v1/skills/", http.HandlerFunc(server.handleSkills))
	mux.Handle("/internal/v1/files", http.HandlerFunc(server.handleFiles))
	mux.Handle("/internal/v1/files/", http.HandlerFunc(server.handleFiles))
	mux.Handle("/internal/v1/terminal/sessions", http.HandlerFunc(server.handleTerminal))
	mux.Handle("/internal/v1/terminal/sessions/", http.HandlerFunc(server.handleTerminal))
	mux.Handle("/internal/v1/agent-runners", http.HandlerFunc(server.handleAgentRunners))
	mux.Handle("/internal/v1/agent-runs", http.HandlerFunc(server.handleAgentRuns))
	mux.Handle("/internal/v1/agent-runs/", http.HandlerFunc(server.handleAgentRuns))
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
		s.appSvc = app.NewServiceAppWithAgentCLI(s.config, s.runtime, s.jobs, s.subagentManager, s.agentCLIManager, s.control())
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
		if retryAfter := s.serviceAuthRetryAfter(r, "pairing"); retryAfter > 0 {
			writeServiceAuthRateLimit(w, r, retryAfter)
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
		pairingScope := fmt.Sprintf("pairing:%d", body.RequestID)
		if retryAfter := s.serviceAuthRetryAfter(r, pairingScope); retryAfter > 0 {
			writeServiceAuthRateLimit(w, r, retryAfter)
			return
		}
		device, token, err := appSvc.ExchangePairingCode(r.Context(), approval.PairingExchangeInput{RequestID: body.RequestID, Code: body.Code})
		if err != nil {
			s.recordServiceAuthFailure(r, "pairing")
			s.recordServiceAuthFailure(r, pairingScope)
			if message, ok := servicePublicPairingExchangeError(err); ok {
				writeServiceJSON(w, http.StatusBadRequest, serviceErrorPayload(r, message))
			} else {
				writeServiceError(w, r, http.StatusBadRequest, "pairing exchange failed", err)
			}
			return
		}
		s.clearServiceAuthFailures(r, "pairing")
		s.clearServiceAuthFailures(r, pairingScope)
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

type serviceFileRoot struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Path     string `json:"path"`
	Writable bool   `json:"writable"`
}

type serviceFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
}

type serviceFileSearchItem struct {
	serviceFileEntry
	RootID    string `json:"root_id"`
	RootLabel string `json:"root_label"`
}

type serviceFileReadResponse struct {
	RootID     string `json:"root_id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	MimeType   string `json:"mime_type,omitempty"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
	Revision   string `json:"revision"`
	Writable   bool   `json:"writable"`
	Content    string `json:"content"`
}

func (s *serviceServer) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/files")
	path = strings.Trim(path, "/")
	switch path {
	case "roots":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": s.serviceFileRoots()})
	case "list":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileList(w, r)
	case "search":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileSearch(w, r)
	case "stat":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileStat(w, r)
	case "read":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileRead(w, r)
	case "download":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileDownload(w, r)
	case "write":
		if r.Method != http.MethodPut {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileWrite(w, r)
	case "upload":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileUpload(w, r)
	case "mkdir":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileMkdir(w, r)
	case "delete":
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file deletion is disabled in v1"})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "file route not found"})
	}
}

func (s *serviceServer) serviceFileRoots() []serviceFileRoot {
	var roots []serviceFileRoot
	splitReadWrite := s.config.Tools.RestrictToWorkspace && s.config.Tools.AllowFullFileRead
	add := func(id, label, path string, writable bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			roots = append(roots, serviceFileRoot{ID: id, Label: label, Path: abs, Writable: writable})
		}
	}
	if splitReadWrite {
		add("computer", "Computer", string(filepath.Separator), false)
	}
	add("allowed", "Allowed Folder", s.config.AllowedDir, !splitReadWrite)
	add("workspace", "Workspace", s.config.WorkspaceDir, true)
	add("artifacts", "Artifacts", s.config.ArtifactsDir, false)
	if len(roots) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			roots = append(roots, serviceFileRoot{ID: "cwd", Label: "Current Directory", Path: cwd, Writable: !splitReadWrite})
		}
	}
	return roots
}

func (s *serviceServer) serviceFileRootByID(id string) (serviceFileRoot, bool) {
	for _, root := range s.serviceFileRoots() {
		if root.ID == id {
			return root, true
		}
	}
	return serviceFileRoot{}, false
}

func (s *serviceServer) defaultSearchFileRoot() (serviceFileRoot, bool) {
	roots := s.serviceFileRoots()
	for _, id := range []string{"workspace", "allowed", "computer", "cwd"} {
		for _, root := range roots {
			if root.ID == id {
				return root, true
			}
		}
	}
	if len(roots) == 0 {
		return serviceFileRoot{}, false
	}
	return roots[0], true
}

func (s *serviceServer) resolveServiceFilePath(rootID, relPath string) (serviceFileRoot, string, string, error) {
	root, ok := s.serviceFileRootByID(strings.TrimSpace(rootID))
	if !ok {
		return serviceFileRoot{}, "", "", fmt.Errorf("unknown file root")
	}
	cleanRel := filepath.Clean(strings.TrimSpace(relPath))
	if cleanRel == "." || cleanRel == string(filepath.Separator) {
		cleanRel = "."
	}
	if filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, "..") || strings.Contains(cleanRel, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return serviceFileRoot{}, "", "", fmt.Errorf("path escapes root")
	}
	absRoot, err := filepath.Abs(root.Path)
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	absPath, err := filepath.Abs(filepath.Join(absRoot, cleanRel))
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	realPath, err := resolveExistingServicePath(realRoot, cleanRel)
	if err != nil {
		return serviceFileRoot{}, "", "", err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return serviceFileRoot{}, "", "", fmt.Errorf("path escapes root")
	}
	displayRel, err := filepath.Rel(absRoot, absPath)
	if err != nil || displayRel == ".." || strings.HasPrefix(displayRel, ".."+string(filepath.Separator)) {
		return serviceFileRoot{}, "", "", fmt.Errorf("path escapes root")
	}
	return root, absPath, filepath.ToSlash(displayRel), nil
}

func resolveExistingServicePath(realRoot, cleanRel string) (string, error) {
	if cleanRel == "." {
		return realRoot, nil
	}
	current := realRoot
	for _, part := range strings.Split(cleanRel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(current, part)
		evaluated, err := filepath.EvalSymlinks(next)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return next, nil
			}
			return "", err
		}
		current = evaluated
	}
	return current, nil
}

func (s *serviceServer) handleFileList(w http.ResponseWriter, r *http.Request) {
	root, absPath, rel, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	items, err := os.ReadDir(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "directory unavailable", err)
		return
	}
	entries := make([]serviceFileEntry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			continue
		}
		entryType := "file"
		if info.IsDir() {
			entryType = "directory"
		}
		entryRel := filepath.ToSlash(filepath.Join(rel, item.Name()))
		if rel == "." {
			entryRel = item.Name()
		}
		entries = append(entries, serviceFileEntry{Name: item.Name(), Path: entryRel, Type: entryType, Size: info.Size(), ModifiedAt: info.ModTime().Format(time.RFC3339), MimeType: mime.TypeByExtension(filepath.Ext(item.Name()))})
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"root_id": root.ID, "path": rel, "entries": entries})
}

func (s *serviceServer) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	if limit > 50 {
		limit = 50
	}
	rootID := strings.TrimSpace(r.URL.Query().Get("root_id"))
	root := serviceFileRoot{}
	var ok bool
	if rootID == "" {
		root, ok = s.defaultSearchFileRoot()
	} else {
		root, ok = s.serviceFileRootByID(rootID)
	}
	if !ok {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown file root"})
		return
	}

	_, absRoot, _, err := s.resolveServiceFilePath(root.ID, ".")
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	items := make([]serviceFileSearchItem, 0, limit)
	visited := 0
	const maxVisited = 5000
	_ = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || path == absRoot {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() && isIgnoredSearchDir(name) {
			return filepath.SkipDir
		}
		visited++
		if visited > maxVisited {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if query != "" && !fileSearchMatches(query, name, slashRel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		items = append(items, serviceFileSearchItem{
			serviceFileEntry: serviceFileEntry{Name: name, Path: slashRel, Type: "file", Size: info.Size(), ModifiedAt: info.ModTime().Format(time.RFC3339), MimeType: mime.TypeByExtension(filepath.Ext(name))},
			RootID:           root.ID,
			RootLabel:        root.Label,
		})
		if len(items) >= limit {
			return filepath.SkipAll
		}
		return nil
	})

	writeServiceJSON(w, http.StatusOK, map[string]any{"root_id": root.ID, "query": query, "items": items})
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func isIgnoredSearchDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", ".nuxt", ".output", "dist", "build", "coverage", ".cache", "vendor":
		return true
	default:
		return false
	}
}

func fileSearchMatches(query, name, path string) bool {
	if query == "" {
		return true
	}
	lowerName := strings.ToLower(name)
	lowerPath := strings.ToLower(path)
	for _, token := range strings.Fields(query) {
		if !strings.Contains(lowerName, token) && !strings.Contains(lowerPath, token) {
			return false
		}
	}
	return true
}

func serviceFileRevision(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UTC().UnixNano(), info.Size())
}

func isTextLikeMime(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	return strings.Contains(mimeType, "json") || strings.Contains(mimeType, "xml") || strings.Contains(mimeType, "javascript") || strings.Contains(mimeType, "yaml") || strings.Contains(mimeType, "toml")
}

func isTextLikeExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env", ".csv", ".ts", ".tsx", ".js", ".jsx", ".vue", ".go", ".py", ".rb", ".php", ".java", ".kt", ".swift", ".sql", ".html", ".css", ".scss", ".sh":
		return true
	default:
		return false
	}
}

func serviceFileTextAllowed(name, mimeType string, content []byte) bool {
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		return false
	}
	if isTextLikeMime(mimeType) || isTextLikeExtension(name) {
		return true
	}
	return false
}

func (s *serviceServer) handleFileStat(w http.ResponseWriter, r *http.Request) {
	root, absPath, rel, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "file unavailable", err)
		return
	}
	entryType := "file"
	if info.IsDir() {
		entryType = "directory"
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"item": serviceFileEntry{Name: info.Name(), Path: rel, Type: entryType, Size: info.Size(), ModifiedAt: info.ModTime().Format(time.RFC3339), MimeType: mime.TypeByExtension(filepath.Ext(info.Name()))}, "root_id": root.ID})
}

func (s *serviceServer) handleFileRead(w http.ResponseWriter, r *http.Request) {
	root, absPath, rel, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "file unavailable", err)
		return
	}
	if info.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "read target is not a file"})
		return
	}
	if info.Size() > serviceFileTextReadLimit {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is too large for inline editing"})
		return
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file read failed", err)
		return
	}
	mimeType := mime.TypeByExtension(filepath.Ext(info.Name()))
	if !serviceFileTextAllowed(info.Name(), mimeType, content) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is not a supported text document"})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"root_id":     root.ID,
		"path":        rel,
		"name":        info.Name(),
		"mime_type":   mimeType,
		"size":        info.Size(),
		"modified_at": info.ModTime().UTC().Format(time.RFC3339),
		"revision":    serviceFileRevision(info),
		"writable":    root.Writable,
		"content":     string(content),
	})
}

func (s *serviceServer) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	_, absPath, _, err := s.resolveServiceFilePath(r.URL.Query().Get("root_id"), r.URL.Query().Get("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	file, err := os.Open(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusNotFound, "file unavailable", err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "download target is not a file"})
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *serviceServer) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceFileTextWriteLimit)
	var body struct {
		RootID           string `json:"root_id"`
		Path             string `json:"path"`
		Content          string `json:"content"`
		ExpectedRevision string `json:"expected_revision"`
		Create           bool   `json:"create"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	root, absPath, rel, err := s.resolveServiceFilePath(body.RootID, body.Path)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !root.Writable {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file root is read-only"})
		return
	}
	contentBytes := []byte(body.Content)
	if int64(len(contentBytes)) > serviceFileTextWriteLimit {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is too large for inline editing"})
		return
	}
	name := filepath.Base(absPath)
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if !serviceFileTextAllowed(name, mimeType, contentBytes) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "file is not a supported text document"})
		return
	}
	parent := filepath.Dir(absPath)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "parent directory does not exist"})
		return
	}
	existingInfo, statErr := os.Stat(absPath)
	if statErr == nil && existingInfo.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "write target is not a file"})
		return
	}
	status := http.StatusOK
	resultStatus := "written"
	if statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			writeServiceError(w, r, http.StatusBadGateway, "file stat failed", statErr)
			return
		}
		if !body.Create {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "file does not exist"})
			return
		}
		status = http.StatusCreated
		resultStatus = "created"
	} else {
		if body.Create {
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "file already exists"})
			return
		}
		if body.ExpectedRevision != "" && body.ExpectedRevision != serviceFileRevision(existingInfo) {
			writeServiceJSON(w, http.StatusConflict, map[string]any{
				"error":            "file has changed on disk",
				"modified_at":      existingInfo.ModTime().UTC().Format(time.RFC3339),
				"current_revision": serviceFileRevision(existingInfo),
			})
			return
		}
	}
	tmp, err := os.CreateTemp(parent, ".or3-write-*")
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "could not prepare file write", err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(contentBytes); err != nil {
		_ = tmp.Close()
		writeServiceError(w, r, http.StatusBadGateway, "file write failed", err)
		return
	}
	if err := tmp.Close(); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file write failed", err)
		return
	}
	if err := os.Rename(tmpName, absPath); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file write failed", err)
		return
	}
	updatedInfo, err := os.Stat(absPath)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file stat failed", err)
		return
	}
	writeServiceJSON(w, status, map[string]any{
		"root_id":     root.ID,
		"path":        rel,
		"status":      resultStatus,
		"modified_at": updatedInfo.ModTime().UTC().Format(time.RFC3339),
		"revision":    serviceFileRevision(updatedInfo),
	})
}

func (s *serviceServer) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, serviceFileUploadBodyLimit)
	if err := r.ParseMultipartForm(serviceFileUploadBodyLimit); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid multipart upload"})
		return
	}
	root, dirPath, rel, err := s.resolveServiceFilePath(r.FormValue("root_id"), r.FormValue("path"))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !root.Writable {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file root is read-only"})
		return
	}
	source, header, err := r.FormFile("file")
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "missing file"})
		return
	}
	defer source.Close()
	name := filepath.Base(header.Filename)
	if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid file name"})
		return
	}
	target := filepath.Join(dirPath, name)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		writeServiceError(w, r, http.StatusConflict, "file already exists or cannot be created", err)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, source); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "file upload failed", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{"root_id": root.ID, "path": filepath.ToSlash(filepath.Join(rel, name)), "status": "uploaded"})
}

func (s *serviceServer) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
	var body struct {
		RootID string `json:"root_id"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	root, dirPath, rel, err := s.resolveServiceFilePath(body.RootID, body.Path)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !root.Writable {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "file root is read-only"})
		return
	}
	name := filepath.Base(strings.TrimSpace(body.Name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid directory name"})
		return
	}
	target := filepath.Join(dirPath, name)
	if err := os.Mkdir(target, 0o700); err != nil {
		writeServiceError(w, r, http.StatusConflict, "directory already exists or cannot be created", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{"root_id": root.ID, "path": filepath.ToSlash(filepath.Join(rel, name)), "status": "created"})
}

func (s *serviceServer) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	if !s.terminalAvailable() {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "terminal mode is not enabled"})
		return
	}
	s.cleanupTerminalSessions()
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/terminal/sessions")
	relative = strings.Trim(relative, "/")
	if relative == "" {
		switch r.Method {
		case http.MethodGet:
			s.listTerminalSessions(w, r)
		case http.MethodPost:
			s.createTerminalSession(w, r)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
		return
	}
	parts := strings.Split(relative, "/")
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.getTerminalSession(w, r, strings.TrimSpace(parts[0]))
		return
	}
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal route not found"})
		return
	}
	sessionID := strings.TrimSpace(parts[0])
	switch strings.TrimSpace(parts[1]) {
	case "stream":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.streamTerminalSession(w, r, sessionID)
	case "ws-ticket":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.issueTerminalWebSocketTicket(w, r, sessionID)
	case "ws":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleTerminalWebSocket(w, r, sessionID)
	case "input":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.writeTerminalInput(w, r, sessionID)
	case "resize":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.resizeTerminalSession(w, r, sessionID)
	case "close":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.closeTerminalSession(w, r, sessionID)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal action not found"})
	}
}

func (s *serviceServer) listTerminalSessions(w http.ResponseWriter, _ *http.Request) {
	if s == nil {
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{}})
		return
	}
	s.terminalMu.Lock()
	sessions := make([]*serviceTerminalSession, 0, len(s.terminalSessions))
	for _, session := range s.terminalSessions {
		if session != nil && session.Status == "running" {
			sessions = append(sessions, session)
		}
	}
	s.terminalMu.Unlock()
	slices.SortFunc(sessions, func(left, right *serviceTerminalSession) int {
		if left == nil && right == nil {
			return 0
		}
		if left == nil {
			return 1
		}
		if right == nil {
			return -1
		}
		if left.LastActiveAt.After(right.LastActiveAt) {
			return -1
		}
		if left.LastActiveAt.Before(right.LastActiveAt) {
			return 1
		}
		if left.CreatedAt.After(right.CreatedAt) {
			return -1
		}
		if left.CreatedAt.Before(right.CreatedAt) {
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
	items := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, session.snapshot())
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *serviceServer) terminalAvailable() bool {
	if s == nil {
		return false
	}
	spec := config.ProfileSpec(s.config.RuntimeProfile)
	if spec.ForbidExecShell || spec.ForbidPrivilegedTools {
		return false
	}
	if spec.RequireSandboxForExec {
		return false
	}
	return s.config.Hardening.GuardedTools && s.config.Hardening.PrivilegedTools && s.config.Hardening.EnableExecShell
}

func (s *serviceServer) cleanupTerminalSessions() {
	if s == nil {
		return
	}
	now := time.Now().UTC()
	s.terminalMu.Lock()
	sessions := make([]*serviceTerminalSession, 0)
	for id, session := range s.terminalSessions {
		if session == nil || now.After(session.ExpiresAt) {
			delete(s.terminalSessions, id)
			if session != nil {
				sessions = append(sessions, session)
			}
		}
	}
	s.terminalMu.Unlock()
	for _, session := range sessions {
		session.close("expired")
	}
}

func (s *serviceServer) getTerminalSessionByID(sessionID string) (*serviceTerminalSession, bool) {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminalSessions == nil {
		return nil, false
	}
	session, ok := s.terminalSessions[sessionID]
	if !ok || session == nil {
		return nil, false
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		delete(s.terminalSessions, sessionID)
		go session.close("expired")
		return nil, false
	}
	return session, true
}

func (s *serviceServer) allocateTerminalSessionID() (string, error) {
	for range 8 {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id := "term_" + hex.EncodeToString(b)
		s.terminalMu.Lock()
		_, exists := s.terminalSessions[id]
		s.terminalMu.Unlock()
		if !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to allocate unique terminal session id")
}

func (s *serviceServer) createTerminalSession(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceTerminalBodyLimit)
	var body struct {
		RootID        string `json:"root_id"`
		Path          string `json:"path"`
		Shell         string `json:"shell"`
		Rows          int    `json:"rows"`
		Cols          int    `json:"cols"`
		ApprovalToken string `json:"approval_token"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	root, workingDir, rel, err := s.resolveServiceFilePath(body.RootID, body.Path)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.terminalMu.Lock()
	activeSessions := len(s.terminalSessions)
	s.terminalMu.Unlock()
	if activeSessions >= serviceTerminalMaxSessions {
		writeServiceJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many active terminal sessions"})
		return
	}
	shellPath, err := resolveTerminalShell(strings.TrimSpace(body.Shell))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	approvalDecision, err := s.evaluateTerminalApproval(r.Context(), shellPath, workingDir, body.ApprovalToken)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "terminal approval failed", err)
		return
	}
	if approvalDecision.RequiresApproval {
		writeServiceJSON(w, http.StatusConflict, map[string]any{
			"error":             "terminal session requires approval",
			"requires_approval": true,
			"request_id":        approvalDecision.RequestID,
			"subject_hash":      approvalDecision.SubjectHash,
			"reason":            approvalDecision.Reason,
		})
		return
	}
	if !approvalDecision.Allowed {
		reason := strings.TrimSpace(approvalDecision.Reason)
		if reason == "" {
			reason = "terminal session denied"
		}
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": reason})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	shellArgs := terminalShellArgs(shellPath)
	cmd := exec.CommandContext(ctx, shellPath, shellArgs...)
	cmd.Dir = workingDir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"SHELL="+shellPath,
	)
	rows := clampTerminalRows(body.Rows)
	cols := clampTerminalCols(body.Cols)
	ptyFile, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		cancel()
		writeServiceError(w, r, http.StatusBadGateway, "failed to allocate terminal pty", err)
		return
	}
	sessionID, err := s.allocateTerminalSessionID()
	if err != nil {
		_ = ptyFile.Close()
		cancel()
		writeServiceError(w, r, http.StatusInternalServerError, "failed to allocate terminal session", err)
		return
	}
	session := &serviceTerminalSession{
		ID:            sessionID,
		RootID:        root.ID,
		RelativePath:  filepath.ToSlash(rel),
		WorkingDir:    workingDir,
		Shell:         shellPath,
		CreatedAt:     time.Now().UTC(),
		ExpiresAt:     time.Now().UTC().Add(serviceTerminalSessionTTL),
		LastActiveAt:  time.Now().UTC(),
		Status:        "running",
		Rows:          rows,
		Cols:          cols,
		ApprovalMode:  string(s.config.Security.Approvals.Exec.Mode),
		ApprovalState: approvalDecision.Reason,
		ApprovalID:    approvalDecision.RequestID,
		cmd:           cmd,
		ptyFile:       ptyFile,
		stdin:         ptyFile, // PTY master is read+write; tests rely on stdin being a WriteCloser
		cancel:        cancel,
		subscribers:   map[chan serviceTerminalEvent]struct{}{},
	}
	session.appendEvent("status", map[string]any{"status": "running"})
	session.appendEvent("snapshot", session.snapshot())
	s.terminalMu.Lock()
	if s.terminalSessions == nil {
		s.terminalSessions = map[string]*serviceTerminalSession{}
	}
	s.terminalSessions[session.ID] = session
	s.terminalMu.Unlock()
	go s.collectTerminalOutput(session, ptyFile, "pty")
	go s.waitForTerminalSession(session)
	writeServiceJSON(w, http.StatusCreated, session.snapshot())
}

func terminalShellArgs(shellPath string) []string {
	switch filepath.Base(shellPath) {
	case "bash", "zsh":
		return []string{"-il"}
	case "sh":
		return []string{"-i"}
	default:
		return nil
	}
}

func (s *serviceServer) evaluateTerminalApproval(ctx context.Context, shellPath, workingDir, approvalToken string) (approval.Decision, error) {
	if s == nil || s.broker == nil {
		return approval.Decision{Allowed: true, Reason: "broker_unavailable"}, nil
	}
	return s.broker.EvaluateExec(ctx, approval.ExecEvaluation{
		ExecutablePath: shellPath,
		Argv:           []string{"terminal"},
		WorkingDir:     workingDir,
		ToolName:       "terminal",
		ApprovalToken:  approvalToken,
	})
}

func resolveTerminalShell(requested string) (string, error) {
	allowed := []string{"bash", "sh", "zsh"}
	if requested == "" {
		requested = strings.TrimSpace(os.Getenv("SHELL"))
	}
	if requested == "" {
		requested = "/bin/sh"
	}
	if strings.ContainsRune(requested, filepath.Separator) {
		base := filepath.Base(requested)
		if !slices.Contains(allowed, base) {
			return "", fmt.Errorf("shell is not allowed")
		}
		if _, err := os.Stat(requested); err != nil {
			return "", fmt.Errorf("shell not found")
		}
		return requested, nil
	}
	if !slices.Contains(allowed, requested) {
		return "", fmt.Errorf("shell is not allowed")
	}
	resolved, err := exec.LookPath(requested)
	if err != nil {
		return "", fmt.Errorf("shell not found")
	}
	return resolved, nil
}

func (s *serviceServer) collectTerminalOutput(session *serviceTerminalSession, reader io.ReadCloser, stream string) {
	defer reader.Close()
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			session.appendEvent("output", map[string]any{"stream": stream, "chunk": string(buf[:n])})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				session.appendEvent("error", map[string]any{"stream": stream, "error": err.Error()})
			}
			return
		}
	}
}

func (s *serviceServer) waitForTerminalSession(session *serviceTerminalSession) {
	err := session.cmd.Wait()
	status := "exited"
	if err != nil {
		status = "failed"
		session.appendEvent("error", map[string]any{"error": err.Error()})
	}
	session.close(status)
}

func (s *serviceServer) getTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	writeServiceJSON(w, http.StatusOK, session.snapshot())
}

func (s *serviceServer) streamTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	if err := beginSSE(w); err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "streaming is not supported", err)
		return
	}
	history, events, unsubscribe := session.subscribe()
	defer unsubscribe()
	for _, event := range history {
		if err := writeSSEEvent(w, event.Type, event.Data); err != nil {
			return
		}
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

func (s *serviceServer) issueTerminalWebSocketTicket(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, ok := s.getTerminalSessionByID(sessionID); !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	ticket, expiresAt, err := s.issueTerminalWebSocketTicketValue(sessionID, time.Now().UTC())
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "failed to issue terminal websocket ticket", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

func (s *serviceServer) issueTerminalWebSocketTicketValue(sessionID string, now time.Time) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("service unavailable")
	}
	ticket, err := randomHex(24)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := now.UTC().Add(serviceTerminalWebSocketTicketTTL)
	hash := terminalWebSocketTicketHash(ticket)
	s.terminalWSTicketMu.Lock()
	defer s.terminalWSTicketMu.Unlock()
	if s.terminalWSTickets == nil {
		s.terminalWSTickets = map[string]serviceTerminalWebSocketTicket{}
	}
	s.cleanupTerminalWebSocketTicketsLocked(now.UTC())
	s.terminalWSTickets[hash] = serviceTerminalWebSocketTicket{SessionID: sessionID, ExpiresAt: expiresAt}
	return ticket, expiresAt, nil
}

func (s *serviceServer) consumeTerminalWebSocketTicket(sessionID string, ticket string, now time.Time) bool {
	if s == nil {
		return false
	}
	ticket = strings.TrimSpace(ticket)
	if sessionID == "" || ticket == "" {
		return false
	}
	hash := terminalWebSocketTicketHash(ticket)
	s.terminalWSTicketMu.Lock()
	defer s.terminalWSTicketMu.Unlock()
	s.cleanupTerminalWebSocketTicketsLocked(now.UTC())
	record, ok := s.terminalWSTickets[hash]
	if !ok {
		return false
	}
	if record.SessionID != sessionID {
		return false
	}
	if now.UTC().After(record.ExpiresAt) {
		delete(s.terminalWSTickets, hash)
		return false
	}
	delete(s.terminalWSTickets, hash)
	return true
}

func (s *serviceServer) cleanupTerminalWebSocketTicketsLocked(now time.Time) {
	for hash, record := range s.terminalWSTickets {
		if now.After(record.ExpiresAt) {
			delete(s.terminalWSTickets, hash)
		}
	}
}

func terminalWebSocketTicketHash(ticket string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ticket)))
	return hex.EncodeToString(sum[:])
}

func terminalWebSocketRequestedProtocols(r *http.Request) []string {
	if r == nil {
		return nil
	}
	raw := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Protocol"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	protocols := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			protocols = append(protocols, part)
		}
	}
	return protocols
}

func terminalWebSocketProtocolRequested(r *http.Request) bool {
	for _, protocol := range terminalWebSocketRequestedProtocols(r) {
		if protocol == serviceTerminalWebSocketProtocol {
			return true
		}
	}
	return false
}

func terminalWebSocketTicketFromRequest(r *http.Request) (string, bool) {
	for _, protocol := range terminalWebSocketRequestedProtocols(r) {
		if strings.HasPrefix(protocol, serviceTerminalWebSocketTicketPrefix) {
			ticket := strings.TrimSpace(strings.TrimPrefix(protocol, serviceTerminalWebSocketTicketPrefix))
			return ticket, ticket != ""
		}
	}
	return "", false
}

func (s *serviceServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !terminalWebSocketProtocolRequested(r) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "terminal websocket protocol is required"})
		return
	}
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: serviceTerminalWebSocketHandshakeTimeout,
		Subprotocols:     []string{serviceTerminalWebSocketProtocol},
		CheckOrigin:      s.terminalWebSocketOriginAllowed,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(serviceTerminalBodyLimit)

	history, events, unsubscribe := session.subscribe()
	defer unsubscribe()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		s.readTerminalWebSocket(conn, sessionID)
	}()

	pings := time.NewTicker(serviceTerminalWebSocketPingInterval)
	defer pings.Stop()

	writeEvent := func(event serviceTerminalEvent) error {
		if err := conn.SetWriteDeadline(time.Now().Add(serviceTerminalWebSocketWriteTimeout)); err != nil {
			return err
		}
		return conn.WriteJSON(event)
	}
	closeNormally := func(reason string) {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), time.Now().Add(serviceTerminalWebSocketWriteTimeout))
	}
	for _, event := range history {
		if err := writeEvent(event); err != nil {
			return
		}
		if terminalSessionEventIsTerminal(event) {
			closeNormally("terminal session ended")
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-readDone:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeEvent(event); err != nil {
				return
			}
			if terminalSessionEventIsTerminal(event) {
				closeNormally("terminal session ended")
				return
			}
		case <-pings.C:
			deadline := time.Now().Add(serviceTerminalWebSocketWriteTimeout)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				return
			}
		}
	}
}

func (s *serviceServer) terminalWebSocketOriginAllowed(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("Origin")) == "" {
		return true
	}
	_, ok := serviceAllowedBrowserOrigin(s.config, r)
	return ok
}

type serviceTerminalWebSocketClientMessage struct {
	Type  string `json:"type"`
	Input string `json:"input"`
	Rows  int    `json:"rows"`
	Cols  int    `json:"cols"`
}

func (s *serviceServer) readTerminalWebSocket(conn *websocket.Conn, sessionID string) {
	for {
		var msg serviceTerminalWebSocketClientMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				terminalWebSocketClose(conn, websocket.CloseUnsupportedData, "invalid terminal websocket message")
			}
			return
		}
		switch strings.TrimSpace(msg.Type) {
		case "input":
			if msg.Input == "" {
				terminalWebSocketClose(conn, websocket.CloseUnsupportedData, "input is required")
				return
			}
			if err := s.terminalWriteInput(sessionID, msg.Input); err != nil {
				terminalWebSocketClose(conn, terminalWebSocketCloseCodeForError(err), err.Error())
				return
			}
		case "resize":
			if msg.Rows <= 0 && msg.Cols <= 0 {
				terminalWebSocketClose(conn, websocket.CloseUnsupportedData, errTerminalResizeRequired.Error())
				return
			}
			if _, _, err := s.terminalResize(sessionID, msg.Rows, msg.Cols); err != nil {
				terminalWebSocketClose(conn, terminalWebSocketCloseCodeForError(err), err.Error())
				return
			}
		case "close":
			if err := s.terminalClose(sessionID, "closed"); err != nil {
				terminalWebSocketClose(conn, terminalWebSocketCloseCodeForError(err), err.Error())
				return
			}
		default:
			terminalWebSocketClose(conn, websocket.CloseUnsupportedData, "unknown terminal websocket message type")
			return
		}
	}
}

func terminalWebSocketClose(conn *websocket.Conn, code int, reason string) {
	if conn == nil {
		return
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(serviceTerminalWebSocketWriteTimeout))
}

func terminalWebSocketCloseCodeForError(err error) int {
	switch {
	case errors.Is(err, errTerminalSessionNotFound):
		return websocket.ClosePolicyViolation
	case errors.Is(err, errTerminalInputRequired), errors.Is(err, errTerminalResizeRequired):
		return websocket.CloseUnsupportedData
	case errors.Is(err, errTerminalSessionNotWritable):
		return websocket.ClosePolicyViolation
	default:
		return websocket.CloseInternalServerErr
	}
}

func terminalSessionEventIsTerminal(event serviceTerminalEvent) bool {
	if event.Type != "status" {
		return false
	}
	status, _ := event.Data["status"].(string)
	return isTerminalSessionStatus(status)
}

func isTerminalSessionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", "failed", "exited", "expired":
		return true
	default:
		return false
	}
}

func (s *serviceServer) writeTerminalInput(w http.ResponseWriter, r *http.Request, sessionID string) {
	limitServiceRequestBody(w, r, serviceTerminalBodyLimit)
	var body struct {
		Input string `json:"input"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	if err := s.terminalWriteInput(sessionID, body.Input); err != nil {
		switch {
		case errors.Is(err, errTerminalSessionNotFound):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		case errors.Is(err, errTerminalInputRequired):
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "input is required"})
		case errors.Is(err, errTerminalSessionNotWritable):
			writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "terminal session is not writable"})
		default:
			writeServiceError(w, r, http.StatusBadGateway, "failed to write terminal input", err)
		}
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID})
}

func (s *serviceServer) terminalWriteInput(sessionID string, input string) error {
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		return errTerminalSessionNotFound
	}
	if input == "" {
		return errTerminalInputRequired
	}
	session.mu.Lock()
	stdin := session.stdin
	status := session.Status
	session.mu.Unlock()
	if stdin == nil || status != "running" {
		return errTerminalSessionNotWritable
	}
	if _, err := io.WriteString(stdin, input); err != nil {
		return fmt.Errorf("failed to write terminal input: %w", err)
	}
	session.appendEvent("input", map[string]any{"size": len(input)})
	return nil
}

func (s *serviceServer) resizeTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	limitServiceRequestBody(w, r, serviceTerminalBodyLimit)
	var body struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	rows, cols, err := s.terminalResize(sessionID, body.Rows, body.Cols)
	if err != nil {
		if errors.Is(err, errTerminalSessionNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
			return
		}
		writeServiceError(w, r, http.StatusBadGateway, "failed to resize terminal pty", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "rows": rows, "cols": cols})
}

func (s *serviceServer) terminalResize(sessionID string, rows int, cols int) (int, int, error) {
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		return 0, 0, errTerminalSessionNotFound
	}
	session.mu.Lock()
	if rows > 0 {
		session.Rows = clampTerminalRows(rows)
	}
	if cols > 0 {
		session.Cols = clampTerminalCols(cols)
	}
	session.LastActiveAt = time.Now().UTC()
	session.ExpiresAt = time.Now().UTC().Add(serviceTerminalSessionTTL)
	rows, cols = session.Rows, session.Cols
	ptyFile := session.ptyFile
	session.mu.Unlock()
	if ptyFile != nil && rows > 0 && cols > 0 {
		if err := pty.Setsize(ptyFile, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
			return rows, cols, fmt.Errorf("failed to resize terminal pty: %w", err)
		}
	}
	session.appendEvent("resize", map[string]any{"rows": rows, "cols": cols})
	return rows, cols, nil
}

func (s *serviceServer) closeTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	if err := s.terminalClose(sessionID, "closed"); err != nil {
		if errors.Is(err, errTerminalSessionNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
			return
		}
		writeServiceError(w, r, http.StatusBadGateway, "failed to close terminal session", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "status": "closed"})
}

func (s *serviceServer) terminalClose(sessionID string, status string) error {
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		return errTerminalSessionNotFound
	}
	if strings.TrimSpace(status) == "" {
		status = "closed"
	}
	session.close(status)
	s.terminalMu.Lock()
	delete(s.terminalSessions, sessionID)
	s.terminalMu.Unlock()
	return nil
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
			s.writeServiceApprovalActionError(w, r, http.StatusBadRequest, id, "approve", "approval failed", err)
			return
		}
		response := map[string]any{"request_id": id, "token": issued.Token, "allowlist_id": issued.AllowlistID}
		if sessionKey := strings.TrimSpace(issued.Request.RequesterSessionID); sessionKey != "" {
			response["session_key"] = sessionKey
		}
		warnings := make([]map[string]any, 0, 2)
		if s.broker != nil && s.broker.DB != nil {
			plans, err := approvalSkillRunPlanLookup(r.Context(), s.broker.DB, id, 20)
			if err != nil {
				warnings = append(warnings, map[string]any{
					"code":    "plan_lookup_failed",
					"message": approvalPlanLookupWarning(err),
				})
			} else {
				if len(plans) == 1 {
					response["plan_id"] = plans[0].ID
				}
				if len(plans) > 0 {
					ids := make([]string, 0, len(plans))
					for _, plan := range plans {
						if strings.TrimSpace(plan.ID) == "" {
							continue
						}
						ids = append(ids, strings.TrimSpace(plan.ID))
					}
					response["plan_ids"] = ids
				}
			}
		}
		resumeJobID, err := s.startApprovedResumeJob(r.Context(), issued, serviceAuthIdentityFromContext(r.Context()))
		if err != nil {
			warnings = append(warnings, map[string]any{
				"code":    "resume_start_failed",
				"message": approvalResumeWarning(err),
			})
		} else if strings.TrimSpace(resumeJobID) != "" {
			response["resume_job_id"] = resumeJobID
		}
		if len(warnings) > 0 {
			response["warnings"] = warnings
		}
		writeServiceJSON(w, http.StatusOK, response)
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
			s.writeServiceApprovalActionError(w, r, http.StatusBadRequest, id, "deny", "approval denial failed", err)
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
			s.writeServiceApprovalActionError(w, r, http.StatusBadRequest, id, "cancel", "approval cancel failed", err)
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
	w.Header().Set("X-Or3-Job-Id", job.ID)
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
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			s.jobs.Complete(jobID, "aborted", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"message": "job aborted"}))
			return
		}
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			s.jobs.Complete(jobID, "approval_required", serviceApprovalRequiredPayload(req.SessionKey, req.Meta, approvalErr))
			return
		}
		if fallback, ok := serviceTurnFallbackText(err, observer); ok {
			observer.finalText = fallback
			s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(req.SessionKey, req.Meta, map[string]any{"final_text": fallback, "degraded": true}))
			return
		}
		s.jobs.Fail(jobID, servicePublicJobError(err), serviceLifecyclePayload(req.SessionKey, req.Meta, nil))
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
	runCtx, cancel := context.WithCancel(withDetachedContext(ctx))
	s.jobs.AttachCancel(job.ID, cancel)
	go s.runApprovedResumeJob(runCtx, job.ID, issued, identity)
	return job.ID, nil
}

func (s *serviceServer) runApprovedResumeJob(ctx context.Context, jobID string, issued approval.IssuedApproval, identity serviceAuthIdentity) {
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
		return
	}
	if strings.TrimSpace(observer.finalText) == "" {
		observer.finalText = strings.TrimSpace(finalText)
	}
	s.jobs.Complete(jobID, "completed", serviceLifecyclePayload(sessionKey, meta, map[string]any{"final_text": observer.finalText}))
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
		if strings.Contains(err.Error(), "invalid subagent status filter") {
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
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not found"):
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
		case strings.Contains(msg, "not available for session"):
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

func (s *serviceServer) handleJobs(w http.ResponseWriter, r *http.Request) {
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/jobs/")
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	if len(parts) == 1 && strings.TrimSpace(parts[0]) != "" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		jobID := strings.TrimSpace(parts[0])
		snapshot, err := s.app().GetJob(jobID)
		if err != nil {
			if s.writePersistedSubagentJobSnapshot(w, r, jobID) {
				return
			}
			if s.writePersistedAgentCLIRunSnapshot(w, r, jobID) {
				return
			}
			if !errors.Is(err, controlplane.ErrJobNotFound) {
				writeServiceError(w, r, http.StatusServiceUnavailable, "job lookup unavailable", err)
				return
			}
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
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

type serviceAppBootstrapWarning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type serviceAppActionDescriptor struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Available       bool   `json:"available"`
	DisabledReason  string `json:"disabled_reason,omitempty"`
	SessionRequired bool   `json:"session_required,omitempty"`
	StepUpRequired  bool   `json:"step_up_required,omitempty"`
	ApprovalLikely  bool   `json:"approval_likely,omitempty"`
}

type serviceAppBootstrapResponse struct {
	Host struct {
		ID          string `json:"id,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		Version     string `json:"version,omitempty"`
	} `json:"host"`
	Pairing struct {
		Paired   bool   `json:"paired"`
		DeviceID string `json:"device_id,omitempty"`
		Role     string `json:"role,omitempty"`
	} `json:"pairing"`
	Auth struct {
		SessionRequired bool   `json:"session_required"`
		SessionActive   bool   `json:"session_active"`
		StepUpActive    bool   `json:"step_up_active"`
		Kind            string `json:"kind,omitempty"`
		Role            string `json:"role,omitempty"`
		ExecAllowed     bool   `json:"exec_allowed"`
		Capabilities    struct {
			PasskeysSupported bool `json:"passkeys_supported"`
			StepUpSupported   bool `json:"step_up_supported"`
		} `json:"capabilities"`
	} `json:"auth"`
	Status struct {
		Health       *controlplane.HealthReport       `json:"health,omitempty"`
		Readiness    *controlplane.ReadinessReport    `json:"readiness,omitempty"`
		Capabilities *controlplane.CapabilitiesReport `json:"capabilities,omitempty"`
		Summary      string                           `json:"summary"`
		Warnings     []serviceAppBootstrapWarning     `json:"warnings,omitempty"`
	} `json:"status"`
	Counts struct {
		PendingApprovals int `json:"pending_approvals"`
		ActiveJobs       int `json:"active_jobs"`
		ActiveTerminals  int `json:"active_terminals,omitempty"`
	} `json:"counts"`
	Actions  []serviceAppActionDescriptor `json:"actions"`
	Features struct {
		AppBootstrap   bool `json:"app_bootstrap"`
		AppEvents      bool `json:"app_events"`
		AppActions     bool `json:"app_actions"`
		FileMetadataV2 bool `json:"file_metadata_v2"`
	} `json:"features"`
}

type serviceActionResponse struct {
	ActionID    string `json:"action_id"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	ApprovalID  int64  `json:"approval_id,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

func (s *serviceServer) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/internal/v1/app/bootstrap" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "app route not found"})
		return
	}
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeServiceValue(w, http.StatusOK, s.buildAppBootstrap(r))
}

func (s *serviceServer) handleActions(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/actions/"), "/")
	if relative == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "action route not found"})
		return
	}
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	switch relative {
	case "restart-service":
		s.handleRestartServiceAction(w, r)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "action not found"})
	}
}

func (s *serviceServer) buildAppBootstrap(r *http.Request) serviceAppBootstrapResponse {
	var response serviceAppBootstrapResponse

	identity := serviceAuthIdentityFromContext(r.Context())
	authSvc := s.app().Auth()
	hostID := s.control().GetCapabilities("", "").HostID
	hostName, _ := os.Hostname()
	if strings.TrimSpace(hostName) == "" {
		hostName = "or3-intern host"
	}

	response.Host.ID = hostID
	response.Host.DisplayName = hostName
	response.Features.AppBootstrap = true
	response.Features.AppEvents = false
	response.Features.AppActions = true
	response.Features.FileMetadataV2 = false

	response.Pairing.Paired = identity.Kind == "paired-device" || identity.Kind == "auth-session"
	response.Pairing.DeviceID = identity.Device
	response.Pairing.Role = identity.Role

	response.Auth.SessionRequired = s.config.Auth.EnforcementMode == config.AuthEnforcementSession
	response.Auth.SessionActive = identity.Kind == "auth-session"
	response.Auth.StepUpActive = identity.StepUpOK
	response.Auth.Kind = identity.Kind
	response.Auth.Role = identity.Role
	response.Auth.ExecAllowed = s.config.Tools.EnableExec && serviceBootstrapExecAllowed(identity.Role)
	response.Auth.Capabilities.PasskeysSupported = authSvc != nil && authSvc.Enabled()
	response.Auth.Capabilities.StepUpSupported = s.config.Auth.Enabled && s.config.Auth.RequirePasskeyForSensitive

	health := s.control().GetHealth()
	readiness := s.control().GetReadiness()
	capabilities := s.control().GetCapabilities("", "")
	response.Status.Health = &health
	response.Status.Readiness = &readiness
	response.Status.Capabilities = &capabilities
	response.Status.Summary = serviceBootstrapSummary(health, readiness)

	warnings := make([]serviceAppBootstrapWarning, 0, 6)
	if !health.RuntimeAvailable {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "runtime_unavailable", Message: "The OR3 runtime is not available right now.", Severity: "error"})
	}
	if !health.JobRegistryAvailable {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "job_registry_unavailable", Message: "Live job tracking is limited right now.", Severity: "warning"})
	}
	if !health.ApprovalBrokerAvailable {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "approval_broker_unavailable", Message: "Approval workflows are unavailable right now.", Severity: "warning"})
	}
	if !readiness.Ready {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "host_not_ready", Message: "This computer still has readiness issues to resolve.", Severity: "warning"})
	}
	if response.Pairing.Paired && !response.Auth.SessionActive {
		warnings = append(warnings, serviceAppBootstrapWarning{Code: "session_not_active", Message: "Passkey sign-in is still required for protected actions.", Severity: "info"})
	}
	if response.Auth.Kind == "shared-secret" && !response.Auth.ExecAllowed {
		warnings = append(warnings, serviceAppBootstrapWarning{
			Code:     "shared_secret_limited",
			Message:  "This connection is using the shared service secret as service-client. Read-only API calls work, but exec and approvals need a paired operator or admin device.",
			Severity: "warning",
		})
	}
	response.Status.Warnings = warnings

	response.Counts.PendingApprovals = s.bootstrapPendingApprovalCount(r.Context())
	response.Counts.ActiveJobs = s.bootstrapActiveJobCount(r.Context())
	response.Counts.ActiveTerminals = s.bootstrapActiveTerminalCount()

	response.Actions = []serviceAppActionDescriptor{
		s.restartActionDescriptor(),
	}
	return response
}

func serviceBootstrapSummary(health controlplane.HealthReport, readiness controlplane.ReadinessReport) string {
	if !health.RuntimeAvailable {
		return "offline"
	}
	if !readiness.Ready || strings.EqualFold(health.Status, "degraded") {
		return "degraded"
	}
	return "ready"
}

func serviceBootstrapExecAllowed(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "operator", "admin":
		return true
	default:
		return false
	}
}

func (s *serviceServer) bootstrapPendingApprovalCount(ctx context.Context) int {
	if s == nil || s.broker == nil {
		return 0
	}
	items, err := s.broker.ListApprovalRequestsFiltered(ctx, approval.StatusPending, "", 200)
	if err != nil {
		return 0
	}
	return len(items)
}

func (s *serviceServer) bootstrapActiveJobCount(ctx context.Context) int {
	if s == nil || s.control() == nil || s.control().DB == nil {
		return 0
	}
	items, err := s.control().DB.ListSubagentJobs(ctx, db.SubagentJobFilter{Status: "active", Limit: 200})
	if err != nil {
		return 0
	}
	return len(items)
}

func (s *serviceServer) bootstrapActiveTerminalCount() int {
	if s == nil {
		return 0
	}
	s.cleanupTerminalSessions()
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	return len(s.terminalSessions)
}

func (s *serviceServer) restartActionDescriptor() serviceAppActionDescriptor {
	descriptor := serviceAppActionDescriptor{
		ID:              "restart-service",
		Title:           "Restart service",
		SessionRequired: true,
		StepUpRequired:  true,
	}
	if s != nil && s.broker != nil && strings.EqualFold(string(s.config.Security.Approvals.Exec.Mode), string(config.ApprovalModeAsk)) {
		descriptor.ApprovalLikely = true
	}
	if !s.terminalAvailable() {
		descriptor.DisabledReason = "Shell access is turned off on this computer."
		return descriptor
	}
	_, _, ok := s.findServiceRestartScript()
	if !ok {
		descriptor.DisabledReason = "The restart script is not available on this computer."
		return descriptor
	}
	descriptor.Available = true
	return descriptor
}

func (s *serviceServer) handleRestartServiceAction(w http.ResponseWriter, r *http.Request) {
	descriptor := s.restartActionDescriptor()
	if !descriptor.Available {
		writeServiceJSON(w, http.StatusServiceUnavailable, serviceErrorPayload(r, serviceFirstNonEmpty(descriptor.DisabledReason, "restart is not available on this computer")))
		return
	}
	scriptPath, workingDir, ok := s.findServiceRestartScript()
	if !ok {
		writeServiceJSON(w, http.StatusServiceUnavailable, serviceErrorPayload(r, "restart is not available on this computer"))
		return
	}
	shellPath, err := resolveTerminalShell("sh")
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "restart shell is not available", err)
		return
	}
	decision, err := s.evaluateTerminalApproval(r.Context(), shellPath, workingDir, "")
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "restart approval failed", err)
		return
	}
	if decision.RequiresApproval {
		writeServiceValue(w, http.StatusConflict, serviceActionResponse{
			ActionID:   "restart-service",
			Status:     "approval_required",
			Message:    "restart service requires approval",
			ApprovalID: decision.RequestID,
		})
		return
	}
	if !decision.Allowed {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "restart service denied"
		}
		writeServiceJSON(w, http.StatusForbidden, serviceErrorPayload(r, reason))
		return
	}
	if err := startDetachedServiceRestart(scriptPath, workingDir, s.unsafeDev); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "restart failed to start", err)
		return
	}
	writeServiceValue(w, http.StatusAccepted, serviceActionResponse{
		ActionID:    "restart-service",
		Status:      "accepted",
		Message:     "restart requested",
		OperationID: newServiceRequestID(),
	})
}

func startDetachedServiceRestart(scriptPath, workingDir string, unsafeDev bool) error {
	scriptPath = strings.TrimSpace(scriptPath)
	workingDir = strings.TrimSpace(workingDir)
	if scriptPath == "" || workingDir == "" {
		return fmt.Errorf("restart script is unavailable")
	}
	cmd := exec.Command(scriptPath, "restart")
	cmd.Dir = workingDir
	if unsafeDev {
		cmd.Env = append(os.Environ(), "OR3_SERVICE_UNSAFE_DEV=true")
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func (s *serviceServer) findServiceRestartScript() (scriptPath string, workingDir string, ok bool) {
	for _, dir := range serviceRestartSearchDirs() {
		script := filepath.Join(dir, "scripts", "restart-service.sh")
		info, err := os.Stat(script)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		return script, dir, true
	}
	return "", "", false
}

func serviceRestartSearchDirs() []string {
	candidates := make([]string, 0, 8)
	appendDir := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dir = filepath.Clean(dir)
		for _, existing := range candidates {
			if existing == dir {
				return
			}
		}
		candidates = append(candidates, dir)
	}
	if cwd, err := os.Getwd(); err == nil {
		appendDir(cwd)
		appendDir(filepath.Join(cwd, "..", "or3-intern"))
		appendDir(filepath.Join(cwd, "or3-intern"))
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		appendDir(exeDir)
		appendDir(filepath.Join(exeDir, ".."))
		appendDir(filepath.Join(exeDir, "..", "or3-intern"))
		appendDir(filepath.Join(exeDir, "or3-intern"))
	}
	return candidates
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
				name := strings.TrimSpace(fmt.Sprint(function["name"]))
				if name == "" || name == "<nil>" {
					name = strings.TrimSpace(fmt.Sprint(call["name"]))
				}
				arguments := strings.TrimSpace(fmt.Sprint(function["arguments"]))
				if arguments == "<nil>" {
					arguments = ""
				}
				data := map[string]any{"name": name, "arguments": arguments}
				if id := strings.TrimSpace(fmt.Sprint(call["id"])); id != "" && id != "<nil>" {
					data["tool_call_id"] = id
				}
				emit("tool_call", data)
			}
		case "tool":
			name := strings.TrimSpace(fmt.Sprint(payload["tool"]))
			if name == "" || name == "<nil>" {
				name = "tool"
			}
			result := strings.TrimSpace(msg.Content)
			if preview := strings.TrimSpace(fmt.Sprint(payload["preview"])); preview != "" && preview != "<nil>" {
				result = preview
			}
			data := map[string]any{"name": name, "result": result}
			if id := strings.TrimSpace(fmt.Sprint(payload["tool_call_id"])); id != "" && id != "<nil>" {
				data["tool_call_id"] = id
			}
			if artifactID := strings.TrimSpace(fmt.Sprint(payload["artifact_id"])); artifactID != "" && artifactID != "<nil>" {
				data["artifact_id"] = artifactID
			}
			emit("tool_result", data)
		}
	}
	return events
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
		log.Printf("service %s %s -> %d", r.Method, r.URL.Path, captured.statusCode)
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

func (r *serviceStatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not implement http.Hijacker")
	}
	if r.statusCode == http.StatusOK {
		r.statusCode = http.StatusSwitchingProtocols
	}
	return hijacker.Hijack()
}

func (s *serviceServer) isMutationRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if s.isTerminalInteractiveMutation(r) {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *serviceServer) isTerminalInteractiveMutation(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	if !strings.HasPrefix(path, "/internal/v1/terminal/sessions/") {
		return false
	}
	return strings.HasSuffix(path, "/input") || strings.HasSuffix(path, "/resize")
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

func servicePublicPairingExchangeError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := strings.TrimSpace(err.Error())
	switch message {
	case "pairing request not found", "pairing request expired", "pairing request is not approved":
		return message, true
	default:
		return "", false
	}
}

func approvalActionPastTense(action string) string {
	switch strings.TrimSpace(action) {
	case "approve":
		return "approved"
	case "deny":
		return "denied"
	case "cancel":
		return "canceled"
	default:
		return "updated"
	}
}

func approvalActionExpiredMessage(action string) string {
	return fmt.Sprintf("This approval request expired before it could be %s. Refresh the approvals list and rerun the request if it is still needed.", approvalActionPastTense(action))
}

func approvalActionResolvedMessage(status string) string {
	switch strings.TrimSpace(status) {
	case approval.StatusApproved:
		return "This approval request was already approved. Refresh the approvals list to see its latest status."
	case approval.StatusDenied:
		return "This approval request was already denied. Refresh the approvals list to see its latest status."
	case approval.StatusCanceled:
		return "This approval request was already canceled. Refresh the approvals list to see its latest status."
	case approval.StatusExpired:
		return "This approval request already expired. Refresh the approvals list and rerun the request if it is still needed."
	default:
		return fmt.Sprintf("This approval request is %s and can no longer be changed. Refresh the approvals list to see its latest status.", strings.TrimSpace(status))
	}
}

func (s *serviceServer) servicePublicApprovalActionError(ctx context.Context, requestID int64, action string, err error) (string, string, bool) {
	if err == nil {
		return "", "", false
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "", "", false
	}
	switch message {
	case "approval request expired":
		return approvalActionExpiredMessage(action), approval.StatusExpired, true
	case "approval request not found":
		return "This approval request no longer exists.", "", true
	case "approval request is not pending":
		status := ""
		if s != nil && s.broker != nil && s.broker.DB != nil {
			rec, lookupErr := s.broker.DB.GetApprovalRequest(ctx, requestID)
			if lookupErr == nil {
				status = strings.TrimSpace(rec.Status)
			}
		}
		switch status {
		case approval.StatusExpired:
			return approvalActionExpiredMessage(action), status, true
		case approval.StatusApproved, approval.StatusDenied, approval.StatusCanceled:
			return approvalActionResolvedMessage(status), status, true
		case "":
			return "This approval request is no longer waiting for action. Refresh the approvals list to see its latest status.", "", true
		default:
			return approvalActionResolvedMessage(status), status, true
		}
	default:
		return "", "", false
	}
}

func (s *serviceServer) writeServiceApprovalActionError(w http.ResponseWriter, r *http.Request, statusCode int, approvalID int64, action, fallback string, err error) {
	if err != nil {
		log.Printf("service %s %s: %v", r.Method, r.URL.Path, err)
	}
	public := strings.TrimSpace(fallback)
	approvalStatus := ""
	if mapped, status, ok := s.servicePublicApprovalActionError(r.Context(), approvalID, action, err); ok {
		public = mapped
		approvalStatus = status
	}
	payload := serviceErrorPayload(r, public)
	payload["approval_id"] = approvalID
	if strings.TrimSpace(approvalStatus) != "" {
		payload["approval_status"] = approvalStatus
	}
	writeServiceJSON(w, statusCode, payload)
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

func writeServiceAuthError(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		log.Printf("service %s %s auth: %v", r.Method, r.URL.Path, err)
	}
	var authErr *auth.Error
	if errors.As(err, &authErr) {
		payload := serviceErrorPayload(r, authErr.Message)
		payload["code"] = authErr.Code
		if authErr.RetryAfter > 0 {
			payload["retry_after_seconds"] = authErr.RetryAfter
		}
		writeServiceJSON(w, authErr.Status, payload)
		return
	}
	status := http.StatusBadRequest
	message := "auth request failed"
	switch {
	case errors.Is(err, auth.ErrInvalidCeremony):
		status = http.StatusBadRequest
		message = "invalid or expired auth challenge"
	case errors.Is(err, auth.ErrRecoveryRequired):
		status = http.StatusConflict
		message = err.Error()
	}
	writeServiceJSON(w, status, serviceErrorPayload(r, message))
}

func serviceAuthSessionToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	return serviceFirstNonEmpty(
		strings.TrimSpace(r.Header.Get("X-Or3-Session")),
		strings.TrimSpace(r.Header.Get("X-Auth-Session")),
		strings.TrimSpace(r.URL.Query().Get("session_token")),
	)
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

func serviceApprovalRequiredPayload(sessionKey string, meta map[string]any, err *tools.ApprovalRequiredError) map[string]any {
	message := "approval is required before this tool can continue"
	var requestID int64
	if err != nil {
		if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
			message = trimmed
		}
		requestID = err.RequestID
	}
	payload := serviceLifecyclePayload(sessionKey, meta, map[string]any{
		"status":  "approval_required",
		"code":    "approval_required",
		"message": message,
	})
	if requestID > 0 {
		payload["request_id"] = requestID
		payload["approval_id"] = requestID
	}
	return payload
}

func serviceTurnFallbackText(err error, observer *serviceObserver) (string, bool) {
	if err == nil || !strings.Contains(err.Error(), "max tool loops exceeded") {
		return "", false
	}
	message := "I couldn't finish that because the tool calls kept failing or looping."
	if observer != nil {
		lastToolError := strings.TrimSpace(observer.lastToolError)
		if lastToolError != "" {
			if len(lastToolError) > 180 {
				lastToolError = lastToolError[:180] + "..."
			}
			message += " Last tool error: " + lastToolError
		}
	}
	return message, true
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

func (s *serviceServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/auth"), "/")
	api := s.app()
	identity := serviceAuthIdentityFromContext(r.Context())
	sessionToken := serviceAuthSessionToken(r)
	switch relative {
	case "capabilities":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{
			"passkeysEnabled":            s.config.Auth.Enabled,
			"passkeyMode":                string(s.config.Auth.EnforcementMode),
			"rpId":                       s.config.Auth.RPID,
			"origins":                    append([]string{}, s.config.Auth.AllowedOrigins...),
			"webauthnAvailable":          api.Auth() != nil && api.Auth().Enabled(),
			"sessionRequired":            s.config.Auth.EnforcementMode == config.AuthEnforcementSession,
			"stepUpRequiredForSensitive": s.config.Auth.RequirePasskeyForSensitive,
			"secureStorageRecommended":   true,
			"fallbackPolicy":             s.config.Auth.FallbackPolicy,
			"sessionHeader":              "X-Or3-Session",
		})
		return
	case "passkeys/registration/begin":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if !requireServiceRole(w, r, approval.RoleOperator) {
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			DisplayName string `json:"displayName"`
			Reason      string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		response, err := api.BeginPasskeyRegistration(r.Context(), auth.BeginRegistrationRequest{DeviceID: identity.Device, DisplayName: body.DisplayName, Reason: body.Reason, SessionToken: sessionToken})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, response)
		return
	case "passkeys/registration/finish":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if !requireServiceRole(w, r, approval.RoleOperator) {
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			CeremonyID string          `json:"ceremonyId"`
			Credential json.RawMessage `json:"credential"`
			Nickname   string          `json:"nickname"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		record, err := api.FinishPasskeyRegistration(r.Context(), auth.FinishRegistrationRequest{CeremonyID: body.CeremonyID, Body: body.Credential, Nickname: body.Nickname, SessionToken: sessionToken})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"passkey": record})
		return
	case "passkeys/login/begin":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		response, err := api.BeginPasskeyLogin(r.Context(), auth.BeginLoginRequest{DeviceID: identity.Device, Reason: body.Reason})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, response)
		return
	case "passkeys/login/finish":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			CeremonyID string          `json:"ceremonyId"`
			Credential json.RawMessage `json:"credential"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		result, err := api.FinishPasskeyLogin(r.Context(), auth.FinishLoginRequest{CeremonyID: body.CeremonyID, Body: body.Credential, DeviceID: identity.Device, FallbackRole: identity.Role})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, result)
		return
	case "step-up/begin":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		response, err := api.BeginStepUp(r.Context(), auth.BeginStepUpRequest{SessionToken: sessionToken, Reason: body.Reason})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, response)
		return
	case "step-up/finish":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			CeremonyID string          `json:"ceremonyId"`
			Credential json.RawMessage `json:"credential"`
			Reason     string          `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		session, err := api.FinishStepUp(r.Context(), auth.FinishStepUpRequest{SessionToken: sessionToken, CeremonyID: body.CeremonyID, Body: body.Credential, Reason: body.Reason})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"session": session})
		return
	case "session":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		claims, err := api.ValidateAuthSession(r.Context(), sessionToken)
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"session": claims.Session, "user": claims.User, "role": claims.Role})
		return
	case "session/revoke":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := api.RevokeAuthSession(r.Context(), sessionToken, body.Reason); err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"status": "revoked"})
		return
	case "passkeys":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		claims, err := api.ValidateAuthSession(r.Context(), sessionToken)
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		items, err := api.ListPasskeys(r.Context(), claims.User.ID)
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"items": items})
		return
	default:
		if strings.HasPrefix(relative, "passkeys/") {
			rest := strings.TrimPrefix(relative, "passkeys/")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
				passkeyID := parts[0]
				switch {
				case len(parts) == 1 && r.Method == http.MethodPatch:
					var body struct {
						Nickname string `json:"nickname"`
					}
					if err := decodeServiceRequestBody(r.Body, &body); err != nil {
						writeServiceRequestDecodeError(w, err)
						return
					}
					if err := api.RenamePasskey(r.Context(), passkeyID, body.Nickname); err != nil {
						writeServiceAuthError(w, r, err)
						return
					}
					writeServiceValue(w, http.StatusOK, map[string]any{"id": passkeyID, "nickname": body.Nickname})
					return
				case len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost:
					var body struct {
						Reason string `json:"reason"`
					}
					if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
						writeServiceRequestDecodeError(w, err)
						return
					}
					if err := api.RevokePasskey(r.Context(), sessionToken, passkeyID, body.Reason); err != nil {
						writeServiceAuthError(w, r, err)
						return
					}
					writeServiceValue(w, http.StatusOK, map[string]any{"id": passkeyID, "status": "revoked"})
					return
				}
			}
		}
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "auth route not found"})
	}
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

type serviceConfigureChange struct {
	Section string               `json:"section"`
	Channel string               `json:"channel"`
	Field   string               `json:"field"`
	Op      string               `json:"op"`
	Value   configureChangeValue `json:"value"`
}

type configureChangeValue string

func (v *configureChangeValue) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*v = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*v = configureChangeValue(text)
		return nil
	}
	var flag bool
	if err := json.Unmarshal(data, &flag); err == nil {
		if flag {
			*v = "true"
		} else {
			*v = "false"
		}
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		*v = configureChangeValue(number.String())
		return nil
	}
	return fmt.Errorf("configure change value must be a string, boolean, number, or null")
}

func (v configureChangeValue) String() string {
	return string(v)
}

type serviceConfigureField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"`
	Value       any      `json:"value,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	EmptyHint   string   `json:"emptyHint,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

func serviceConfigureFieldKind(kind configureFieldKind) string {
	switch kind {
	case configureFieldSecret:
		return "secret"
	case configureFieldToggle:
		return "toggle"
	case configureFieldChoice:
		return "choice"
	default:
		return "text"
	}
}

func serviceConfigureFieldValue(field configureField) any {
	if field.Kind == configureFieldToggle {
		return strings.EqualFold(strings.TrimSpace(field.Value), "on")
	}
	return field.Value
}

func toServiceConfigureFields(fields []configureField) []serviceConfigureField {
	result := make([]serviceConfigureField, 0, len(fields))
	for _, field := range fields {
		result = append(result, serviceConfigureField{
			Key:         field.Key,
			Label:       field.Label,
			Description: field.Description,
			Kind:        serviceConfigureFieldKind(field.Kind),
			Value:       serviceConfigureFieldValue(field),
			Choices:     append([]string{}, field.Choices...),
			EmptyHint:   field.EmptyHint,
			Placeholder: field.EmptyHint,
		})
	}
	return result
}

type serviceSkillItem struct {
	Name             string   `json:"name"`
	Key              string   `json:"key"`
	Description      string   `json:"description,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Homepage         string   `json:"homepage,omitempty"`
	Source           string   `json:"source"`
	Location         string   `json:"location"`
	Eligible         bool     `json:"eligible"`
	Disabled         bool     `json:"disabled"`
	Hidden           bool     `json:"hidden"`
	Status           string   `json:"status"`
	PermissionState  string   `json:"permission_state"`
	PermissionNotes  []string `json:"permission_notes,omitempty"`
	Missing          []string `json:"missing,omitempty"`
	Unsupported      []string `json:"unsupported,omitempty"`
	ParseError       string   `json:"parse_error,omitempty"`
	UserInvocable    bool     `json:"user_invocable"`
	PrimaryEnv       string   `json:"primary_env,omitempty"`
	RequiredEnv      []string `json:"required_env,omitempty"`
	ConfigFields     []string `json:"config_fields,omitempty"`
	APIKeyConfigured bool     `json:"api_key_configured"`
}

type serviceSkillRoot struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
}

type serviceSkillSettingsRequest struct {
	Enabled     *bool             `json:"enabled"`
	APIKey      *string           `json:"api_key"`
	APIKeyCamel *string           `json:"apiKey"`
	Env         map[string]string `json:"env"`
	Config      map[string]any    `json:"config"`
}

func (s *serviceServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/skills"), "/")
	if path == "" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		inv := s.serviceSkillsInventory(r.Context(), s.config)
		writeServiceValue(w, http.StatusOK, map[string]any{
			"items":                 serviceSkillItems(inv, s.config),
			"roots":                 serviceSkillRoots(s.config),
			"global_dir":            s.config.Skills.Load.GlobalDir,
			"global_skills_enabled": !s.config.Skills.Load.DisableGlobalDir,
		})
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "settings" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "skills route not found"})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(name) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid skill name"})
		return
	}
	s.handleSkillSettingsUpdate(w, r, name)
}

func (s *serviceServer) handleSkillSettingsUpdate(w http.ResponseWriter, r *http.Request, name string) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body serviceSkillSettingsRequest
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	inv := s.serviceSkillsInventory(r.Context(), s.config)
	skill, ok := inv.Get(name)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "skill not found"})
		return
	}
	next := s.config
	if next.Skills.Entries == nil {
		next.Skills.Entries = map[string]config.SkillEntryConfig{}
	}
	entryKey := serviceSkillEntryKey(skill)
	entry := next.Skills.Entries[entryKey]
	if body.Enabled != nil {
		enabled := *body.Enabled
		entry.Enabled = &enabled
	}
	if apiKey := firstStringPointer(body.APIKey, body.APIKeyCamel); apiKey != nil {
		entry.APIKey = *apiKey
	}
	if body.Env != nil {
		entry.Env = mergeServiceSkillEnv(entry.Env, body.Env)
	}
	if body.Config != nil {
		entry.Config = mergeServiceSkillConfig(entry.Config, body.Config)
	}
	next.Skills.Entries[entryKey] = entry
	path := s.configPath
	if strings.TrimSpace(path) == "" {
		path = cfgPathOrDefault("")
	}
	if err := config.Save(path, next); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "skill settings save failed", err)
		return
	}
	s.config = next
	updated := s.serviceSkillsInventory(r.Context(), next)
	s.applyServiceSkillsInventory(updated)
	itemSkill, _ := updated.Get(skill.Name)
	writeServiceValue(w, http.StatusOK, map[string]any{
		"ok":          true,
		"config_path": path,
		"skill":       serviceSkillItemFromMeta(itemSkill, next),
	})
}

func (s *serviceServer) serviceSkillsInventory(ctx context.Context, cfg config.Config) skills.Inventory {
	return buildSkillsInventory(cfg, s.serviceBundledSkillsDir(), s.serviceAvailableToolNames(ctx, cfg))
}

func (s *serviceServer) serviceAvailableToolNames(ctx context.Context, cfg config.Config) map[string]struct{} {
	toolNames := filterAdvertisedToolNames(cfg, availableToolNames(cfg.Cron.Enabled, cfg.Subagents.Enabled))
	if s.runtime != nil && s.runtime.Tools != nil {
		for _, name := range s.runtime.Tools.Names() {
			toolNames[name] = struct{}{}
		}
	}
	return toolNames
}

func (s *serviceServer) serviceBundledSkillsDir() string {
	cfgPath := strings.TrimSpace(s.configPath)
	if cfgPath == "" {
		cfgPath = cfgPathOrDefault("")
	}
	return filepath.Join(filepath.Dir(cfgPath), "builtin_skills")
}

func (s *serviceServer) applyServiceSkillsInventory(inv skills.Inventory) {
	if s.runtime == nil || s.runtime.Builder == nil {
		return
	}
	s.runtime.Builder.Skills = inv
	if s.runtime.Tools == nil {
		return
	}
	if tool, ok := s.runtime.Tools.Get("read_skill").(*tools.ReadSkill); ok {
		tool.Inventory = &s.runtime.Builder.Skills
	}
	if tool, ok := s.runtime.Tools.Get("run_skill").(*tools.RunSkill); ok {
		tool.Inventory = &s.runtime.Builder.Skills
	}
	if tool, ok := s.runtime.Tools.Get("run_skill_script").(*tools.RunSkillScript); ok {
		tool.Inventory = &s.runtime.Builder.Skills
	}
}

func serviceSkillRoots(cfg config.Config) []serviceSkillRoot {
	roots := buildSkillRoots(cfg, "")
	out := make([]serviceSkillRoot, 0, len(roots))
	for _, root := range roots {
		out = append(out, serviceSkillRoot{
			Path:    root.Path,
			Source:  string(root.Source),
			Enabled: strings.TrimSpace(root.Path) != "",
		})
	}
	return out
}

func serviceSkillItems(inv skills.Inventory, cfg config.Config) []serviceSkillItem {
	items := make([]serviceSkillItem, 0, len(inv.Skills))
	for _, skill := range inv.Skills {
		items = append(items, serviceSkillItemFromMeta(skill, cfg))
	}
	return items
}

func serviceSkillItemFromMeta(skill skills.SkillMeta, cfg config.Config) serviceSkillItem {
	entry := cfg.Skills.Entries[serviceSkillEntryKey(skill)]
	permissionState := strings.TrimSpace(skill.PermissionState)
	if permissionState == "" {
		permissionState = "approved"
	}
	return serviceSkillItem{
		Name:             skill.Name,
		Key:              serviceSkillEntryKey(skill),
		Description:      skill.Description,
		Summary:          skill.Summary,
		Homepage:         skill.Homepage,
		Source:           string(skill.Source),
		Location:         skill.Dir,
		Eligible:         skill.Eligible,
		Disabled:         skill.Disabled,
		Hidden:           skill.Hidden,
		Status:           serviceSkillStatus(skill),
		PermissionState:  permissionState,
		PermissionNotes:  append([]string{}, skill.PermissionNotes...),
		Missing:          append([]string{}, skill.Missing...),
		Unsupported:      append([]string{}, skill.Unsupported...),
		ParseError:       skill.ParseError,
		UserInvocable:    skill.UserInvocable,
		PrimaryEnv:       skill.Metadata.PrimaryEnv,
		RequiredEnv:      append([]string{}, skill.Metadata.Requires.Env...),
		ConfigFields:     append([]string{}, skill.Metadata.Requires.Config...),
		APIKeyConfigured: strings.TrimSpace(entry.APIKey) != "",
	}
}

func serviceSkillEntryKey(skill skills.SkillMeta) string {
	if strings.TrimSpace(skill.Key) != "" {
		return strings.TrimSpace(skill.Key)
	}
	return strings.TrimSpace(skill.Name)
}

func serviceSkillStatus(skill skills.SkillMeta) string {
	switch {
	case strings.TrimSpace(skill.ParseError) != "":
		return "parse-error"
	case skill.Disabled:
		return "disabled"
	case skill.Hidden:
		return "hidden"
	case !skill.Eligible:
		return "ineligible"
	default:
		return "eligible"
	}
}

func firstStringPointer(values ...*string) *string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func mergeServiceSkillEnv(current map[string]string, updates map[string]string) map[string]string {
	if current == nil {
		current = map[string]string{}
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if strings.TrimSpace(value) == "" {
			delete(current, key)
			continue
		}
		current[key] = value
	}
	return current
}

func mergeServiceSkillConfig(current map[string]any, updates map[string]any) map[string]any {
	if current == nil {
		current = map[string]any{}
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value == nil {
			delete(current, key)
			continue
		}
		current[key] = value
	}
	return current
}

func (s *serviceServer) handleConfigure(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/configure"), "/")
	switch {
	case path == "" || path == "sections":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		items := make([]map[string]any, 0, len(configureSections))
		for _, section := range configureSections {
			items = append(items, map[string]any{
				"key":         section.Key,
				"label":       section.Label,
				"description": section.Description,
				"status":      sectionStatus(s.config, section.Key),
			})
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
	case path == "fields":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		section := normalizeConfigureSectionKey(serviceFirstNonEmpty(r.URL.Query().Get("section"), r.URL.Query().Get("sectionKey")))
		if section == "" {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "section is required"})
			return
		}
		channel := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("channel")))
		var fields []configureField
		if section == "channels" {
			if channel == "" {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "channel is required for channels section"})
				return
			}
			fields = buildChannelFields(s.config, channel)
		} else {
			fields = buildSectionFields(s.config, section, "")
		}
		writeServiceValue(w, http.StatusOK, map[string]any{
			"section": section,
			"channel": channel,
			"fields":  toServiceConfigureFields(fields),
		})
	case path == "providers":
		switch r.Method {
		case http.MethodGet:
			writeServiceValue(w, http.StatusOK, serviceProviderStatus(s.config))
		case http.MethodPost:
			s.handleConfigureProviderSave(w, r, "")
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case strings.HasPrefix(path, "providers/"):
		key := strings.Trim(strings.TrimPrefix(path, "providers/"), "/")
		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			s.handleConfigureProviderSave(w, r, key)
		case http.MethodDelete:
			s.handleConfigureProviderDelete(w, r, key)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case path == "models":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleConfigureModels(w, r)
	case path == "favorite-models":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleConfigureFavoriteModel(w, r)
	case path == "test":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleConfigureProviderTest(w, r)
	case path == "apply":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
		var body struct {
			Changes []serviceConfigureChange `json:"changes"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if len(body.Changes) == 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "changes are required"})
			return
		}
		next := s.config
		for _, change := range body.Changes {
			section := normalizeConfigureSectionKey(change.Section)
			channel := strings.TrimSpace(change.Channel)
			field := strings.TrimSpace(change.Field)
			if section == "" || field == "" {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "section and field are required for each change"})
				return
			}
			switch strings.ToLower(strings.TrimSpace(change.Op)) {
			case "", "set":
				changed, err := applyFieldValue(&next, section, channel, field, change.Value.String())
				if err != nil {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if !changed {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported field update: " + section + "." + field})
					return
				}
			case "toggle":
				if !toggleFieldValue(&next, section, channel, field) {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported toggle field: " + section + "." + field})
					return
				}
			case "choose":
				changed, err := applyChoiceSelection(&next, section, channel, field, change.Value.String())
				if err != nil {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if !changed {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported choice field: " + section + "." + field})
					return
				}
			default:
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported op"})
				return
			}
		}
		path := s.configPath
		if strings.TrimSpace(path) == "" {
			path = cfgPathOrDefault("")
		}
		if err := config.Save(path, next); err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
			return
		}
		s.config = next
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "configure route not found"})
	}
}

func (s *serviceServer) saveConfigureConfig(next config.Config) (string, error) {
	path := s.configPath
	if strings.TrimSpace(path) == "" {
		path = cfgPathOrDefault("")
	}
	if err := config.Save(path, next); err != nil {
		return path, err
	}
	s.config = next
	return path, nil
}

func serviceNormalizeProviderKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *serviceServer) handleConfigureProviderSave(w http.ResponseWriter, r *http.Request, pathKey string) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body struct {
		Key               string `json:"key"`
		Label             string `json:"label"`
		APIBase           string `json:"apiBase"`
		APIKey            string `json:"apiKey"`
		TimeoutSeconds    int    `json:"timeoutSeconds"`
		EnableVision      bool   `json:"enableVision"`
		DefaultChatModel  string `json:"defaultChatModel"`
		DefaultEmbedModel string `json:"defaultEmbedModel"`
		DefaultDimensions int    `json:"defaultDimensions"`
		ClearAPIKey       bool   `json:"clearApiKey"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	key := serviceNormalizeProviderKey(serviceFirstNonEmpty(pathKey, body.Key))
	if key == "" {
		key = serviceNormalizeProviderKey(body.Label)
	}
	if key == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider key or label is required"})
		return
	}
	if strings.TrimSpace(body.APIBase) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "apiBase is required"})
		return
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(body.APIBase)); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "apiBase must be a valid URL"})
		return
	}
	next := s.config
	if next.Providers == nil {
		next.Providers = config.ProviderProfiles{}
	}
	profile := next.Providers[key]
	profile.Label = strings.TrimSpace(serviceFirstNonEmpty(body.Label, profile.Label, key))
	profile.APIBase = strings.TrimRight(strings.TrimSpace(body.APIBase), "/")
	if body.ClearAPIKey {
		profile.APIKey = ""
	} else if strings.TrimSpace(body.APIKey) != "" {
		profile.APIKey = strings.TrimSpace(body.APIKey)
	}
	if body.TimeoutSeconds > 0 {
		profile.TimeoutSeconds = body.TimeoutSeconds
	}
	profile.EnableVision = body.EnableVision
	profile.DefaultChatModel = strings.TrimSpace(body.DefaultChatModel)
	profile.DefaultEmbedModel = strings.TrimSpace(body.DefaultEmbedModel)
	if body.DefaultDimensions >= 0 {
		profile.DefaultDimensions = body.DefaultDimensions
	}
	next.Providers[key] = profile
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "provider": key})
}

func (s *serviceServer) handleConfigureProviderDelete(w http.ResponseWriter, r *http.Request, key string) {
	key = serviceNormalizeProviderKey(key)
	if key == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider key is required"})
		return
	}
	if key == "openai" || key == "openrouter" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "built-in providers cannot be deleted"})
		return
	}
	next := s.config
	if next.Providers != nil {
		delete(next.Providers, key)
	}
	if next.FavoriteModels != nil {
		delete(next.FavoriteModels, key)
	}
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path})
}

func serviceProviderStatus(cfg config.Config) map[string]any {
	providerItems := make([]map[string]any, 0, len(cfg.Providers))
	keys := make([]string, 0, len(cfg.Providers))
	for key := range cfg.Providers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		profile := cfg.Providers[key]
		providerItems = append(providerItems, map[string]any{
			"key":               key,
			"label":             profile.Label,
			"apiBase":           profile.APIBase,
			"apiKeyConfigured":  strings.TrimSpace(profile.APIKey) != "",
			"timeoutSeconds":    profile.TimeoutSeconds,
			"enableVision":      profile.EnableVision,
			"defaultChatModel":  profile.DefaultChatModel,
			"defaultEmbedModel": profile.DefaultEmbedModel,
			"defaultDimensions": profile.DefaultDimensions,
			"favorites":         cfg.FavoriteModels[key],
		})
	}
	roleItems := map[string]any{}
	for _, roleName := range []string{config.ModelRoleChat, config.ModelRoleAgents, config.ModelRoleSubagents, config.ModelRoleSummarization, config.ModelRoleContextManager, config.ModelRoleEmbeddings} {
		role := cfg.ModelRole(roleName)
		roleItems[roleName] = map[string]any{
			"primary":         role.Primary,
			"fallbacks":       role.Fallbacks,
			"embedDimensions": role.EmbedDimensions,
			"warnings":        providerRoleWarnings(cfg, role),
		}
	}
	return map[string]any{"providers": providerItems, "roles": roleItems}
}

func providerRoleWarnings(cfg config.Config, role config.ModelRoleConfig) []string {
	var warnings []string
	check := func(ref config.ModelRef) {
		profile, ok := cfg.ProviderProfile(ref.Provider)
		if !ok {
			warnings = append(warnings, "provider not configured: "+ref.Provider)
			return
		}
		if strings.TrimSpace(profile.APIBase) == "" {
			warnings = append(warnings, "provider missing API base: "+ref.Provider)
		}
		if strings.TrimSpace(profile.APIKey) == "" {
			warnings = append(warnings, "provider missing API key: "+ref.Provider)
		}
	}
	check(role.Primary)
	seen := map[string]struct{}{}
	for _, fallback := range role.Fallbacks {
		key := fallback.Provider + "/" + fallback.Model
		if _, ok := seen[key]; ok {
			warnings = append(warnings, "duplicate fallback: "+key)
		}
		seen[key] = struct{}{}
		check(fallback)
	}
	return warnings
}

func (s *serviceServer) handleConfigureFavoriteModel(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Label    string `json:"label"`
		Favorite *bool  `json:"favorite"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	provider := serviceNormalizeProviderKey(body.Provider)
	model := strings.TrimSpace(body.Model)
	if provider == "" || model == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider and model are required"})
		return
	}
	favorite := true
	if body.Favorite != nil {
		favorite = *body.Favorite
	}
	next := s.config
	if next.FavoriteModels == nil {
		next.FavoriteModels = config.FavoriteModelsConfig{}
	}
	current := next.FavoriteModels[provider]
	out := make([]config.FavoriteModelConfig, 0, len(current)+1)
	for _, item := range current {
		if strings.TrimSpace(item.Model) == model {
			continue
		}
		out = append(out, item)
	}
	if favorite {
		out = append([]config.FavoriteModelConfig{{Model: model, Label: strings.TrimSpace(body.Label)}}, out...)
	}
	next.FavoriteModels[provider] = out
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "favorites": next.FavoriteModels[provider]})
}

func (s *serviceServer) handleConfigureModels(w http.ResponseWriter, r *http.Request) {
	provider := serviceNormalizeProviderKey(r.URL.Query().Get("provider"))
	if provider == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider is required"})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind == "" {
		kind = "chat"
	}
	refresh := r.URL.Query().Get("refresh") == "1" || strings.EqualFold(r.URL.Query().Get("refresh"), "true")
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	userFiltered := r.URL.Query().Get("user") == "1" || strings.EqualFold(r.URL.Query().Get("user"), "true")
	items, fetchedAt, err := s.configureModelCatalog(r.Context(), provider, kind, category, userFiltered, refresh)
	if err != nil {
		writeServiceJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"provider":  provider,
		"kind":      kind,
		"fetchedAt": fetchedAt.Format(time.RFC3339),
		"items":     items,
	})
}

func (s *serviceServer) configureModelCatalog(ctx context.Context, provider, kind, category string, userFiltered, refresh bool) ([]serviceModelCatalogItem, time.Time, error) {
	cacheKey := strings.Join([]string{provider, kind, category, strconv.FormatBool(userFiltered)}, "|")
	now := time.Now()
	s.modelCatalogMu.Lock()
	if s.modelCatalogCache == nil {
		s.modelCatalogCache = map[string]serviceModelCatalogCacheEntry{}
	}
	if entry, ok := s.modelCatalogCache[cacheKey]; ok && !refresh && now.Sub(entry.FetchedAt) < 24*time.Hour {
		items := append([]serviceModelCatalogItem(nil), entry.Items...)
		s.modelCatalogMu.Unlock()
		return items, entry.FetchedAt, nil
	}
	s.modelCatalogMu.Unlock()

	items, err := s.fetchConfigureModelCatalog(ctx, provider, kind, category, userFiltered)
	if err != nil {
		return nil, time.Time{}, err
	}
	s.modelCatalogMu.Lock()
	s.modelCatalogCache[cacheKey] = serviceModelCatalogCacheEntry{FetchedAt: now, Items: items}
	s.modelCatalogMu.Unlock()
	return items, now, nil
}

func (s *serviceServer) fetchConfigureModelCatalog(ctx context.Context, provider, kind, category string, userFiltered bool) ([]serviceModelCatalogItem, error) {
	profile, ok := s.config.ProviderProfile(provider)
	if !ok {
		return nil, fmt.Errorf("provider is not configured: %s", provider)
	}
	base := strings.TrimRight(strings.TrimSpace(profile.APIBase), "/")
	if base == "" {
		return nil, fmt.Errorf("provider missing API base: %s", provider)
	}
	endpoint := base + "/models"
	query := url.Values{}
	if provider == "openrouter" {
		if kind == "embeddings" {
			endpoint = base + "/embeddings/models"
		} else if userFiltered && strings.TrimSpace(profile.APIKey) != "" {
			endpoint = base + "/models/user"
		}
		if category != "" {
			query.Set("category", category)
		}
	} else if kind == "embeddings" && strings.Contains(base, "openrouter.ai") {
		endpoint = base + "/embeddings/models"
	}
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(profile.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(profile.APIKey))
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("model list failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]serviceModelCatalogItem, 0, len(payload.Data))
	for _, raw := range payload.Data {
		item := serviceModelCatalogItem{
			ID:          serviceString(raw["id"]),
			Name:        serviceString(raw["name"]),
			Description: serviceString(raw["description"]),
			Provider:    provider,
			Pricing:     serviceMap(raw["pricing"]),
		}
		if item.ID == "" {
			continue
		}
		item.ContextLength = serviceInt(raw["context_length"])
		if item.ContextLength == 0 {
			item.ContextLength = serviceInt(raw["contextLength"])
		}
		if arch := serviceMap(raw["architecture"]); arch != nil {
			item.InputModalities = serviceStringSlice(arch["input_modalities"])
			item.OutputModalities = serviceStringSlice(arch["output_modalities"])
		}
		if topProvider := serviceMap(raw["top_provider"]); topProvider != nil {
			item.RawProvider = serviceString(topProvider["name"])
		}
		if kind == "embeddings" && len(item.OutputModalities) > 0 && !slices.Contains(item.OutputModalities, "embeddings") {
			continue
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b serviceModelCatalogItem) int {
		return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
	})
	return items, nil
}

func serviceString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func serviceMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if v, ok := value.(map[string]any); ok {
		return v
	}
	return nil
}

func serviceInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func serviceStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if strings.TrimSpace(serviceString(value)) == "" {
			return nil
		}
		return []string{serviceString(value)}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s := serviceString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (s *serviceServer) handleConfigureProviderTest(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body struct {
		Role     string `json:"role"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	cfg := s.config
	roleName := strings.TrimSpace(body.Role)
	if roleName == "" {
		roleName = config.ModelRoleChat
	}
	role := cfg.ModelRole(roleName)
	if strings.TrimSpace(body.Provider) != "" {
		role.Primary.Provider = strings.TrimSpace(body.Provider)
	}
	if strings.TrimSpace(body.Model) != "" {
		role.Primary.Model = strings.TrimSpace(body.Model)
	}
	client := newModelRefClient(cfg, role.Primary, 15*time.Second)
	if client == nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "provider is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch roleName {
	case config.ModelRoleEmbeddings:
		_, err := client.Embed(ctx, role.Primary.Model, "or3 provider test")
		if err != nil {
			writeServiceJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "transient": providers.IsTransientError(err)})
			return
		}
	default:
		_, err := client.Chat(ctx, providers.ChatCompletionRequest{Model: role.Primary.Model, Messages: []providers.ChatMessage{{Role: "user", Content: "Reply with ok."}}, Temperature: 0})
		if err != nil {
			writeServiceJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "transient": providers.IsTransientError(err)})
			return
		}
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *serviceServer) handleCron(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/cron"), "/")
	if path == "" {
		path = "status"
	}
	switch path {
	case "status":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		status := map[string]any{"enabled": s.config.Cron.Enabled, "available": s.cronSvc != nil}
		if s.cronSvc != nil {
			schedulerStatus, err := s.cronSvc.Status()
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "cron status unavailable", err)
				return
			}
			for key, value := range schedulerStatus {
				status[key] = value
			}
		}
		writeServiceJSON(w, http.StatusOK, status)
	case "jobs":
		svc := s.requireCronService(w)
		if svc == nil {
			return
		}
		switch r.Method {
		case http.MethodGet:
			jobs, err := svc.List()
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "cron jobs unavailable", err)
				return
			}
			writeServiceJSON(w, http.StatusOK, map[string]any{"items": jobs})
		case http.MethodPost:
			limitServiceRequestBody(w, r, serviceCronBodyLimit)
			job, err := decodeServiceCronJobRequest(r.Body, true)
			if err != nil {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if err := svc.Add(job); err != nil {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			jobs, _ := svc.List()
			created := findServiceCronJob(jobs, job.ID)
			if created == nil && len(jobs) > 0 {
				created = &jobs[len(jobs)-1]
			}
			writeServiceJSON(w, http.StatusCreated, map[string]any{"job": created})
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	default:
		if !strings.HasPrefix(path, "jobs/") {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron route not found"})
			return
		}
		svc := s.requireCronService(w)
		if svc == nil {
			return
		}
		parts := strings.Split(strings.TrimPrefix(path, "jobs/"), "/")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job route not found"})
			return
		}
		id := strings.TrimSpace(parts[0])
		if len(parts) == 1 {
			s.handleCronJob(w, r, svc, id)
			return
		}
		if len(parts) != 2 {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job route not found"})
			return
		}
		s.handleCronJobAction(w, r, svc, id, parts[1])
	}
}

func (s *serviceServer) requireCronService(w http.ResponseWriter) *cron.Service {
	if s.cronSvc == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cron service unavailable", "enabled": s.config.Cron.Enabled})
		return nil
	}
	return s.cronSvc
}

func (s *serviceServer) handleCronJob(w http.ResponseWriter, r *http.Request, svc *cron.Service, id string) {
	switch r.Method {
	case http.MethodGet:
		jobs, err := svc.List()
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "cron jobs unavailable", err)
			return
		}
		job := findServiceCronJob(jobs, id)
		if job == nil {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"job": job})
	case http.MethodPatch, http.MethodPut:
		limitServiceRequestBody(w, r, serviceCronBodyLimit)
		job, err := decodeServiceCronJobRequest(r.Body, false)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		found, updated, err := svc.Update(id, job)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if !found {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"job": updated})
	case http.MethodDelete:
		found, err := svc.Remove(id)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "cron job delete failed", err)
			return
		}
		if !found {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "deleted"})
	default:
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *serviceServer) handleCronJobAction(w http.ResponseWriter, r *http.Request, svc *cron.Service, id string, action string) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	switch action {
	case "run":
		limitServiceRequestBody(w, r, serviceCronBodyLimit)
		force := serviceCronRunForce(r.Body)
		found, err := svc.RunNow(r.Context(), id, force)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if !found {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found or disabled"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "ran"})
	case "pause":
		s.writeCronEnabledState(w, svc, id, false)
	case "resume":
		s.writeCronEnabledState(w, svc, id, true)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron action not found"})
	}
}

func (s *serviceServer) writeCronEnabledState(w http.ResponseWriter, svc *cron.Service, id string, enabled bool) {
	found, job, err := svc.SetEnabled(id, enabled)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !found {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"job": job})
}

func decodeServiceCronJobRequest(body io.Reader, defaultEnabled bool) (cron.CronJob, error) {
	var raw map[string]json.RawMessage
	if err := decodeServiceRequestBody(body, &raw); err != nil {
		return cron.CronJob{}, err
	}
	if len(raw) == 0 {
		return cron.CronJob{}, fmt.Errorf("job is required")
	}
	jobRaw, ok := raw["job"]
	if !ok {
		b, err := json.Marshal(raw)
		if err != nil {
			return cron.CronJob{}, err
		}
		jobRaw = b
	}
	var job cron.CronJob
	decoder := json.NewDecoder(bytes.NewReader(jobRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&job); err != nil {
		return cron.CronJob{}, err
	}
	if defaultEnabled {
		var jobMap map[string]json.RawMessage
		if err := json.Unmarshal(jobRaw, &jobMap); err == nil {
			if _, hasEnabled := jobMap["enabled"]; !hasEnabled {
				job.Enabled = true
			}
		}
	}
	if job.Payload.Kind == "" {
		job.Payload.Kind = "agent_turn"
	}
	job.Payload = cron.NormalizePayload(job.Payload)
	return job, nil
}

func serviceCronRunForce(body io.Reader) bool {
	force := true
	var raw map[string]json.RawMessage
	if err := decodeServiceRequestBody(body, &raw); err != nil || len(raw) == 0 {
		return force
	}
	if value, ok := raw["force"]; ok {
		_ = json.Unmarshal(value, &force)
	}
	return force
}

func findServiceCronJob(jobs []cron.CronJob, id string) *cron.CronJob {
	for i := range jobs {
		if jobs[i].ID == id {
			return &jobs[i]
		}
	}
	return nil
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
