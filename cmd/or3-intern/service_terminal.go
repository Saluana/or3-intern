package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

type serviceTerminalEvent struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

type serviceTerminalWebSocketTicket struct {
	SessionID string
	ExpiresAt time.Time
}

type serviceTerminalManager struct {
	mu       sync.Mutex
	sessions map[string]*serviceTerminalSession
}

type serviceTerminalWebSocketTicketStore struct {
	mu      sync.Mutex
	tickets map[string]serviceTerminalWebSocketTicket
}

func (s *serviceServer) terminals() *serviceTerminalManager {
	s.components()
	return s.terminalManager
}

func (s *serviceServer) terminalTickets() *serviceTerminalWebSocketTicketStore {
	s.components()
	return s.terminalTicketStore
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
			delete(s.subscribers, subscriber)
			close(subscriber)
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
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
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
	s.terminals().mu.Lock()
	sessions := make([]*serviceTerminalSession, 0, len(s.terminals().sessions))
	for _, session := range s.terminals().sessions {
		if session != nil && session.Status == "running" {
			sessions = append(sessions, session)
		}
	}
	s.terminals().mu.Unlock()
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
	s.terminals().mu.Lock()
	sessions := make([]*serviceTerminalSession, 0)
	for id, session := range s.terminals().sessions {
		if session == nil || terminalSessionExpired(session, now) {
			delete(s.terminals().sessions, id)
			if session != nil {
				sessions = append(sessions, session)
			}
		}
	}
	s.terminals().mu.Unlock()
	for _, session := range sessions {
		session.close("expired")
	}
}

func (s *serviceServer) getTerminalSessionByID(sessionID string) (*serviceTerminalSession, bool) {
	s.terminals().mu.Lock()
	defer s.terminals().mu.Unlock()
	if s.terminals().sessions == nil {
		return nil, false
	}
	session, ok := s.terminals().sessions[sessionID]
	if !ok || session == nil {
		return nil, false
	}
	if terminalSessionExpired(session, time.Now().UTC()) {
		delete(s.terminals().sessions, sessionID)
		go session.close("expired")
		return nil, false
	}
	return session, true
}

func terminalSessionExpired(session *serviceTerminalSession, now time.Time) bool {
	if session == nil {
		return true
	}
	session.mu.Lock()
	expiresAt := session.ExpiresAt
	session.mu.Unlock()
	return now.After(expiresAt)
}

func (s *serviceServer) allocateTerminalSessionID() (string, error) {
	for range 8 {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id := "term_" + hex.EncodeToString(b)
		s.terminals().mu.Lock()
		_, exists := s.terminals().sessions[id]
		s.terminals().mu.Unlock()
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
	s.terminals().mu.Lock()
	activeSessions := len(s.terminals().sessions)
	s.terminals().mu.Unlock()
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
	s.terminals().mu.Lock()
	if s.terminals().sessions == nil {
		s.terminals().sessions = map[string]*serviceTerminalSession{}
	}
	s.terminals().sessions[session.ID] = session
	s.terminals().mu.Unlock()
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
	s.terminalTickets().mu.Lock()
	defer s.terminalTickets().mu.Unlock()
	if s.terminalTickets().tickets == nil {
		s.terminalTickets().tickets = map[string]serviceTerminalWebSocketTicket{}
	}
	s.cleanupTerminalWebSocketTicketsLocked(now.UTC())
	s.terminalTickets().tickets[hash] = serviceTerminalWebSocketTicket{SessionID: sessionID, ExpiresAt: expiresAt}
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
	s.terminalTickets().mu.Lock()
	defer s.terminalTickets().mu.Unlock()
	s.cleanupTerminalWebSocketTicketsLocked(now.UTC())
	record, ok := s.terminalTickets().tickets[hash]
	if !ok {
		return false
	}
	if record.SessionID != sessionID {
		return false
	}
	if now.UTC().After(record.ExpiresAt) {
		delete(s.terminalTickets().tickets, hash)
		return false
	}
	delete(s.terminalTickets().tickets, hash)
	return true
}

func (s *serviceServer) cleanupTerminalWebSocketTicketsLocked(now time.Time) {
	for hash, record := range s.terminalTickets().tickets {
		if now.After(record.ExpiresAt) {
			delete(s.terminalTickets().tickets, hash)
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
	s.terminals().mu.Lock()
	delete(s.terminals().sessions, sessionID)
	s.terminals().mu.Unlock()
	return nil
}
