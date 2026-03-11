package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
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

func TestValidateServiceAuthorization(t *testing.T) {
	secret := strings.Repeat("s", 32)
	now := time.Unix(1_700_000_000, 0)
	valid := mustIssueServiceTokenAt(t, secret, now.Add(-time.Minute))
	expired := mustIssueServiceTokenAt(t, secret, now.Add(-serviceTokenMaxAge-time.Second))
	future := mustIssueServiceTokenAt(t, secret, now.Add(31*time.Second))
	badSignature := valid[:len(valid)-1] + "0"
	invalidPayload := "%%%." + hex.EncodeToString(signServiceToken(secret, "%%%"))
	invalidJSONPayload := signedServiceToken(t, secret, base64.RawURLEncoding.EncodeToString([]byte("{")))
	missingNonce := signedServiceToken(t, secret, encodeServiceClaims(t, serviceTokenClaims{IssuedAt: now.Unix()}))
	invalidTimestamp := signedServiceToken(t, secret, encodeServiceClaims(t, serviceTokenClaims{IssuedAt: 0, Nonce: "nonce"}))

	tests := []struct {
		name    string
		header  string
		now     time.Time
		wantErr string
	}{
		{
			name:    "accepts valid token",
			header:  "Bearer " + valid,
			now:     now,
			wantErr: "",
		},
		{
			name:    "rejects missing bearer",
			header:  "",
			now:     now,
			wantErr: "missing bearer token",
		},
		{
			name:    "rejects bad format",
			header:  "Bearer no-dot-token",
			now:     now,
			wantErr: "invalid bearer token format",
		},
		{
			name:    "rejects bad signature",
			header:  "Bearer " + badSignature,
			now:     now,
			wantErr: "invalid bearer token signature",
		},
		{
			name:    "rejects invalid payload encoding",
			header:  "Bearer " + invalidPayload,
			now:     now,
			wantErr: "invalid bearer token payload",
		},
		{
			name:    "rejects invalid payload json",
			header:  "Bearer " + invalidJSONPayload,
			now:     now,
			wantErr: "invalid bearer token payload",
		},
		{
			name:    "rejects expired token",
			header:  "Bearer " + expired,
			now:     now,
			wantErr: "bearer token expired",
		},
		{
			name:    "rejects future token",
			header:  "Bearer " + future,
			now:     now,
			wantErr: "bearer token timestamp is in the future",
		},
		{
			name:    "rejects missing nonce",
			header:  "Bearer " + missingNonce,
			now:     now,
			wantErr: "invalid bearer token nonce",
		},
		{
			name:    "rejects invalid timestamp",
			header:  "Bearer " + invalidTimestamp,
			now:     now,
			wantErr: "invalid bearer token timestamp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServiceAuthorization(secret, tc.header, tc.now)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected authorization to succeed, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expected %q, got %v", tc.wantErr, err)
			}
		})
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
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("s", 32), server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, strings.Repeat("s", 32), http.MethodPost, "/internal/v1/turns", `{"session_key":"svc:test","message":"hello"}`)
	req.Header.Set("Accept", "text/event-stream")
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

func TestServiceTurns_JSONResponse(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":" json"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	})
	defer cleanup()
	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("j", 32), server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, strings.Repeat("j", 32), http.MethodPost, "/internal/v1/turns", `{"session_key":"svc:json","message":"hello"}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload["kind"] != "turn" || payload["status"] != "completed" {
		t.Fatalf("expected completed turn payload, got %#v", payload)
	}
	if payload["final_text"] != "Hello json" {
		t.Fatalf("expected final_text to be replayed, got %#v", payload)
	}
	if _, ok := payload["job_id"].(string); !ok {
		t.Fatalf("expected job_id in response, got %#v", payload)
	}
}

func TestServiceTurns_JSONFailureStatus(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider down", http.StatusBadGateway)
	})
	defer cleanup()
	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("f", 32), server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, strings.Repeat("f", 32), http.MethodPost, "/internal/v1/turns", `{"session_key":"svc:fail","message":"hello"}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", payload)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error in response, got %#v", payload)
	}
}

