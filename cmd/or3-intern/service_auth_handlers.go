package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/auth"
	"or3-intern/internal/config"
)

func (s *serviceServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/auth"), "/")
	api := s.app()
	identity := serviceAuthIdentityFromContext(r.Context())
	sessionToken := serviceAuthSessionToken(r)
	switch relative {
	case "capabilities":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{
			"passkeysEnabled":            s.config.Auth.Enabled,
			"passkeyMode":                string(s.config.Auth.EnforcementMode),
			"rpId":                       s.config.Auth.RPID,
			"origins":                    append([]string{}, s.config.Auth.AllowedOrigins...),
			"webauthnAvailable":          api.Auth() != nil && api.Auth().Enabled(),
			"sessionRequired":            s.config.Auth.EnforcementMode == config.AuthEnforcementSession,
			"stepUpRequiredForSensitive": s.config.Auth.RequirePasskeyForSensitive,
			"secureStorageRecommended":   true,
			"fallbackPolicy":             s.config.Auth.FallbackPolicy,
			"sessionHeader":              "X-Or3-Session",
		})
		return
	case "passkeys/registration/begin":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if !requireServiceRole(w, r, approval.RoleOperator) {
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			DisplayName string `json:"displayName"`
			Reason      string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		response, err := api.BeginPasskeyRegistration(r.Context(), auth.BeginRegistrationRequest{DeviceID: identity.Device, DisplayName: body.DisplayName, Reason: body.Reason, SessionToken: sessionToken})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, response)
		return
	case "passkeys/registration/finish":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if !requireServiceRole(w, r, approval.RoleOperator) {
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			CeremonyID string          `json:"ceremonyId"`
			Credential json.RawMessage `json:"credential"`
			Nickname   string          `json:"nickname"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		record, err := api.FinishPasskeyRegistration(r.Context(), auth.FinishRegistrationRequest{CeremonyID: body.CeremonyID, Body: body.Credential, Nickname: body.Nickname, SessionToken: sessionToken})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"passkey": record})
		return
	case "passkeys/login/begin":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		response, err := api.BeginPasskeyLogin(r.Context(), auth.BeginLoginRequest{DeviceID: identity.Device, Reason: body.Reason})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, response)
		return
	case "passkeys/login/finish":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			CeremonyID string          `json:"ceremonyId"`
			Credential json.RawMessage `json:"credential"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		result, err := api.FinishPasskeyLogin(r.Context(), auth.FinishLoginRequest{CeremonyID: body.CeremonyID, Body: body.Credential, DeviceID: identity.Device, FallbackRole: identity.Role})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, result)
		return
	case "step-up/begin":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		response, err := api.BeginStepUp(r.Context(), auth.BeginStepUpRequest{SessionToken: sessionToken, Reason: body.Reason})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, response)
		return
	case "step-up/finish":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			CeremonyID string          `json:"ceremonyId"`
			Credential json.RawMessage `json:"credential"`
			Reason     string          `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		session, err := api.FinishStepUp(r.Context(), auth.FinishStepUpRequest{SessionToken: sessionToken, CeremonyID: body.CeremonyID, Body: body.Credential, Reason: body.Reason})
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"session": session})
		return
	case "session":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		claims, err := api.ValidateAuthSession(r.Context(), sessionToken)
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"session": claims.Session, "user": claims.User, "role": claims.Role})
		return
	case "session/revoke":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := api.RevokeAuthSession(r.Context(), sessionToken, body.Reason); err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"status": "revoked"})
		return
	case "passkeys":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		claims, err := api.ValidateAuthSession(r.Context(), sessionToken)
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		items, err := api.ListPasskeys(r.Context(), claims.User.ID)
		if err != nil {
			writeServiceAuthError(w, r, err)
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"items": items})
		return
	default:
		if strings.HasPrefix(relative, "passkeys/") {
			rest := strings.TrimPrefix(relative, "passkeys/")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
				passkeyID := parts[0]
				switch {
				case len(parts) == 1 && r.Method == http.MethodPatch:
					var body struct {
						Nickname string `json:"nickname"`
					}
					if err := decodeServiceRequestBody(r.Body, &body); err != nil {
						writeServiceRequestDecodeError(w, err)
						return
					}
					if err := api.RenamePasskey(r.Context(), passkeyID, body.Nickname); err != nil {
						writeServiceAuthError(w, r, err)
						return
					}
					writeServiceValue(w, http.StatusOK, map[string]any{"id": passkeyID, "nickname": body.Nickname})
					return
				case len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost:
					var body struct {
						Reason string `json:"reason"`
					}
					if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
						writeServiceRequestDecodeError(w, err)
						return
					}
					if err := api.RevokePasskey(r.Context(), sessionToken, passkeyID, body.Reason); err != nil {
						writeServiceAuthError(w, r, err)
						return
					}
					writeServiceValue(w, http.StatusOK, map[string]any{"id": passkeyID, "status": "revoked"})
					return
				}
			}
		}
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "auth route not found"})
	}
}
