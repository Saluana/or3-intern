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
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
	"or3-intern/internal/tools"
)

type serviceTestTool struct {
	name string
}

func (t serviceTestTool) Name() string               { return t.name }
func (t serviceTestTool) Description() string        { return t.name }
func (t serviceTestTool) Parameters() map[string]any { return map[string]any{} }
func (t serviceTestTool) Schema() map[string]any     { return map[string]any{} }
func (t serviceTestTool) Execute(context.Context, map[string]any) (string, error) {
	return "", nil
}

type serviceContextTool struct{}

func (serviceContextTool) Name() string               { return "context_probe" }
func (serviceContextTool) Description() string        { return "context_probe" }
func (serviceContextTool) Parameters() map[string]any { return map[string]any{} }
func (serviceContextTool) Schema() map[string]any     { return map[string]any{} }
func (serviceContextTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	token := tools.ApprovalTokenFromContext(ctx)
	identity := tools.RequesterIdentityFromContext(ctx)
	wantToken := strings.TrimSpace(fmt.Sprint(params["token"]))
	wantActor := strings.TrimSpace(fmt.Sprint(params["actor"]))
	wantRole := strings.TrimSpace(fmt.Sprint(params["role"]))
	if token != wantToken {
		return "", fmt.Errorf("approval token mismatch: got %q want %q", token, wantToken)
	}
	if identity.Actor != wantActor || identity.Role != wantRole {
		return "", fmt.Errorf("requester identity mismatch: got %#v want actor=%q role=%q", identity, wantActor, wantRole)
	}
	return "ok", nil
}

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
	badSignature := mutateServiceTokenSignature(valid)
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

func TestDecodeServiceTurnRequest_AcceptsToolPolicyAliases(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})

	req, err := decodeServiceTurnRequest(strings.NewReader(`{
		"intern_session_key":"svc:alias",
		"message":"hello",
		"tool_policy":{"mode":"deny_all"},
		"profileName":"ops",
		"meta":{"trace_id":"trace-1"}
	}`), registry)
	if err != nil {
		t.Fatalf("decodeServiceTurnRequest: %v", err)
	}
	if req.SessionKey != "svc:alias" {
		t.Fatalf("expected intern session alias to populate session key, got %#v", req)
	}
	if !req.RestrictTools || len(req.AllowedTools) != 0 {
		t.Fatalf("expected deny_all tool policy to restrict to zero tools, got %#v", req)
	}
	if req.ProfileName != "ops" {
		t.Fatalf("expected profile alias to populate profile name, got %#v", req)
	}
	if req.Meta["trace_id"] != "trace-1" {
		t.Fatalf("expected meta to be preserved, got %#v", req.Meta)
	}
}

func TestDecodeServiceTurnRequest_AcceptsApprovalTokenAliases(t *testing.T) {
	registry := tools.NewRegistry()

	req, err := decodeServiceTurnRequest(strings.NewReader(`{
		"session_key":"svc:approval",
		"message":"hello",
		"approvalToken":"token-1"
	}`), registry)
	if err != nil {
		t.Fatalf("decodeServiceTurnRequest: %v", err)
	}
	if req.ApprovalToken != "token-1" {
		t.Fatalf("expected approval token alias to populate approval token, got %#v", req)
	}
}

func TestDecodeServiceSubagentRequest_AcceptsSessionAndToolPolicyAliases(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})
	registry.Register(serviceTestTool{name: "write_file"})

	req, err := decodeServiceSubagentRequest(strings.NewReader(`{
		"sessionKey":"svc:parent",
		"task":"background task",
		"promptSnapshot":[{"role":"user","content":"remember this"}],
		"toolPolicy":{"mode":"deny_list","blockedTools":["write_file"]},
		"timeout":9,
		"profile_name":"or3-default",
		"channel":"service",
		"replyTo":"or3-net"
	}`), registry)
	if err != nil {
		t.Fatalf("decodeServiceSubagentRequest: %v", err)
	}
	if req.ParentSessionKey != "svc:parent" {
		t.Fatalf("expected session key alias to populate parent session key, got %#v", req)
	}
	if !req.RestrictTools || len(req.AllowedTools) != 1 || req.AllowedTools[0] != "read_file" {
		t.Fatalf("expected deny_list to resolve surviving tools, got %#v", req)
	}
	if req.TimeoutSeconds != 9 {
		t.Fatalf("expected timeout alias to populate timeout seconds, got %#v", req)
	}
	if len(req.PromptSnapshot) != 1 || req.PromptSnapshot[0].Role != "user" {
		t.Fatalf("expected prompt snapshot alias to populate prompt snapshot, got %#v", req.PromptSnapshot)
	}
	if req.ReplyTo != "or3-net" {
		t.Fatalf("expected replyTo alias to populate reply target, got %#v", req)
	}
}