func TestServiceJobsStream_ReplaysCompletedJob(t *testing.T) {
	jobs := agent.NewJobRegistry(time.Minute, 32)
	job := jobs.RegisterWithID("job_replay", "turn")
	jobs.Publish(job.ID, "queued", map[string]any{"status": "queued"})
	jobs.Publish(job.ID, "started", map[string]any{"status": "running"})
	jobs.Publish(job.ID, "text_delta", map[string]any{"content": "Hello replay"})
	jobs.Complete(job.ID, "completed", map[string]any{"final_text": "done"})
	server := &serviceServer{jobs: jobs}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/jobs/"+job.ID+"/stream", nil)
	server.handleJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"event: queued", "event: started", "event: text_delta", "Hello replay", "event: completion", "final_text"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected replayed stream to contain %q, got %s", want, body)
		}
	}
}

func TestServiceJobs_MethodGuardsAndNotFound(t *testing.T) {
	server := &serviceServer{jobs: agent.NewJobRegistry(time.Minute, 32)}
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "turns rejects non post",
			method:     http.MethodGet,
			path:       "/internal/v1/turns",
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "stream rejects non get",
			method:     http.MethodPost,
			path:       "/internal/v1/jobs/job_1/stream",
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "abort rejects non post",
			method:     http.MethodGet,
			path:       "/internal/v1/jobs/job_1/abort",
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "stream missing job returns not found",
			method:     http.MethodGet,
			path:       "/internal/v1/jobs/job_missing/stream",
			wantStatus: http.StatusNotFound,
			wantBody:   "job not found",
		},
		{
			name:       "unknown action returns not found",
			method:     http.MethodGet,
			path:       "/internal/v1/jobs/job_1/unknown",
			wantStatus: http.StatusNotFound,
			wantBody:   "job action not found",
		},
		{
			name:       "malformed job path returns not found",
			method:     http.MethodGet,
			path:       "/internal/v1/jobs/job_1",
			wantStatus: http.StatusNotFound,
			wantBody:   "job route not found",
		},
		{
			name:       "subagents rejects non post",
			method:     http.MethodGet,
			path:       "/internal/v1/subagents",
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			switch {
			case tc.path == "/internal/v1/turns":
				server.handleTurns(rec, req)
			case tc.path == "/internal/v1/subagents":
				server.handleSubagents(rec, req)
			default:
				server.handleJobs(rec, req)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("expected body to contain %q, got %s", tc.wantBody, rec.Body.String())
			}
		})
	}
}

func TestServiceServer_AbortTurnJob(t *testing.T) {
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{jobs: jobs}
	job := jobs.Register("turn")
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() {
		close(started)
		<-ctx.Done()
		jobs.Complete(job.ID, "aborted", map[string]any{"message": "job aborted"})
	}()
	jobs.AttachCancel(job.ID, cancel)
	<-started

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

func TestServiceAbortJob_Matrix(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	jobs := agent.NewJobRegistry(time.Minute, 32)
	manager := &agent.SubagentManager{DB: database, Jobs: jobs}
	server := &serviceServer{subagentManager: manager, jobs: jobs}

	queuedJob := db.SubagentJob{
		ID:               "subagent-queued",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:queued",
		Task:             "queued task",
		Status:           db.SubagentStatusQueued,
	}
	if err := database.EnqueueSubagentJob(context.Background(), queuedJob); err != nil {
		t.Fatalf("EnqueueSubagentJob queued: %v", err)
	}
	jobs.RegisterWithID(queuedJob.ID, "subagent")

	conflictJob := jobs.RegisterWithID("job_conflict", "turn")
	jobs.Publish(conflictJob.ID, "started", map[string]any{"status": "running"})

	doneJob := jobs.RegisterWithID("job_done", "turn")
	jobs.Complete(doneJob.ID, "completed", map[string]any{"final_text": "done"})

	tests := []struct {
		name       string
		jobID      string
		wantStatus int
		wantBody   string
		verify     func(t *testing.T)
	}{
		{
			name:       "terminal job returns status",
			jobID:      doneJob.ID,
			wantStatus: http.StatusOK,
			wantBody:   `"status":"completed"`,
		},
		{
			name:       "missing job returns not found",
			jobID:      "job_missing",
			wantStatus: http.StatusNotFound,
			wantBody:   "job not found",
		},
		{
			name:       "queued subagent aborts",
			jobID:      queuedJob.ID,
			wantStatus: http.StatusOK,
			wantBody:   `"ok":true`,
			verify: func(t *testing.T) {
				t.Helper()
				stored, ok, err := database.GetSubagentJob(context.Background(), queuedJob.ID)
				if err != nil {
					t.Fatalf("GetSubagentJob queued: %v", err)
				}
				if !ok || stored.Status != db.SubagentStatusInterrupted {
					t.Fatalf("expected queued subagent to be interrupted, got %#v ok=%v", stored, ok)
				}
			},
		},
		{
			name:       "non cancelable running job conflicts",
			jobID:      conflictJob.ID,
			wantStatus: http.StatusConflict,
			wantBody:   "job is not abortable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/jobs/"+tc.jobID+"/abort", nil)
			server.abortJob(rec, req, tc.jobID)
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("expected body to contain %q, got %s", tc.wantBody, rec.Body.String())
			}
			if tc.verify != nil {
				tc.verify(t)
			}
		})
	}
}

