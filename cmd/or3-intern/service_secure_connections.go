package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
	"or3-intern/internal/secureconn"
	"or3-intern/internal/security"
)

func (s *serviceServer) secureConnectionTrustStore(ctx context.Context) (*secureconn.TrustStore, error) {
	if s == nil || s.broker == nil || s.broker.DB == nil {
		return nil, fmt.Errorf("approval broker unavailable")
	}
	key, err := security.LoadOrCreateKey(s.config.Security.SecretStore.KeyFile)
	if err != nil {
		return nil, err
	}
	secrets := &security.SecretManager{DB: s.broker.DB, Key: key}
	identity, _, err := (&secureconn.IdentityStore{Secrets: secrets}).LoadOrCreate(ctx, "OR3 Desktop")
	if err != nil {
		return nil, err
	}
	store := &secureconn.TrustStore{DB: s.broker.DB, Identity: identity}
	if err := store.PinHostIdentity(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *serviceServer) handleSecureConnections(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	store, err := s.secureConnectionTrustStore(r.Context())
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "secure connections unavailable", err)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/secure-connections")
	path = strings.Trim(path, "/")
	switch {
	case path == "capabilities":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"secure_connections": secureconn.CurrentCapabilityDiscovery()})
	case path == "host-identity":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"host": store.Identity.Public()})
	case path == "devices":
		s.handleSecureConnectionDevices(w, r, store)
	case strings.HasPrefix(path, "devices/"):
		s.handleSecureConnectionDeviceAction(w, r, store, strings.TrimPrefix(path, "devices/"))
	case path == "pairing/intents":
		s.handleSecureConnectionPairingIntent(w, r, store)
	case path == "pairing/approve":
		s.handleSecureConnectionPairingApprove(w, r, store)
	case path == "sessions":
		s.handleSecureConnectionSessions(w, r, store)
	case path == "sessions/expire":
		s.handleSecureConnectionSessionExpiry(w, r, store)
	case strings.HasPrefix(path, "sessions/"):
		s.handleSecureConnectionSessionAction(w, r, store, strings.TrimPrefix(path, "sessions/"))
	case path == "relay/rendezvous":
		s.handleRelayRendezvous(w, r, store)
	case path == "relay/rendezvous/expire":
		s.handleRelayRendezvousExpire(w, r, store)
	case strings.HasPrefix(path, "relay/rendezvous/"):
		s.handleRelayRendezvousAction(w, r, store, strings.TrimPrefix(path, "relay/rendezvous/"))
	case path == "relay/host/ws":
		s.handleSecureRelayHostWebSocket(w, r, store)
	case path == "relay/device/ws":
		s.handleSecureRelayDeviceWebSocket(w, r, store)
	case path == "relay/routes":
		s.handleSecureRelayRouteRequest(w, r, store)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "secure connection route not found"})
	}
}

func (s *serviceServer) handleSecureConnectionDevices(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	items, err := store.ListDevices(r.Context(), r.URL.Query().Get("status"), 200)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure device list failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *serviceServer) handleSecureConnectionDeviceAction(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore, tail string) {
	if strings.Trim(tail, "/") == "lookup-by-noise-key" {
		s.handleSecureConnectionDeviceLookupByNoiseKey(w, r, store)
		return
	}
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) < 2 {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "secure device action not found"})
		return
	}
	deviceID, action := parts[0], parts[1]
	switch action {
	case "remote-revoke":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		if err := store.RevokeDevice(r.Context(), deviceID, "remote account request"); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "secure remote revocation failed", err)
			return
		}
		s.auditSecureConnection(r.Context(), "secure_connection.remote_revoke_requested", "", deviceID, map[string]any{"device_id": deviceID, "authority": "account"})
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "relay_routing": "blocked", "host_trust": secureconn.StatusRevoked})
	case "trust":
		if r.Method != http.MethodPatch && r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Role         string   `json:"role"`
			Capabilities []string `json:"capabilities"`
			TrustLevel   string   `json:"trust_level"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		rec, err := store.UpdateDeviceTrust(r.Context(), deviceID, body.Role, body.Capabilities, body.TrustLevel)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "secure device trust update failed", err)
			return
		}
		s.auditSecureConnection(r.Context(), "secure_connection.device_trust_updated", "", deviceID, map[string]any{"device_id": deviceID, "role": rec.Role, "trust_level": rec.TrustLevel})
		writeServiceJSON(w, http.StatusOK, map[string]any{"device": rec})
	case "revoke":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if err := store.RevokeDevice(r.Context(), deviceID, body.Reason); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "secure device revoke failed", err)
			return
		}
		s.auditSecureConnection(r.Context(), "secure_connection.device_revoked", "", deviceID, map[string]any{"device_id": deviceID, "reason": body.Reason})
		writeServiceJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "status": secureconn.StatusRevoked})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "secure device action not found"})
	}
}

func (s *serviceServer) handleSecureConnectionDeviceLookupByNoiseKey(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, servicePairingBodyLimit)
	var body struct {
		DeviceNoisePublicKey string `json:"device_noise_public_key"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	rec, ok, err := store.DB.FindSecureConnectionDeviceByNoiseKey(r.Context(), store.Identity.HostID, body.DeviceNoisePublicKey)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure device lookup failed", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "device not found"})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"device": rec})
}

