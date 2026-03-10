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
	"net/http"
	"strings"
	"time"
)

const serviceTokenMaxAge = 5 * time.Minute

type serviceTokenClaims struct {
	IssuedAt int64  `json:"iat"`
	Nonce    string `json:"nonce"`
}

func serviceAuthMiddleware(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validateServiceAuthorization(secret, r.Header.Get("Authorization"), time.Now()); err != nil {
			writeServiceJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
			return
		}
		next.ServeHTTP(w, r)
	})
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
