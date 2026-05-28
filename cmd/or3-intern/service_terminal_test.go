package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

type serviceTerminalTestWriteCloser struct {
	bytes.Buffer
}

func (w *serviceTerminalTestWriteCloser) Close() error {
	return nil
}

func TestServiceTerminalSlowSubscriberIsClosed(t *testing.T) {
	session := &serviceTerminalSession{ID: "term-test"}
	_, events, unsubscribe := session.subscribe()
	defer unsubscribe()

	for i := 0; i < 33; i++ {
		session.appendEvent("output", map[string]any{"i": i})
	}

	for range 32 {
		if _, ok := <-events; !ok {
			t.Fatal("subscriber closed before buffered events were readable")
		}
	}
	if _, ok := <-events; ok {
		t.Fatal("expected slow subscriber channel to be closed")
	}
}

func TestServiceComponentsConcurrentInit(t *testing.T) {
	server := &serviceServer{}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = server.terminals()
			_ = server.terminalTickets()
			_ = server.serviceRateLimiter()
			_ = server.serviceAuthFailures()
			_ = server.serviceModelCatalog()
		}()
	}
	wg.Wait()
}

func TestServiceTerminalDisabledWhenShellModeOff(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Service.SharedSecretRole = approval.RoleAdmin
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = false
	server := &serviceServer{config: cfg}
	httpServer := newServiceTestHTTPServer(t, "terminal-secret", server)
	defer httpServer.Close()

	resp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions", `{"root_id":"workspace","path":"."}`))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 503, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestServiceTerminalDisabledWhenRuntimeRequiresSandbox(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.RuntimeProfile = config.ProfileHostedRemoteSandbox
	cfg.Service.SharedSecretRole = approval.RoleAdmin
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	cfg.Hardening.Sandbox.Enabled = true
	server := &serviceServer{config: cfg}
	httpServer := newServiceTestHTTPServer(t, "terminal-secret", server)
	defer httpServer.Close()

	resp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions", `{"root_id":"workspace","path":"."}`))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 503, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestServiceTerminalSessionLifecycle(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Service.SharedSecretRole = approval.RoleAdmin
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	server := &serviceServer{config: cfg}
	httpServer := newServiceTestHTTPServer(t, "terminal-secret", server)
	defer httpServer.Close()

	createResp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions", `{"root_id":"workspace","path":".","shell":"sh"}`))
	if err != nil {
		t.Fatalf("Do create: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, string(body))
	}
	var session map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&session); err != nil {
		t.Fatalf("Decode create response: %v", err)
	}
	sessionID, _ := session["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	runningDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(runningDeadline) {
		sessionRecord, ok := server.getTerminalSessionByID(sessionID)
		if ok {
			sessionRecord.mu.Lock()
			status := sessionRecord.Status
			sessionRecord.mu.Unlock()
			if status == "running" {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	inputResp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions/"+sessionID+"/input", `{"input":"printf 'hello from test\\n'\nexit\n"}`))
	if err != nil {
		t.Fatalf("Do input: %v", err)
	}
	inputResp.Body.Close()
	if inputResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from input, got %d", inputResp.StatusCode)
	}

	newlineResp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions/"+sessionID+"/input", `{"input":"\n"}`))
	if err != nil {
		t.Fatalf("Do newline input: %v", err)
	}
	newlineResp.Body.Close()
	if newlineResp.StatusCode != http.StatusOK && newlineResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 200/409 from newline input, got %d", newlineResp.StatusCode)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessionRecord, ok := server.getTerminalSessionByID(sessionID)
		if ok {
			sessionRecord.mu.Lock()
			status := sessionRecord.Status
			events := append([]serviceTerminalEvent(nil), sessionRecord.events...)
			sessionRecord.mu.Unlock()
			if status == "exited" || status == "failed" || status == "closed" {
				joined := ""
				for _, event := range events {
					if event.Type == "output" {
						joined += event.Data["chunk"].(string)
					}
				}
				if !strings.Contains(joined, "hello from test") {
					t.Fatalf("expected output chunk in terminal events, got %q", joined)
				}
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	closeResp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions/"+sessionID+"/close", `{}`))
	if err != nil {
		t.Fatalf("Do close: %v", err)
	}
	defer closeResp.Body.Close()
	if closeResp.StatusCode != http.StatusOK && closeResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(closeResp.Body)
		t.Fatalf("expected 200/404 from close, got %d: %s", closeResp.StatusCode, string(body))
	}
}

func TestTerminalWebSocketTicketLifecycle(t *testing.T) {
	server := &serviceServer{}
	now := time.Now().UTC()

	ticket, expiresAt, err := server.issueTerminalWebSocketTicketValue("term-1", now)
	if err != nil {
		t.Fatalf("issueTerminalWebSocketTicketValue: %v", err)
	}
	if ticket == "" {
		t.Fatal("expected non-empty ticket")
	}
	if !expiresAt.After(now) {
		t.Fatalf("expected future expiry, got %s", expiresAt)
	}

	hash := terminalWebSocketTicketHash(ticket)
	server.terminalTickets().mu.Lock()
	if _, ok := server.terminalTickets().tickets[ticket]; ok {
		t.Fatal("raw websocket ticket was stored as a map key")
	}
	record, ok := server.terminalTickets().tickets[hash]
	server.terminalTickets().mu.Unlock()
	if !ok {
		t.Fatal("expected hashed websocket ticket record")
	}
	if record.SessionID != "term-1" {
		t.Fatalf("expected ticket to bind term-1, got %q", record.SessionID)
	}

	if server.consumeTerminalWebSocketTicket("term-2", ticket, now.Add(time.Second)) {
		t.Fatal("ticket consumed for wrong terminal session")
	}
	if !server.consumeTerminalWebSocketTicket("term-1", ticket, now.Add(time.Second)) {
		t.Fatal("expected ticket to be consumed for the matching session")
	}
	if server.consumeTerminalWebSocketTicket("term-1", ticket, now.Add(2*time.Second)) {
		t.Fatal("ticket was reusable after first consume")
	}

	expiredTicket, _, err := server.issueTerminalWebSocketTicketValue("term-1", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("issue expired ticket: %v", err)
	}
	if server.consumeTerminalWebSocketTicket("term-1", expiredTicket, now) {
		t.Fatal("expired ticket was accepted")
	}
}

func TestServiceTerminalWebSocketLifecycle(t *testing.T) {
	server, httpServer := newTerminalWebSocketTestServer(t)
	defer httpServer.Close()

	sessionID := createTerminalWebSocketTestSession(t, httpServer)
	conn := dialTerminalWebSocketWithTicket(t, server, httpServer, sessionID, "")
	defer conn.Close()

	seenSnapshot := false
	output := ""
	if err := conn.WriteJSON(serviceTerminalWebSocketClientMessage{Type: "input", Input: "printf 'hello from websocket\\n'\n"}); err != nil {
		t.Fatalf("write input: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		event := readTerminalWebSocketEvent(t, conn, time.Second)
		switch event.Type {
		case "snapshot":
			seenSnapshot = true
		case "output":
			chunk, _ := event.Data["chunk"].(string)
			output += chunk
			if strings.Contains(output, "hello from websocket") {
				goto gotOutput
			}
		}
	}
	t.Fatalf("expected websocket terminal output, got %q", output)

gotOutput:
	if !seenSnapshot {
		t.Fatal("expected websocket history replay to include snapshot")
	}
	if err := conn.WriteJSON(serviceTerminalWebSocketClientMessage{Type: "resize", Rows: 40, Cols: 120}); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		event := readTerminalWebSocketEvent(t, conn, time.Second)
		if event.Type != "resize" {
			continue
		}
		if rows, _ := event.Data["rows"].(float64); rows != 40 {
			t.Fatalf("expected resize rows 40, got %#v", event.Data["rows"])
		}
		if cols, _ := event.Data["cols"].(float64); cols != 120 {
			t.Fatalf("expected resize cols 120, got %#v", event.Data["cols"])
		}
		goto gotResize
	}
	t.Fatal("expected websocket resize event")

gotResize:
	if err := conn.WriteJSON(serviceTerminalWebSocketClientMessage{Type: "close"}); err != nil {
		t.Fatalf("write close: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		event := readTerminalWebSocketEvent(t, conn, time.Second)
		if event.Type == "status" && event.Data["status"] == "closed" {
			return
		}
	}
	t.Fatal("expected websocket closed status event")
}

func TestServiceTerminalWebSocketRejectsMissingTicket(t *testing.T) {
	_, httpServer := newTerminalWebSocketTestServer(t)
	defer httpServer.Close()
	sessionID := createTerminalWebSocketTestSession(t, httpServer)

	wsURL := terminalWebSocketURL(httpServer, sessionID)
	dialer := websocket.Dialer{Subprotocols: []string{serviceTerminalWebSocketProtocol}}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected websocket dial without auth ticket to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		if resp == nil {
			t.Fatal("expected 401 response, got nil")
		}
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestServiceTerminalWebSocketRejectsUntrustedOrigin(t *testing.T) {
	server, httpServer := newTerminalWebSocketTestServer(t)
	defer httpServer.Close()
	sessionID := createTerminalWebSocketTestSession(t, httpServer)

	ticket := issueTerminalWebSocketTicketForTest(t, httpServer, sessionID)
	dialer := websocket.Dialer{Subprotocols: []string{serviceTerminalWebSocketProtocol, serviceTerminalWebSocketTicketPrefix + ticket}}
	header := http.Header{}
	header.Set("Origin", "https://evil.example")
	conn, resp, err := dialer.Dial(terminalWebSocketURL(httpServer, sessionID), header)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected websocket dial with untrusted origin to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		if resp == nil {
			t.Fatal("expected 403 response, got nil")
		}
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	server.terminalClose(sessionID, "closed")
}

func TestAllocateTerminalSessionIDUsesRandomHexID(t *testing.T) {
	server := &serviceServer{}
	id1, err := server.allocateTerminalSessionID()
	if err != nil {
		t.Fatalf("allocate first id: %v", err)
	}
	id2, err := server.allocateTerminalSessionID()
	if err != nil {
		t.Fatalf("allocate second id: %v", err)
	}
	pattern := regexp.MustCompile(`^term_[0-9a-f]{24}$`)
	if !pattern.MatchString(id1) {
		t.Fatalf("expected random hex terminal id, got %q", id1)
	}
	if !pattern.MatchString(id2) {
		t.Fatalf("expected random hex terminal id, got %q", id2)
	}
	if id1 == id2 {
		t.Fatalf("expected unique terminal ids, got duplicate %q", id1)
	}
	if regexp.MustCompile(`^term_\d+_\d+$`).MatchString(id1) {
		t.Fatalf("terminal id still uses predictable sequence format: %q", id1)
	}
}

func TestCollectTerminalOutputStreamsPartialChunks(t *testing.T) {
	reader, writer := io.Pipe()
	session := &serviceTerminalSession{ID: "test", subscribers: map[chan serviceTerminalEvent]struct{}{}}
	server := &serviceServer{}
	done := make(chan struct{})
	go func() {
		server.collectTerminalOutput(session, reader, "stdout")
		close(done)
	}()

	if _, err := writer.Write([]byte("prompt> ")); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		events := append([]serviceTerminalEvent(nil), session.events...)
		session.mu.Unlock()
		for _, event := range events {
			if event.Type == "output" && event.Data["chunk"] == "prompt> " {
				_ = writer.Close()
				<-done
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = writer.Close()
	<-done
	t.Fatal("expected partial chunk before newline or EOF")
}

func TestWriteTerminalInputAcceptsNewlineOnlyInput(t *testing.T) {
	writer := &serviceTerminalTestWriteCloser{}
	session := &serviceTerminalSession{ID: "term-test", Status: "running", ExpiresAt: time.Now().Add(time.Minute), stdin: writer, subscribers: map[chan serviceTerminalEvent]struct{}{}}
	server := &serviceServer{terminalManager: &serviceTerminalManager{sessions: map[string]*serviceTerminalSession{session.ID: session}}}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-test/input", strings.NewReader(`{"input":"\n"}`))
	rec := httptest.NewRecorder()

	server.writeTerminalInput(rec, req, session.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if writer.String() != "\n" {
		t.Fatalf("expected newline input, got %q", writer.String())
	}
}

func TestWriteTerminalInputRefreshesSessionExpiry(t *testing.T) {
	writer := &serviceTerminalTestWriteCloser{}
	oldExpiry := time.Now().Add(2 * time.Second).UTC()
	session := &serviceTerminalSession{
		ID:          "term-refresh-input",
		Status:      "running",
		ExpiresAt:   oldExpiry,
		stdin:       writer,
		subscribers: map[chan serviceTerminalEvent]struct{}{},
	}
	server := &serviceServer{terminalManager: &serviceTerminalManager{sessions: map[string]*serviceTerminalSession{session.ID: session}}}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-refresh-input/input", strings.NewReader(`{"input":"echo hi\n"}`))
	rec := httptest.NewRecorder()

	server.writeTerminalInput(rec, req, session.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.ExpiresAt.After(oldExpiry) {
		t.Fatalf("expected session expiry to refresh, old=%s new=%s", oldExpiry, session.ExpiresAt)
	}
	if session.LastActiveAt.IsZero() {
		t.Fatal("expected last active time to be updated")
	}
}

func TestAppendTerminalOutputRefreshesSessionExpiry(t *testing.T) {
	oldExpiry := time.Now().Add(2 * time.Second).UTC()
	session := &serviceTerminalSession{
		ID:          "term-refresh-output",
		ExpiresAt:   oldExpiry,
		subscribers: map[chan serviceTerminalEvent]struct{}{},
	}

	session.appendEvent("output", map[string]any{"chunk": "prompt> "})

	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.ExpiresAt.After(oldExpiry) {
		t.Fatalf("expected output to refresh session expiry, old=%s new=%s", oldExpiry, session.ExpiresAt)
	}
	if session.LastActiveAt.IsZero() {
		t.Fatal("expected last active time to be updated")
	}
}

func TestAppendTerminalEventDropsFullSubscriberWithoutBlocking(t *testing.T) {
	full := make(chan serviceTerminalEvent)
	session := &serviceTerminalSession{
		ID:          "term-safe-publish",
		ExpiresAt:   time.Now().Add(time.Minute),
		subscribers: map[chan serviceTerminalEvent]struct{}{full: {}},
	}

	session.appendEvent("output", map[string]any{"chunk": "hello"})

	session.mu.Lock()
	_, stillSubscribed := session.subscribers[full]
	eventCount := len(session.events)
	session.mu.Unlock()
	if stillSubscribed {
		t.Fatalf("expected blocked subscriber to be dropped")
	}
	if eventCount != 1 {
		t.Fatalf("expected event to be retained, got %d", eventCount)
	}
	select {
	case _, ok := <-full:
		if ok {
			t.Fatalf("expected dropped subscriber channel to be closed")
		}
	default:
		t.Fatalf("expected dropped subscriber channel to be closed")
	}
}

func TestAppendTerminalOutputCoalescesAdjacentChunks(t *testing.T) {
	session := &serviceTerminalSession{
		ID:          "term-coalesce",
		ExpiresAt:   time.Now().Add(time.Minute),
		subscribers: map[chan serviceTerminalEvent]struct{}{},
	}
	live := make(chan serviceTerminalEvent, 4)
	session.subscribers[live] = struct{}{}

	session.appendEvent("output", map[string]any{"stream": "stdout", "chunk": "hel"})
	session.appendEvent("output", map[string]any{"stream": "stdout", "chunk": "lo"})

	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.events) != 1 {
		t.Fatalf("expected one buffered output event, got %d", len(session.events))
	}
	if got := session.events[0].Data["chunk"]; got != "hello" {
		t.Fatalf("expected coalesced chunk hello, got %v", got)
	}

	select {
	case event := <-live:
		if event.Data["chunk"] != "hel" {
			t.Fatalf("expected first live chunk hel, got %v", event.Data["chunk"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected first live output event")
	}
	select {
	case event := <-live:
		if event.Data["chunk"] != "lo" {
			t.Fatalf("expected second live chunk lo, got %v", event.Data["chunk"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected second live output event")
	}
}

func TestAppendTerminalReplayBufferIsBoundedByBytes(t *testing.T) {
	session := &serviceTerminalSession{
		ID:          "term-trim",
		ExpiresAt:   time.Now().Add(time.Minute),
		subscribers: map[chan serviceTerminalEvent]struct{}{},
	}
	chunk := strings.Repeat("a", 32<<10)
	for range 12 {
		session.appendEvent("output", map[string]any{"stream": "stdout", "chunk": chunk})
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if got := terminalReplayOutputBytes(session.events); got > serviceTerminalReplayMaxBytes {
		t.Fatalf("expected replay buffer <= %d bytes, got %d", serviceTerminalReplayMaxBytes, got)
	}
	if len(session.events) == 0 {
		t.Fatal("expected replay buffer to retain a recent output tail")
	}
}

func TestListTerminalSessionsReturnsMostRecentFirst(t *testing.T) {
	now := time.Now().UTC()
	server := &serviceServer{terminalManager: &serviceTerminalManager{sessions: map[string]*serviceTerminalSession{
		"term-old": {
			ID:           "term-old",
			Shell:        "/bin/zsh",
			WorkingDir:   "/tmp/old",
			RelativePath: ".",
			RootID:       "workspace",
			CreatedAt:    now.Add(-2 * time.Minute),
			LastActiveAt: now.Add(-90 * time.Second),
			ExpiresAt:    now.Add(time.Minute),
			Status:       "running",
			Rows:         24,
			Cols:         80,
		},
		"term-new": {
			ID:           "term-new",
			Shell:        "/bin/zsh",
			WorkingDir:   "/tmp/new",
			RelativePath: ".",
			RootID:       "workspace",
			CreatedAt:    now.Add(-time.Minute),
			LastActiveAt: now,
			ExpiresAt:    now.Add(time.Minute),
			Status:       "running",
			Rows:         24,
			Cols:         80,
		},
	}}}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/terminal/sessions", nil)
	rec := httptest.NewRecorder()

	server.listTerminalSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(body.Items))
	}
	if got := body.Items[0]["session_id"]; got != "term-new" {
		t.Fatalf("expected newest session first, got %v", got)
	}
}

func TestTerminalShellArgs(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{shell: "/bin/zsh", want: []string{"-il"}},
		{shell: "/bin/bash", want: []string{"-il"}},
		{shell: "/bin/sh", want: []string{"-i"}},
	}
	for _, tt := range tests {
		got := terminalShellArgs(tt.shell)
		if !slices.Equal(got, tt.want) {
			t.Fatalf("terminalShellArgs(%q) = %v, want %v", tt.shell, got, tt.want)
		}
	}
}

func TestResizeTerminalSessionClampsRowsCols(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	session := &serviceTerminalSession{
		ID:          "term-resize",
		Status:      "running",
		Rows:        24,
		Cols:        80,
		ExpiresAt:   time.Now().Add(time.Minute),
		ptyFile:     ptmx,
		subscribers: map[chan serviceTerminalEvent]struct{}{},
	}
	server := &serviceServer{terminalManager: &serviceTerminalManager{sessions: map[string]*serviceTerminalSession{session.ID: session}}}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-resize/resize", strings.NewReader(`{"rows":999999,"cols":888888}`))
	rec := httptest.NewRecorder()

	server.resizeTerminalSession(rec, req, session.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if session.Rows != 200 || session.Cols != 400 {
		t.Fatalf("expected clamped size 200x400, got %dx%d", session.Rows, session.Cols)
	}
	if !strings.Contains(rec.Body.String(), `"rows":200`) || !strings.Contains(rec.Body.String(), `"cols":400`) {
		t.Fatalf("expected clamped response body, got %s", rec.Body.String())
	}
}

func TestResizeTerminalSessionReturnsErrorWhenPTYResizeFails(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	_ = tty.Close()
	if err := ptmx.Close(); err != nil {
		t.Fatalf("close ptmx: %v", err)
	}

	session := &serviceTerminalSession{
		ID:          "term-resize-error",
		Status:      "running",
		Rows:        24,
		Cols:        80,
		ExpiresAt:   time.Now().Add(time.Minute),
		ptyFile:     ptmx,
		subscribers: map[chan serviceTerminalEvent]struct{}{},
	}
	server := &serviceServer{terminalManager: &serviceTerminalManager{sessions: map[string]*serviceTerminalSession{session.ID: session}}}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-resize-error/resize", strings.NewReader(`{"rows":40,"cols":120}`))
	rec := httptest.NewRecorder()

	server.resizeTerminalSession(rec, req, session.ID)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	for _, event := range session.events {
		if event.Type == "resize" {
			t.Fatalf("unexpected resize event after PTY resize failure")
		}
	}
}

func newTerminalWebSocketTestServer(t *testing.T) (*serviceServer, *httptest.Server) {
	t.Helper()
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Service.SharedSecretRole = approval.RoleAdmin
	cfg.Hardening.GuardedTools = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.EnableExecShell = true
	server := &serviceServer{config: cfg}
	httpServer := newServiceTestHTTPServer(t, "terminal-secret", server)
	return server, httpServer
}

func createTerminalWebSocketTestSession(t *testing.T, httpServer *httptest.Server) string {
	t.Helper()
	resp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions", `{"root_id":"workspace","path":".","shell":"sh","rows":24,"cols":80}`))
	if err != nil {
		t.Fatalf("create terminal session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 creating terminal session, got %d: %s", resp.StatusCode, string(body))
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode create terminal response: %v", err)
	}
	sessionID, _ := body["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response: %#v", body)
	}
	return sessionID
}

func issueTerminalWebSocketTicketForTest(t *testing.T, httpServer *httptest.Server, sessionID string) string {
	t.Helper()
	resp, err := http.DefaultClient.Do(mustServiceRequest(t, httpServer, "terminal-secret", http.MethodPost, "/internal/v1/terminal/sessions/"+sessionID+"/ws-ticket", `{}`))
	if err != nil {
		t.Fatalf("issue websocket ticket: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 issuing websocket ticket, got %d: %s", resp.StatusCode, string(body))
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode websocket ticket: %v", err)
	}
	if body.Ticket == "" {
		t.Fatal("expected websocket ticket in response")
	}
	return body.Ticket
}

func dialTerminalWebSocketWithTicket(t *testing.T, _ *serviceServer, httpServer *httptest.Server, sessionID string, origin string) *websocket.Conn {
	t.Helper()
	ticket := issueTerminalWebSocketTicketForTest(t, httpServer, sessionID)
	dialer := websocket.Dialer{Subprotocols: []string{serviceTerminalWebSocketProtocol, serviceTerminalWebSocketTicketPrefix + ticket}}
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	conn, resp, err := dialer.Dial(terminalWebSocketURL(httpServer, sessionID), header)
	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial failed with status %d: %v", resp.StatusCode, err)
		}
		t.Fatalf("websocket dial failed: %v", err)
	}
	if got := conn.Subprotocol(); got != serviceTerminalWebSocketProtocol {
		conn.Close()
		t.Fatalf("expected websocket subprotocol %q, got %q", serviceTerminalWebSocketProtocol, got)
	}
	return conn
}

func terminalWebSocketURL(httpServer *httptest.Server, sessionID string) string {
	return "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/internal/v1/terminal/sessions/" + sessionID + "/ws"
}

func readTerminalWebSocketEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration) serviceTerminalEvent {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var event serviceTerminalEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read websocket event: %v", err)
	}
	return event
}