func (s *serviceServer) handleSecureConnectionPairingIntent(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, servicePairingBodyLimit)
	var body struct {
		RelayOrigin        string   `json:"relay_origin"`
		HostDisplayName    string   `json:"host_display_name"`
		RequestedRole      string   `json:"requested_role"`
		Capabilities       []string `json:"capabilities"`
		RequestedAccountID string   `json:"requested_account_id"`
		TTLSeconds         int      `json:"ttl_seconds"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	result, err := store.CreatePairingIntent(r.Context(), secureconn.PairingIntent{
		RelayOrigin:        body.RelayOrigin,
		HostDisplayName:    body.HostDisplayName,
		RequestedRole:      body.RequestedRole,
		Capabilities:       body.Capabilities,
		RequestedAccountID: body.RequestedAccountID,
		TTL:                time.Duration(body.TTLSeconds) * time.Second,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure pairing intent failed", err)
		return
	}
	if err := store.DB.CreateRelayRendezvous(r.Context(), db.RelayRendezvousRecord{
		RendezvousID:     result.Payload.RendezvousID,
		AccountID:        result.Payload.RequestedAccountID,
		HostIDHash:       secureconn.HashBase64URL([]byte(result.Payload.HostID)),
		SecretCommitment: result.SecretCommitment,
		Status:           secureconn.StatusCreated,
		CreatedAt:        time.Now().UTC().UnixMilli(),
		ExpiresAt:        result.Payload.ExpiresAtUnixMs,
		Metadata: map[string]any{
			"relay_origin": result.Payload.RelayOrigin,
			"protocol":     "or3-secure-pairing",
		},
	}); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "relay rendezvous registration failed", err)
		return
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{
		"qr":                result.Encoded,
		"secret_commitment": result.SecretCommitment,
		"rendezvous_id":     result.Payload.RendezvousID,
		"expires_at":        result.Payload.ExpiresAtUnixMs,
	})
}

func (s *serviceServer) handleSecureConnectionPairingApprove(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, servicePairingBodyLimit)
	var body struct {
		RendezvousID  string                                `json:"rendezvous_id"`
		PairingSecret string                                `json:"pairing_secret"`
		Proposal      secureconn.DeviceEnrollmentProposalV1 `json:"proposal"`
		TrustLevel    string                                `json:"trust_level"`
		ExpiresAt     int64                                 `json:"expires_at"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	var expiresAt time.Time
	if body.ExpiresAt > 0 {
		expiresAt = time.UnixMilli(body.ExpiresAt).UTC()
	}
	cert, rec, err := store.ApproveEnrollmentFromPairing(r.Context(), secureconn.EnrollmentApprovalInput{
		RendezvousID:  body.RendezvousID,
		PairingSecret: body.PairingSecret,
		Proposal:      body.Proposal,
		TrustLevel:    body.TrustLevel,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure enrollment approval failed", err)
		return
	}
	hash, _ := secureconn.EnrollmentCertificateHash(cert)
	writeServiceJSON(w, http.StatusCreated, map[string]any{"certificate": cert, "certificate_hash": hash, "device": rec})
}

func (s *serviceServer) handleSecureConnectionSessions(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	limitServiceRequestBody(w, r, servicePairingBodyLimit)
	var body struct {
		DeviceID                  string                          `json:"device_id"`
		DeviceNoisePublicKey      string                          `json:"device_noise_public_key"`
		RelayRouteID              string                          `json:"relay_route_id"`
		RelayOrigin               string                          `json:"relay_origin"`
		EnrollmentCertificateHash string                          `json:"enrollment_certificate_hash"`
		AccountID                 string                          `json:"account_id"`
		NoiseHandshake            secureconn.NoiseHandshakeInitV1 `json:"noise_handshake"`
		TTLSeconds                int                             `json:"ttl_seconds"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	var stepUpAt time.Time
	identity := serviceAuthIdentityFromContext(r.Context())
	if identity.Kind == "auth-session" && identity.StepUpOK && identity.StepUpAt > 0 {
		stepUpAt = time.UnixMilli(identity.StepUpAt).UTC()
	}
	manager := &secureconn.SessionManager{DB: store.DB, Identity: store.Identity}
	claims, rec, err := manager.StartVerifiedSession(r.Context(), secureconn.SessionStartInput{
		DeviceID:                  body.DeviceID,
		DeviceNoisePublicKey:      body.DeviceNoisePublicKey,
		RelayRouteID:              body.RelayRouteID,
		RelayOrigin:               body.RelayOrigin,
		EnrollmentCertificateHash: body.EnrollmentCertificateHash,
		AccountID:                 body.AccountID,
		NoiseHandshake:            body.NoiseHandshake,
		AuthenticatedStepUpAt:     stepUpAt,
		TTL:                       time.Duration(body.TTLSeconds) * time.Second,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure session start failed", err)
		return
	}
	s.auditSecureConnection(r.Context(), "secure_connection.session_started", claims.SessionID, claims.DeviceID, map[string]any{
		"device_id":        claims.DeviceID,
		"route_id":         claims.RelayRouteID,
		"role":             claims.Role,
		"trust_level":      claims.TrustLevel,
		"expires_at":       claims.ExpiresAtUnixMs,
		"enrollment_epoch": claims.EnrollmentEpoch,
	})
	writeServiceJSON(w, http.StatusCreated, map[string]any{"claims": claims, "session": rec})
}

func (s *serviceServer) handleSecureConnectionSessionExpiry(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	manager := &secureconn.SessionManager{DB: store.DB, Identity: store.Identity}
	count, err := manager.ExpireStaleSessions(r.Context())
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure session expiry failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"expired": count})
}

func (s *serviceServer) handleSecureConnectionSessionAction(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) < 2 {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "secure session action not found"})
		return
	}
	sessionID, action := parts[0], parts[1]
	switch action {
	case "authorize":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, servicePairingBodyLimit)
		var body struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Tool   string `json:"tool"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		manager := &secureconn.SessionManager{DB: store.DB, Identity: store.Identity}
		claims, err := manager.LoadActiveSessionClaims(r.Context(), sessionID)
		if err != nil {
			writeServiceError(w, r, http.StatusForbidden, "secure session is not active", err)
			return
		}
		actionReq := secureconn.ClassifyAction(body.Method, body.Path, body.Tool)
		decision := secureconn.AuthorizeAction(claims, actionReq, time.Now().UTC())
		s.auditSecureConnection(r.Context(), "secure_connection.action_authorized", claims.SessionID, claims.DeviceID, decision.AuditPayload)
		status := http.StatusOK
		if !decision.Allowed {
			status = http.StatusForbidden
		}
		writeServiceJSON(w, status, map[string]any{"decision": decision})
	case "step-up":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		identity := serviceAuthIdentityFromContext(r.Context())
		if identity.Kind != "auth-session" || !identity.StepUpOK || identity.StepUpAt <= 0 {
			writeServiceJSON(w, http.StatusUnauthorized, map[string]any{"error": "recent passkey step-up required", "code": "recent_step_up_required"})
			return
		}
		manager := &secureconn.SessionManager{DB: store.DB, Identity: store.Identity}
		claims, err := manager.LoadActiveSessionClaims(r.Context(), sessionID)
		if err != nil {
			writeServiceError(w, r, http.StatusForbidden, "secure session is not active", err)
			return
		}
		verifiedAt := time.UnixMilli(identity.StepUpAt).UTC()
		if err := store.DB.UpdateSecureConnectionSessionStepUp(r.Context(), sessionID, verifiedAt.UnixMilli()); err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "secure session step-up update failed", err)
			return
		}
		s.auditSecureConnection(r.Context(), "secure_connection.step_up", sessionID, claims.DeviceID, map[string]any{"verified_at": verifiedAt.UnixMilli(), "auth_session_id": identity.Session})
		writeServiceJSON(w, http.StatusOK, map[string]any{"session_id": sessionID, "step_up_at": verifiedAt.UnixMilli()})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "secure session action not found"})
	}
}

