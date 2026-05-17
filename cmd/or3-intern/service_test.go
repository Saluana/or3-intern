package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/mcp"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type serviceTestTool struct {
	name string
}

type fakeServiceMCPTestManager struct {
	connectErr error
	closeErr   error
	status     map[string]mcp.ServerStatus
}

func (m *fakeServiceMCPTestManager) Connect(context.Context) error {
	return m.connectErr
}

func (m *fakeServiceMCPTestManager) Close() error {
	return m.closeErr
}

func (m *fakeServiceMCPTestManager) ServerStatus() map[string]mcp.ServerStatus {
	return m.status
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

type serviceReplayTool struct{}

func (serviceReplayTool) Name() string               { return "replay_probe" }
func (serviceReplayTool) Description() string        { return "replay_probe" }
func (serviceReplayTool) Parameters() map[string]any { return map[string]any{} }
func (serviceReplayTool) Schema() map[string]any     { return map[string]any{} }
func (serviceReplayTool) Execute(_ context.Context, params map[string]any) (string, error) {
	return fmt.Sprintf("replayed:%s", strings.TrimSpace(fmt.Sprint(params["value"]))), nil
}

type serviceReplayFailingTool struct{}

func (serviceReplayFailingTool) Name() string               { return "replay_probe" }
func (serviceReplayFailingTool) Description() string        { return "replay_probe" }
func (serviceReplayFailingTool) Parameters() map[string]any { return map[string]any{} }
func (serviceReplayFailingTool) Schema() map[string]any     { return map[string]any{} }
func (serviceReplayFailingTool) Execute(_ context.Context, params map[string]any) (string, error) {
	value := strings.TrimSpace(fmt.Sprint(params["value"]))
	return fmt.Sprintf("stdout:\n\n\nstderr:\nreplay failed for %s", value), fmt.Errorf("exec failed: exit status 3")
}

type countingReplayTool struct {
	count *int
}

func (countingReplayTool) Name() string               { return "replay_probe" }
func (countingReplayTool) Description() string        { return "replay_probe" }
func (countingReplayTool) Parameters() map[string]any { return map[string]any{} }
func (countingReplayTool) Schema() map[string]any     { return map[string]any{} }
func (t countingReplayTool) Execute(_ context.Context, params map[string]any) (string, error) {
	if t.count != nil {
		*t.count = *t.count + 1
	}
	return fmt.Sprintf("replayed:%s", strings.TrimSpace(fmt.Sprint(params["value"]))), nil
}

type serviceApprovalTool struct{}

func (serviceApprovalTool) Name() string               { return "exec" }
func (serviceApprovalTool) Description() string        { return "exec approval probe" }
func (serviceApprovalTool) Parameters() map[string]any { return map[string]any{} }
func (serviceApprovalTool) Schema() map[string]any     { return map[string]any{} }
func (serviceApprovalTool) Execute(context.Context, map[string]any) (string, error) {
	return "", &tools.ApprovalRequiredError{ToolName: "exec", RequestID: 77}
}

func TestServiceObserver_RecoversEmptyFinalTextAfterToolWork(t *testing.T) {
	observer := &serviceObserver{}
	observer.OnToolLifecycle(context.Background(), agent.ToolLifecycleEvent{
		ToolCallID:    "call_exec",
		Name:          "exec",
		Status:        "completed",
		ResultPreview: "stdout:\nv22.21.1",
	})

	finalText, recovered := observer.finalTextForCompletion("")
	if !recovered {
		t.Fatal("expected empty final text to be recovered after tool work")
	}
	if !strings.Contains(finalText, "tool finished") || !strings.Contains(finalText, "exec") {
		t.Fatalf("expected visible fallback final text, got %q", finalText)
	}
}

func TestServiceObserver_RecoversEmptyApprovalResumeWithoutToolWork(t *testing.T) {
	observer := &serviceObserver{}

	finalText, recovered := observer.finalTextForCompletion("resume did not produce text")
	if !recovered {
		t.Fatal("expected default resume fallback to be used")
	}
	if finalText != "resume did not produce text" {
		t.Fatalf("unexpected fallback final text: %q", finalText)
	}
}

func TestBoundedServiceLogPreview_RedactsTokenShapedValues(t *testing.T) {
	preview := boundedServiceLogPreview(`stdout:
token: ya29.a0AQvPyExampleSecret
Authorization: Bearer abc.def.ghi
{"access_token":"plain-secret"}`, 500)

	for _, leaked := range []string{"ya29.a0AQvPyExampleSecret", "abc.def.ghi", "plain-secret"} {
		if strings.Contains(preview, leaked) {
			t.Fatalf("expected preview to redact %q, got %q", leaked, preview)
		}
	}
	if !strings.Contains(preview, "[redacted]") {
		t.Fatalf("expected redaction marker in preview, got %q", preview)
	}
}

func TestServiceServerControl_CachesWrapper(t *testing.T) {
	server := &serviceServer{jobs: agent.NewJobRegistry(time.Minute, 32)}

	first := server.control()
	second := server.control()
	if first == nil {
		t.Fatal("expected control service")
	}
	if first != second {
		t.Fatal("expected control service wrapper to be cached")
	}
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
	payload := mustDecodeJSONBody(t, rec.Body)
	if payload["code"] != serviceCodeMissingToken {
		t.Fatalf("expected missing_token code, got %#v", payload)
	}
}

func TestServiceAuthMiddleware_RateLimitsFailedBearerAttempts(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{Secret: strings.Repeat("s", 32), SharedSecretRole: approval.RoleServiceClient}}
	server := &serviceServer{config: cfg}
	var handled int
	handler := serviceAuthMiddlewareWithBrokerAndLimiter(cfg, nil, nil, server, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled++
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	for i := 0; i < serviceAuthFailureThreshold+1; i++ {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/turns", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set("Authorization", "Bearer invalid-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d (%s)", i+1, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/turns", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated auth failures, got %d (%s)", rec.Code, rec.Body.String())
	}
	payload := mustDecodeJSONBody(t, rec.Body)
	if payload["code"] != serviceCodeAuthRateLimited {
		t.Fatalf("expected auth_rate_limited code, got %#v", payload)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header")
	}
	if handled != 0 {
		t.Fatalf("expected unauthorized requests not to reach handler, got %d calls", handled)
	}
}

func TestServiceAuthMiddleware_PairingUnauthorizedExplainsTrustedOrigin(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("p", 32)
	cfg.Service.AllowUnauthenticatedPairing = true
	cfg.Service.AllowRemoteUnauthenticatedPairing = true
	cfg.Service.TrustedPairingOrigins = []string{"http://trusted.example"}
	handler := serviceAuthMiddlewareWithBrokerAndLimiter(cfg, nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("pairing request should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/pairing/requests", strings.NewReader(`{"role":"operator"}`))
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	payload := mustDecodeJSONBody(t, rec.Body)
	if payload["code"] != "trusted_pairing_required" {
		t.Fatalf("expected trusted pairing guidance, got %#v", payload)
	}
	origins, _ := payload["trusted_origins"].([]any)
	if len(origins) != 1 || origins[0] != "http://trusted.example" {
		t.Fatalf("expected trusted origin list, got %#v", payload)
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

func TestValidateServiceAuthorizationBoundRejectsReplayAndBindingMismatch(t *testing.T) {
	secret := strings.Repeat("s", 32)
	now := time.Unix(1_700_000_000, 0)
	binding := serviceTokenBinding{Method: http.MethodPost, Path: "/internal/v1/turns"}
	token, err := issueServiceBearerTokenBound(secret, now.Add(-time.Minute), binding)
	if err != nil {
		t.Fatalf("issue bound token: %v", err)
	}
	guard := newServiceNonceReplayGuard(16)
	header := "Bearer " + token
	if err := validateServiceAuthorizationBound(secret, header, now, binding, guard); err != nil {
		t.Fatalf("expected first use to succeed, got %v", err)
	}
	if err := validateServiceAuthorizationBound(secret, header, now, binding, guard); err == nil || err.Error() != "bearer token replay detected" {
		t.Fatalf("expected replay rejection, got %v", err)
	}
	if err := validateServiceAuthorizationBound(secret, header, now, serviceTokenBinding{Method: http.MethodGet, Path: binding.Path}, newServiceNonceReplayGuard(16)); err == nil || err.Error() != "bearer token method binding mismatch" {
		t.Fatalf("expected method binding rejection, got %v", err)
	}
	if err := validateServiceAuthorizationBound(secret, header, now, serviceTokenBinding{Method: binding.Method, Path: "/internal/v1/jobs"}, newServiceNonceReplayGuard(16)); err == nil || err.Error() != "bearer token path binding mismatch" {
		t.Fatalf("expected path binding rejection, got %v", err)
	}
}

func TestValidateServiceAuthorizationBoundAcceptsLegacyTokenWithoutOptionalClaims(t *testing.T) {
	secret := strings.Repeat("s", 32)
	now := time.Unix(1_700_000_000, 0)
	token := signedServiceToken(t, secret, encodeServiceClaims(t, serviceTokenClaims{IssuedAt: now.Add(-time.Minute).Unix(), Nonce: "legacy-nonce"}))
	if err := validateServiceAuthorizationBound(secret, "Bearer "+token, now, serviceTokenBinding{Method: http.MethodPost, Path: "/different"}, nil); err != nil {
		t.Fatalf("expected legacy token without bindings to remain valid, got %v", err)
	}
}

func TestServiceNonceReplayGuardAllowsNonceAfterExpiry(t *testing.T) {
	guard := newServiceNonceReplayGuard(16)
	now := time.Unix(1_700_000_000, 0)
	if !guard.Accept("nonce", now.Add(time.Minute), now) {
		t.Fatalf("expected first nonce use to be accepted")
	}
	if guard.Accept("nonce", now.Add(time.Minute), now.Add(time.Second)) {
		t.Fatalf("expected duplicate nonce to be rejected before expiry")
	}
	if !guard.Accept("nonce", now.Add(2*time.Minute), now.Add(time.Minute+time.Second)) {
		t.Fatalf("expected nonce to be accepted after expiry cleanup")
	}
}

func TestServiceJobSummaryPersistsAcrossServerRestart(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service-jobs.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	jobs := agent.NewJobRegistry(0, 0)
	server := &serviceServer{runtime: &agent.Runtime{DB: database}, jobs: jobs}
	jobs.RegisterWithID("job-persist", "turn")
	jobs.Complete("job-persist", "completed", map[string]any{"final_text": "done"})
	server.persistServiceJobSummary(context.Background(), "job-persist")

	restarted := &serviceServer{runtime: &agent.Runtime{DB: database}, jobs: agent.NewJobRegistry(0, 0)}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/jobs/job-persist", nil)
	rec := httptest.NewRecorder()
	if !restarted.writePersistedServiceJobSnapshot(rec, req, "job-persist") {
		t.Fatalf("expected persisted job snapshot")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	payload := mustDecodeJSONBody(t, rec.Body)
	if payload["job_id"] != "job-persist" || payload["status"] != "completed" || payload["final_text"] != "done" {
		t.Fatalf("unexpected persisted job payload: %#v", payload)
	}
}

func TestServiceErrorsRedactInternalsAndAddRecoveryGuidance(t *testing.T) {
	if got := servicePublicJobError(fmt.Errorf("provider API key sk-secret failed at /tmp/private")); got != "job failed" {
		t.Fatalf("expected generic job error, got %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/test", nil)
	payload := serviceErrorPayload(req, "provider endpoint is unavailable")
	if payload["recovery"] == "" {
		t.Fatalf("expected provider recovery guidance, got %#v", payload)
	}
	payload = serviceErrorPayload(req, "runner is not authenticated")
	if payload["recovery"] == "" {
		t.Fatalf("expected runner auth recovery guidance, got %#v", payload)
	}
	payload = serviceErrorPayload(req, "sqlite database is read only")
	if payload["recovery"] == "" {
		t.Fatalf("expected storage recovery guidance, got %#v", payload)
	}
}

func TestServiceBoundary_RateLimitIsPerActorAndPathAndEchoesRequestID(t *testing.T) {
	server := &serviceServer{config: config.Config{Service: config.ServiceConfig{MutationRateLimitPerMinute: 1}}}
	var handled int
	handler := serviceBoundaryMiddleware(server, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled++
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	newReq := func(actor, path, requestID string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if requestID != "" {
			req.Header.Set("X-Request-Id", requestID)
		}
		ctx := context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: actor, Role: approval.RoleOperator})
		return req.WithContext(ctx)
	}

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, newReq("actor-a", "/internal/v1/scope/links", "req-rate-1"))
	if first.Code != http.StatusOK || first.Header().Get("X-Request-Id") != "req-rate-1" {
		t.Fatalf("expected first mutation through with echoed request ID, got code=%d id=%q body=%s", first.Code, first.Header().Get("X-Request-Id"), first.Body.String())
	}

	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, newReq("actor-a", "/internal/v1/scope/links", "req-rate-2"))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second same actor/path mutation to be rate limited, got %d (%s)", limited.Code, limited.Body.String())
	}
	if !strings.Contains(limited.Body.String(), "req-rate-2") {
		t.Fatalf("expected rate limit response to include request ID, got %s", limited.Body.String())
	}

	differentPath := httptest.NewRecorder()
	handler.ServeHTTP(differentPath, newReq("actor-a", "/internal/v1/approvals/expire", ""))
	if differentPath.Code != http.StatusOK {
		t.Fatalf("expected same actor on different path to pass, got %d", differentPath.Code)
	}

	differentActor := httptest.NewRecorder()
	handler.ServeHTTP(differentActor, newReq("actor-b", "/internal/v1/scope/links", ""))
	if differentActor.Code != http.StatusOK {
		t.Fatalf("expected different actor on same path to pass, got %d", differentActor.Code)
	}
	if handled != 3 {
		t.Fatalf("expected downstream handler to run exactly 3 times, got %d", handled)
	}
}

func TestTerminalInteractiveMutationsBypassGenericMutationLimiter(t *testing.T) {
	server := &serviceServer{config: config.Config{Service: config.ServiceConfig{MutationRateLimitPerMinute: 1}}}

	inputReq1 := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-1/input", nil)
	inputReq1.RemoteAddr = "127.0.0.1:1234"
	if !server.isTerminalInteractiveMutation(inputReq1) {
		t.Fatal("expected terminal input to be classified as interactive mutation")
	}
	if server.isMutationRequest(inputReq1) {
		t.Fatal("expected terminal input to bypass generic mutation limiting")
	}

	inputReq2 := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-1/input", nil)
	inputReq2.RemoteAddr = "127.0.0.1:1234"
	if server.isMutationRequest(inputReq2) {
		t.Fatal("expected repeated terminal input to keep bypassing generic mutation limiting")
	}

	resizeReq := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-1/resize", nil)
	resizeReq.RemoteAddr = "127.0.0.1:1234"
	if !server.isTerminalInteractiveMutation(resizeReq) {
		t.Fatal("expected terminal resize to be classified as interactive mutation")
	}
	if server.isMutationRequest(resizeReq) {
		t.Fatal("expected terminal resize to bypass generic mutation limiting")
	}

	closeReq1 := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-1/close", nil)
	closeReq1.RemoteAddr = "127.0.0.1:1234"
	if server.isTerminalInteractiveMutation(closeReq1) {
		t.Fatal("did not expect terminal close to bypass generic mutation limiting")
	}
	if !server.isMutationRequest(closeReq1) {
		t.Fatal("expected terminal close to remain a generic mutation")
	}
	if !server.allowMutationRequest(closeReq1) {
		t.Fatal("expected first terminal close request to be allowed")
	}

	closeReq2 := httptest.NewRequest(http.MethodPost, "/internal/v1/terminal/sessions/term-1/close", nil)
	closeReq2.RemoteAddr = "127.0.0.1:1234"
	if !server.isMutationRequest(closeReq2) {
		t.Fatal("expected second terminal close to remain subject to limiting")
	}
	if server.allowMutationRequest(closeReq2) {
		t.Fatal("expected second terminal close request to be rate limited")
	}
}

func TestServiceBrowserMiddleware_AllowsLoopbackPreflight(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{Listen: "127.0.0.1:9100"}}
	called := false
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodOptions, "/internal/v1/pairing/requests", nil)
	req.RemoteAddr = "127.0.0.1:43210"
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("expected preflight to short-circuit before downstream handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight response, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("expected allow-origin header, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Fatalf("expected allow-headers to reflect requested headers, got %q", got)
	}
}

