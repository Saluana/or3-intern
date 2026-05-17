package main

import (
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
