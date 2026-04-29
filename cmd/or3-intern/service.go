package main

import (
	"context"
	"crypto/rand"
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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type serviceServer struct {
	config           config.Config
	configPath       string
	runtime          *agent.Runtime
	subagentManager  *agent.SubagentManager
	jobs             *agent.JobRegistry
	broker           *approval.Broker
	controlOnce      sync.Once
	controlSvc       *controlplane.Service
	appOnce          sync.Once
	appSvc           *app.ServiceApp
	terminalMu       sync.Mutex
	terminalSessions map[string]*serviceTerminalSession
	rateMu           sync.Mutex
	rateWindow       time.Time
	rateCounts       map[string]int
	authFailureMu    sync.Mutex
	authFailures     map[string]serviceAuthFailureState
}

type serviceAuthFailureState struct {
	Count        int
	FirstAttempt time.Time
	BlockedUntil time.Time
}

const (
	serviceTurnsBodyLimit      int64 = 1 << 20
	serviceSubagentsBodyLimit  int64 = 1 << 20
	servicePairingBodyLimit    int64 = 64 << 10
	serviceApprovalBodyLimit   int64 = 64 << 10
	serviceEmbeddingsBodyLimit int64 = 64 << 10
	serviceScopeBodyLimit      int64 = 64 << 10
	serviceConfigureBodyLimit  int64 = 256 << 10
	serviceFileUploadBodyLimit int64 = 128 << 20
	serviceTerminalBodyLimit   int64 = 64 << 10
	serviceTerminalSessionTTL        = 10 * time.Minute
	serviceTerminalMaxSessions       = 4
)

type serviceTerminalEvent struct {
	Type string
	Data map[string]any
}

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
	s.LastActiveAt = time.Now().UTC()
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
	s.stdin = nil
	s.cancel = nil
	s.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cancel != nil {
		cancel()
	}
	s.appendEvent("status", map[string]any{"status": status})
}

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
	server := &serviceServer{config: cfg, configPath: cfgPathOrDefault(""), runtime: rt, subagentManager: subagentManager, jobs: jobs, broker: broker}
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
	mux.Handle("/internal/v1/jobs/", http.HandlerFunc(server.handleJobs))
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
	mux.Handle("/internal/v1/embeddings", http.HandlerFunc(server.handleEmbeddings))
	mux.Handle("/internal/v1/embeddings/", http.HandlerFunc(server.handleEmbeddings))
	mux.Handle("/internal/v1/audit", http.HandlerFunc(server.handleAudit))
	mux.Handle("/internal/v1/audit/", http.HandlerFunc(server.handleAudit))
	mux.Handle("/internal/v1/scope", http.HandlerFunc(server.handleScope))
	mux.Handle("/internal/v1/scope/", http.HandlerFunc(server.handleScope))
	mux.Handle("/internal/v1/configure", http.HandlerFunc(server.handleConfigure))
	mux.Handle("/internal/v1/configure/", http.HandlerFunc(server.handleConfigure))
	mux.Handle("/internal/v1/files", http.HandlerFunc(server.handleFiles))
	mux.Handle("/internal/v1/files/", http.HandlerFunc(server.handleFiles))
	mux.Handle("/internal/v1/terminal/sessions", http.HandlerFunc(server.handleTerminal))
	mux.Handle("/internal/v1/terminal/sessions/", http.HandlerFunc(server.handleTerminal))
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
		s.appSvc = app.NewServiceApp(s.config, s.runtime, s.jobs, s.subagentManager, s.control())
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
	case "download":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleFileDownload(w, r)
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
	add := func(id, label, path string, writable bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			roots = append(roots, serviceFileRoot{ID: id, Label: label, Path: abs, Writable: writable})
		}
	}
	add("allowed", "Allowed Folder", s.config.AllowedDir, true)
	add("workspace", "Workspace", s.config.WorkspaceDir, true)
	add("artifacts", "Artifacts", s.config.ArtifactsDir, false)
	if len(roots) == 0 {
		if cwd, err := os.Getwd(); err == nil {
			roots = append(roots, serviceFileRoot{ID: "cwd", Label: "Current Directory", Path: cwd, Writable: true})
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
	for _, id := range []string{"workspace", "allowed", "cwd"} {
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
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.createTerminalSession(w, r)
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
	cmd := exec.CommandContext(ctx, shellPath)
	cmd.Dir = workingDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		writeServiceError(w, r, http.StatusBadGateway, "failed to open terminal stdin", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		writeServiceError(w, r, http.StatusBadGateway, "failed to open terminal stdout", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		writeServiceError(w, r, http.StatusBadGateway, "failed to open terminal stderr", err)
		return
	}
	sessionID, err := s.allocateTerminalSessionID()
	if err != nil {
		_ = stdin.Close()
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
		Status:        "starting",
		Rows:          max(body.Rows, 24),
		Cols:          max(body.Cols, 80),
		ApprovalMode:  string(s.config.Security.Approvals.Exec.Mode),
		ApprovalState: approvalDecision.Reason,
		ApprovalID:    approvalDecision.RequestID,
		cmd:           cmd,
		stdin:         stdin,
		cancel:        cancel,
		subscribers:   map[chan serviceTerminalEvent]struct{}{},
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		writeServiceError(w, r, http.StatusBadGateway, "failed to start terminal shell", err)
		return
	}
	session.mu.Lock()
	session.Status = "running"
	session.mu.Unlock()
	session.appendEvent("status", map[string]any{"status": "running"})
	session.appendEvent("snapshot", session.snapshot())
	s.terminalMu.Lock()
	if s.terminalSessions == nil {
		s.terminalSessions = map[string]*serviceTerminalSession{}
	}
	s.terminalSessions[session.ID] = session
	s.terminalMu.Unlock()
	go s.collectTerminalOutput(session, stdout, "stdout")
	go s.collectTerminalOutput(session, stderr, "stderr")
	go s.waitForTerminalSession(session)
	writeServiceJSON(w, http.StatusCreated, session.snapshot())
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

func (s *serviceServer) writeTerminalInput(w http.ResponseWriter, r *http.Request, sessionID string) {
	limitServiceRequestBody(w, r, serviceTerminalBodyLimit)
	var body struct {
		Input string `json:"input"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	if body.Input == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "input is required"})
		return
	}
	session.mu.Lock()
	stdin := session.stdin
	status := session.Status
	session.mu.Unlock()
	if stdin == nil || status != "running" {
		writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "terminal session is not writable"})
		return
	}
	if _, err := io.WriteString(stdin, body.Input); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "failed to write terminal input", err)
		return
	}
	session.appendEvent("input", map[string]any{"size": len(body.Input)})
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID})
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
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	session.mu.Lock()
	if body.Rows > 0 {
		session.Rows = body.Rows
	}
	if body.Cols > 0 {
		session.Cols = body.Cols
	}
	session.LastActiveAt = time.Now().UTC()
	session.ExpiresAt = time.Now().UTC().Add(serviceTerminalSessionTTL)
	rows, cols := session.Rows, session.Cols
	session.mu.Unlock()
	session.appendEvent("resize", map[string]any{"rows": rows, "cols": cols})
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "rows": rows, "cols": cols})
}

func (s *serviceServer) closeTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	session.close("closed")
	s.terminalMu.Lock()
	delete(s.terminalSessions, sessionID)
	s.terminalMu.Unlock()
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "status": "closed"})
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
	Section string `json:"section"`
	Channel string `json:"channel"`
	Field   string `json:"field"`
	Op      string `json:"op"`
	Value   string `json:"value"`
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

func (s *serviceServer) handleConfigure(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/configure"), "/")
	switch path {
	case "", "sections":
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
	case "fields":
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
	case "apply":
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
				changed, err := applyFieldValue(&next, section, channel, field, change.Value)
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
				changed, err := applyChoiceSelection(&next, section, channel, field, change.Value)
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
