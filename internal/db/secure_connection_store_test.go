package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelayRendezvousRejectsPlaintextSecretMetadata(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	err = database.CreateRelayRendezvous(context.Background(), RelayRendezvousRecord{
		RendezvousID:     "rv",
		HostIDHash:       "host-hash",
		SecretCommitment: "commitment",
		Status:           "created",
		CreatedAt:        1,
		ExpiresAt:        2,
		Metadata:         map[string]any{"pairing_secret": "raw-secret"},
	})
	if err == nil {
		t.Fatal("expected relay metadata with plaintext secret to be rejected")
	}
	if !strings.Contains(err.Error(), "plaintext secret") {
		t.Fatalf("expected plaintext secret error, got %v", err)
	}
}

func TestRelayRouteRejectsNestedPlaintextMetadata(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "relay-route.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	err = database.CreateRelayRoute(context.Background(), RelayRouteRecord{
		RouteID:    "route-1",
		HostIDHash: "host-hash",
		Status:     "created",
		CreatedAt:  1,
		ExpiresAt:  2,
		Metadata:   map[string]any{"nested": map[string]any{"terminal_output": "pwd"}},
	})
	if err == nil {
		t.Fatal("expected relay route metadata with nested plaintext to be rejected")
	}
	if !strings.Contains(err.Error(), "plaintext secret") {
		t.Fatalf("expected plaintext secret error, got %v", err)
	}
}

func TestRelayRendezvousExpireAndReject(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "relay-expire.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if err := database.CreateRelayRendezvous(context.Background(), RelayRendezvousRecord{
		RendezvousID:     "rv-expired",
		HostIDHash:       "host-hash",
		SecretCommitment: "commitment",
		Status:           "created",
		CreatedAt:        1,
		ExpiresAt:        10,
	}); err != nil {
		t.Fatalf("CreateRelayRendezvous expired: %v", err)
	}
	if err := database.CreateRelayRendezvous(context.Background(), RelayRendezvousRecord{
		RendezvousID:     "rv-live",
		HostIDHash:       "host-hash",
		SecretCommitment: "commitment2",
		Status:           "created",
		CreatedAt:        1,
		ExpiresAt:        100,
	}); err != nil {
		t.Fatalf("CreateRelayRendezvous live: %v", err)
	}
	expired, err := database.ExpireRelayRendezvous(context.Background(), 20)
	if err != nil {
		t.Fatalf("ExpireRelayRendezvous: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expected one expired rendezvous, got %d", expired)
	}
	rejected, err := database.RejectRelayRendezvous(context.Background(), "rv-live", 21)
	if err != nil {
		t.Fatalf("RejectRelayRendezvous: %v", err)
	}
	if !rejected {
		t.Fatal("expected live rendezvous to reject")
	}
}

func TestSecureConnectionDeviceLookupByNoiseKey(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "secure.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	_, err = database.UpsertSecureConnectionDevice(context.Background(), SecureConnectionDeviceRecord{
		DeviceID:               "device-1",
		HostID:                 "host-1",
		DisplayName:            "Phone",
		Platform:               "ios",
		Role:                   "operator",
		Capabilities:           []string{"chat", "files"},
		TrustLevel:             "native-software",
		DeviceSigningPublicKey: "sign",
		DeviceNoisePublicKey:   "noise",
		EnrollmentCertificate:  []byte("cert"),
		EnrollmentEpoch:        1,
		Status:                 "active",
		CreatedAt:              1,
	})
	if err != nil {
		t.Fatalf("UpsertSecureConnectionDevice: %v", err)
	}
	rec, ok, err := database.FindSecureConnectionDeviceByNoiseKey(context.Background(), "host-1", "noise")
	if err != nil {
		t.Fatalf("FindSecureConnectionDeviceByNoiseKey: %v", err)
	}
	if !ok || rec.DeviceID != "device-1" || len(rec.Capabilities) != 2 {
		t.Fatalf("unexpected lookup result: ok=%v rec=%#v", ok, rec)
	}
}
