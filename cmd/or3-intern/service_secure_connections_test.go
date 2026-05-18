package main

import (
	"context"
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

func TestSecureConnectionCompatibilityExchangeIsSingleUse(t *testing.T) {
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
	intent, err := store.CreatePairingIntent(context.Background(), secureconn.PairingIntent{
		HostDisplayName: "Desk",
		RequestedRole:   approval.RoleOperator,
		Capabilities:    []string{"chat"},
		TTL:             time.Minute,
	})
	if err != nil {
		t.Fatalf("CreatePairingIntent: %v", err)
	}
	body := fmt.Sprintf(`{"rendezvous_id":%q,"pairing_secret":%q,"device_name":"Phone"}`, intent.Payload.RendezvousID, intent.Payload.PairingSecret)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/secure-connections/pairing/exchange", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.handleSecureConnectionPairingExchange(rec, req, store)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected first exchange 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	replay := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "/internal/v1/secure-connections/pairing/exchange", strings.NewReader(body))
	replayReq.Header.Set("Content-Type", "application/json")
	server.handleSecureConnectionPairingExchange(replay, replayReq, store)
	if replay.Code != http.StatusConflict {
		t.Fatalf("expected replay 409, got %d (%s)", replay.Code, replay.Body.String())
	}

	devices, err := server.broker.ListDevices(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected exactly one compatibility device, got %d", len(devices))
	}
}
