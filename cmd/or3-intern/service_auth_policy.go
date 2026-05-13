package main

import (
	"net/http"
	"strings"

	"or3-intern/internal/auth"
	"or3-intern/internal/config"
)

func serviceWriteAuthChallengeIfNeeded(cfg config.Config, authSvc *auth.Service, w http.ResponseWriter, r *http.Request, requirement serviceRouteRequirement, identity serviceAuthIdentity, authErr error) bool {
	challenge := serviceAuthChallengeError(cfg, authSvc, requirement, identity, authErr)
	if challenge == nil {
		return false
	}
	if authErr == nil && strings.EqualFold(string(cfg.Auth.EnforcementMode), string(config.AuthEnforcementWarn)) {
		serviceAuditAuthEvent(authSvc, r.Context(), "auth.policy.warn", identity, map[string]any{"path": r.URL.Path, "method": r.Method, "code": challenge.Code})
		w.Header().Set("X-Or3-Auth-Warning", challenge.Code)
		if strings.TrimSpace(challenge.Message) != "" {
			w.Header().Set("X-Or3-Auth-Reason", challenge.Message)
		}
		return false
	}
	actor := identity
	if strings.TrimSpace(actor.Actor) == "" {
		actor = serviceAuthIdentity{Actor: "anonymous"}
	}
	serviceAuditAuthEvent(authSvc, r.Context(), "auth.policy.denied", actor, map[string]any{"path": r.URL.Path, "method": r.Method, "code": challenge.Code})
	writeServiceAuthError(w, r, challenge)
	return true
}