func TestServiceBrowserMiddleware_AddsLoopbackCORSHeadersToRequests(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{Listen: "127.0.0.1:9100"}}
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/pairing/requests", strings.NewReader(`{"role":"operator"}`))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected downstream handler response, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3000" {
		t.Fatalf("expected allow-origin header, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-Request-Id") {
		t.Fatalf("expected expose headers to include X-Request-Id, got %q", got)
	}
}

func TestServiceBrowserMiddleware_AllowsTrustedElectronAppOriginFromLoopback(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{
		Listen: "0.0.0.0:9100",
	}}
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodOptions, "/internal/v1/app/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Origin", "app://or3")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization,x-or3-auth-method,content-type")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight response, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "app://or3" {
		t.Fatalf("expected trusted Electron app allow-origin header, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "x-or3-auth-method") {
		t.Fatalf("expected desktop auth method header to be allowed, got %q", got)
	}
}

func TestServiceBrowserMiddleware_AllowsOpaqueElectronOriginFromLoopback(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{
		Listen: "127.0.0.1:9100",
	}}
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodOptions, "/internal/v1/chat-sessions", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization,x-or3-auth-method")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight response, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "null" {
		t.Fatalf("expected opaque local allow-origin header, got %q", got)
	}
}

func TestServiceBrowserMiddleware_DoesNotAllowOpaquePairingPreflight(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{
		Listen:                       "127.0.0.1:9100",
		AllowUnauthenticatedPairing: true,
	}}
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodOptions, "/internal/v1/pairing/requests", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") == "null" {
		t.Fatal("opaque origins should not receive CORS access to unauthenticated pairing")
	}
}

func TestServiceBrowserMiddleware_AddsTrustedTailscaleCORSHeadersToRemoteAppRequests(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{
		Listen:                "100.64.0.42:9100",
		TrustedBrowserOrigins: []string{"http://100.64.0.42:3060", "http://100.64.0.42:3070"},
		TrustedBrowserCIDRs:   []string{"100.64.0.0/10"},
	}}
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodOptions, "/internal/v1/turns", nil)
	req.RemoteAddr = "100.64.0.42:54321"
	req.Header.Set("Origin", "http://100.64.0.42:3060")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight response, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://100.64.0.42:3060" {
		t.Fatalf("expected trusted Tailscale allow-origin header, got %q", got)
	}
}

func TestServiceBrowserMiddleware_TrustedPairingOriginsRemainBrowserCORSFallback(t *testing.T) {
	cfg := config.Config{Service: config.ServiceConfig{
		Listen:                "100.64.0.42:9100",
		TrustedPairingOrigins: []string{"http://100.64.0.42:3060"},
		TrustedPairingCIDRs:   []string{"100.64.0.0/10"},
	}}
	handler := serviceBrowserMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodOptions, "/internal/v1/turns", nil)
	req.RemoteAddr = "100.64.0.42:54321"
	req.Header.Set("Origin", "http://100.64.0.42:3060")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected legacy trusted pairing origin to allow browser preflight, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://100.64.0.42:3060" {
		t.Fatalf("expected trusted pairing allow-origin fallback, got %q", got)
	}
}

func TestWriteServiceErrorRedactsInternalError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/turns", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceRequestContextKey{}, serviceRequestContext{RequestID: "req-redact"}))
	rec := httptest.NewRecorder()

	writeServiceError(rec, req, http.StatusBadGateway, "turn failed", fmt.Errorf("database password leaked in stack trace"))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "turn failed") || !strings.Contains(body, "req-redact") {
		t.Fatalf("expected public error and request ID, got %s", body)
	}
	if strings.Contains(body, "database password") || strings.Contains(body, "stack trace") {
		t.Fatalf("expected internal error to be redacted, got %s", body)
	}
}

func TestServicePublicPairingExchangeError_OnlyExposesPairingState(t *testing.T) {
	for _, message := range []string{
		"pairing request not found",
		"pairing request expired",
		"pairing request is not approved",
	} {
		got, ok := servicePublicPairingExchangeError(fmt.Errorf("%s", message))
		if !ok || got != message {
			t.Fatalf("expected public pairing error %q, got %q ok=%v", message, got, ok)
		}
	}

	if got, ok := servicePublicPairingExchangeError(fmt.Errorf("database password leaked")); ok || got != "" {
		t.Fatalf("expected internal pairing exchange error to be redacted, got %q ok=%v", got, ok)
	}
}

