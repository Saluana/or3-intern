package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

type serviceTerminalTestWriteCloser struct {
	bytes.Buffer
}

func (w *serviceTerminalTestWriteCloser) Close() error {
	return nil
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
	server := &serviceServer{terminalSessions: map[string]*serviceTerminalSession{session.ID: session}}
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
