package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	or3log "or3-intern/internal/log"
	"or3-intern/internal/mcp"
)

type serviceServer struct {
	config                config.Config
	configPath            string
	runtime               *agent.Runtime
	cronSvc               *cron.Service
	subagentManager       *agent.SubagentManager
	agentCLIManager       *agentcli.Manager
	chatManager           *agentcli.ChatManager
	mcpManager            *mcp.Manager
	mcpTestManagerFactory serviceMCPTestManagerFactory
	jobs                  *agent.JobRegistry
	broker                *approval.Broker
	unsafeDev             bool
	controlOnce           sync.Once
	controlSvc            *controlplane.Service
	appOnce               sync.Once
	appSvc                *app.ServiceApp
	componentsOnce        sync.Once
	terminalManager       *serviceTerminalManager
	terminalTicketStore   *serviceTerminalWebSocketTicketStore
	rateLimiter           *serviceRateLimiter
	authFailures          *serviceAuthFailureTracker
	nonceGuard            *serviceNonceReplayGuard
	modelCatalog          *serviceModelCatalogCache
	secureRelayHub        *secureConnectionRelayHub
	doctorTurnMu          sync.Mutex
	doctorTurnOnce        sync.Once
	doctorActiveTurns     map[string]doctorSessionTurnLease
}

type serviceDeviceResponse struct {
	DeviceID    string `json:"device_id"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role,omitempty"`
	Status      string `json:"status,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	LastSeenAt  int64  `json:"last_seen_at,omitempty"`
	RevokedAt   int64  `json:"revoked_at,omitempty"`
}

func (s *serviceServer) initComponents() {
	if s.terminalManager == nil {
		s.terminalManager = &serviceTerminalManager{sessions: map[string]*serviceTerminalSession{}}
	}
	if s.terminalTicketStore == nil {
		s.terminalTicketStore = &serviceTerminalWebSocketTicketStore{tickets: map[string]serviceTerminalWebSocketTicket{}}
	}
	if s.rateLimiter == nil {
		s.rateLimiter = &serviceRateLimiter{}
	}
	if s.authFailures == nil {
		s.authFailures = &serviceAuthFailureTracker{}
	}
	if s.nonceGuard == nil {
		s.nonceGuard = newServiceNonceReplayGuard(4096)
	}
	if s.modelCatalog == nil {
		s.modelCatalog = newServiceModelCatalogCache(64, 24*time.Hour)
	}
	if s.secureRelayHub == nil {
		s.secureRelayHub = newSecureConnectionRelayHub()
	}
}

func (s *serviceServer) components() {
	s.componentsOnce.Do(s.initComponents)
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
	return runServiceCommandWithBrokerOptionsCronMCP(ctx, cfg, rt, subagentManager, agentCLIManager, jobs, broker, unsafeDev, cronSvc, nil)
}

func runServiceCommandWithBrokerOptionsCronMCP(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, agentCLIManager *agentcli.Manager, jobs *agent.JobRegistry, broker *approval.Broker, unsafeDev bool, cronSvc *cron.Service, mcpManager *mcp.Manager) error {
	or3log.InstallStdlibSink()
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
	server := &serviceServer{config: cfg, configPath: cfgPathOrDefault(""), runtime: rt, cronSvc: cronSvc, subagentManager: subagentManager, agentCLIManager: agentCLIManager, chatManager: nil, mcpManager: mcpManager, jobs: jobs, broker: broker, unsafeDev: unsafeDev}
	server.registerDoctorAdminBrainTools()
	if rt.DB != nil {
		server.chatManager = &agentcli.ChatManager{DB: rt.DB, Manager: agentCLIManager, Jobs: jobs, Broker: broker}
		if err := server.chatManager.ReconcileOnStartup(ctx); err != nil {
			log.Printf("chat manager: startup reconciliation failed: %v", err)
		}
	}
	authSvc := server.app().Auth()
	mux := newServiceMux(server)

	httpServer := &http.Server{
		Addr:              cfg.Service.Listen,
		Handler:           serviceBrowserMiddleware(cfg, serviceAuthMiddlewareWithBrokerAndLimiter(cfg, broker, authSvc, server, serviceBoundaryMiddleware(server, mux))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return serveHTTPWithConfiguredTransport(ctx, httpServer, cfg)
}

func serveHTTPWithConfiguredTransport(ctx context.Context, httpServer *http.Server, cfg config.Config) error {
	errCh := make(chan error, 1)
	socketPath := strings.TrimSpace(cfg.Service.UnixSocket)
	if socketPath != "" {
		if err := prepareUnixSocketPath(socketPath); err != nil {
			return err
		}
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			return err
		}
		go func() {
			log.Printf("or3-intern service listening on unix socket %s", socketPath)
			if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		}()
	} else {
		go func() {
			log.Printf("or3-intern service listening on %s", cfg.Service.Listen)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		}()
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		err := httpServer.Shutdown(shutdownCtx)
		if socketPath != "" {
			cleanupUnixSocketPath(socketPath)
		}
		return err
	case err := <-errCh:
		if socketPath != "" {
			cleanupUnixSocketPath(socketPath)
		}
		return err
	}
}

func prepareUnixSocketPath(socketPath string) error {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil
	}
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("unix socket path %s already exists and is not a socket", socketPath)
	}
	return os.Remove(socketPath)
}

func cleanupUnixSocketPath(socketPath string) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return
	}
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return
	}
	if err := os.Remove(socketPath); err != nil {
		log.Printf("unix socket cleanup failed for %s: %v", socketPath, err)
	}
}

func newServiceMux(server *serviceServer) *http.ServeMux {
	mux := http.NewServeMux()
	for _, route := range serviceRouteSpecs(server) {
		registerServiceRoute(mux, route)
	}
	mux.Handle("/internal/v1/", http.HandlerFunc(handleUnknownServiceRoute))
	return mux
}

func (s *serviceServer) control() *controlplane.Service {
	s.controlOnce.Do(func() {
		s.controlSvc = controlplane.New(s.config, s.runtime, s.broker, s.jobs, s.subagentManager)
		s.controlSvc.MCPStatus = s.mcpManager
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
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": serviceDeviceResponses(items)})
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

func serviceDeviceResponses(items []db.PairedDeviceRecord) []serviceDeviceResponse {
	out := make([]serviceDeviceResponse, 0, len(items))
	for _, item := range items {
		out = append(out, serviceDeviceResponse{
			DeviceID:    item.DeviceID,
			DisplayName: item.DisplayName,
			Role:        item.Role,
			Status:      item.Status,
			CreatedAt:   item.CreatedAt,
			LastSeenAt:  item.LastSeenAt,
			RevokedAt:   item.RevokedAt,
		})
	}
	return out
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
			if s.writePersistedServiceJobSnapshot(w, r, jobID) {
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
