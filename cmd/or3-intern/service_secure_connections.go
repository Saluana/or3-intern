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
	case path == "host-identity":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"host": store.Identity.Public()})
	case path == "pairing/intents":
		s.handleSecureConnectionPairingIntent(w, r, store)
	case path == "pairing/approve":
		s.handleSecureConnectionPairingApprove(w, r, store)
	case path == "relay/rendezvous":
		s.handleRelayRendezvous(w, r, store)
	case strings.HasPrefix(path, "relay/rendezvous/"):
		s.handleRelayRendezvousAction(w, r, store, strings.TrimPrefix(path, "relay/rendezvous/"))
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "secure connection route not found"})
	}
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
		"payload":           result.Payload,
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
		Proposal     secureconn.DeviceEnrollmentProposalV1 `json:"proposal"`
		Role         string                                `json:"role"`
		Capabilities []string                              `json:"capabilities"`
		TrustLevel   string                                `json:"trust_level"`
		AccountID    string                                `json:"account_id"`
		ExpiresAt    int64                                 `json:"expires_at"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	var expiresAt time.Time
	if body.ExpiresAt > 0 {
		expiresAt = time.UnixMilli(body.ExpiresAt).UTC()
	}
	cert, rec, err := store.ApproveEnrollment(r.Context(), body.Proposal, body.Role, body.Capabilities, body.TrustLevel, body.AccountID, expiresAt)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "secure enrollment approval failed", err)
		return
	}
	hash, _ := secureconn.EnrollmentCertificateHash(cert)
	writeServiceJSON(w, http.StatusCreated, map[string]any{"certificate": cert, "certificate_hash": hash, "device": rec})
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
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "relay rendezvous action not found"})
	}
}
