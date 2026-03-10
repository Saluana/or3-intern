package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

func TestServiceAuthMiddleware_RejectsMissingBearer(t *testing.T) {
	handler := serviceAuthMiddleware("super-secret-super-secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/turns", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestServiceTurns_SSEStreamsLifecycleEvents(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	})
	defer cleanup()
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{runtime: rt, jobs: jobs}
	mux := http.NewServeMux()
	mux.Handle("/internal/v1/turns", http.HandlerFunc(server.handleTurns))
	httpServer := httptest.NewServer(serviceAuthMiddleware(strings.Repeat("s", 32), mux))
	defer httpServer.Close()

	token, err := issueServiceBearerToken(strings.Repeat("s", 32), time.Now())
	if err != nil {
		t.Fatalf("issueServiceBearerToken: %v", err)
	}
	body := strings.NewReader(`{"session_key":"svc:test","message":"hello"}`)
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/internal/v1/turns", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	text := string(payload)
	for _, want := range []string{"event: queued", "event: started", "event: text_delta", "Hello", "event: completion", "completed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected SSE payload to contain %q, got %s", want, text)
		}
	}
}

func TestServiceServer_AbortTurnJob(t *testing.T) {
	started := make(chan struct{}, 1)
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	})
	defer cleanup()
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{runtime: rt, jobs: jobs}
	job := jobs.Register("turn")
	ctx, cancel := context.WithCancel(context.Background())
	jobs.AttachCancel(job.ID, cancel)
	go server.runTurnJob(ctx, job.ID, serviceTurnRequest{SessionKey: "svc:abort", Message: "hang"})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected provider request to start")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/jobs/"+job.ID+"/abort", nil)
	server.abortJob(rec, req, job.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected abort to succeed, got %d (%s)", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, ok := jobs.Snapshot(job.ID)
		if ok && snapshot.Status == "aborted" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	snapshot, _ := jobs.Snapshot(job.ID)
	b, _ := json.Marshal(snapshot)
	t.Fatalf("expected aborted job status, got %s", string(b))
}

func buildServiceTestRuntime(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (*agent.Runtime, func()) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "service-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	providerServer := httptest.NewServer(http.HandlerFunc(handler))
	provider := providers.New(providerServer.URL, "test-key", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &agent.Runtime{
		DB:           database,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      &agent.Builder{DB: database, HistoryMax: 10},
		MaxToolLoops: 2,
	}
	cleanup := func() {
		providerServer.Close()
		database.Close()
	}
	return rt, cleanup
}