func TestDecodeServiceTurnRequest_RejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})
	for _, body := range []string{
		`{"session_key":"svc:test","message":"hi","unexpected":true}`,
		`{"session_key":"svc:test","message":"hi"} {"extra":true}`,
	} {
		if _, err := decodeServiceTurnRequest(strings.NewReader(body), registry); err == nil {
			t.Fatalf("expected decode failure for body %q", body)
		}
	}
}

func TestDecodeServiceSubagentRequest_RejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	registry := tools.NewRegistry()
	for _, body := range []string{
		`{"parent_session_key":"svc:test","task":"run","unexpected":true}`,
		`{"parent_session_key":"svc:test","task":"run"} []`,
	} {
		if _, err := decodeServiceSubagentRequest(strings.NewReader(body), registry); err == nil {
			t.Fatalf("expected decode failure for body %q", body)
		}
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
	req.Header.Set("X-Request-Id", "req_intern_turn")
	req.Header.Set("X-Workspace-Id", "ws_intern")
	req.Header.Set("X-Network-Session-Id", "sess_intern")
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
	jobID, _ := payload["job_id"].(string)
	snapshot, ok := server.jobs.Snapshot(jobID)
	if !ok {
		t.Fatalf("expected stored snapshot for %s", jobID)
	}
	for _, event := range snapshot.Events {
		if event.Type != "queued" && event.Type != "started" && event.Type != "completion" && event.Type != "error" {
			continue
		}
		if event.Data["request_id"] != "req_intern_turn" {
			t.Fatalf("expected request_id in lifecycle event, got %#v", event.Data)
		}
		if event.Data["workspace_id"] != "ws_intern" {
			t.Fatalf("expected workspace_id in lifecycle event, got %#v", event.Data)
		}
		if event.Data["network_session_id"] != "sess_intern" {
			t.Fatalf("expected network_session_id in lifecycle event, got %#v", event.Data)
		}
	}
}