func TestServiceSubagents_EnqueueAndAbortQueuedJob(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	jobs := agent.NewJobRegistry(time.Minute, 32)
	manager := &agent.SubagentManager{DB: database, Jobs: jobs, MaxQueued: 4}
	server := &serviceServer{subagentManager: manager, jobs: jobs}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("q", 32), server)
	defer httpServer.Close()

	reqBody := `{
		"parent_session_key":"parent",
		"task":"background task",
		"prompt_snapshot":[{"role":"user","content":"remember this"}],
		"allowed_tools":["read_file"],
		"timeout_seconds":9,
		"meta":{"trace_id":"trace-1"},
		"profile_name":"or3-default",
		"channel":"service",
		"reply_to":"or3-net"
	}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("q", 32), http.MethodPost, "/internal/v1/subagents", reqBody)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do subagents: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode subagents: %v", err)
	}
	jobID, _ := payload["job_id"].(string)
	if jobID == "" || payload["status"] != db.SubagentStatusQueued {
		t.Fatalf("expected queued subagent payload, got %#v", payload)
	}
	childKey, _ := payload["child_session_key"].(string)
	if childKey == "" || !strings.Contains(childKey, jobID) {
		t.Fatalf("expected child session key to include job id, got %#v", payload)
	}

	stored, ok, err := database.GetSubagentJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatalf("expected stored subagent job for %s", jobID)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(stored.MetadataJSON), &metadata); err != nil {
		t.Fatalf("Unmarshal metadata: %v", err)
	}
	if metadata["profile_name"] != "or3-default" {
		t.Fatalf("expected profile metadata to persist, got %#v", metadata)
	}
	if metadata["timeout_seconds"] != float64(9) {
		t.Fatalf("expected timeout metadata to persist, got %#v", metadata)
	}

	abortReq := mustServiceRequest(t, httpServer, strings.Repeat("q", 32), http.MethodPost, "/internal/v1/jobs/"+jobID+"/abort", "")
	abortResp, err := httpServer.Client().Do(abortReq)
	if err != nil {
		t.Fatalf("Do abort: %v", err)
	}
	defer abortResp.Body.Close()
	if abortResp.StatusCode != http.StatusOK {
		t.Fatalf("expected abort 200, got %d (%s)", abortResp.StatusCode, mustReadBody(t, abortResp.Body))
	}
	stored, ok, err = database.GetSubagentJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetSubagentJob after abort: %v", err)
	}
	if !ok || stored.Status != db.SubagentStatusInterrupted {
		t.Fatalf("expected interrupted subagent after abort, got %#v ok=%v", stored, ok)
	}
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
		providerServer.CloseClientConnections()
		providerServer.Close()
		database.Close()
	}
	return rt, cleanup
}

func openServiceTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "service-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	return database, func() {
		database.Close()
	}
}

func newServiceTestHTTPServer(t *testing.T, secret string, server *serviceServer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/internal/v1/turns", http.HandlerFunc(server.handleTurns))
	mux.Handle("/internal/v1/subagents", http.HandlerFunc(server.handleSubagents))
	mux.Handle("/internal/v1/jobs/", http.HandlerFunc(server.handleJobs))
	return httptest.NewServer(serviceAuthMiddleware(secret, mux))
}

func mustServiceRequest(t *testing.T, server *httptest.Server, secret, method, path, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	token := mustIssueServiceTokenAt(t, secret, time.Now())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func mustIssueServiceTokenAt(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	token, err := issueServiceBearerToken(secret, now)
	if err != nil {
		t.Fatalf("issueServiceBearerToken: %v", err)
	}
	return token
}

func encodeServiceClaims(t *testing.T, claims serviceTokenClaims) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("Marshal claims: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func signedServiceToken(t *testing.T, secret string, payloadPart string) string {
	t.Helper()
	return payloadPart + "." + hex.EncodeToString(signServiceToken(secret, payloadPart))
}

func mustReadBody(t *testing.T, body io.Reader) string {
	t.Helper()
	payload, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(payload)
}
