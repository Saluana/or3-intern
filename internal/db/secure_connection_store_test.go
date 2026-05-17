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