func TestServiceTurns_PropagatesApprovalContext(t *testing.T) {
	callCount := 0
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 0 {
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "context_probe",
								Arguments: `{"token":"approve-token-1","actor":"service:shared-secret","role":"admin"}`,
							},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{Role: "assistant", Content: "approved"},
				}},
			}
		}
		callCount++
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	})
	defer cleanup()
	rt.Tools.Register(serviceContextTool{})
	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("c", 32), server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, strings.Repeat("c", 32), http.MethodPost, "/internal/v1/turns", `{"session_key":"svc:ctx","message":"hello","approval_token":"approve-token-1"}`)
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
	if payload["status"] != "completed" {
		t.Fatalf("expected completed job, got %#v", payload)
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
			name:       "snapshot missing job returns not found",
			method:     http.MethodGet,
			path:       "/internal/v1/jobs/job_1",
			wantStatus: http.StatusNotFound,
			wantBody:   "job not found",
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
	req.Header.Set("X-Request-Id", "req_subagent")
	req.Header.Set("X-Workspace-Id", "ws_subagent")
	req.Header.Set("X-Network-Session-Id", "sess_subagent")
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
	serviceMeta, _ := metadata["service_meta"].(map[string]any)
	if serviceMeta["request_id"] != "req_subagent" || serviceMeta["workspace_id"] != "ws_subagent" || serviceMeta["network_session_id"] != "sess_subagent" {
		t.Fatalf("expected audit headers to persist in service_meta, got %#v", metadata)
	}
	snapshot, ok := jobs.Snapshot(jobID)
	if !ok {
		t.Fatalf("expected queued job snapshot for %s", jobID)
	}
	queuedEventFound := false
	for _, event := range snapshot.Events {
		if event.Type != "queued" {
			continue
		}
		queuedEventFound = true
		if event.Data["request_id"] != "req_subagent" || event.Data["workspace_id"] != "ws_subagent" || event.Data["network_session_id"] != "sess_subagent" {
			t.Fatalf("expected audit headers in queued subagent lifecycle event, got %#v", event.Data)
		}
	}
	if !queuedEventFound {
		t.Fatalf("expected queued lifecycle event for %s", jobID)
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
	return httptest.NewServer(serviceAuthMiddlewareWithBroker(secret, server.broker, newServiceMux(server)))
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

func mutateServiceTokenSignature(token string) string {
	if token == "" {
		return "0"
	}
	last := token[len(token)-1]
	replacement := byte('0')
	if last == '0' {
		replacement = '1'
	}
	return token[:len(token)-1] + string(replacement)
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

func buildServiceTestBroker(t *testing.T, mutate func(*config.ApprovalConfig)) (*approval.Broker, func()) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "service-approval-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Enabled = true
	if mutate != nil {
		mutate(&approvalCfg)
	}
	broker := &approval.Broker{
		DB:      database,
		Config:  approvalCfg,
		HostID:  approvalCfg.HostID,
		SignKey: []byte(strings.Repeat("k", 32)),
	}
	return broker, func() {
		database.Close()
	}
}

func mustJSONRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func mustDecodeJSONBody(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return payload
}

func readLatestAuditEvent(t *testing.T, database *db.DB) db.AuditEvent {
	t.Helper()
	row := database.SQL.QueryRow(`SELECT id, event_type, session_key, actor, payload_json, prev_hash, record_hash, created_at FROM audit_events ORDER BY id DESC LIMIT 1`)
	var event db.AuditEvent
	if err := row.Scan(&event.ID, &event.EventType, &event.SessionKey, &event.Actor, &event.PayloadJSON, &event.PrevHash, &event.RecordHash, &event.CreatedAt); err != nil {
		t.Fatalf("scan audit event: %v", err)
	}
	return event
}

func TestServicePairingWorkflow_AllowsUnauthenticatedBootstrapAndPairedOperatorRoutes(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("p", 32), server)
	defer httpServer.Close()

	createReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/requests", `{"role":"operator","display_name":"Ops Laptop"}`)
	createResp, err := httpServer.Client().Do(createReq)
	if err != nil {
		t.Fatalf("Do create pairing: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected create pairing 202, got %d (%s)", createResp.StatusCode, mustReadBody(t, createResp.Body))
	}
	created := mustDecodeJSONBody(t, createResp.Body)
	requestID := int64(created["id"].(float64))
	code := created["code"].(string)

	approveReq := mustServiceRequest(t, httpServer, strings.Repeat("p", 32), http.MethodPost, fmt.Sprintf("/internal/v1/pairing/requests/%d/approve", requestID), "")
	approveResp, err := httpServer.Client().Do(approveReq)
	if err != nil {
		t.Fatalf("Do approve pairing: %v", err)
	}
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approve pairing 200, got %d (%s)", approveResp.StatusCode, mustReadBody(t, approveResp.Body))
	}

	exchangeReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/exchange", fmt.Sprintf(`{"request_id":%d,"code":%q}`, requestID, code))
	exchangeResp, err := httpServer.Client().Do(exchangeReq)
	if err != nil {
		t.Fatalf("Do exchange pairing: %v", err)
	}
	defer exchangeResp.Body.Close()
	if exchangeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected exchange pairing 200, got %d (%s)", exchangeResp.StatusCode, mustReadBody(t, exchangeResp.Body))
	}
	exchanged := mustDecodeJSONBody(t, exchangeResp.Body)
	token := exchanged["token"].(string)
	if token == "" {
		t.Fatal("expected paired device token")
	}

	deviceReq := mustJSONRequest(t, http.MethodGet, httpServer.URL+"/internal/v1/devices", "")
	deviceReq.Header.Set("Authorization", "Bearer "+token)
	deviceResp, err := httpServer.Client().Do(deviceReq)
	if err != nil {
		t.Fatalf("Do list devices: %v", err)
	}
	defer deviceResp.Body.Close()
	if deviceResp.StatusCode != http.StatusOK {
		t.Fatalf("expected paired operator device access 200, got %d (%s)", deviceResp.StatusCode, mustReadBody(t, deviceResp.Body))
	}
}

func TestServicePairingWorkflow_RejectsNonOperatorDeviceOnOperatorRoutes(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("r", 32), server)
	defer httpServer.Close()

	createReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/requests", `{"role":"service-client","display_name":"Worker"}`)
	createResp, err := httpServer.Client().Do(createReq)
	if err != nil {
		t.Fatalf("Do create pairing: %v", err)
	}
	defer createResp.Body.Close()
	created := mustDecodeJSONBody(t, createResp.Body)
	requestID := int64(created["id"].(float64))
	code := created["code"].(string)

	approveReq := mustServiceRequest(t, httpServer, strings.Repeat("r", 32), http.MethodPost, fmt.Sprintf("/internal/v1/pairing/requests/%d/approve", requestID), "")
	approveResp, err := httpServer.Client().Do(approveReq)
	if err != nil {
		t.Fatalf("Do approve pairing: %v", err)
	}
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approve pairing 200, got %d (%s)", approveResp.StatusCode, mustReadBody(t, approveResp.Body))
	}

	exchangeReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/exchange", fmt.Sprintf(`{"request_id":%d,"code":%q}`, requestID, code))
	exchangeResp, err := httpServer.Client().Do(exchangeReq)
	if err != nil {
		t.Fatalf("Do exchange pairing: %v", err)
	}
	defer exchangeResp.Body.Close()
	exchanged := mustDecodeJSONBody(t, exchangeResp.Body)
	token := exchanged["token"].(string)

	deviceReq := mustJSONRequest(t, http.MethodGet, httpServer.URL+"/internal/v1/devices", "")
	deviceReq.Header.Set("Authorization", "Bearer "+token)
	deviceResp, err := httpServer.Client().Do(deviceReq)
	if err != nil {
		t.Fatalf("Do list devices: %v", err)
	}
	defer deviceResp.Body.Close()
	if deviceResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected non-operator device to be forbidden, got %d (%s)", deviceResp.StatusCode, mustReadBody(t, deviceResp.Body))
	}
}