func (s *serviceServer) auditSecureConnection(ctx context.Context, eventType, sessionID, actor string, payload map[string]any) {
	if s == nil || s.runtime == nil || s.runtime.Audit == nil {
		return
	}
	_ = s.runtime.Audit.Record(ctx, eventType, sessionID, actor, payload)
}

func (s *serviceServer) handleRelayRendezvous(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodGet {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	rec, ok, err := store.DB.GetRelayRendezvous(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "relay rendezvous lookup failed", err)
		return
	}
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "rendezvous not found"})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"item": rec})
}

func (s *serviceServer) handleRelayRendezvousExpire(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	count, err := store.DB.ExpireRelayRendezvous(r.Context(), time.Now().UTC().UnixMilli())
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "relay rendezvous expiry failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"expired": count})
}

func (s *serviceServer) handleRelayRendezvousAction(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) != 2 {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "relay rendezvous action not found"})
		return
	}
	id, action := parts[0], parts[1]
	now := time.Now().UTC().UnixMilli()
	switch action {
	case "join":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		rec, err := store.DB.JoinRelayRendezvous(r.Context(), id, now, 3)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "relay rendezvous join failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"item": rec})
	case "consume":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		ok, err := store.DB.ConsumeRelayRendezvous(r.Context(), id, now)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "relay rendezvous consume failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"rendezvous_id": id, "consumed": ok})
	case "reject":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		ok, err := store.DB.RejectRelayRendezvous(r.Context(), id, now)
		if err != nil {
			writeServiceError(w, r, http.StatusBadRequest, "relay rendezvous reject failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"rendezvous_id": id, "rejected": ok})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "relay rendezvous action not found"})
	}
}
