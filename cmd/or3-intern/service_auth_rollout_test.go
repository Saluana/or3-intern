package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func TestServiceAuthMiddleware_PublicAuthCapabilitiesDoesNotRequireBearer(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementWarn)
	handler := serviceAuthMiddlewareWithBroker(cfg, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := serviceAuthKindFromContext(r.Context()); got != "public" {
			t.Fatalf("expected public auth kind, got %q", got)
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/auth/capabilities", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestServiceAuthMiddleware_InternalCapabilitiesRequiresBearer(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementWarn)
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	authSvc, err := auth.NewService(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	handler := serviceAuthMiddlewareWithBroker(cfg, nil, authSvc, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/capabilities", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestServiceAuthMiddleware_AppBootstrapRequiresBearer(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementWarn)
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	authSvc, err := auth.NewService(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	handler := serviceAuthMiddlewareWithBroker(cfg, nil, authSvc, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/app/bootstrap", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestServiceAuthMiddleware_RolloutModesForLegacyPairedTokens(t *testing.T) {
	ctx := context.Background()
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := rolloutAuthTestConfig(config.AuthEnforcementOff)
	broker := &approval.Broker{DB: database, Config: cfg.Security.Approvals}
	_, token, err := broker.RotateDeviceToken(ctx, "device-1", approval.RoleAdmin, "Legacy App", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}

	tests := []struct {
		name       string
		mode       config.AuthEnforcementMode
		path       string
		method     string
		wantStatus int
		wantCode   string
		wantWarn   string
	}{
		{name: "off allows sensitive paired token workflow", mode: config.AuthEnforcementOff, path: "/internal/v1/configure/security", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "warn allows sensitive paired token workflow with header", mode: config.AuthEnforcementWarn, path: "/internal/v1/configure/security", method: http.MethodPost, wantStatus: http.StatusOK, wantWarn: auth.CodeSessionRequired},
		{name: "enforce sensitive blocks paired token without passkey", mode: config.AuthEnforcementSensitive, path: "/internal/v1/configure/security", method: http.MethodPost, wantStatus: http.StatusUnauthorized, wantCode: auth.CodeSessionRequired},
		{name: "enforce sensitive keeps doctor status paired token workflow", mode: config.AuthEnforcementSensitive, path: "/internal/v1/doctor/status", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "enforce sensitive blocks doctor skill diagnostics paired token without passkey", mode: config.AuthEnforcementSensitive, path: "/internal/v1/doctor/skills/demo/diagnostics", method: http.MethodGet, wantStatus: http.StatusUnauthorized, wantCode: auth.CodeSessionRequired},
		{name: "enforce sensitive blocks doctor apply paired token without passkey", mode: config.AuthEnforcementSensitive, path: "/internal/v1/doctor/plans/plan-1/apply", method: http.MethodPost, wantStatus: http.StatusUnauthorized, wantCode: auth.CodeSessionRequired},
		{name: "enforce sensitive keeps low risk paired token workflow", mode: config.AuthEnforcementSensitive, path: "/internal/v1/turns", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "enforce session blocks low risk paired token without session", mode: config.AuthEnforcementSession, path: "/internal/v1/turns", method: http.MethodPost, wantStatus: http.StatusUnauthorized, wantCode: auth.CodeSessionRequired},
		{name: "enforce session allows paired token for passkey login begin", mode: config.AuthEnforcementSession, path: "/internal/v1/auth/passkeys/login/begin", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "enforce session allows paired token for passkey login finish", mode: config.AuthEnforcementSession, path: "/internal/v1/auth/passkeys/login/finish", method: http.MethodPost, wantStatus: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := rolloutAuthTestConfig(tc.mode)
			handler := serviceAuthMiddlewareWithBroker(cfg, broker, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
			}))
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d (%s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantWarn != "" && rec.Header().Get("X-Or3-Auth-Warning") != tc.wantWarn {
				t.Fatalf("expected warning %q, got %q", tc.wantWarn, rec.Header().Get("X-Or3-Auth-Warning"))
			}
			if tc.wantCode != "" {
				var payload map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if payload["code"] != tc.wantCode {
					t.Fatalf("expected code %q, got %#v", tc.wantCode, payload)
				}
			}
		})
	}
}

func TestServiceAuthMiddleware_AuthMethodSelection(t *testing.T) {
	ctx := context.Background()
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := rolloutAuthTestConfig(config.AuthEnforcementOff)
	cfg.Service.Secret = strings.Repeat("m", 32)
	cfg.Service.SharedSecretRole = approval.RoleAdmin
	broker := &approval.Broker{DB: database, Config: cfg.Security.Approvals}
	_, pairedToken, err := broker.RotateDeviceToken(ctx, "device-method", approval.RoleOperator, "Method Device", nil)
	if err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}
	authSvc, sessionToken := seedServiceAuthSession(t, ctx, cfg, database)
	sharedToken := mustIssueServiceTokenAt(t, cfg.Service.Secret, time.Now())

	tests := []struct {
		name       string
		token      string
		method     string
		wantStatus int
		wantKind   string
		wantCode   string
	}{
		{name: "default prefers shared secret", token: sharedToken, wantStatus: http.StatusOK, wantKind: "shared-secret"},
		{name: "default accepts paired device", token: pairedToken, wantStatus: http.StatusOK, wantKind: "paired-device"},
		{name: "explicit shared secret", token: sharedToken, method: "shared-secret", wantStatus: http.StatusOK, wantKind: "shared-secret"},
		{name: "explicit paired device", token: pairedToken, method: "paired-device", wantStatus: http.StatusOK, wantKind: "paired-device"},
		{name: "explicit session", token: sessionToken, method: "session", wantStatus: http.StatusOK, wantKind: "auth-session"},
		{name: "paired token rejected as shared secret", token: pairedToken, method: "shared-secret", wantStatus: http.StatusUnauthorized, wantCode: serviceCodeInvalidToken},
		{name: "unsupported method", token: sharedToken, method: "bogus", wantStatus: http.StatusUnauthorized, wantCode: serviceCodeValidationFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := serviceAuthMiddlewareWithBroker(cfg, broker, authSvc, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				identity := serviceAuthIdentityFromContext(r.Context())
				writeServiceJSON(w, http.StatusOK, map[string]any{"kind": identity.Kind})
			}))
			req := httptest.NewRequest(http.MethodGet, "/internal/v1/turns", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			if tc.method != "" {
				req.Header.Set("X-Or3-Auth-Method", tc.method)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d (%s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
			payload := mustDecodeJSONBody(t, rec.Body)
			if tc.wantKind != "" && payload["kind"] != tc.wantKind {
				t.Fatalf("expected kind %q, got %#v", tc.wantKind, payload)
			}
			if tc.wantCode != "" && payload["code"] != tc.wantCode {
				t.Fatalf("expected code %q, got %#v", tc.wantCode, payload)
			}
		})
	}
}

func TestServiceAuthMiddleware_SecureConnectionsRejectsSharedSecretWithoutSession(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementSensitive)
	cfg.Service.Secret = strings.Repeat("m", 32)
	cfg.Service.SharedSecretRole = approval.RoleAdmin
	handler := serviceAuthMiddlewareWithBroker(cfg, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/secure-connections/pairing/intents", strings.NewReader(`{"relay_origin":"https://relay.or3.chat"}`))
	req.Header.Set("Authorization", "Bearer "+mustIssueServiceTokenAt(t, cfg.Service.Secret, time.Now()))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	payload := mustDecodeJSONBody(t, rec.Body)
	if payload["code"] != auth.CodeSessionRequired {
		t.Fatalf("expected session-required code, got %#v", payload)
	}
}

func seedServiceAuthSession(t *testing.T, ctx context.Context, cfg config.Config, database *db.DB) (*auth.Service, string) {
	t.Helper()
	authSvc, err := auth.NewService(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	user, err := database.UpsertAuthUser(ctx, db.AuthUserRecord{ID: auth.DefaultUserID, DisplayName: auth.DefaultUserDisplayName})
	if err != nil {
		t.Fatalf("UpsertAuthUser: %v", err)
	}
	rawToken := "session-token-method-selection"
	hash := sha256.Sum256([]byte(rawToken))
	now := time.Now().UTC()
	if _, err := database.CreateAuthSession(ctx, db.AuthSessionRecord{
		ID:                "session-method-selection",
		UserID:            user.ID,
		DeviceID:          "device-method",
		CredentialID:      "credential-method",
		TokenHash:         hash[:],
		Role:              approval.RoleAdmin,
		CreatedAt:         now.UnixMilli(),
		LastSeenAt:        now.UnixMilli(),
		IdleExpiresAt:     now.Add(time.Hour).UnixMilli(),
		AbsoluteExpiresAt: now.Add(2 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("CreateAuthSession: %v", err)
	}
	return authSvc, rawToken
}

func TestServiceAuthMiddleware_EnforcementReturnsUpgradeGuidanceErrors(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementSession)
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	authSvc, err := auth.NewService(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	handler := serviceAuthMiddlewareWithBroker(cfg, nil, authSvc, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/turns", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["code"] != auth.CodeSessionRequired || !strings.Contains(strings.ToLower(payload["error"].(string)), "session") {
		t.Fatalf("expected machine-readable session guidance, got %#v", payload)
	}
}

func TestServiceRouteRequirementForRequest_SensitivityMatrix(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementWarn)
	tests := []struct {
		method      string
		path        string
		want        serviceRouteSensitivity
		sessionOnly bool
		stepUpOnly  bool
	}{
		{method: http.MethodGet, path: "/internal/v1/auth/capabilities", want: serviceRoutePublic},
		{method: http.MethodGet, path: "/internal/v1/readiness", want: serviceRoutePublic},
		{method: http.MethodGet, path: "/internal/v1/capabilities", want: serviceRouteLowRisk},
		{method: http.MethodGet, path: "/internal/v1/app/bootstrap", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/login/begin", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/login/finish", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/registration/begin", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/registration/finish", want: serviceRouteLowRisk},
		{method: http.MethodGet, path: "/internal/v1/files/search", want: serviceRouteLowRisk},
		{method: http.MethodGet, path: "/internal/v1/files/read", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/files/upload", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPut, path: "/internal/v1/files/write", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodGet, path: "/internal/v1/terminal/sessions/term-1/stream", want: serviceRouteLowRisk, sessionOnly: true},
		{method: http.MethodGet, path: "/internal/v1/terminal/sessions/term-1/ws", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/terminal/sessions", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/terminal/sessions/term-1/ws-ticket", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/terminal/sessions/term-1/input", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodGet, path: "/internal/v1/approvals", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/approvals/12/approve", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/devices/device-1/revoke", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/configure/security", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodGet, path: "/internal/v1/doctor/status", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/doctor/run", want: serviceRouteLowRisk},
		{method: http.MethodGet, path: "/internal/v1/doctor/skills/demo/diagnostics", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/doctor/sessions", want: serviceRouteLowRisk, sessionOnly: true},
		{method: http.MethodGet, path: "/internal/v1/doctor/plans/plan-1", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/doctor/plans/plan-1/validate", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/doctor/plans/plan-1/apply", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/doctor/plans/plan-1/rollback", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/actions/restart-service", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodGet, path: "/internal/v1/mcp/servers", want: serviceRouteLowRisk, sessionOnly: true},
		{method: http.MethodPost, path: "/internal/v1/mcp/servers", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/mcp/servers/local/test", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodDelete, path: "/internal/v1/mcp/servers/local", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodGet, path: "/internal/v1/secure-connections/capabilities", want: serviceRouteLowRisk},
		{method: http.MethodGet, path: "/internal/v1/secure-connections/host-identity", want: serviceRouteLowRisk, sessionOnly: true},
		{method: http.MethodGet, path: "/internal/v1/secure-connections/devices", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/secure-connections/pairing/intents", want: serviceRouteLowRisk, sessionOnly: true},
		{method: http.MethodPost, path: "/internal/v1/secure-connections/pairing/approve", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/secure-connections/pairing/exchange", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/secure-connections/sessions", want: serviceRouteLowRisk},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			got := serviceRouteRequirementForRequest(cfg, req)
			if got.Sensitivity != tc.want || got.SessionOnly != tc.sessionOnly || got.StepUpOnly != tc.stepUpOnly {
				t.Fatalf("unexpected requirement: got %#v", got)
			}
		})
	}
}

func TestServiceAuthMiddleware_PublicReadinessDoesNotRequireBearer(t *testing.T) {
	cfg := rolloutAuthTestConfig(config.AuthEnforcementSession)
	handler := serviceAuthMiddlewareWithBroker(cfg, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := serviceAuthKindFromContext(r.Context()); got != "public" {
			t.Fatalf("expected public auth kind, got %q", got)
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/readiness", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func rolloutAuthTestConfig(mode config.AuthEnforcementMode) config.Config {
	cfg := config.Default()
	cfg.Auth.Enabled = true
	cfg.Auth.RPID = "localhost"
	cfg.Auth.RPDisplayName = "OR3 Test"
	cfg.Auth.AllowedOrigins = []string{"http://localhost:3000"}
	cfg.Auth.SessionIdleTTLSeconds = 300
	cfg.Auth.SessionAbsoluteTTLSeconds = 3600
	cfg.Auth.StepUpTTLSeconds = 120
	cfg.Auth.FallbackPolicy = config.AuthFallbackPairedTokenPlusWarn
	cfg.Auth.EnforcementMode = mode
	cfg.Auth.RequirePasskeyForSensitive = true
	return cfg
}