func TestServicePublicApprovalActionError_ExposesExpiredStatus(t *testing.T) {
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
	if err := broker.DB.UpdateApprovalRequestResolution(
		context.Background(),
		decision.RequestID,
		approval.StatusExpired,
		time.Now().UnixMilli(),
		"system",
		approval.StatusExpired,
		"expired during test",
	); err != nil {
		t.Fatalf("UpdateApprovalRequestResolution: %v", err)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	message, status, ok := server.servicePublicApprovalActionError(context.Background(), decision.RequestID, "approve", fmt.Errorf("approval request is not pending"))
	if !ok {
		t.Fatal("expected public approval action error")
	}
	if status != approval.StatusExpired {
		t.Fatalf("expected expired status, got %q", status)
	}
	if !strings.Contains(message, "expired before it could be approved") {
		t.Fatalf("expected expired approval message, got %q", message)
	}
	if strings.Contains(message, "database") {
		t.Fatalf("expected safe public approval message, got %q", message)
	}
}

func TestDecodeServiceSubagentRequest_RejectsInvalidTimeoutTypes(t *testing.T) {
	registry := tools.NewRegistry()
	for _, body := range []string{
		`{"parent_session_key":"svc:test","task":"run","tool_policy":{"mode":"deny_all"},"timeout_seconds":1.5}`,
		`{"parent_session_key":"svc:test","task":"run","tool_policy":{"mode":"deny_all"},"timeout_seconds":"slow"}`,
	} {
		if _, err := decodeServiceSubagentRequest(strings.NewReader(body), registry); err == nil {
			t.Fatalf("expected invalid timeout to fail for body %s", body)
		}
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

func TestServiceConfigureFields_ReturnsSectionFields(t *testing.T) {
	server := &serviceServer{config: config.Default()}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/configure/fields?section=provider", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	fields, ok := body["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("expected fields array, got %#v", body["fields"])
	}
}

func TestServiceConfigureFields_UsesFrontendFriendlyShape(t *testing.T) {
	server := &serviceServer{config: config.Default()}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/configure/fields?section=runtime", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Fields []map[string]any `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Fields) == 0 {
		t.Fatal("expected fields to be returned")
	}
	first := body.Fields[0]
	if _, ok := first["label"].(string); !ok {
		t.Fatalf("expected lowercase label field, got %#v", first)
	}
	if _, exists := first["Label"]; exists {
		t.Fatalf("expected no Go-style Label key, got %#v", first)
	}

	var toggle map[string]any
	for _, field := range body.Fields {
		if field["key"] == "runtime_consolidation_enabled" {
			toggle = field
			break
		}
	}
	if toggle == nil {
		t.Fatalf("expected runtime_consolidation_enabled field, got %#v", body.Fields)
	}
	if toggle["kind"] != "toggle" {
		t.Fatalf("expected toggle kind, got %#v", toggle["kind"])
	}
	if _, ok := toggle["value"].(bool); !ok {
		t.Fatalf("expected boolean toggle value, got %#v", toggle["value"])
	}
}

func TestServiceConfigureTelegramChatsDiscoversRecentChats(t *testing.T) {
	telegramAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottelegram-token/getUpdates" {
			t.Fatalf("unexpected Telegram path %s", r.URL.Path)
		}
		if r.URL.Query().Get("timeout") != "0" {
			t.Fatalf("expected timeout=0, got %q", r.URL.Query().Get("timeout"))
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"result": []map[string]any{
				{"update_id": 1, "message": map[string]any{"message_id": 10, "date": 100, "text": "older", "chat": map[string]any{"id": 123, "type": "private", "first_name": "Brendon"}}},
				{"update_id": 2, "message": map[string]any{"message_id": 11, "date": 200, "text": "hello bot", "chat": map[string]any{"id": 123, "type": "private", "first_name": "Brendon"}}},
				{"update_id": 3, "message": map[string]any{"message_id": 12, "date": 150, "text": "group ping", "chat": map[string]any{"id": -100456, "type": "group", "title": "Ops Group"}}},
			},
		})
	}))
	defer telegramAPI.Close()

	cfg := config.Default()
	cfg.Channels.Telegram.Token = "telegram-token"
	cfg.Channels.Telegram.APIBase = telegramAPI.URL
	server := &serviceServer{config: cfg}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/configure/channels/telegram/chats", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []serviceTelegramChatCandidate `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("expected 2 deduped chats, got %#v", body.Items)
	}
	if body.Items[0].ID != "123" || body.Items[0].DisplayName != "Brendon" || body.Items[0].LastMessageText != "hello bot" {
		t.Fatalf("unexpected first chat: %#v", body.Items[0])
	}
	if body.Items[1].ID != "-100456" || body.Items[1].DisplayName != "Ops Group" {
		t.Fatalf("unexpected second chat: %#v", body.Items[1])
	}
}

func TestServiceConfigureTelegramChatsRequiresSavedToken(t *testing.T) {
	server := &serviceServer{config: config.Default()}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/configure/channels/telegram/chats", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestServiceConfigureTelegramChatsShowsConfiguredChatsWithoutToken(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.DefaultChatID = "123"
	cfg.Channels.Telegram.AllowedChatIDs = []string{"123", "-100456"}
	server := &serviceServer{config: cfg}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/configure/channels/telegram/chats", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []serviceTelegramChatCandidate `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("expected configured chats, got %#v", body.Items)
	}
	if body.Items[0].ID != "123" || body.Items[1].ID != "-100456" {
		t.Fatalf("unexpected configured chats: %#v", body.Items)
	}
}

func TestServiceConfigureTelegramChatsAcceptsUnsavedToken(t *testing.T) {
	telegramAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottelegram-token/getUpdates" {
			t.Fatalf("unexpected Telegram path %s", r.URL.Path)
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"result": []map[string]any{
				{"update_id": 1, "message": map[string]any{"message_id": 10, "date": 100, "text": "setup", "chat": map[string]any{"id": 123, "type": "private", "first_name": "Brendon"}}},
			},
		})
	}))
	defer telegramAPI.Close()

	cfg := config.Default()
	cfg.Channels.Telegram.APIBase = telegramAPI.URL
	server := &serviceServer{config: cfg}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/configure/channels/telegram/chats", strings.NewReader(`{"token":"telegram-token","limit":5}`))
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []serviceTelegramChatCandidate `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "123" {
		t.Fatalf("unexpected chats: %#v", body.Items)
	}
}

func TestServiceConfigureDiscordTargetsShowsConfiguredDestinationWithoutToken(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Discord.DefaultChannelID = "C123"
	server := &serviceServer{config: cfg}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/configure/channels/discord/targets", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []serviceDiscordTargetCandidate `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ChannelID != "C123" {
		t.Fatalf("unexpected configured Discord targets: %#v", body.Items)
	}
}

