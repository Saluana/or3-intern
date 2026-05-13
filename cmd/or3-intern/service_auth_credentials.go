package main

import (
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
)

const (
	serviceCodeMissingToken    = "missing_token"
	serviceCodeInvalidToken    = "invalid_token"
	serviceCodeTokenReplay     = "token_replay"
	serviceCodeAuthRateLimited = "auth_rate_limited"
)

func serviceAuthenticateRequest(cfg config.Config, broker *approval.Broker, authSvc *auth.Service, r *http.Request, now time.Time, nonceGuard *serviceNonceReplayGuard) (serviceAuthIdentity, error) {
	return authenticateServiceRequest(cfg, broker, authSvc, r.Header.Get("Authorization"), now, r.Context(), r, nonceGuard)
}

func serviceAuthErrorCode(err error) string {
	if err == nil {
		return serviceCodeUnauthorized
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "missing bearer") || strings.Contains(message, "not configured"):
		return serviceCodeMissingToken
	case strings.Contains(message, "replay"):
		return serviceCodeTokenReplay
	case strings.Contains(message, "unsupported auth method"):
		return serviceCodeValidationFailed
	case strings.Contains(message, "invalid") || strings.Contains(message, "expired") || strings.Contains(message, "unavailable"):
		return serviceCodeInvalidToken
	default:
		return serviceCodeUnauthorized
	}
}

func serviceAuthIdentityFromValidatedSession(authSvc *auth.Service, token string, claims auth.SessionClaims) serviceAuthIdentity {
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
	}
}
