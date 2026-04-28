package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
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
		{name: "enforce sensitive keeps low risk paired token workflow", mode: config.AuthEnforcementSensitive, path: "/internal/v1/turns", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "enforce session blocks low risk paired token without session", mode: config.AuthEnforcementSession, path: "/internal/v1/turns", method: http.MethodPost, wantStatus: http.StatusUnauthorized, wantCode: auth.CodeSessionRequired},
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
		{method: http.MethodGet, path: "/internal/v1/capabilities", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/login/begin", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/login/finish", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/registration/begin", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/auth/passkeys/registration/finish", want: serviceRouteLowRisk},
		{method: http.MethodGet, path: "/internal/v1/files/search", want: serviceRouteLowRisk},
		{method: http.MethodPost, path: "/internal/v1/files/upload", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/terminal/sessions", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/terminal/sessions/term-1/input", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/approvals/12/approve", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/devices/device-1/revoke", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
		{method: http.MethodPost, path: "/internal/v1/configure/security", want: serviceRouteSensitive, sessionOnly: true, stepUpOnly: true},
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
