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
	consumed, err := database.ConsumeRelayRendezvous(context.Background(), "rv-expired", 22)
	if err != nil {
		t.Fatalf("ConsumeRelayRendezvous: %v", err)
	}
	if consumed {
		t.Fatal("expected expired rendezvous not to consume")
	}
}

func TestRelayMetadataRejectsUnsupportedFields(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "relay-metadata.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	err = database.CreateRelayRoute(context.Background(), RelayRouteRecord{
		RouteID:    "route-unsupported",
		HostIDHash: "host-hash",
		Status:     "created",
		CreatedAt:  1,
		ExpiresAt:  2,
		Metadata:   map[string]any{"notes": "should not be accepted"},
	})
	if err == nil {
		t.Fatal("expected unsupported relay metadata field to be rejected")
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

func TestPurgeTerminalPairingSessions(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "purge-pairing.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	for _, tc := range []struct {
		id       string
		status   string
		consumed int64
	}{
		{"rv-old-consumed", "consumed", 100},
		{"rv-old-expired", "expired", 200},
		{"rv-new-consumed", "consumed", 999},
		{"rv-active", "created", 0},
	} {
		if err := database.CreateSecureConnectionPairingSession(context.Background(), SecureConnectionPairingSessionRecord{
			RendezvousID:     tc.id,
			HostID:           "host-1",
			SecretCommitment: "commitment-" + tc.id,
			Status:           tc.status,
			RequestedRole:    "operator",
			CreatedAt:        1,
			ExpiresAt:        2,
			ConsumedAt:       tc.consumed,
		}); err != nil {
			t.Fatalf("Create %s: %v", tc.id, err)
		}
	}

	purged, err := database.PurgeTerminalPairingSessions(context.Background(), 500)
	if err != nil {
		t.Fatalf("PurgeTerminalPairingSessions: %v", err)
	}
	if purged != 2 {
		t.Fatalf("expected 2 purged, got %d", purged)
	}

	_, err = database.GetSecureConnectionPairingSession(context.Background(), "rv-new-consumed")
	if err != nil {
		t.Fatalf("Get rv-new-consumed: %v", err)
	}
	_, err = database.GetSecureConnectionPairingSession(context.Background(), "rv-active")
	if err != nil {
		t.Fatalf("Get rv-active: %v", err)
	}
}

func TestPurgeExpiredSecureConnectionSessions(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "purge-sessions.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if _, err := database.UpsertSecureConnectionDevice(context.Background(), SecureConnectionDeviceRecord{
		DeviceID:               "device-1",
		HostID:                 "host-1",
		DisplayName:            "Device",
		Platform:               "ios",
		Role:                   "operator",
		TrustLevel:             "native-software",
		DeviceSigningPublicKey: "sign-1",
		DeviceNoisePublicKey:   "noise-1",
		EnrollmentCertificate:  []byte("cert"),
		EnrollmentEpoch:        1,
		Status:                 "active",
		CreatedAt:              1,
	}); err != nil {
		t.Fatalf("Create device: %v", err)
	}

	for _, tc := range []struct {
		id       string
		status   string
		lastSeen int64
	}{
		{"s-old-expired", "expired", 100},
		{"s-old-revoked", "revoked", 200},
		{"s-new-expired", "expired", 999},
		{"s-active", "active", 500},
	} {
		if _, err := database.CreateSecureConnectionSession(context.Background(), SecureConnectionSessionRecord{
			SessionID:  tc.id,
			DeviceID:   "device-1",
			HostID:     "host-1",
			Status:     tc.status,
			CreatedAt:  1,
			LastSeenAt: tc.lastSeen,
			ExpiresAt:  2,
		}); err != nil {
			t.Fatalf("Create %s: %v", tc.id, err)
		}
	}

	purged, err := database.PurgeExpiredSecureConnectionSessions(context.Background(), 500)
	if err != nil {
		t.Fatalf("PurgeExpiredSecureConnectionSessions: %v", err)
	}
	if purged != 2 {
		t.Fatalf("expected 2 purged, got %d", purged)
	}
}

func TestPurgeRevokedDevices(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "purge-devices.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	for _, tc := range []struct {
		id      string
		status  string
		revoked int64
	}{
		{"d-old-revoked", "revoked", 100},
		{"d-new-revoked", "revoked", 999},
		{"d-active", "active", 0},
	} {
		if _, err := database.UpsertSecureConnectionDevice(context.Background(), SecureConnectionDeviceRecord{
			DeviceID:               tc.id,
			HostID:                 "host-1",
			DisplayName:            "Device",
			Platform:               "ios",
			Role:                   "operator",
			TrustLevel:             "native-software",
			DeviceSigningPublicKey: "sign-" + tc.id,
			DeviceNoisePublicKey:   "noise-" + tc.id,
			EnrollmentCertificate:  []byte("cert"),
			EnrollmentEpoch:        1,
			Status:                 tc.status,
			CreatedAt:              1,
			RevokedAt:              tc.revoked,
		}); err != nil {
			t.Fatalf("Create %s: %v", tc.id, err)
		}
	}

	purged, err := database.PurgeRevokedDevices(context.Background(), 500)
	if err != nil {
		t.Fatalf("PurgeRevokedDevices: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged, got %d", purged)
	}
}

func TestUpdateSecureConnectionSessionStepUpGuards(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "stepup-guards.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if _, err := database.UpsertSecureConnectionDevice(context.Background(), SecureConnectionDeviceRecord{
		DeviceID:               "device-1",
		HostID:                 "host-1",
		DisplayName:            "Device",
		Platform:               "ios",
		Role:                   "operator",
		TrustLevel:             "native-software",
		DeviceSigningPublicKey: "sign-1",
		DeviceNoisePublicKey:   "noise-1",
		EnrollmentCertificate:  []byte("cert"),
		EnrollmentEpoch:        1,
		Status:                 "active",
		CreatedAt:              1,
	}); err != nil {
		t.Fatalf("Create device: %v", err)
	}

	if _, err := database.CreateSecureConnectionSession(context.Background(), SecureConnectionSessionRecord{
		SessionID:  "s-expired",
		DeviceID:   "device-1",
		HostID:     "host-1",
		Status:     "active",
		CreatedAt:  1,
		LastSeenAt: 1,
		ExpiresAt:  10,
	}); err != nil {
		t.Fatalf("Create expired session: %v", err)
	}
	if _, err := database.CreateSecureConnectionSession(context.Background(), SecureConnectionSessionRecord{
		SessionID:  "s-active",
		DeviceID:   "device-1",
		HostID:     "host-1",
		Status:     "active",
		CreatedAt:  1,
		LastSeenAt: 1,
		ExpiresAt:  9999,
	}); err != nil {
		t.Fatalf("Create active session: %v", err)
	}

	err = database.UpdateSecureConnectionSessionStepUp(context.Background(), "s-expired", 20)
	if err == nil {
		t.Fatal("expected step-up on expired session to fail")
	}

	err = database.UpdateSecureConnectionSessionStepUp(context.Background(), "s-active", 20)
	if err != nil {
		t.Fatalf("expected step-up on active session to succeed: %v", err)
	}
}
