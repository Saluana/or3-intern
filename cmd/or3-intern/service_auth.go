package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
)

const serviceTokenMaxAge = 5 * time.Minute

type serviceTokenClaims struct {
	IssuedAt int64  `json:"iat"`
	Nonce    string `json:"nonce"`
}

type serviceAuthContextKey struct{}
type serviceAuthKindContextKey struct{}

type serviceAuthIdentity struct {
	Kind      string
	Actor     string
	Role      string
	Device    string
	User      string
	Session   string
	StepUpAt  int64
	StepUpOK  bool
	Challenge string
}

type serviceRouteSensitivity int

const (
	serviceRoutePublic serviceRouteSensitivity = iota
	serviceRouteLowRisk
	serviceRouteSensitive
)

type serviceRouteRequirement struct {
	Sensitivity serviceRouteSensitivity
	SessionOnly bool
	StepUpOnly  bool
	Reason      string
}

func serviceAuthMiddleware(secret string, next http.Handler) http.Handler {
	return serviceAuthMiddlewareWithBroker(config.Config{Service: config.ServiceConfig{Secret: secret, SharedSecretRole: approval.RoleServiceClient}}, nil, nil, next)
}

func serviceBrowserMiddleware(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin, ok := serviceAllowedBrowserOrigin(cfg, r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		serviceWriteCORSHeaders(w.Header(), r, origin)
		if serviceIsCORSPreflight(r) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serviceAuthMiddlewareWithBroker(cfg config.Config, broker *approval.Broker, authSvc *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowsUnauthenticatedPairingRoute(cfg, r) {
			ctx := approval.ContextWithAuditAuthKind(r.Context(), "unauthenticated")
			ctx = approval.ContextWithAuditActor(ctx, "anonymous")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		requirement := serviceRouteRequirementForRequest(cfg, r)
		identity, err := authenticateServiceRequest(cfg, broker, authSvc, r.Header.Get("Authorization"), time.Now(), r.Context())
		if err != nil {
			if challenge := serviceAuthChallengeError(cfg, authSvc, requirement, serviceAuthIdentity{}, err); challenge != nil {
				serviceAuditAuthEvent(authSvc, r.Context(), "auth.policy.denied", serviceAuthIdentity{Actor: "anonymous"}, map[string]any{"path": r.URL.Path, "method": r.Method, "code": challenge.Code})
				writeServiceAuthError(w, r, challenge)
				return
			}
			writeServiceJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		if challenge := serviceAuthChallengeError(cfg, authSvc, requirement, identity, nil); challenge != nil {
			if strings.EqualFold(string(cfg.Auth.EnforcementMode), string(config.AuthEnforcementWarn)) {
				serviceAuditAuthEvent(authSvc, r.Context(), "auth.policy.warn", identity, map[string]any{"path": r.URL.Path, "method": r.Method, "code": challenge.Code})
				w.Header().Set("X-Or3-Auth-Warning", challenge.Code)
				if strings.TrimSpace(challenge.Message) != "" {
					w.Header().Set("X-Or3-Auth-Reason", challenge.Message)
				}
			} else {
				serviceAuditAuthEvent(authSvc, r.Context(), "auth.policy.denied", identity, map[string]any{"path": r.URL.Path, "method": r.Method, "code": challenge.Code})
				writeServiceAuthError(w, r, challenge)
				return
			}
		}
		ctx := context.WithValue(r.Context(), serviceAuthContextKey{}, identity)
		ctx = context.WithValue(ctx, serviceAuthKindContextKey{}, identity.Kind)
		ctx = approval.ContextWithAuditAuthKind(ctx, identity.Kind)
		ctx = approval.ContextWithAuditActor(ctx, identity.Actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func serviceAllowedBrowserOrigin(cfg config.Config, r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return "", false
	}
	if !serviceListenIsLoopback(cfg.Service.Listen) {
		return "", false
	}
	if strings.TrimSpace(r.RemoteAddr) != "" && !requestRemoteIsLoopback(r.RemoteAddr) {
		return "", false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return "", false
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil {
		return origin, ip.IsLoopback()
	}
	return origin, strings.EqualFold(host, "localhost")
}

func serviceIsCORSPreflight(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodOptions {
		return false
	}
	return strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != ""
}

func serviceWriteCORSHeaders(header http.Header, r *http.Request, origin string) {
	if header == nil {
		return
	}
	serviceAppendVary(header, "Origin")
	serviceAppendVary(header, "Access-Control-Request-Method")
	serviceAppendVary(header, "Access-Control-Request-Headers")
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	header.Set("Access-Control-Expose-Headers", "X-Request-Id")
	header.Set("Access-Control-Max-Age", "600")
	requestedHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
	if requestedHeaders == "" {
		requestedHeaders = "Authorization, Content-Type, Accept, X-Request-Id"
	}
	header.Set("Access-Control-Allow-Headers", requestedHeaders)
}

func serviceAppendVary(header http.Header, value string) {
	if header == nil || strings.TrimSpace(value) == "" {
		return
	}
	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func authenticateServiceRequest(cfg config.Config, broker *approval.Broker, authSvc *auth.Service, header string, now time.Time, ctx context.Context) (serviceAuthIdentity, error) {
	if err := validateServiceAuthorization(cfg.Service.Secret, header, now); err == nil {
		role := strings.TrimSpace(cfg.Service.SharedSecretRole)
		if role == "" {
			role = approval.RoleServiceClient
		}
		return serviceAuthIdentity{Kind: "shared-secret", Actor: "service:shared-secret", Role: role}, nil
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return serviceAuthIdentity{}, fmt.Errorf("missing bearer token")
	}
	if broker != nil {
		device, err := broker.AuthenticateDeviceToken(ctx, token)
		if err == nil {
			return serviceAuthIdentity{Kind: "paired-device", Actor: "device:" + device.DeviceID, Role: device.Role, Device: device.DeviceID}, nil
		}
	}
	if authSvc != nil && authSvc.Enabled() {
		claims, err := authSvc.ValidateSessionToken(ctx, token)
		if err == nil {
			actor := "user:" + claims.User.ID
			if strings.TrimSpace(claims.Session.DeviceID) != "" {
				actor = actor + ":device:" + claims.Session.DeviceID
			}
			return serviceAuthIdentity{
				Kind:      "auth-session",
				Actor:     actor,
				Role:      claims.Role,
				Device:    claims.Session.DeviceID,
				User:      claims.User.ID,
				Session:   claims.Session.ID,
				StepUpAt:  claims.Session.LastStepUpAt,
				StepUpOK:  authSvc.HasRecentStepUp(claims.Session),
				Challenge: token,
			}, nil
		}
		return serviceAuthIdentity{}, err
	}
	return serviceAuthIdentity{}, fmt.Errorf("invalid bearer token")
}

func serviceAuthIdentityFromContext(ctx context.Context) serviceAuthIdentity {
	if ctx == nil {
		return serviceAuthIdentity{}
	}
	identity, _ := ctx.Value(serviceAuthContextKey{}).(serviceAuthIdentity)
	return identity
}

func serviceAuthKindFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	kind, _ := ctx.Value(serviceAuthKindContextKey{}).(string)
	return kind
}

func allowsUnauthenticatedPairingRoute(cfg config.Config, r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	if serviceIsCORSPreflight(r) {
		return true
	}
	if !cfg.Service.AllowUnauthenticatedPairing {
		return false
	}
	if !serviceListenIsLoopback(cfg.Service.Listen) {
		return false
	}
	if !requestRemoteIsLoopback(r.RemoteAddr) {
		return false
	}
	if r.Method != http.MethodPost {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	return path == "/internal/v1/pairing/requests" || path == "/internal/v1/pairing/exchange"
}

func serviceRouteRequirementForRequest(cfg config.Config, r *http.Request) serviceRouteRequirement {
	requirement := serviceRouteRequirement{Sensitivity: serviceRouteLowRisk}
	if r == nil || r.URL == nil {
		return requirement
	}
	path := strings.TrimSpace(r.URL.Path)
	method := r.Method
	switch {
	case path == "/internal/v1/health", path == "/internal/v1/ready", path == "/internal/v1/capabilities":
		return serviceRouteRequirement{Sensitivity: serviceRoutePublic}
	case path == "/internal/v1/auth/capabilities":
		return serviceRouteRequirement{Sensitivity: serviceRoutePublic}
	case path == "/internal/v1/auth/passkeys/login/begin", path == "/internal/v1/auth/passkeys/login/finish":
		return serviceRouteRequirement{Sensitivity: serviceRoutePublic}
	case path == "/internal/v1/auth/passkeys/registration/begin", path == "/internal/v1/auth/passkeys/registration/finish":
		return serviceRouteRequirement{Sensitivity: serviceRouteLowRisk, SessionOnly: true}
	case path == "/internal/v1/auth/session" || path == "/internal/v1/auth/session/revoke":
		return serviceRouteRequirement{Sensitivity: serviceRouteLowRisk, SessionOnly: true}
	case path == "/internal/v1/auth/step-up/begin" || path == "/internal/v1/auth/step-up/finish":
		return serviceRouteRequirement{Sensitivity: serviceRouteLowRisk, SessionOnly: true}
	case strings.HasPrefix(path, "/internal/v1/auth/passkeys"):
		if method == http.MethodGet && path == "/internal/v1/auth/passkeys" {
			return serviceRouteRequirement{Sensitivity: serviceRouteLowRisk, SessionOnly: true}
		}
		return serviceRouteRequirement{Sensitivity: serviceRouteSensitive, SessionOnly: true, StepUpOnly: true, Reason: "recent passkey verification required"}
	case method == http.MethodDelete || method == http.MethodPatch || method == http.MethodPut:
		return serviceRouteRequirement{Sensitivity: serviceRouteSensitive, Reason: "recent passkey verification required"}
	default:
		mode := string(cfg.Auth.EnforcementMode)
		if strings.EqualFold(mode, string(config.AuthEnforcementSession)) {
			requirement.SessionOnly = true
		}
		return requirement
	}
}

func serviceAuthChallengeError(cfg config.Config, authSvc *auth.Service, requirement serviceRouteRequirement, identity serviceAuthIdentity, authErr error) *auth.Error {
	if requirement.Sensitivity == serviceRoutePublic {
		return nil
	}
	mode := string(cfg.Auth.EnforcementMode)
	if strings.EqualFold(mode, string(config.AuthEnforcementOff)) {
		return nil
	}
	if authErr != nil {
		var typed *auth.Error
		if errors.As(authErr, &typed) {
			return typed
		}
		if authSvc == nil || !authSvc.Enabled() {
			return auth.ErrAuthDisabled
		}
		if requirement.SessionOnly || strings.EqualFold(mode, string(config.AuthEnforcementSession)) {
			return auth.ErrSessionRequired
		}
		if requirement.Sensitivity == serviceRouteSensitive {
			return auth.ErrPasskeyRequired
		}
		return nil
	}
	if strings.EqualFold(identity.Kind, "shared-secret") {
		return nil
	}
	enforceSession := requirement.SessionOnly || strings.EqualFold(mode, string(config.AuthEnforcementSession))
	if enforceSession && identity.Kind != "auth-session" {
		return auth.ErrSessionRequired
	}
	if requirement.Sensitivity != serviceRouteSensitive {
		return nil
	}
	if identity.Kind != "auth-session" {
		return auth.ErrPasskeyRequired
	}
	if requirement.StepUpOnly || strings.TrimSpace(requirement.Reason) != "" {
		if !identity.StepUpOK {
			return auth.ErrRecentStepUp
		}
	}
	return nil
}

func serviceAuditAuthEvent(authSvc *auth.Service, ctx context.Context, eventType string, identity serviceAuthIdentity, payload map[string]any) {
	if authSvc == nil {
		return
	}
	actor := strings.TrimSpace(identity.User)
	if actor == "" {
		actor = strings.TrimSpace(identity.Actor)
	}
	if actor == "" {
		actor = "anonymous"
	}
	authSvc.Audit(ctx, eventType, actor, payload)
}

func requireServiceRole(w http.ResponseWriter, r *http.Request, roles ...string) bool {
	identity := serviceAuthIdentityFromContext(r.Context())
	if len(roles) == 0 {
		return true
	}
	for _, role := range roles {
		if serviceRoleRank(identity.Role) >= serviceRoleRank(role) {
			return true
		}
	}
	writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
	return false
}

func serviceRoleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case approval.RoleAdmin:
		return 4
	case approval.RoleOperator:
		return 3
	case approval.RoleServiceClient, approval.RoleWebUI, approval.RoleNode:
		return 2
	case approval.RoleViewer:
		return 1
	default:
		return 0
	}
}

func serviceListenIsLoopback(addr string) bool {
	host := strings.TrimSpace(addr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func requestRemoteIsLoopback(addr string) bool {
	host := strings.TrimSpace(addr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func validateServiceAuthorization(secret string, header string, now time.Time) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("service secret is not configured")
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return fmt.Errorf("missing bearer token")
	}
	return validateServiceBearerToken(secret, token, now)
}

func validateServiceBearerToken(secret string, token string, now time.Time) error {
	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payloadPart == "" || signaturePart == "" {
		return fmt.Errorf("invalid bearer token format")
	}
	signature, err := hex.DecodeString(signaturePart)
	if err != nil {
		return fmt.Errorf("invalid bearer token signature")
	}
	expected := signServiceToken(secret, payloadPart)
	if !hmac.Equal(signature, expected) {
		return fmt.Errorf("invalid bearer token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return fmt.Errorf("invalid bearer token payload")
	}
	var claims serviceTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("invalid bearer token payload")
	}
	if claims.IssuedAt <= 0 {
		return fmt.Errorf("invalid bearer token timestamp")
	}
	issuedAt := time.Unix(claims.IssuedAt, 0)
	if issuedAt.After(now.Add(30 * time.Second)) {
		return fmt.Errorf("bearer token timestamp is in the future")
	}
	if now.Sub(issuedAt) > serviceTokenMaxAge {
		return fmt.Errorf("bearer token expired")
	}
	if strings.TrimSpace(claims.Nonce) == "" {
		return fmt.Errorf("invalid bearer token nonce")
	}
	return nil
}

func issueServiceBearerToken(secret string, now time.Time) (string, error) {
	nonce, err := randomHex(12)
	if err != nil {
		return "", err
	}
	claims := serviceTokenClaims{IssuedAt: now.Unix(), Nonce: nonce}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	signature := hex.EncodeToString(signServiceToken(secret, payloadPart))
	return payloadPart + "." + signature, nil
}

func signServiceToken(secret string, payloadPart string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payloadPart))
	return mac.Sum(nil)
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", nil
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func withDetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}