func TestServiceConfigureApply_PersistsConfigChanges(t *testing.T) {
	clearConfigEnvForTest(t)
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "or3-intern.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	runtime := &agent.Runtime{}
	server := &serviceServer{config: cfg, configPath: cfgPath, runtime: runtime}
	_ = server.control()
	reqBody := strings.NewReader(`{
		"changes":[
			{"section":"provider","field":"provider_model","op":"set","value":"gpt-4.1"},
			{"section":"tools","field":"tools_enable_exec","op":"set","value":true},
			{"section":"tools","field":"tools_exec_timeout","op":"set","value":45},
			{"section":"service","field":"service_max_capability","op":"set","value":"guarded"},
			{"section":"service","field":"service_enabled","op":"toggle"},
			{"section":"channels","channel":"slack","field":"access","op":"choose","value":"allowlist"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/configure/apply", reqBody)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if loaded.Provider.Model != "gpt-4.1" {
		t.Fatalf("expected provider model update, got %q", loaded.Provider.Model)
	}
	if live := runtime.CurrentModelConfig(); live.Model != "gpt-4.1" || live.SubagentModel != "gpt-4.1" {
		t.Fatalf("expected live runtime model update, got %#v", live)
	}
	if server.controlSvc == nil || server.controlSvc.Config.Provider.Model != "gpt-4.1" {
		t.Fatalf("expected cached controlplane config update, got %#v", server.controlSvc)
	}
	if !loaded.Service.Enabled {
		t.Fatal("expected service_enabled toggle to set true")
	}
	if !loaded.Tools.EnableExec {
		t.Fatal("expected tools_enable_exec boolean set to enable exec")
	}
	if loaded.Tools.ExecTimeoutSeconds != 45 {
		t.Fatalf("expected numeric exec timeout update, got %d", loaded.Tools.ExecTimeoutSeconds)
	}
	if loaded.Service.MaxCapability != "guarded" {
		t.Fatalf("expected service max capability guarded, got %q", loaded.Service.MaxCapability)
	}
	if loaded.Channels.Slack.InboundPolicy != config.InboundPolicyAllowlist {
		t.Fatalf("expected slack allowlist policy, got %q", loaded.Channels.Slack.InboundPolicy)
	}
}

func TestServiceConfigureApply_DefaultsDiscordInboundPolicyWhenEnabled(t *testing.T) {
	clearConfigEnvForTest(t)
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "or3-intern.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	server := &serviceServer{config: cfg, configPath: cfgPath}
	reqBody := strings.NewReader(`{
		"changes":[
			{"section":"channels","channel":"discord","field":"token","op":"set","value":"discord-token"},
			{"section":"channels","channel":"discord","field":"enabled","op":"set","value":true}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/configure/apply", reqBody)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !loaded.Channels.Discord.Enabled {
		t.Fatal("expected discord enabled to persist")
	}
	if loaded.Channels.Discord.InboundPolicy != config.InboundPolicyDeny {
		t.Fatalf("expected discord inbound policy to default to deny, got %q", loaded.Channels.Discord.InboundPolicy)
	}
}

func TestServiceConfigureApply_SetsToggleFieldsFromBooleanValues(t *testing.T) {
	clearConfigEnvForTest(t)
	cfg := config.Default()
	cfg.Channels.Telegram.Token = "telegram-token"
	cfg.Channels.Telegram.InboundPolicy = config.InboundPolicyPairing
	cfgPath := filepath.Join(t.TempDir(), "or3-intern.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	server := &serviceServer{config: cfg, configPath: cfgPath}
	reqBody := strings.NewReader(`{
		"changes":[
			{"section":"provider","field":"provider_vision","op":"set","value":true},
			{"section":"channels","channel":"telegram","field":"enabled","op":"set","value":true},
			{"section":"channels","channel":"slack","field":"require_mention","op":"set","value":true}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/configure/apply", reqBody)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()

	server.handleConfigure(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !loaded.Provider.EnableVision {
		t.Fatal("expected provider vision toggle to persist true")
	}
	if !loaded.Channels.Telegram.Enabled {
		t.Fatal("expected telegram enabled toggle to persist true")
	}
	if !loaded.Channels.Slack.RequireMention {
		t.Fatal("expected slack require_mention toggle to persist true")
	}
}

func TestServiceMCPServers_CRUDAndAuth(t *testing.T) {
	clearConfigEnvForTest(t)
	cfg := config.Default()
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	secret := strings.Repeat("m", 32)
	server := &serviceServer{config: cfg, configPath: cfgPath}
	httpServer := newServiceTestHTTPServer(t, secret, server)
	defer httpServer.Close()

	unauthResp, err := httpServer.Client().Get(httpServer.URL + "/internal/v1/mcp/servers")
	if err != nil {
		t.Fatalf("unauth GET: %v", err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated MCP list to be rejected, got %d", unauthResp.StatusCode)
	}

	create := mustServiceRequest(t, httpServer, secret, http.MethodPost, "/internal/v1/mcp/servers", `{
		"name":"local",
		"config":{"enabled":true,"transport":"stdio","command":"mcp-local","args":["--demo"],"env":{"API_KEY":"secret-env"},"headers":{"Authorization":"Bearer secret-header"},"connectTimeoutSeconds":5,"toolTimeoutSeconds":7}
	}`)
	createResp, err := httpServer.Client().Do(create)
	if err != nil {
		t.Fatalf("create MCP server: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("expected create 200, got %d (%s)", createResp.StatusCode, mustReadBody(t, createResp.Body))
	}
	created := mustDecodeJSONBody(t, createResp.Body)
	if created["restartRequired"] != true {
		t.Fatalf("expected restartRequired response, got %#v", created)
	}

	list := mustServiceRequest(t, httpServer, secret, http.MethodGet, "/internal/v1/mcp/servers", "")
	listResp, err := httpServer.Client().Do(list)
	if err != nil {
		t.Fatalf("list MCP servers: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list 200, got %d (%s)", listResp.StatusCode, mustReadBody(t, listResp.Body))
	}
	listed := mustDecodeJSONBody(t, listResp.Body)
	items, _ := listed["servers"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one server, got %#v", listed)
	}
	listedServer, _ := items[0].(map[string]any)
	listedConfig, _ := listedServer["config"].(map[string]any)
	listedEnv, _ := listedConfig["env"].(map[string]any)
	listedHeaders, _ := listedConfig["headers"].(map[string]any)
	if listedEnv["API_KEY"] != serviceMCPRedactedValue || listedHeaders["Authorization"] != serviceMCPRedactedValue {
		t.Fatalf("expected MCP secrets to be redacted, got env=%#v headers=%#v", listedEnv, listedHeaders)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config after create: %v", err)
	}
	if loaded.Tools.MCPServers["local"].Command != "mcp-local" {
		t.Fatalf("expected persisted MCP config, got %#v", loaded.Tools.MCPServers)
	}
	if loaded.Tools.MCPServers["local"].Env["API_KEY"] != "secret-env" || loaded.Tools.MCPServers["local"].Headers["Authorization"] != "Bearer secret-header" {
		t.Fatalf("expected persisted MCP secrets to remain unredacted, got %#v", loaded.Tools.MCPServers["local"])
	}

	update := mustServiceRequest(t, httpServer, secret, http.MethodPost, "/internal/v1/mcp/servers", `{
		"name":"local",
		"config":{"enabled":true,"transport":"stdio","command":"mcp-local","args":["--demo","--updated"],"env":{"API_KEY":"configured"},"headers":{"Authorization":"configured"},"connectTimeoutSeconds":5,"toolTimeoutSeconds":7}
	}`)
	updateResp, err := httpServer.Client().Do(update)
	if err != nil {
		t.Fatalf("update MCP server: %v", err)
	}
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected update 200, got %d (%s)", updateResp.StatusCode, mustReadBody(t, updateResp.Body))
	}
	loaded, err = config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config after update: %v", err)
	}
	if loaded.Tools.MCPServers["local"].Env["API_KEY"] != "secret-env" || loaded.Tools.MCPServers["local"].Headers["Authorization"] != "Bearer secret-header" {
		t.Fatalf("expected redacted MCP secrets to preserve previous values, got %#v", loaded.Tools.MCPServers["local"])
	}

	del := mustServiceRequest(t, httpServer, secret, http.MethodDelete, "/internal/v1/mcp/servers/local", "")
	delResp, err := httpServer.Client().Do(del)
	if err != nil {
		t.Fatalf("delete MCP server: %v", err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("expected delete 200, got %d (%s)", delResp.StatusCode, mustReadBody(t, delResp.Body))
	}
	loaded, err = config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config after delete: %v", err)
	}
	if _, ok := loaded.Tools.MCPServers["local"]; ok {
		t.Fatalf("expected MCP server to be deleted, got %#v", loaded.Tools.MCPServers)
	}
}

func TestServiceMCPServers_ValidationAndTestEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Tools.MCPServers = map[string]config.MCPServerConfig{
		"local": {Enabled: true, Transport: "stdio", Command: "mcp-local"},
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	secret := strings.Repeat("n", 32)
	server := &serviceServer{
		config:     cfg,
		configPath: cfgPath,
		mcpTestManagerFactory: func(map[string]config.MCPServerConfig) serviceMCPTestManager {
			return &fakeServiceMCPTestManager{status: map[string]mcp.ServerStatus{
				"local": {Connected: true, ToolCount: 1, Tools: []string{"mcp_local_echo"}},
			}}
		},
	}
	httpServer := newServiceTestHTTPServer(t, secret, server)
	defer httpServer.Close()

	invalid := mustServiceRequest(t, httpServer, secret, http.MethodPost, "/internal/v1/mcp/servers", `{
		"name":"bad",
		"config":{"enabled":true,"transport":"streamableHttp","url":"http://example.com/mcp"}
	}`)
	invalidResp, err := httpServer.Client().Do(invalid)
	if err != nil {
		t.Fatalf("invalid create: %v", err)
	}
	defer invalidResp.Body.Close()
	if invalidResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid create 400, got %d (%s)", invalidResp.StatusCode, mustReadBody(t, invalidResp.Body))
	}

	testReq := mustServiceRequest(t, httpServer, secret, http.MethodPost, "/internal/v1/mcp/servers/local/test", `{}`)
	testResp, err := httpServer.Client().Do(testReq)
	if err != nil {
		t.Fatalf("test MCP server: %v", err)
	}
	defer testResp.Body.Close()
	if testResp.StatusCode != http.StatusOK {
		t.Fatalf("expected test 200, got %d (%s)", testResp.StatusCode, mustReadBody(t, testResp.Body))
	}
	result := mustDecodeJSONBody(t, testResp.Body)
	if result["ok"] != true || result["toolCount"].(float64) != 1 {
		t.Fatalf("unexpected test result: %#v", result)
	}
}

func TestServiceCron_CRUDAndRun(t *testing.T) {
	cfg := config.Default()
	cfg.Cron.Enabled = true
	cfg.Cron.StorePath = filepath.Join(t.TempDir(), "cron.json")
	runs := 0
	cronSvc := cron.New(cfg.Cron.StorePath, func(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
		runs++
		return cron.RunResult{}, nil
	})
	if err := cronSvc.Start(); err != nil {
		t.Fatalf("cron start: %v", err)
	}
	t.Cleanup(cronSvc.Stop)
	server := &serviceServer{config: cfg, cronSvc: cronSvc}

	createReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs", strings.NewReader(`{
		"name":"Morning summary",
		"schedule":{"kind":"every","every_ms":3600000},
		"payload":{"kind":"agent_turn","message":"Summarize overnight changes","session_key":"cron:test"}
	}`))
	createReq = createReq.WithContext(context.WithValue(createReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	createRec := httptest.NewRecorder()
	server.handleCron(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := mustDecodeJSONBody(t, createRec.Body)
	jobBody, ok := created["job"].(map[string]any)
	if !ok {
		t.Fatalf("expected job body, got %#v", created)
	}
	jobID, _ := jobBody["id"].(string)
	if jobID == "" {
		t.Fatalf("expected generated job id, got %#v", jobBody)
	}
	if jobBody["enabled"] != true {
		t.Fatalf("expected default enabled job, got %#v", jobBody["enabled"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/internal/v1/cron/jobs", nil)
	listReq = listReq.WithContext(context.WithValue(listReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	listRec := httptest.NewRecorder()
	server.handleCron(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listRec.Code, listRec.Body.String())
	}
	listed := mustDecodeJSONBody(t, listRec.Body)
	items, ok := listed["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one cron job, got %#v", listed)
	}

	pauseReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs/"+jobID+"/pause", nil)
	pauseReq = pauseReq.WithContext(context.WithValue(pauseReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	pauseRec := httptest.NewRecorder()
	server.handleCron(pauseRec, pauseReq)
	if pauseRec.Code != http.StatusOK {
		t.Fatalf("expected pause 200, got %d (%s)", pauseRec.Code, pauseRec.Body.String())
	}

	runReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs/"+jobID+"/run", nil)
	runReq = runReq.WithContext(context.WithValue(runReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	runRec := httptest.NewRecorder()
	server.handleCron(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("expected run 200, got %d (%s)", runRec.Code, runRec.Body.String())
	}
	if runs != 1 {
		t.Fatalf("expected manual run to call runner once, got %d", runs)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/internal/v1/cron/jobs/"+jobID, nil)
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	deleteRec := httptest.NewRecorder()
	server.handleCron(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d (%s)", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestServiceCron_AgentCLIRunPayloadCRUDAndRun(t *testing.T) {
	cfg := config.Default()
	cfg.Cron.Enabled = true
	cfg.Cron.StorePath = filepath.Join(t.TempDir(), "cron.json")
	var captured cron.CronJob
	cronSvc := cron.New(cfg.Cron.StorePath, func(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
		captured = job
		return cron.RunResult{EnqueuedJobID: "job-agentcli-test", EnqueuedRunID: "acr_test"}, nil
	})
	if err := cronSvc.Start(); err != nil {
		t.Fatalf("cron start: %v", err)
	}
	t.Cleanup(cronSvc.Stop)
	server := &serviceServer{config: cfg, cronSvc: cronSvc}

	createReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs", strings.NewReader(`{
		"name":"Weekly external review",
		"schedule":{"kind":"every","every_ms":3600000},
		"payload":{
			"kind":"agent_cli_run",
			"session_key":"cron:agents",
			"agent_run":{"runner_id":"codex","task":"review the repo"}
		}
	}`))
	createReq = createReq.WithContext(context.WithValue(createReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	createRec := httptest.NewRecorder()
	server.handleCron(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	created := mustDecodeJSONBody(t, createRec.Body)
	jobBody := created["job"].(map[string]any)
	jobID := jobBody["id"].(string)
	payload := jobBody["payload"].(map[string]any)
	agentRun := payload["agent_run"].(map[string]any)
	if agentRun["mode"] != cron.DefaultAgentCLICronMode {
		t.Fatalf("expected review default, got %#v", agentRun)
	}
	if agentRun["isolation"] != cron.DefaultAgentCLICronIsolation {
		t.Fatalf("expected host_readonly default, got %#v", agentRun)
	}

	runReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs/"+jobID+"/run", nil)
	runReq = runReq.WithContext(context.WithValue(runReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	runRec := httptest.NewRecorder()
	server.handleCron(runRec, runReq)
	if runRec.Code != http.StatusOK {
		t.Fatalf("expected run 200, got %d (%s)", runRec.Code, runRec.Body.String())
	}
	if captured.Payload.AgentRun == nil || captured.Payload.AgentRun.RunnerID != "codex" {
		t.Fatalf("expected captured agent run payload, got %#v", captured.Payload)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/internal/v1/cron/jobs/"+jobID, nil)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	getRec := httptest.NewRecorder()
	server.handleCron(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d (%s)", getRec.Code, getRec.Body.String())
	}
	got := mustDecodeJSONBody(t, getRec.Body)
	gotJob := got["job"].(map[string]any)
	state := gotJob["state"].(map[string]any)
	if state["last_enqueued_job_id"] != "job-agentcli-test" {
		t.Fatalf("expected enqueued job id in state, got %#v", state)
	}
	if state["last_enqueued_run_id"] != "acr_test" {
		t.Fatalf("expected enqueued run id in state, got %#v", state)
	}
}

func TestServiceCron_RunDisabledWithoutForceReportsSkipped(t *testing.T) {
	cfg := config.Default()
	cfg.Cron.Enabled = true
	cfg.Cron.StorePath = filepath.Join(t.TempDir(), "cron.json")
	runs := 0
	cronSvc := cron.New(cfg.Cron.StorePath, func(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
		runs++
		return cron.RunResult{}, nil
	})
	if err := cronSvc.Start(); err != nil {
		t.Fatalf("cron start: %v", err)
	}
	t.Cleanup(cronSvc.Stop)
	if err := cronSvc.Add(cron.CronJob{
		ID:       "disabled",
		Enabled:  false,
		Schedule: cron.CronSchedule{Kind: cron.KindEvery, EveryMS: 3600000},
		Payload:  cron.CronPayload{Kind: cron.PayloadAgentTurn, Message: "skip me"},
	}); err != nil {
		t.Fatalf("cron add: %v", err)
	}
	server := &serviceServer{config: cfg, cronSvc: cronSvc}

	runReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs/disabled/run", strings.NewReader(`{"force":false}`))
	runReq = runReq.WithContext(context.WithValue(runReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	runRec := httptest.NewRecorder()
	server.handleCron(runRec, runReq)

	if runRec.Code != http.StatusOK {
		t.Fatalf("expected run 200, got %d (%s)", runRec.Code, runRec.Body.String())
	}
	body := mustDecodeJSONBody(t, runRec.Body)
	if body["status"] != "skipped" || body["ran"] != false {
		t.Fatalf("expected skipped run response, got %#v", body)
	}
	if runs != 0 {
		t.Fatalf("expected disabled job not to run, got %d runs", runs)
	}
}

func TestServiceCron_AgentCLIRunPayloadValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Cron.Enabled = true
	cfg.Cron.StorePath = filepath.Join(t.TempDir(), "cron.json")
	cronSvc := cron.New(cfg.Cron.StorePath, func(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
		return cron.RunResult{}, nil
	})
	if err := cronSvc.Start(); err != nil {
		t.Fatalf("cron start: %v", err)
	}
	t.Cleanup(cronSvc.Stop)
	server := &serviceServer{config: cfg, cronSvc: cronSvc}

	createReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs", strings.NewReader(`{
		"name":"Bad external review",
		"schedule":{"kind":"every","every_ms":3600000},
		"payload":{"kind":"agent_cli_run","agent_run":{"task":"review the repo"}}
	}`))
	createReq = createReq.WithContext(context.WithValue(createReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	createRec := httptest.NewRecorder()
	server.handleCron(createRec, createReq)
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing runner_id, got %d (%s)", createRec.Code, createRec.Body.String())
	}

	unknownReq := httptest.NewRequest(http.MethodPost, "/internal/v1/cron/jobs", strings.NewReader(`{
		"name":"Bad external review",
		"schedule":{"kind":"every","every_ms":3600000},
		"payload":{"kind":"agent_turn","message":"hello","surprise":true}
	}`))
	unknownReq = unknownReq.WithContext(context.WithValue(unknownReq.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	unknownRec := httptest.NewRecorder()
	server.handleCron(unknownRec, unknownReq)
	if unknownRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown payload field, got %d (%s)", unknownRec.Code, unknownRec.Body.String())
	}
}

func TestServiceSkills_ListAndUpdateSettings(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "or3-intern.json")
	globalRoot := filepath.Join(t.TempDir(), "agents-skills")
	skillDir := filepath.Join(globalRoot, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: demo
description: Demo shared skill
metadata:
  openclaw:
    primaryEnv: DEMO_API_KEY
    requires:
      config: [demo.enabled]
---
# Demo
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	cfg.Skills.Load.GlobalDir = globalRoot
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	reg := tools.NewRegistry()
	reg.Register(&tools.ReadSkill{})
	rt := &agent.Runtime{Builder: &agent.Builder{}, Tools: reg}
	server := &serviceServer{config: cfg, configPath: cfgPath, runtime: rt}

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/skills", nil)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec := httptest.NewRecorder()
	server.handleSkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var listBody struct {
		Items []serviceSkillItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listBody.Items) != 1 || listBody.Items[0].Name != "demo" || listBody.Items[0].Source != string(skills.SourceGlobal) {
		t.Fatalf("expected demo global skill, got %#v", listBody.Items)
	}

	reqBody := strings.NewReader(`{"enabled":false,"apiKey":"secret-value","config":{"demo.enabled":true}}`)
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/skills/demo/settings", reqBody)
	req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
	rec = httptest.NewRecorder()
	server.handleSkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	entry := loaded.Skills.Entries["demo"]
	if entry.Enabled == nil || *entry.Enabled || entry.APIKey != "secret-value" || entry.Config["demo.enabled"] != true {
		t.Fatalf("expected persisted skill settings, got %#v", entry)
	}
	if skill, ok := rt.Builder.Skills.Get("demo"); !ok || !skill.Disabled {
		t.Fatalf("expected runtime skill inventory to refresh disabled demo, got %#v ok=%t", skill, ok)
	}
	if readSkill, ok := reg.Get("read_skill").(*tools.ReadSkill); !ok || readSkill.Inventory == nil {
		t.Fatalf("expected read_skill inventory pointer to refresh")
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
		"toolPolicy":{"mode":"allow_list","allowedTools":["read_file"]},
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
		t.Fatalf("expected allow_list alias to resolve tools, got %#v", req)
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

func TestDecodeServiceTurnRequest_AllowsDocumentedToolPolicyModes(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(serviceTestTool{name: "read_file"})
	registry.Register(serviceTestTool{name: "exec"})

	allowAll, err := decodeServiceTurnRequest(strings.NewReader(`{
		"session_key":"svc:key",
		"message":"hi",
		"tool_policy":{"mode":"allow_all"}
	}`), registry)
	if err != nil {
		t.Fatalf("decode allow_all: %v", err)
	}
	if allowAll.RestrictTools || len(allowAll.AllowedTools) != 0 {
		t.Fatalf("expected allow_all to avoid tool restriction, got %#v", allowAll)
	}

	denyList, err := decodeServiceTurnRequest(strings.NewReader(`{
		"session_key":"svc:key",
		"message":"hi",
		"tool_policy":{"mode":"deny_list","blocked_tools":["exec"]}
	}`), registry)
	if err != nil {
		t.Fatalf("decode deny_list: %v", err)
	}
	if !denyList.RestrictTools || len(denyList.AllowedTools) != 1 || denyList.AllowedTools[0] != "read_file" {
		t.Fatalf("expected deny_list to expose only read_file, got %#v", denyList)
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
	if strings.TrimSpace(resp.Header.Get("X-Or3-Job-Id")) == "" {
		t.Fatalf("expected X-Or3-Job-Id header on SSE response")
	}
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
	if strings.TrimSpace(resp.Header.Get("X-Or3-Job-Id")) == "" {
		t.Fatalf("expected X-Or3-Job-Id header on JSON response")
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

func TestServiceTurns_ReplayToolCallContinuesConversation(t *testing.T) {
	callCount := 0
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode provider request: %v", err)
		}
		if len(req.Messages) < 2 {
			t.Fatalf("expected replay continuation prompt, got %#v", req.Messages)
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role != "user" || !strings.Contains(fmt.Sprint(last.Content), "Approval was granted") {
			t.Fatalf("expected continuation prompt after replay, got %#v", last)
		}
		foundReplayToolResult := false
		for _, msg := range req.Messages {
			if msg.Role != "tool" {
				continue
			}
			if strings.Contains(fmt.Sprint(msg.Content), "replayed:ok") {
				foundReplayToolResult = true
				break
			}
		}
		if !foundReplayToolResult {
			t.Fatalf("expected replayed tool result in prompt history, got %#v", req.Messages)
		}
		resp := providers.ChatCompletionResponse{
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
				}{Role: "assistant", Content: "continued after replay"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode response: %v", err)
		}
	})
	defer cleanup()
	rt.Tools.Register(serviceReplayTool{})
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:replay", "user", "resume approved tool", nil); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:replay", "assistant", "", map[string]any{
		"tool_calls": []providers.ToolCall{{
			ID:   "tc-replay",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "replay_probe",
				Arguments: `{"value":"ok"}`,
			},
		}},
	}); err != nil {
		t.Fatalf("Append assistant tool call: %v", err)
	}
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:replay", "tool", tools.EncodeToolFailure("replay_probe", map[string]any{"value": "ok"}, "", &tools.ApprovalRequiredError{ToolName: "replay_probe", RequestID: 99}), map[string]any{
		"tool_call_id": "tc-replay",
	}); err != nil {
		t.Fatalf("Append approval-required tool result: %v", err)
	}
	server := &serviceServer{
		config: config.Config{
			Tools:   config.ToolsConfig{EnableExec: true},
			Service: config.ServiceConfig{Secret: strings.Repeat("r", 32), MaxCapability: string(tools.CapabilityGuarded)},
		},
		runtime: rt,
		jobs:    agent.NewJobRegistry(time.Minute, 32),
	}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("r", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:replay","message":"resume approved tool","replay_tool_call":{"name":"replay_probe","arguments_json":"{\"value\":\"ok\"}"}}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("r", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["final_text"] != "continued after replay" {
		t.Fatalf("expected continued final text, got %#v", payload)
	}
	if callCount != 1 {
		t.Fatalf("expected one provider continuation call, got %d", callCount)
	}
}

func TestServiceTurns_ReplayToolCallFailureStillContinuesConversation(t *testing.T) {
	callCount := 0
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode provider request: %v", err)
		}
		if len(req.Messages) < 2 {
			t.Fatalf("expected replay continuation prompt, got %#v", req.Messages)
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role != "user" || !strings.Contains(fmt.Sprint(last.Content), "Approval was granted") {
			t.Fatalf("expected continuation prompt after replay, got %#v", last)
		}
		foundReplayFailure := false
		for _, msg := range req.Messages {
			if msg.Role != "tool" {
				continue
			}
			text := fmt.Sprint(msg.Content)
			if strings.Contains(text, "exec failed: exit status 3") && strings.Contains(text, "replay failed for bad") {
				foundReplayFailure = true
				break
			}
		}
		if !foundReplayFailure {
			t.Fatalf("expected replayed tool failure in prompt history, got %#v", req.Messages)
		}
		resp := providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{
				{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{Role: "assistant", Content: "continued after replay failure"},
				},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode response: %v", err)
		}
	})
	defer cleanup()
	rt.Tools.Register(serviceReplayFailingTool{})
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:replay-fail", "user", "resume approved tool", nil); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:replay-fail", "assistant", "", map[string]any{
		"tool_calls": []providers.ToolCall{{
			ID:   "tc-replay-fail",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "replay_probe",
				Arguments: `{"value":"bad"}`,
			},
		}},
	}); err != nil {
		t.Fatalf("Append assistant tool call: %v", err)
	}
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:replay-fail", "tool", tools.EncodeToolFailure("replay_probe", map[string]any{"value": "bad"}, "", &tools.ApprovalRequiredError{ToolName: "replay_probe", RequestID: 99}), map[string]any{
		"tool_call_id": "tc-replay-fail",
	}); err != nil {
		t.Fatalf("Append approval-required tool result: %v", err)
	}
	server := &serviceServer{
		config: config.Config{
			Tools:   config.ToolsConfig{EnableExec: true},
			Service: config.ServiceConfig{Secret: strings.Repeat("r", 32), MaxCapability: string(tools.CapabilityGuarded)},
		},
		runtime: rt,
		jobs:    agent.NewJobRegistry(time.Minute, 32),
	}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("r", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:replay-fail","message":"resume approved tool","replay_tool_call":{"name":"replay_probe","arguments_json":"{\"value\":\"bad\"}"}}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("r", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["final_text"] != "continued after replay failure" {
		t.Fatalf("expected continued final text, got %#v", payload)
	}
	if callCount != 1 {
		t.Fatalf("expected one provider continuation call, got %d", callCount)
	}
}

func TestServiceTurns_ReplayToolCallRequiresPriorAssistantCall(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("provider should not be called for rejected replay")
	})
	defer cleanup()
	executions := 0
	rt.Tools.Register(countingReplayTool{count: &executions})
	server := &serviceServer{
		config: config.Config{
			Service: config.ServiceConfig{Secret: strings.Repeat("r", 32), MaxCapability: string(tools.CapabilityGuarded)},
		},
		runtime: rt,
		jobs:    agent.NewJobRegistry(time.Minute, 32),
	}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("r", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:missing-prior-tool-call","message":"resume approved tool","replay_tool_call":{"name":"replay_probe","arguments_json":"{\"value\":\"ok\"}"}}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("r", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	if executions != 0 {
		t.Fatalf("replay tool executed without a prior assistant tool call")
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if !strings.Contains(fmt.Sprint(payload["error"]), "job failed") {
		t.Fatalf("expected public job failure, got %#v", payload)
	}
}

func TestServiceTurns_ReplayToolCallRejectsChangedArguments(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("provider should not be called for rejected replay")
	})
	defer cleanup()
	executions := 0
	rt.Tools.Register(countingReplayTool{count: &executions})
	if _, err := rt.DB.AppendMessage(context.Background(), "svc:changed-replay", "assistant", "", map[string]any{
		"tool_calls": []providers.ToolCall{{
			ID:   "tc-replay",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "replay_probe",
				Arguments: `{"value":"approved"}`,
			},
		}},
	}); err != nil {
		t.Fatalf("Append assistant tool call: %v", err)
	}
	server := &serviceServer{
		config: config.Config{
			Service: config.ServiceConfig{Secret: strings.Repeat("r", 32), MaxCapability: string(tools.CapabilityGuarded)},
		},
		runtime: rt,
		jobs:    agent.NewJobRegistry(time.Minute, 32),
	}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("r", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:changed-replay","message":"resume approved tool","replay_tool_call":{"name":"replay_probe","arguments_json":"{\"value\":\"changed\"}"}}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("r", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	if executions != 0 {
		t.Fatalf("replay tool executed with changed arguments")
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

func TestServiceTurns_JSONApprovalRequiredStatus(t *testing.T) {
	callCount := 0
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount > 1 {
			t.Fatalf("provider should not be called again after approval is required")
		}
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"tc-exec","type":"function","function":{"name":"exec","arguments":"{\"program\":\"pwd\"}"}}]}}]}`)
	})
	defer cleanup()
	rt.Tools.Register(serviceApprovalTool{})
	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("a", 32), server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, strings.Repeat("a", 32), http.MethodPost, "/internal/v1/turns", `{"session_key":"svc:approval","message":"run pwd"}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["status"] != "approval_required" {
		t.Fatalf("expected approval_required status, got %#v", payload)
	}
	if payload["request_id"] != float64(77) && payload["request_id"] != int64(77) {
		t.Fatalf("expected request id 77, got %#v", payload)
	}
	if callCount != 1 {
		t.Fatalf("expected one provider call before pause, got %d", callCount)
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
			method:     http.MethodDelete,
			path:       "/internal/v1/subagents",
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "subagents list requires database",
			method:     http.MethodGet,
			path:       "/internal/v1/subagents",
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "subagent history is not available",
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
		"tool_policy":{"mode":"allow_list","allowed_tools":["read_file"]},
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

func TestServiceSubagents_ListReturnsSanitizedHistory(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	jobs := agent.NewJobRegistry(time.Minute, 32)
	manager := &agent.SubagentManager{DB: database, Jobs: jobs, MaxQueued: 4}
	rt := &agent.Runtime{DB: database}
	server := &serviceServer{runtime: rt, subagentManager: manager, jobs: jobs}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("l", 32), server)
	defer httpServer.Close()

	ctx := context.Background()
	now := db.NowMS()
	seed := []db.SubagentJob{
		{ID: "list-a", ParentSessionKey: "alice", ChildSessionKey: "alice:s:a", Task: "research X", Status: db.SubagentStatusQueued, RequestedAt: now - 3000, MetadataJSON: `{"secret":"do-not-leak"}`},
		{ID: "list-b", ParentSessionKey: "bob", ChildSessionKey: "bob:s:b", Task: "draft email", Status: db.SubagentStatusSucceeded, RequestedAt: now - 5000, FinishedAt: now - 1000, ResultPreview: "draft ready", MetadataJSON: `{"secret":"do-not-leak"}`},
	}
	for _, job := range seed {
		if err := database.EnqueueSubagentJob(ctx, job); err != nil {
			t.Fatalf("EnqueueSubagentJob %s: %v", job.ID, err)
		}
		if job.Status == db.SubagentStatusSucceeded {
			if err := database.MarkSubagentSucceeded(ctx, job.ID, job.ResultPreview, ""); err != nil {
				t.Fatalf("MarkSubagentSucceeded: %v", err)
			}
		}
	}

	req := mustServiceRequest(t, httpServer, strings.Repeat("l", 32), http.MethodGet, "/internal/v1/subagents?limit=10", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do list: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	body := mustReadBody(t, resp.Body)
	if strings.Contains(body, "do-not-leak") {
		t.Fatalf("response leaked metadata_json: %s", body)
	}
	if !strings.Contains(body, "list-a") || !strings.Contains(body, "list-b") {
		t.Fatalf("expected both jobs in response: %s", body)
	}
	if !strings.Contains(body, `"kind":"subagent"`) {
		t.Fatalf("expected sanitized kind in response: %s", body)
	}
	if !strings.Contains(body, `"task":"research X"`) || !strings.Contains(body, `"task":"draft email"`) {
		t.Fatalf("expected task labels in response: %s", body)
	}

	t.Run("status filter", func(t *testing.T) {
		req := mustServiceRequest(t, httpServer, strings.Repeat("l", 32), http.MethodGet, "/internal/v1/subagents?status=terminal", "")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do list terminal: %v", err)
		}
		defer resp.Body.Close()
		body := mustReadBody(t, resp.Body)
		if strings.Contains(body, "list-a") {
			t.Fatalf("queued job should not appear in terminal filter: %s", body)
		}
		if !strings.Contains(body, "list-b") {
			t.Fatalf("succeeded job should appear in terminal filter: %s", body)
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		req := mustServiceRequest(t, httpServer, strings.Repeat("l", 32), http.MethodGet, "/internal/v1/subagents?status=bogus", "")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do list bogus: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for bogus status, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid limit rejected", func(t *testing.T) {
		req := mustServiceRequest(t, httpServer, strings.Repeat("l", 32), http.MethodGet, "/internal/v1/subagents?limit=zero", "")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do list bad limit: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-numeric limit, got %d", resp.StatusCode)
		}
	})

	t.Run("requires authorization", func(t *testing.T) {
		anonReq, err := http.NewRequest(http.MethodGet, httpServer.URL+"/internal/v1/subagents", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := http.DefaultClient.Do(anonReq)
		if err != nil {
			t.Fatalf("Do anon: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})
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
	rt.Tools.Register(serviceTestTool{name: "read_file"})
	rt.Tools.Register(serviceTestTool{name: "write_file"})
	cleanup := func() {
		providerServer.CloseClientConnections()
		providerServer.Close()
		database.Close()
	}
	return rt, cleanup
}

func TestServiceTurns_MaxToolLoopsReturnsFallbackResponse(t *testing.T) {
	callCount := 0
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		resp := providers.ChatCompletionResponse{
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
						ID:   fmt.Sprintf("loop-%d", callCount),
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "read_file",
							Arguments: `{}`,
						},
					}},
				},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	})
	defer cleanup()

	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("t", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:loop-fallback","message":"loop forever","meta":{"request_id":"req-loop-fallback"}}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("t", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if got := fmt.Sprint(payload["status"]); got != "completed" {
		t.Fatalf("expected completed status, got %#v", payload)
	}
	finalText := fmt.Sprint(payload["final_text"])
	if !strings.Contains(finalText, "tool calls kept failing or looping") {
		t.Fatalf("expected fallback final_text, got %#v", payload)
	}
}

func TestServiceTurns_EmptyAssistantResponseReturnsFallbackText(t *testing.T) {
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := providers.ChatCompletionResponse{
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
				}{Role: "assistant", Content: ""},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	})
	defer cleanup()

	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("e", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:empty-final","message":"say something"}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("e", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if got := fmt.Sprint(payload["final_text"]); !strings.Contains(got, "completed without a final response") {
		t.Fatalf("expected fallback final_text, got %#v", payload)
	}
	if payload["empty_final_text_recovered"] != true {
		t.Fatalf("expected empty_final_text_recovered marker, got %#v", payload)
	}
}

func TestServiceTurns_MaxToolLoopsRequestsApprovalWhenBrokerAvailable(t *testing.T) {
	callCount := 0
	rt, cleanup := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		resp := providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{
				{Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{
					Role: "assistant",
					ToolCalls: []providers.ToolCall{{
						ID:   fmt.Sprintf("loop-%d", callCount),
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "read_file", Arguments: `{}`},
					}},
				}},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	})
	defer cleanup()

	approvalCfg := config.Default().Security.Approvals
	approvalCfg.HostID = "svc-loop-host"
	rt.MaxToolLoopsExceededAction = config.QuotaExceededActionAsk
	rt.ApprovalBroker = &approval.Broker{
		DB:      rt.DB,
		Config:  approvalCfg,
		HostID:  approvalCfg.HostID,
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
		Now: func() time.Time {
			return time.Unix(1_700_000_000, 0).UTC()
		},
	}

	server := &serviceServer{runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("t", 32), server)
	defer httpServer.Close()

	body := `{"session_key":"svc:loop-approval","message":"loop forever"}`
	req := mustServiceRequest(t, httpServer, strings.Repeat("t", 32), http.MethodPost, "/internal/v1/turns", body)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["status"] != "approval_required" {
		t.Fatalf("expected approval_required status, got %#v", payload)
	}
	if payload["request_id"] == nil {
		t.Fatalf("expected request id in payload, got %#v", payload)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 provider calls before approval pause, got %d", callCount)
	}
}

func mustUseServiceTestWorkingDir(t *testing.T, dir string) {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(current); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func writeServiceTestRestartScript(t *testing.T, dir string, body string) string {
	t.Helper()
	scriptDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll script dir: %v", err)
	}
	scriptPath := filepath.Join(scriptDir, "restart-service.sh")
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile restart script: %v", err)
	}
	return scriptPath
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
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
	server.config.Service.Secret = secret
	if server.config.Service.SharedSecretRole == "" {
		server.config.Service.SharedSecretRole = approval.RoleOperator
	}
	if server.broker != nil && strings.TrimSpace(server.config.Service.Listen) == "" {
		server.config.Service.Listen = "127.0.0.1:0"
	}
	if server.broker != nil && !server.config.Service.AllowUnauthenticatedPairing {
		server.config.Service.AllowUnauthenticatedPairing = true
	}
	if server.subagentManager != nil && server.subagentManager.BackgroundTools == nil {
		server.subagentManager.BackgroundTools = func() *tools.Registry {
			registry := tools.NewRegistry()
			registry.Register(serviceTestTool{name: "read_file"})
			registry.Register(serviceTestTool{name: "write_file"})
			return registry
		}
	}
	return httptest.NewServer(serviceBrowserMiddleware(server.config, serviceAuthMiddlewareWithBrokerAndLimiter(server.config, server.broker, server.app().Auth(), server, serviceBoundaryMiddleware(server, newServiceMux(server)))))
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
	server.config.Service.AllowUnauthenticatedPairing = true
	server.config.Service.Listen = "127.0.0.1:0"
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
	listed := mustDecodeJSONBody(t, deviceResp.Body)
	items, ok := listed["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one listed device, got %#v", listed["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected listed device object, got %#v", items[0])
	}
	if item["device_id"] == "" || item["DeviceID"] != nil {
		t.Fatalf("expected snake_case device payload, got %#v", item)
	}
}

func TestServicePairingWorkflow_RejectsNonOperatorDeviceOnOperatorRoutes(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Pairing.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	server.config.Service.AllowUnauthenticatedPairing = true
	server.config.Service.Listen = "127.0.0.1:0"
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

func TestServiceApprovals_Approve_ReturnsPlanIDsWhenPresent(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.SkillExecution.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	plan, err := broker.DB.CreateSkillRunPlan(context.Background(), db.SkillRunPlanRecord{
		ID:              "srp_service_plan",
		SkillID:         "runner",
		SkillDir:        "/tmp/runner",
		Entrypoint:      "hello",
		TimeoutSeconds:  30,
		CommandJSON:     `["bash","/tmp/runner/tool.sh"]`,
		ScriptHash:      "script-hash",
		EnvBindingHash:  "env-hash",
		PlanHash:        "plan-hash",
		ExecutionHostID: "test-host",
		Status:          "prepared",
		CreatedAt:       1,
	})
	if err != nil {
		t.Fatalf("CreateSkillRunPlan: %v", err)
	}
	decision, err := broker.EvaluateSkillExec(context.Background(), approval.SkillEvaluation{
		SkillID:        "runner",
		PlanID:         plan.ID,
		PlanHash:       plan.PlanHash,
		ScriptHash:     plan.ScriptHash,
		EnvBindingHash: plan.EnvBindingHash,
		TimeoutSeconds: plan.TimeoutSeconds,
	})
	if err != nil {
		t.Fatalf("EvaluateSkillExec: %v", err)
	}
	if err := broker.DB.UpdateSkillRunPlanApproval(context.Background(), plan.ID, decision.RequestID, decision.SubjectHash, "pending_approval", 2); err != nil {
		t.Fatalf("UpdateSkillRunPlanApproval: %v", err)
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
	if payload["plan_id"] != plan.ID {
		t.Fatalf("expected plan_id %q, got %#v", plan.ID, payload)
	}
	planIDs, _ := payload["plan_ids"].([]any)
	if len(planIDs) != 1 || planIDs[0] != plan.ID {
		t.Fatalf("expected plan_ids to include %q, got %#v", plan.ID, payload)
	}
}

func TestServiceApprovals_Approve_StartsResumeJobWhenBlockedTurnExists(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
		SessionID:      "sess-approval-resume",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if !decision.RequiresApproval || decision.RequestID == 0 {
		t.Fatalf("expected pending approval request, got %#v", decision)
	}

	providerCalls := 0
	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","content":"continued after service approval"}}]}`)
	}))
	defer chatServer.Close()

	provider := providers.New(chatServer.URL, "test-key", 10*time.Second)
	provider.HTTP = chatServer.Client()
	registry := tools.NewRegistry()
	registry.Register(serviceReplayTool{})
	rt := &agent.Runtime{
		DB:       broker.DB,
		Provider: provider,
		Model:    "gpt-4",
		Tools:    registry,
		Builder:  &agent.Builder{DB: broker.DB, HistoryMax: 2},
	}
	server := &serviceServer{broker: broker, runtime: rt, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("a", 32), server)
	defer httpServer.Close()

	toolCall := providers.ToolCall{
		ID:   "tc-approve-resume",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "replay_probe", Arguments: `{"value":"hello"}`},
	}
	if _, err := broker.DB.AppendMessage(context.Background(), "sess-approval-resume", "user", "run it", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := broker.DB.AppendMessage(context.Background(), "sess-approval-resume", "assistant", "", map[string]any{"tool_calls": []providers.ToolCall{toolCall}}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	blocked := tools.EncodeToolFailure("replay_probe", map[string]any{"value": "hello"}, "", &tools.ApprovalRequiredError{ToolName: "replay_probe", RequestID: decision.RequestID})
	if _, err := broker.DB.AppendMessage(context.Background(), "sess-approval-resume", "tool", blocked, map[string]any{"tool_call_id": "tc-approve-resume"}); err != nil {
		t.Fatalf("AppendMessage tool: %v", err)
	}

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
	resumeJobID, _ := payload["resume_job_id"].(string)
	if strings.TrimSpace(resumeJobID) == "" {
		t.Fatalf("expected resume_job_id in response, got %#v", payload)
	}
	if payload["session_key"] != "sess-approval-resume" {
		t.Fatalf("expected session_key for resume job routing, got %#v", payload)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snapshot, ok := server.jobs.Wait(ctx, resumeJobID)
	if !ok {
		t.Fatalf("expected resume job %q to complete", resumeJobID)
	}
	if snapshot.Status != "completed" {
		t.Fatalf("expected completed resume job, got %#v", snapshot)
	}
	if providerCalls != 1 {
		t.Fatalf("expected one continuation provider call, got %d", providerCalls)
	}
	foundAssistant := false
	for _, event := range snapshot.Events {
		if event.Type == "assistant" && event.Data["content"] == "continued after service approval" {
			foundAssistant = true
			break
		}
	}
	if !foundAssistant {
		t.Fatalf("expected assistant continuation event, got %#v", snapshot.Events)
	}
}

func TestServiceApprovals_Approve_PlanLookupFailureWarnsButStillSucceeds(t *testing.T) {
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
	prevLookup := approvalSkillRunPlanLookup
	approvalSkillRunPlanLookup = func(context.Context, *db.DB, int64, int) ([]db.SkillRunPlanRecord, error) {
		return nil, fmt.Errorf("lookup down")
	}
	defer func() { approvalSkillRunPlanLookup = prevLookup }()

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("w", 32), server)
	defer httpServer.Close()

	req := mustJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/internal/v1/approvals/%d/approve", httpServer.URL, decision.RequestID), `{"note":"approved"}`)
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do approve: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected approval 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if strings.TrimSpace(fmt.Sprint(payload["token"])) == "" {
		t.Fatalf("expected token in response, got %#v", payload)
	}
	warnings, _ := payload["warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", payload)
	}
	warning, _ := warnings[0].(map[string]any)
	if warning["code"] != "plan_lookup_failed" {
		t.Fatalf("expected plan lookup warning, got %#v", payload)
	}
}

