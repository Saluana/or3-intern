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

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
	or3log "or3-intern/internal/log"
	"or3-intern/internal/memory"
	"or3-intern/internal/memorysvc"
	"or3-intern/internal/providers"
	"or3-intern/internal/runners"
	"or3-intern/internal/security"
)

type serviceServer struct {
	config              config.Config
	configPath          string
	database            *db.DB
	audit               *security.AuditLogger
	artifacts           *artifacts.Store
	memRetriever        *memory.Retriever
	docRetriever        *memory.DocRetriever
	embedProvider       *providers.Client
	cronSvc             *cron.Service
	runnerManager       *runners.Manager
	chatManager         *runners.ChatManager
	turnOrchestrator    *app.RunnerTurnOrchestrator
	jobs                *jobs.Registry
	channelDeliverer    channels.MetaDeliverer
	broker              *approval.Broker
	unsafeDev           bool
	controlOnce         sync.Once
	controlSvc          *controlplane.Service
	appOnce             sync.Once
	appSvc              *app.ServiceApp
	componentsOnce      sync.Once
	terminalManager     *serviceTerminalManager
	terminalTicketStore *serviceTerminalWebSocketTicketStore
	rateLimiter         *serviceRateLimiter
	authFailures        *serviceAuthFailureTracker
	nonceGuard          *serviceNonceReplayGuard
	modelCatalog        *serviceModelCatalogCache
	secureRelayHub      *secureConnectionRelayHub
	memorySvc           *memorysvc.Service
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
	servicePairingBodyLimit                  int64 = 64 << 10
	serviceApprovalBodyLimit                 int64 = 64 << 10
	serviceEmbeddingsBodyLimit               int64 = 64 << 10
	serviceScopeBodyLimit                    int64 = 64 << 10
	serviceConfigureBodyLimit                int64 = 256 << 10
	serviceFileUploadBodyLimit               int64 = 128 << 20
	serviceFileTextReadLimit                 int64 = 1 << 20
	serviceFileTextWriteLimit                int64 = 1 << 20
	serviceTerminalBodyLimit                 int64 = 64 << 10
	serviceRunnerRunsBodyLimit               int64 = 256 << 10
	serviceCronBodyLimit                     int64 = 64 << 10
	serviceTerminalSessionTTL                      = 10 * time.Minute
	serviceTerminalMaxSessions                     = 4
	serviceTerminalWebSocketTicketTTL              = 30 * time.Second
	serviceTerminalWebSocketPingInterval           = 25 * time.Second
	serviceTerminalWebSocketWriteTimeout           = 10 * time.Second
	serviceTerminalWebSocketHandshakeTimeout       = 5 * time.Second
	serviceTerminalWebSocketProtocol               = "or3.terminal.v1"
	serviceTerminalWebSocketTicketPrefix           = "or3.ticket."
	serviceTerminalReplayMaxBytes                  = 256 << 10
	serviceJobStreamHeartbeatInterval              = 15 * time.Second
)

func runServiceCommand(ctx context.Context, cfg config.Config, runnerManager *runners.Manager, jobRegistry *jobs.Registry) error {
	return runServiceCommandWithBroker(ctx, cfg, runnerManager, jobRegistry, nil)
}

func runServiceCommandWithBroker(ctx context.Context, cfg config.Config, runnerManager *runners.Manager, jobRegistry *jobs.Registry, broker *approval.Broker) error {
	return runServiceCommandWithBrokerOptions(ctx, cfg, runnerManager, jobRegistry, broker, false)
}

func runServiceCommandWithBrokerOptions(ctx context.Context, cfg config.Config, runnerManager *runners.Manager, jobRegistry *jobs.Registry, broker *approval.Broker, unsafeDev bool) error {
	return runServiceCommandWithBrokerOptionsAndCron(ctx, cfg, runnerManager, jobRegistry, broker, unsafeDev, nil)
}

func runServiceCommandWithBrokerOptionsAndCron(ctx context.Context, cfg config.Config, runnerManager *runners.Manager, jobRegistry *jobs.Registry, broker *approval.Broker, unsafeDev bool, cronSvc *cron.Service) error {
	return runServiceCommandWithBrokerOptionsCronMCP(ctx, cfg, runnerManager, nil, jobRegistry, broker, unsafeDev, cronSvc)
}

func runServiceCommandWithBrokerOptionsCronMCP(ctx context.Context, cfg config.Config, runnerManager *runners.Manager, turnOrchestrator *app.RunnerTurnOrchestrator, jobRegistry *jobs.Registry, broker *approval.Broker, unsafeDev bool, cronSvc *cron.Service) error {
	return runServiceCommandWithBrokerOptionsCronMCPAndChannels(ctx, cfg, serviceHostDeps{}, runnerManager, nil, turnOrchestrator, jobRegistry, broker, unsafeDev, cronSvc, nil)
}

func runServiceCommandWithBrokerOptionsCronMCPAndChannels(ctx context.Context, cfg config.Config, host serviceHostDeps, runnerManager *runners.Manager, chatManager *runners.ChatManager, turnOrchestrator *app.RunnerTurnOrchestrator, jobRegistry *jobs.Registry, broker *approval.Broker, unsafeDev bool, cronSvc *cron.Service, channelDeliverer channels.MetaDeliverer) error {
	or3log.InstallStdlibSink()
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		return fmt.Errorf("service secret is required")
	}
	if err := validateStartupCommandWithOptions("service", cfg, unsafeDev, false); err != nil {
		return err
	}
	if host.DB == nil {
		return fmt.Errorf("database not configured")
	}
	if jobRegistry == nil {
		jobRegistry = jobs.NewRegistry(0, 0)
	}
	server := &serviceServer{config: cfg, configPath: cfgPathOrDefault(""), cronSvc: cronSvc, runnerManager: runnerManager, chatManager: nil, turnOrchestrator: turnOrchestrator, jobs: jobRegistry, channelDeliverer: channelDeliverer, broker: broker, unsafeDev: unsafeDev}
	server.applyHostDeps(host)
	if db := server.serviceDB(); db != nil {
		server.memorySvc = memorysvc.New(cfg, db, server.serviceEmbedProvider(), currentEmbedFingerprint(cfg))
	}
	if chatManager != nil {
		server.chatManager = chatManager
	} else if db := server.serviceDB(); db != nil {
		server.chatManager = buildRuntimeChatManager(cfg, db, runnerManager, jobRegistry, broker)
	}
	if turnOrchestrator == nil && server.chatManager != nil {
		turnOrchestrator = buildRunnerTurnOrchestrator(cfg, server.chatManager, server.serviceDB(), server.serviceMemRetriever(), server.serviceDocRetriever(), server.serviceEmbedProvider())
	}
	server.turnOrchestrator = turnOrchestrator
	if server.chatManager != nil {
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
		s.controlSvc = controlplane.New(s.config, s.serviceDB(), s.serviceEmbedProvider(), s.serviceAudit(), s.broker, s.jobs)
	})
	return s.controlSvc
}

func (s *serviceServer) app() *app.ServiceApp {
	s.appOnce.Do(func() {
		s.appSvc = app.NewServiceAppWithRunnerTurns(s.config, s.jobs, s.runnerManager, s.turnOrchestrator, s.control())
	})
	return s.appSvc
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
			if s.writePersistedRunnerRunSnapshot(w, r, jobID) {
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