func TestServicePairing_AllowlistMode_AnonymousCannotReissueExistingDeviceToken(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAllowlist
	})
	defer cleanup()

	if _, _, err := broker.RotateDeviceToken(context.Background(), "device-1", approval.RoleOperator, "Ops Laptop", nil); err != nil {
		t.Fatalf("RotateDeviceToken seed: %v", err)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("l", 32), server)
	defer httpServer.Close()

	createReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/requests", `{"role":"operator","device_id":"device-1","display_name":"Ops Laptop"}`)
	createResp, err := httpServer.Client().Do(createReq)
	if err != nil {
		t.Fatalf("Do create pairing: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected create pairing 202, got %d (%s)", createResp.StatusCode, mustReadBody(t, createResp.Body))
	}
	created := mustDecodeJSONBody(t, createResp.Body)
	requestID := int64(created["id"].(float64))
	record, err := broker.DB.GetPairingRequest(context.Background(), requestID)
	if err != nil {
		t.Fatalf("GetPairingRequest: %v", err)
	}
	if record.Status != approval.StatusPending {
		t.Fatalf("expected anonymous allowlist pairing to remain pending, got %#v", record)
	}

	exchangeReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/exchange", fmt.Sprintf(`{"request_id":%d,"code":%q}`, requestID, created["code"].(string)))
	exchangeResp, err := httpServer.Client().Do(exchangeReq)
	if err != nil {
		t.Fatalf("Do exchange pairing: %v", err)
	}
	defer exchangeResp.Body.Close()
	if exchangeResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected pending exchange to fail, got %d (%s)", exchangeResp.StatusCode, mustReadBody(t, exchangeResp.Body))
	}
}

func TestServicePairing_TrustedMode_AnonymousRequestStaysPending(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeTrusted
	})
	defer cleanup()

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("t", 32), server)
	defer httpServer.Close()

	createReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/requests", `{"role":"operator","display_name":"Ops Laptop"}`)
	createResp, err := httpServer.Client().Do(createReq)
	if err != nil {
		t.Fatalf("Do create pairing: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected create pairing 202, got %d (%s)", createResp.StatusCode, mustReadBody(t, createResp.Body))
	}
	created := mustDecodeJSONBody(t, createResp.Body)
	requestID := int64(created["id"].(float64))
	record, err := broker.DB.GetPairingRequest(context.Background(), requestID)
	if err != nil {
		t.Fatalf("GetPairingRequest: %v", err)
	}
	if record.Status != approval.StatusPending {
		t.Fatalf("expected anonymous trusted pairing to remain pending, got %#v", record)
	}
}

