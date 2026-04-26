package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"or3-intern/internal/approval"
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
	Kind   string
	Actor  string
	Role   string
	Device string
}

func serviceAuthMiddleware(secret string, next http.Handler) http.Handler {
	return serviceAuthMiddlewareWithBroker(config.Config{Service: config.ServiceConfig{Secret: secret, SharedSecretRole: approval.RoleServiceClient}}, nil, next)
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

func serviceAuthMiddlewareWithBroker(cfg config.Config, broker *approval.Broker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowsUnauthenticatedPairingRoute(cfg, r) {
			ctx := approval.ContextWithAuditAuthKind(r.Context(), "unauthenticated")
			ctx = approval.ContextWithAuditActor(ctx, "anonymous")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		identity, err := authenticateServiceRequest(cfg, broker, r.Header.Get("Authorization"), time.Now(), r.Context())
		if err != nil {
			writeServiceJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
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

func authenticateServiceRequest(cfg config.Config, broker *approval.Broker, header string, now time.Time, ctx context.Context) (serviceAuthIdentity, error) {
	if err := validateServiceAuthorization(cfg.Service.Secret, header, now); err == nil {
		role := strings.TrimSpace(cfg.Service.SharedSecretRole)
		if role == "" {
			role = approval.RoleServiceClient
		}
		return serviceAuthIdentity{Kind: "shared-secret", Actor: "service:shared-secret", Role: role}, nil
	}
	if broker == nil {
		return serviceAuthIdentity{}, fmt.Errorf("missing bearer token")
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return serviceAuthIdentity{}, fmt.Errorf("missing bearer token")
	}
	device, err := broker.AuthenticateDeviceToken(ctx, token)
	if err != nil {
		return serviceAuthIdentity{}, err
	}
	return serviceAuthIdentity{Kind: "paired-device", Actor: "device:" + device.DeviceID, Role: device.Role, Device: device.DeviceID}, nil
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
