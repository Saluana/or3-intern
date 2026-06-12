package main

import (
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/secureconn"

	"golang.org/x/crypto/curve25519"
)

func TestHandleSecureConnectionPairingIntentDoesNotExposeRawPayload(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := config.Default()
	cfg.Security.SecretStore.KeyFile = filepath.Join(t.TempDir(), "secure-connections.key")
	server := &serviceServer{
		config: cfg,
		broker: &approval.Broker{DB: database, Config: cfg.Security.Approvals},
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/secure-connections/pairing/intents", strings.NewReader(`{"relay_origin":"https://relay.or3.chat","host_display_name":"Desk","requested_role":"operator","capabilities":["chat"]}`))
	req.Header.Set("Content-Type", "application/json")
	req = serviceRequestWithAuthIdentity(req, serviceAuthIdentity{Kind: "auth-session", Actor: "user:test", Role: approval.RoleAdmin, StepUpOK: true, StepUpAt: time.Now().UnixMilli()})
	rec := httptest.NewRecorder()

	server.handleSecureConnections(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["payload"]; ok {
		t.Fatalf("expected pairing intent response not to expose raw payload, got %#v", payload)
	}
	if strings.TrimSpace(fmt.Sprint(payload["qr"])) == "" || strings.TrimSpace(fmt.Sprint(payload["rendezvous_id"])) == "" {
		t.Fatalf("expected pairing intent response to include qr and rendezvous_id, got %#v", payload)
	}
}

func TestSecureConnectionApproveReturnsCertificateOnly(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := config.Default()
	cfg.Security.SecretStore.KeyFile = filepath.Join(t.TempDir(), "secure-connections.key")
	server := &serviceServer{
		config: cfg,
		broker: &approval.Broker{DB: database, Config: cfg.Security.Approvals},
	}
	store, err := server.secureConnectionTrustStore(context.Background())
	if err != nil {
		t.Fatalf("secureConnectionTrustStore: %v", err)
	}
	devicePub, devicePriv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	noisePriv, err := secureconn.RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	noisePriv[0] &= 248
	noisePriv[31] &= 127
	noisePriv[31] |= 64
	noisePub, err := curve25519.X25519(noisePriv, curve25519.Basepoint)
	if err != nil {
		t.Fatalf("curve25519.X25519: %v", err)
	}
	proposal := secureconn.DeviceEnrollmentProposalV1{
		Version:                secureconn.ProtocolVersion,
		DeviceID:               "device-approve",
		DeviceDisplayName:      "Phone",
		Platform:               secureconn.PlatformIOS,
		DeviceSigningPublicKey: secureconn.Base64URL(devicePub),
		DeviceNoisePublicKey:   secureconn.Base64URL(noisePub),
		RequestedRole:          approval.RoleOperator,
		RequestedCapabilities:  []string{secureconn.CapabilityChat},
		CreatedAtUnixMs:        time.Now().UTC().UnixMilli(),
	}
	signingBytes, err := secureconn.EnrollmentProposalSigningBytes(proposal)
	if err != nil {
		t.Fatalf("EnrollmentProposalSigningBytes: %v", err)
	}
	proposal.Signature = secureconn.Base64URL(ed25519.Sign(devicePriv, signingBytes))
	intent, err := store.CreatePairingIntent(context.Background(), secureconn.PairingIntent{
		HostDisplayName: "Desk",
		RequestedRole:   approval.RoleOperator,
		Capabilities:    []string{"chat"},
		TTL:             time.Minute,
	})
	if err != nil {
		t.Fatalf("CreatePairingIntent: %v", err)
	}
	body := fmt.Sprintf(`{"rendezvous_id":%q,"pairing_secret":%q,"proposal":%s,"trust_level":%q}`,
		intent.Payload.RendezvousID,
		intent.Payload.PairingSecret,
		mustJSON(t, proposal),
		secureconn.TrustNativeSoftware,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/secure-connections/pairing/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.handleSecureConnectionPairingApprove(rec, req, store)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["certificate"]; !ok {
		t.Fatalf("expected certificate in response, got %#v", payload)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(encoded)
}