func TestServiceTurns_RejectsOversizedBody(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	})
	defer cleanup()
	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("o", 32), server)
	defer httpServer.Close()

	oversized := `{"session_key":"svc:big","message":"` + strings.Repeat("x", int(serviceTurnsBodyLimit)) + `"}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("o", 32), http.MethodPost, "/internal/v1/turns", oversized)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do oversized turn: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
}

func TestServicePairing_UnauthenticatedRoutesStampAnonymousAuditKind(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()
	broker.Audit = &security.AuditLogger{DB: broker.DB, Key: []byte(strings.Repeat("z", 32))}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("z", 32), server)
	defer httpServer.Close()

	createReq := mustJSONRequest(t, http.MethodPost, httpServer.URL+"/internal/v1/pairing/requests", `{"role":"operator","display_name":"Ops Laptop"}`)
	createResp, err := httpServer.Client().Do(createReq)
	if err != nil {
		t.Fatalf("Do create pairing: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected create pairing 202, got %d (%s)", createResp.StatusCode, mustReadBody(t, createResp.Body))
	}

	event := readLatestAuditEvent(t, broker.DB)
	if event.Actor != "anonymous" {
		t.Fatalf("expected anonymous audit actor, got %#v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload["auth_kind"] != "unauthenticated" {
		t.Fatalf("expected unauthenticated auth_kind, got %#v", payload)
	}
}

func TestServiceApprovals_PairedOperatorCanApprovePendingRequest(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if !decision.RequiresApproval || decision.RequestID == 0 {
		t.Fatalf("expected pending approval request, got %#v", decision)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("a", 32), server)
	defer httpServer.Close()

	_, deviceToken, err := broker.RotateDeviceToken(context.Background(), "operator-1", approval.RoleOperator, "Operator", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}

	approveReq := mustJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/internal/v1/approvals/%d/approve", httpServer.URL, decision.RequestID), `{"note":"approved"}`)
	approveReq.Header.Set("Authorization", "Bearer "+deviceToken)
	approveResp, err := httpServer.Client().Do(approveReq)
	if err != nil {
		t.Fatalf("Do approve approval: %v", err)
	}
	defer approveResp.Body.Close()
	if approveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected approval 200, got %d (%s)", approveResp.StatusCode, mustReadBody(t, approveResp.Body))
	}
	payload := mustDecodeJSONBody(t, approveResp.Body)
	approvalToken, _ := payload["token"].(string)
	if approvalToken == "" {
		t.Fatal("expected approval token in response")
	}
	if err := broker.VerifyApprovalToken(context.Background(), approvalToken, decision.SubjectHash, broker.HostID); err != nil {
		t.Fatalf("VerifyApprovalToken: %v", err)
	}
}

func TestServiceApprovals_Approve_RejectsMalformedJSON(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	_, deviceToken, err := broker.RotateDeviceToken(context.Background(), "operator-1", approval.RoleOperator, "Operator", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("m", 32), server)
	defer httpServer.Close()

	req := mustJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/internal/v1/approvals/%d/approve", httpServer.URL, decision.RequestID), `{"note":`)
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do malformed approve: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected malformed approve 400, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
}

func TestServiceApprovals_Deny_RejectsMalformedJSON(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	_, deviceToken, err := broker.RotateDeviceToken(context.Background(), "operator-1", approval.RoleOperator, "Operator", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("n", 32), server)
	defer httpServer.Close()

	req := mustJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/internal/v1/approvals/%d/deny", httpServer.URL, decision.RequestID), `{"note":`)
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do malformed deny: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected malformed deny 400, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
}

func TestServiceDevices_Rotate_RevokedDeviceFails(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	req, code, err := broker.CreatePairingRequest(context.Background(), approval.PairingRequestInput{
		Role:        approval.RoleOperator,
		DisplayName: "Ops Laptop",
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := broker.ApprovePairingRequest(context.Background(), req.ID, "cli"); err != nil {
		t.Fatalf("ApprovePairingRequest: %v", err)
	}
	device, _, err := broker.ExchangePairingCode(context.Background(), approval.PairingExchangeInput{RequestID: req.ID, Code: code})
	if err != nil {
		t.Fatalf("ExchangePairingCode: %v", err)
	}
	if err := broker.RevokeDevice(context.Background(), device.DeviceID, "cli"); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	secret := strings.Repeat("d", 32)
	httpServer := newServiceTestHTTPServer(t, secret, server)
	defer httpServer.Close()

	rotateReq := mustServiceRequest(t, httpServer, secret, http.MethodPost, fmt.Sprintf("/internal/v1/devices/%s/rotate", device.DeviceID), "")
	rotateResp, err := httpServer.Client().Do(rotateReq)
	if err != nil {
		t.Fatalf("Do rotate device: %v", err)
	}
	defer rotateResp.Body.Close()
	if rotateResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected revoked rotate 400, got %d (%s)", rotateResp.StatusCode, mustReadBody(t, rotateResp.Body))
	}
}

func TestServiceApprovals_CancelRoute(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()
	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("x", 32), server)
	defer httpServer.Close()

	req := mustJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/internal/v1/approvals/%d/cancel", httpServer.URL, decision.RequestID), `{"note":"hold"}`)
	req.Header.Set("Authorization", "Bearer "+mustIssueServiceTokenAt(t, strings.Repeat("x", 32), time.Now()))
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do cancel approval: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	item, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if item.Status != approval.StatusCanceled {
		t.Fatalf("expected canceled status, got %#v", item)
	}
}

func TestServiceApprovals_ExpireRoute(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
		cfg.PendingTTLSeconds = 1
	})
	defer cleanup()
	now := time.Unix(1_700_000_000, 0).UTC()
	broker.Now = func() time.Time { return now }
	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	broker.Now = func() time.Time { return now.Add(5 * time.Second) }
	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("y", 32), server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, strings.Repeat("y", 32), http.MethodPost, "/internal/v1/approvals/expire", "")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do expire approvals: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected expire 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["expired"] != float64(1) {
		t.Fatalf("expected expired count 1, got %#v", payload)
	}
	item, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if item.Status != approval.StatusExpired {
		t.Fatalf("expected expired status, got %#v", item)
	}
}

func TestServiceApprovals_AllowlistsCRUD(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, nil)
	defer cleanup()
	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	secret := strings.Repeat("g", 32)
	httpServer := newServiceTestHTTPServer(t, secret, server)
	defer httpServer.Close()

	addReq := mustServiceRequest(t, httpServer, secret, http.MethodPost, "/internal/v1/approvals/allowlists", `{"domain":"exec","scope":{"host_id":"`+broker.HostID+`","tool":"exec"},"matcher":{"executable_path":"/bin/echo"}}`)
	addResp, err := httpServer.Client().Do(addReq)
	if err != nil {
		t.Fatalf("Do add allowlist: %v", err)
	}
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected add allowlist 202, got %d (%s)", addResp.StatusCode, mustReadBody(t, addResp.Body))
	}
	added := mustDecodeJSONBody(t, addResp.Body)
	item, ok := added["item"].(map[string]any)
	if !ok {
		t.Fatalf("expected allowlist item payload, got %#v", added)
	}
	id := int64(item["ID"].(float64))

	listReq := mustServiceRequest(t, httpServer, secret, http.MethodGet, "/internal/v1/approvals/allowlists?domain=exec", "")
	listResp, err := httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("Do list allowlists: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list allowlists 200, got %d (%s)", listResp.StatusCode, mustReadBody(t, listResp.Body))
	}

	removeReq := mustServiceRequest(t, httpServer, secret, http.MethodPost, fmt.Sprintf("/internal/v1/approvals/allowlists/%d/remove", id), "")
	removeResp, err := httpServer.Client().Do(removeReq)
	if err != nil {
		t.Fatalf("Do remove allowlist: %v", err)
	}
	defer removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected remove allowlist 200, got %d (%s)", removeResp.StatusCode, mustReadBody(t, removeResp.Body))
	}
}

func TestServiceJobs_GetSnapshotRoute(t *testing.T) {
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{jobs: jobs}
	job := jobs.Register("turn")
	jobs.Publish(job.ID, "started", map[string]any{"status": "running"})
	jobs.Complete(job.ID, "completed", map[string]any{"final_text": "done"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/jobs/"+job.ID, nil)
	server.handleJobs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	payload := mustDecodeJSONBody(t, rec.Body)
	if payload["final_text"] != "done" {
		t.Fatalf("expected final_text in snapshot, got %#v", payload)
	}
	if _, ok := payload["events"].([]any); !ok {
		t.Fatalf("expected events array in snapshot payload, got %#v", payload)
	}
}

func TestServiceStatusEndpoints(t *testing.T) {
	cfg := hostedNoExecBaseConfig()
	cfg.Channels.Slack.Enabled = true
	rt := &agent.Runtime{Tools: tools.NewRegistry()}
	server := &serviceServer{config: cfg, runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	for _, tc := range []struct {
		path       string
		wantStatus int
		wantKey    string
	}{
		{path: "/internal/v1/health", wantStatus: http.StatusOK, wantKey: "status"},
		{path: "/internal/v1/readiness", wantStatus: http.StatusServiceUnavailable, wantKey: "ready"},
		{path: "/internal/v1/capabilities", wantStatus: http.StatusOK, wantKey: "runtimeProfile"},
	} {
		req := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodGet, tc.path, "")
		resp, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatalf("Do %s: %v", tc.path, err)
		}
		if resp.StatusCode != tc.wantStatus {
			t.Fatalf("expected %s to return %d, got %d (%s)", tc.path, tc.wantStatus, resp.StatusCode, mustReadBody(t, resp.Body))
		}
		payload := mustDecodeJSONBody(t, resp.Body)
		resp.Body.Close()
		if _, ok := payload[tc.wantKey]; !ok {
			t.Fatalf("expected key %q in %s payload, got %#v", tc.wantKey, tc.path, payload)
		}
	}
}

func TestServiceStatusEndpoints_RejectNonGET(t *testing.T) {
	server := &serviceServer{jobs: agent.NewJobRegistry(time.Minute, 32)}
	tests := []struct {
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{path: "/internal/v1/health", handler: server.handleHealth},
		{path: "/internal/v1/readiness", handler: server.handleReadiness},
		{path: "/internal/v1/capabilities", handler: server.handleCapabilities},
	}
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.path, nil)
		tc.handler(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %s to reject POST with 405, got %d (%s)", tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestRunServiceCommand_RefusesInvalidHostedProfile(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {})
	defer cleanup()

	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("x", 32)
	cfg.Service.Listen = "127.0.0.1:0"
	cfg.RuntimeProfile = config.ProfileHostedService
	// Deliberately misconfigure: hosted profile requires secret store and audit
	cfg.Security.SecretStore.Enabled = false
	cfg.Security.Audit.Enabled = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runServiceCommand(ctx, cfg, rt, nil, nil)
	if err == nil {
		t.Fatal("expected error for misconfigured hosted profile, got nil")
	}
	if !strings.Contains(err.Error(), "service startup refused") {
		t.Fatalf("expected 'service startup refused' in error, got: %v", err)
	}
}

func hostedNoExecBaseConfig() config.Config {
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("x", 32)
	cfg.Service.Listen = "127.0.0.1:0"
	cfg.RuntimeProfile = config.ProfileHostedNoExec
	cfg.Security.SecretStore.Enabled = true
	cfg.Security.SecretStore.Required = true
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.Strict = true
	cfg.Security.Audit.VerifyOnStart = true
	cfg.Security.Network.Enabled = true
	cfg.Security.Network.DefaultDeny = true
	cfg.Hardening.EnableExecShell = false
	cfg.Hardening.PrivilegedTools = false
	return cfg
}

func TestRunServiceCommand_HostedNoExec_RefusesExecShell(t *testing.T) {
	cfg := hostedNoExecBaseConfig()
	cfg.Hardening.EnableExecShell = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runServiceCommand(ctx, cfg, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for hosted-no-exec with enableExecShell=true, got nil")
	}
	if !strings.Contains(err.Error(), "startup refused") {
		t.Fatalf("expected 'startup refused' in error, got: %v", err)
	}
}

func TestRunServiceCommand_HostedNoExec_RefusesPrivilegedTools(t *testing.T) {
	cfg := hostedNoExecBaseConfig()
	cfg.Hardening.PrivilegedTools = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runServiceCommand(ctx, cfg, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for hosted-no-exec with privilegedTools=true, got nil")
	}
	if !strings.Contains(err.Error(), "startup refused") {
		t.Fatalf("expected 'startup refused' in error, got: %v", err)
	}
}

func TestRunServiceCommand_HostedNoExec_AllowsCleanConfig(t *testing.T) {
	cfg := hostedNoExecBaseConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Profile validation runs before the runtime nil check, so a nil runtime
	// here means we reach "runtime not configured" only if profile checks pass.
	// If profile validation had rejected the config it would return "startup refused".
	err := runServiceCommand(ctx, cfg, nil, nil, nil)
	if err != nil && strings.Contains(err.Error(), "startup refused") {
		t.Fatalf("clean hosted-no-exec config should not be refused, got: %v", err)
	}
}

// TestV1TurnsContractAliases pins the accepted session key aliases and allowed_tools
// alias for POST /internal/v1/turns so that regressions break CI.
func TestV1TurnsContractAliases(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})

	cases := []struct {
		name    string
		body    string
		wantKey string
	}{
		{
			name:    "intern_session_key",
			body:    `{"intern_session_key":"svc:intern","message":"hi"}`,
			wantKey: "svc:intern",
		},
		{
			name:    "sessionKey camelCase",
			body:    `{"sessionKey":"svc:camel","message":"hi"}`,
			wantKey: "svc:camel",
		},
		{
			name:    "internSessionKey camelCase",
			body:    `{"internSessionKey":"svc:camel-intern","message":"hi"}`,
			wantKey: "svc:camel-intern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := decodeServiceTurnRequest(strings.NewReader(tc.body), registry)
			if err != nil {
				t.Fatalf("decodeServiceTurnRequest: %v", err)
			}
			if req.SessionKey != tc.wantKey {
				t.Fatalf("expected session key %q, got %q", tc.wantKey, req.SessionKey)
			}
		})
	}

	// allowedTools camelCase alias should work the same as allowed_tools
	t.Run("allowedTools camelCase", func(t *testing.T) {
		req, err := decodeServiceTurnRequest(strings.NewReader(`{"session_key":"svc:key","message":"hi","allowedTools":["read_file"]}`), registry)
		if err != nil {
			t.Fatalf("decodeServiceTurnRequest: %v", err)
		}
		if !req.RestrictTools || len(req.AllowedTools) != 1 || req.AllowedTools[0] != "read_file" {
			t.Fatalf("expected allowedTools alias to restrict to [read_file], got RestrictTools=%v AllowedTools=%v", req.RestrictTools, req.AllowedTools)
		}
	})
}

// TestV1SubagentsContractAliases pins the accepted parent_session_key aliases and
// timeout aliases for POST /internal/v1/subagents.
func TestV1SubagentsContractAliases(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})

	sessionAliases := []struct {
		name    string
		body    string
		wantKey string
	}{
		{
			name:    "session_key",
			body:    `{"session_key":"svc:sk","task":"do it"}`,
			wantKey: "svc:sk",
		},
		{
			name:    "intern_session_key",
			body:    `{"intern_session_key":"svc:isk","task":"do it"}`,
			wantKey: "svc:isk",
		},
		{
			name:    "sessionKey camelCase",
			body:    `{"sessionKey":"svc:skc","task":"do it"}`,
			wantKey: "svc:skc",
		},
		{
			name:    "parentSessionKey camelCase",
			body:    `{"parentSessionKey":"svc:psk","task":"do it"}`,
			wantKey: "svc:psk",
		},
		{
			name:    "internSessionKey camelCase",
			body:    `{"internSessionKey":"svc:iskc","task":"do it"}`,
			wantKey: "svc:iskc",
		},
	}
	for _, tc := range sessionAliases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := decodeServiceSubagentRequest(strings.NewReader(tc.body), registry)
			if err != nil {
				t.Fatalf("decodeServiceSubagentRequest: %v", err)
			}
			if req.ParentSessionKey != tc.wantKey {
				t.Fatalf("expected parent_session_key %q, got %q", tc.wantKey, req.ParentSessionKey)
			}
		})
	}

	timeoutAliases := []struct {
		name    string
		body    string
		wantSec int
	}{
		{
			name:    "timeout alias",
			body:    `{"parent_session_key":"svc:p","task":"do it","timeout":30}`,
			wantSec: 30,
		},
		{
			name:    "timeoutSeconds camelCase alias",
			body:    `{"parent_session_key":"svc:p","task":"do it","timeoutSeconds":60}`,
			wantSec: 60,
		},
	}
	for _, tc := range timeoutAliases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := decodeServiceSubagentRequest(strings.NewReader(tc.body), registry)
			if err != nil {
				t.Fatalf("decodeServiceSubagentRequest: %v", err)
			}
			if req.TimeoutSeconds != tc.wantSec {
				t.Fatalf("expected TimeoutSeconds %d, got %d", tc.wantSec, req.TimeoutSeconds)
			}
		})
	}
}

// TestV1ToolPolicyShape pins the tool_policy JSON shape in both snake_case and
// camelCase forms for turns and subagents.
func TestV1ToolPolicyShape(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})
	registry.Register(serviceTestTool{name: "exec"})

	cases := []struct {
		name        string
		body        string
		wantMode    string
		wantAllowed []string
		wantBlocked []string
	}{
		{
			name: "snake_case tool_policy",
			body: `{
				"session_key":"svc:k","message":"hi",
				"tool_policy":{
					"mode":"allow_list",
					"allowed_tools":["read_file"],
					"blocked_tools":["exec"]
				}
			}`,
			wantMode:    "allow_list",
			wantAllowed: []string{"read_file"},
			wantBlocked: []string{"exec"},
		},
		{
			name: "camelCase toolPolicy",
			body: `{
				"session_key":"svc:k","message":"hi",
				"toolPolicy":{
					"mode":"allow_list",
					"allowedTools":["read_file"],
					"blockedTools":["exec"]
				}
			}`,
			wantMode:    "allow_list",
			wantAllowed: []string{"read_file"},
			wantBlocked: []string{"exec"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := decodeServiceTurnRequest(strings.NewReader(tc.body), registry)
			if err != nil {
				t.Fatalf("decodeServiceTurnRequest: %v", err)
			}
			// allow_list: only read_file survives
			if !req.RestrictTools {
				t.Fatalf("expected RestrictTools=true for allow_list mode")
			}
			if len(req.AllowedTools) != 1 || req.AllowedTools[0] != "read_file" {
				t.Fatalf("expected AllowedTools=[read_file], got %v", req.AllowedTools)
			}
		})
	}
}

// TestV1JobsStreamRoute pins the stable v1 route shapes for job stream and abort.
func TestV1JobsStreamRoute(t *testing.T) {
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{jobs: jobs}

	routes := []struct {
		method   string
		path     string
		wantCode int
	}{
		// GET /stream on unknown job → 404
		{http.MethodGet, "/internal/v1/jobs/unknown-job-id/stream", http.StatusNotFound},
		// POST /stream → 405
		{http.MethodPost, "/internal/v1/jobs/unknown-job-id/stream", http.StatusMethodNotAllowed},
		// POST /abort on unknown job → 404
		{http.MethodPost, "/internal/v1/jobs/unknown-job-id/abort", http.StatusNotFound},
		// GET /abort → 405
		{http.MethodGet, "/internal/v1/jobs/unknown-job-id/abort", http.StatusMethodNotAllowed},
	}
	for _, tc := range routes {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			server.handleJobs(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d (%s)", tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestV1JobsAbortCompletedJob verifies that aborting an already-completed job
// returns 200 (ok:true) rather than a conflict error.
func TestV1JobsAbortCompletedJob(t *testing.T) {
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{jobs: jobs}

	snapshot := jobs.Register("turn")
	jobs.Complete(snapshot.ID, "completed", map[string]any{"final_text": "done"})

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/jobs/"+snapshot.ID+"/abort", nil)
	rec := httptest.NewRecorder()
	server.handleJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for completed job abort, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok:true in abort response for completed job, got %#v", payload)
	}
}

// TestV1TurnsRequiresSessionKeyAndMessage verifies that a request missing
// session_key returns HTTP 400.
func TestV1TurnsRequiresSessionKeyAndMessage(t *testing.T) {
	rt := &agent.Runtime{Tools: tools.NewRegistry()}
	jobs := agent.NewJobRegistry(time.Minute, 32)
	server := &serviceServer{runtime: rt, jobs: jobs}

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/turns", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.handleTurns(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing session_key, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, hasError := payload["error"]; !hasError {
		t.Fatalf("expected error field in 400 response, got %#v", payload)
	}
}