func TestServiceApprovals_Approve_ExpiredRequestReturnsHelpfulMessage(t *testing.T) {
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
	if err := broker.DB.UpdateApprovalRequestResolution(
		context.Background(),
		decision.RequestID,
		approval.StatusExpired,
		time.Now().UnixMilli(),
		"system",
		approval.StatusExpired,
		"expired during test",
	); err != nil {
		t.Fatalf("UpdateApprovalRequestResolution: %v", err)
	}

	_, deviceToken, err := broker.RotateDeviceToken(context.Background(), "operator-1", approval.RoleOperator, "Operator", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("e", 32), server)
	defer httpServer.Close()

	req := mustJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/internal/v1/approvals/%d/approve", httpServer.URL, decision.RequestID), `{"note":"approved"}`)
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do approve: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected approval 400, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["approval_status"] != approval.StatusExpired {
		t.Fatalf("expected expired approval_status, got %#v", payload)
	}
	errText := fmt.Sprint(payload["error"])
	if !strings.Contains(errText, "expired before it could be approved") {
		t.Fatalf("expected helpful expired error, got %#v", payload)
	}
	approvalID := fmt.Sprint(payload["approval_id"])
	if approvalID != fmt.Sprint(decision.RequestID) {
		t.Fatalf("expected approval_id %d, got %#v", decision.RequestID, payload)
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

func TestServiceAppBootstrapRoute_PairedOperatorWithoutSession(t *testing.T) {
	workDir := t.TempDir()
	mustUseServiceTestWorkingDir(t, workDir)
	writeServiceTestRestartScript(t, workDir, "#!/bin/sh\nexit 0\n")

	rt, cleanupRuntime := buildServiceTestRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	defer cleanupRuntime()
	broker, cleanupBroker := buildServiceTestBroker(t, nil)
	defer cleanupBroker()

	cfg := rolloutAuthTestConfig(config.AuthEnforcementSession)
	cfg.Service.Secret = strings.Repeat("b", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Hardening.EnableExecShell = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.GuardedTools = true

	server := &serviceServer{
		config:  cfg,
		runtime: rt,
		jobs:    agent.NewJobRegistry(time.Minute, 32),
		broker:  broker,
	}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	_, deviceToken, err := broker.RotateDeviceToken(context.Background(), "operator-1", approval.RoleOperator, "Operator", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}

	req := mustJSONRequest(t, http.MethodGet, httpServer.URL+"/internal/v1/app/bootstrap", "")
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do bootstrap: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected bootstrap 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	pairing, _ := payload["pairing"].(map[string]any)
	authState, _ := payload["auth"].(map[string]any)
	actions, _ := payload["actions"].([]any)
	if pairing["paired"] != true || pairing["device_id"] != "operator-1" {
		t.Fatalf("expected paired operator payload, got %#v", payload)
	}
	if authState["session_active"] != false || authState["session_required"] != true {
		t.Fatalf("expected paired-without-session auth state, got %#v", authState)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one action descriptor, got %#v", actions)
	}
	action, _ := actions[0].(map[string]any)
	if action["id"] != "restart-service" || action["available"] != true || action["session_required"] != true || action["step_up_required"] != true {
		t.Fatalf("unexpected restart action descriptor: %#v", action)
	}
}

func TestServiceAppBootstrapRoute_SharedSecretWarnsAboutLimitedExec(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("e", 32)
	cfg.Service.SharedSecretRole = approval.RoleServiceClient
	cfg.Tools.EnableExec = true
	server := &serviceServer{config: cfg, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	req := mustJSONRequest(t, http.MethodGet, httpServer.URL+"/internal/v1/app/bootstrap", "")
	req.Header.Set("Authorization", "Bearer "+mustIssueServiceTokenAt(t, cfg.Service.Secret, time.Now()))
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do bootstrap: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected bootstrap 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	authState, _ := payload["auth"].(map[string]any)
	status, _ := payload["status"].(map[string]any)
	warnings, _ := status["warnings"].([]any)
	if authState["kind"] != "shared-secret" || authState["role"] != string(approval.RoleServiceClient) || authState["exec_allowed"] != false {
		t.Fatalf("expected shared-secret auth details, got %#v", authState)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected bootstrap warnings, got %#v", payload)
	}
	foundWarning := false
	for _, raw := range warnings {
		warning, _ := raw.(map[string]any)
		if warning["code"] == "shared_secret_limited" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected limited shared-secret warning, got %#v", warnings)
	}
}

func TestServiceAppBootstrapRoute_DegradedWarnings(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("d", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	server := &serviceServer{config: cfg, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodGet, "/internal/v1/app/bootstrap", "")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do bootstrap: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected bootstrap 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	status, _ := payload["status"].(map[string]any)
	if status["summary"] != "offline" {
		t.Fatalf("expected offline summary, got %#v", status)
	}
	warnings, _ := status["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("expected warnings in degraded bootstrap payload, got %#v", payload)
	}
}

func TestServiceRestartActionRoute_StartsScript(t *testing.T) {
	workDir := t.TempDir()
	mustUseServiceTestWorkingDir(t, workDir)
	marker := filepath.Join(workDir, "restart-ran")
	writeServiceTestRestartScript(t, workDir, "#!/bin/sh\necho script-output\nprintf 'ok' > "+strconv.Quote(marker)+"\n")

	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("r", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Hardening.EnableExecShell = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.GuardedTools = true
	server := &serviceServer{config: cfg, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/actions/restart-service", `{}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do restart action: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected restart action 202, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["action_id"] != "restart-service" || payload["status"] != "accepted" {
		t.Fatalf("unexpected restart action payload: %#v", payload)
	}
	if operationID, ok := payload["operation_id"].(string); !ok || strings.TrimSpace(operationID) == "" {
		t.Fatalf("expected operation_id in restart payload, got %#v", payload)
	}
	logPath, ok := payload["log_path"].(string)
	if !ok {
		t.Fatalf("expected restart log path under .run, got %#v", payload["log_path"])
	}
	resolvedLogDir, logDirErr := filepath.EvalSymlinks(filepath.Dir(logPath))
	resolvedRunDir, runDirErr := filepath.EvalSymlinks(filepath.Join(workDir, ".run"))
	if logDirErr != nil || runDirErr != nil || resolvedLogDir != resolvedRunDir {
		t.Fatalf("expected restart log path under .run, got %q", logPath)
	}
	waitForFile(t, marker)
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read restart log: %v", err)
	}
	if !strings.Contains(string(logData), "restart requested") || !strings.Contains(string(logData), "script-output") {
		t.Fatalf("expected restart log to include operation and script output, got %q", string(logData))
	}
}

func TestServiceRestartActionRoute_PreservesUnsafeDevMode(t *testing.T) {
	workDir := t.TempDir()
	mustUseServiceTestWorkingDir(t, workDir)
	marker := filepath.Join(workDir, "restart-env")
	writeServiceTestRestartScript(t, workDir, "#!/bin/sh\nprintf '%s' \"$OR3_SERVICE_UNSAFE_DEV\" > "+strconv.Quote(marker)+"\n")

	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("r", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Hardening.EnableExecShell = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.GuardedTools = true
	server := &serviceServer{config: cfg, jobs: agent.NewJobRegistry(time.Minute, 32), unsafeDev: true}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/actions/restart-service", `{}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do restart action: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected restart action 202, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	waitForFile(t, marker)
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if strings.TrimSpace(string(data)) != "true" {
		t.Fatalf("expected restart action to preserve unsafe-dev env, got %q", string(data))
	}
}

func TestServiceRestartActionRoute_RequiresApprovalWhenExecModeAsks(t *testing.T) {
	workDir := t.TempDir()
	mustUseServiceTestWorkingDir(t, workDir)
	writeServiceTestRestartScript(t, workDir, "#!/bin/sh\nexit 0\n")

	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
	})
	defer cleanup()

	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("q", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Hardening.EnableExecShell = true
	cfg.Hardening.PrivilegedTools = true
	cfg.Hardening.GuardedTools = true
	cfg.Security.Approvals = broker.Config

	server := &serviceServer{config: cfg, jobs: agent.NewJobRegistry(time.Minute, 32), broker: broker}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/actions/restart-service", `{}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do restart action: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected restart action 409, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := mustDecodeJSONBody(t, resp.Body)
	if payload["status"] != "approval_required" {
		t.Fatalf("unexpected approval payload: %#v", payload)
	}
}

func TestServiceRestartActionRoute_DisabledWithoutShellAccess(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("u", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	server := &serviceServer{config: cfg, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	req := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/actions/restart-service", `{}`)
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do restart action: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected restart action 503, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
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

func TestServiceEmbeddingsRoutes(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	if _, err := database.InsertMemoryNote(context.Background(), "sess-1", "hello memory", nil, sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": []float32{0.1, 0.2}}}})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "", 5*time.Second)
	provider.HTTP = providerServer.Client()
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("e", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Provider.APIBase = providerServer.URL
	cfg.Provider.EmbedModel = "text-embedding-3-small"
	server := &serviceServer{config: cfg, runtime: &agent.Runtime{DB: database, Provider: provider}, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	statusReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodGet, "/internal/v1/embeddings/status", "")
	statusResp, err := httpServer.Client().Do(statusReq)
	if err != nil {
		t.Fatalf("Do embeddings status: %v", err)
	}
	defer statusResp.Body.Close()
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("expected embeddings status 200, got %d (%s)", statusResp.StatusCode, mustReadBody(t, statusResp.Body))
	}
	statusPayload := mustDecodeJSONBody(t, statusResp.Body)
	if statusPayload["status"] != "ok" {
		t.Fatalf("unexpected embeddings status payload: %#v", statusPayload)
	}

	rebuildReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/embeddings/rebuild", `{"target":"memory"}`)
	rebuildResp, err := httpServer.Client().Do(rebuildReq)
	if err != nil {
		t.Fatalf("Do embeddings rebuild: %v", err)
	}
	defer rebuildResp.Body.Close()
	if rebuildResp.StatusCode != http.StatusOK {
		t.Fatalf("expected embeddings rebuild 200, got %d (%s)", rebuildResp.StatusCode, mustReadBody(t, rebuildResp.Body))
	}
	rebuildPayload := mustDecodeJSONBody(t, rebuildResp.Body)
	if rebuildPayload["memoryNotesRebuilt"] != float64(1) {
		t.Fatalf("unexpected embeddings rebuild payload: %#v", rebuildPayload)
	}
}

func TestServiceEmbeddingsRoutes_MethodGuards(t *testing.T) {
	server := &serviceServer{jobs: agent.NewJobRegistry(time.Minute, 32)}
	tests := []struct {
		method  string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{method: http.MethodPost, path: "/internal/v1/embeddings/status", handler: server.handleEmbeddings},
		{method: http.MethodGet, path: "/internal/v1/embeddings/rebuild", handler: server.handleEmbeddings},
	}
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Kind: "shared-secret", Actor: "service:shared-secret", Role: "admin"}))
		tc.handler(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %s %s to return 405, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestServiceAuditRoutes(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	audit := &security.AuditLogger{DB: database, Key: []byte(strings.Repeat("a", 32)), Strict: true}
	if err := audit.Record(context.Background(), "tool.execute", "sess-1", "cli", map[string]any{"tool": "exec"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("a", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	cfg.Security.Audit.Enabled = true
	server := &serviceServer{config: cfg, runtime: &agent.Runtime{DB: database, Audit: audit}, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	statusReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodGet, "/internal/v1/audit", "")
	statusResp, err := httpServer.Client().Do(statusReq)
	if err != nil {
		t.Fatalf("Do audit status: %v", err)
	}
	defer statusResp.Body.Close()
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("expected audit status 200, got %d (%s)", statusResp.StatusCode, mustReadBody(t, statusResp.Body))
	}
	statusPayload := mustDecodeJSONBody(t, statusResp.Body)
	if statusPayload["eventCount"] != float64(1) {
		t.Fatalf("unexpected audit status payload: %#v", statusPayload)
	}

	verifyReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/audit/verify", "")
	verifyResp, err := httpServer.Client().Do(verifyReq)
	if err != nil {
		t.Fatalf("Do audit verify: %v", err)
	}
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected audit verify 200, got %d (%s)", verifyResp.StatusCode, mustReadBody(t, verifyResp.Body))
	}
	verifyPayload := mustDecodeJSONBody(t, verifyResp.Body)
	if verifyPayload["verified"] != true {
		t.Fatalf("unexpected audit verify payload: %#v", verifyPayload)
	}
}

func TestServiceScopeRoutes(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("s", 32)
	cfg.Service.SharedSecretRole = approval.RoleOperator
	server := &serviceServer{config: cfg, runtime: &agent.Runtime{DB: database}, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, cfg.Service.Secret, server)
	defer httpServer.Close()

	linkReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodPost, "/internal/v1/scope/links", `{"session_key":"sess-a","scope_key":"shared-1"}`)
	linkResp, err := httpServer.Client().Do(linkReq)
	if err != nil {
		t.Fatalf("Do scope link: %v", err)
	}
	defer linkResp.Body.Close()
	if linkResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected scope link 202, got %d (%s)", linkResp.StatusCode, mustReadBody(t, linkResp.Body))
	}

	resolveReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodGet, "/internal/v1/scope/resolve?session_key=sess-a", "")
	resolveResp, err := httpServer.Client().Do(resolveReq)
	if err != nil {
		t.Fatalf("Do scope resolve: %v", err)
	}
	defer resolveResp.Body.Close()
	if resolveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected scope resolve 200, got %d (%s)", resolveResp.StatusCode, mustReadBody(t, resolveResp.Body))
	}
	resolvePayload := mustDecodeJSONBody(t, resolveResp.Body)
	if resolvePayload["scope_key"] != "shared-1" {
		t.Fatalf("unexpected scope resolve payload: %#v", resolvePayload)
	}

	sessionsReq := mustServiceRequest(t, httpServer, cfg.Service.Secret, http.MethodGet, "/internal/v1/scope/sessions?scope_key=shared-1", "")
	sessionsResp, err := httpServer.Client().Do(sessionsReq)
	if err != nil {
		t.Fatalf("Do scope sessions: %v", err)
	}
	defer sessionsResp.Body.Close()
	if sessionsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected scope sessions 200, got %d (%s)", sessionsResp.StatusCode, mustReadBody(t, sessionsResp.Body))
	}
	sessionsPayload := mustDecodeJSONBody(t, sessionsResp.Body)
	sessions, ok := sessionsPayload["sessions"].([]any)
	if !ok || len(sessions) != 1 || sessions[0] != "sess-a" {
		t.Fatalf("unexpected scope sessions payload: %#v", sessionsPayload)
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

	err := runServiceCommand(ctx, cfg, rt, nil, nil, nil)
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

	err := runServiceCommand(ctx, cfg, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for hosted-no-exec with enableExecShell=true, got nil")
	}
	if !strings.Contains(err.Error(), "startup refused") {
		t.Fatalf("expected 'startup refused' in error, got: %v", err)
	}
}

func TestRunServiceCommandWithBrokerOptions_AllowsUnsafeDevOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Secret = strings.Repeat("x", 32)
	cfg.Service.Listen = "127.0.0.1:0"
	cfg.Service.MaxCapability = "guarded"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runServiceCommandWithBrokerOptions(ctx, cfg, nil, nil, nil, nil, nil, true)
	if err == nil {
		t.Fatal("expected runtime error after unsafe-dev bypass, got nil")
	}
	if strings.Contains(err.Error(), "service.maxCapability must remain safe") {
		t.Fatalf("unsafe-dev should bypass strict service posture, got: %v", err)
	}
	if !strings.Contains(err.Error(), "runtime not configured") {
		t.Fatalf("expected to reach runtime validation after bypass, got: %v", err)
	}
}

func TestRunServiceCommand_HostedNoExec_RefusesPrivilegedTools(t *testing.T) {
	cfg := hostedNoExecBaseConfig()
	cfg.Hardening.PrivilegedTools = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runServiceCommand(ctx, cfg, nil, nil, nil, nil)
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
	err := runServiceCommand(ctx, cfg, nil, nil, nil, nil)
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
		req, err := decodeServiceTurnRequest(strings.NewReader(`{"session_key":"svc:key","message":"hi","toolPolicy":{"mode":"allow_list","allowedTools":["read_file"]}}`), registry)
		if err != nil {
			t.Fatalf("decodeServiceTurnRequest: %v", err)
		}
		if !req.RestrictTools || len(req.AllowedTools) != 1 || req.AllowedTools[0] != "read_file" {
			t.Fatalf("expected toolPolicy.allowedTools alias to restrict to [read_file], got RestrictTools=%v AllowedTools=%v", req.RestrictTools, req.AllowedTools)
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
