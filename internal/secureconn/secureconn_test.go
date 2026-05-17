package secureconn

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/db"
)

func TestPairingQREnforcesEntropyExpiryAndCommitment(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	secret, err := RandomBase64URL(32)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	payload := PairingQRCodeV1{
		Version:              ProtocolVersion,
		RelayOrigin:          "https://relay.or3.chat",
		RendezvousID:         "rv_test",
		HostID:               identity.HostID,
		HostDisplayName:      "Test Host",
		HostSigningPublicKey: identity.HostSigningPublicKey,
		HostNoisePublicKey:   identity.HostNoisePublicKey,
		PairingSecret:        secret,
		ExpiresAtUnixMs:      now.Add(time.Minute).UnixMilli(),
		Capabilities:         []string{"chat"},
		QRNonce:              "nonce",
	}
	encoded, err := EncodePairingQR(payload)
	if err != nil {
		t.Fatalf("EncodePairingQR: %v", err)
	}
	decoded, err := DecodePairingQR(encoded, now)
	if err != nil {
		t.Fatalf("DecodePairingQR: %v", err)
	}
	if decoded.PairingSecret != secret {
		t.Fatal("decoded QR pairing secret mismatch")
	}
	commitment, err := RendezvousCommitment(secret)
	if err != nil {
		t.Fatalf("RendezvousCommitment: %v", err)
	}
	if commitment == "" || commitment == secret {
		t.Fatal("commitment must be non-empty and must not equal the raw secret")
	}
	payload.PairingSecret = Base64URL([]byte("too-short"))
	if _, err := EncodePairingQR(payload); err == nil {
		t.Fatal("expected short pairing secret to be rejected")
	}
	payload.PairingSecret = secret
	payload.ExpiresAtUnixMs = now.Add(-time.Second).UnixMilli()
	if _, err := EncodePairingQR(payload); err == nil {
		t.Fatal("expected expired QR to be rejected")
	}
}

func TestEnrollmentCertificateRejectsTampering(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	deviceNoise, err := RandomBase64URL(32)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	deviceSign, err := RandomBase64URL(32)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-1",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{"chat", "terminal"},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	cert, err := NewEnrollmentCertificate(identity, proposal, RoleOperator, []string{"chat"}, TrustNativeSoftware, "acct", now.UnixMilli(), time.Time{}, now)
	if err != nil {
		t.Fatalf("NewEnrollmentCertificate: %v", err)
	}
	if err := VerifyEnrollmentCertificate(cert, now); err != nil {
		t.Fatalf("VerifyEnrollmentCertificate: %v", err)
	}
	cert.Role = RoleAdmin
	if err := VerifyEnrollmentCertificate(cert, now); err == nil {
		t.Fatal("expected tampered certificate role to fail signature verification")
	}
}

func TestReplayWindowRejectsDuplicatesAndOldFrames(t *testing.T) {
	window := NewReplayWindow(2)
	if err := window.Accept(1); err != nil {
		t.Fatalf("accept 1: %v", err)
	}
	if err := window.Accept(2); err != nil {
		t.Fatalf("accept 2: %v", err)
	}
	if err := window.Accept(2); err == nil {
		t.Fatal("expected duplicate sequence rejection")
	}
	if err := window.Accept(5); err != nil {
		t.Fatalf("accept 5: %v", err)
	}
	if err := window.Accept(1); err == nil {
		t.Fatal("expected stale sequence rejection")
	}
}

func TestSecureFrameRejectsOversizedBody(t *testing.T) {
	frame := SecureFrameV1{
		Version:       ProtocolVersion,
		Kind:          FrameControl,
		SessionID:     "session",
		Sequence:      1,
		CorrelationID: "corr",
		SentAtUnixMs:  time.Now().UTC().UnixMilli(),
		Body:          make([]byte, MaxSecureFrameBodyBytes+1),
	}
	if err := ValidateSecureFrame(frame, "session", nil, time.Now().UTC()); err == nil {
		t.Fatal("expected oversized frame body to be rejected")
	}
}

func TestSecureSessionLifecycleAuthorizationAndRekey(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	deviceNoise, err := RandomBase64URL(32)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	deviceSign, err := RandomBase64URL(32)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-session",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat, CapabilityTerminal},
		AccountBinding:         map[string]any{"accountId": "acct-1"},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	cert, err := NewEnrollmentCertificate(identity, proposal, RoleOperator, []string{CapabilityChat, CapabilityTerminal}, TrustNativeSoftware, "acct-1", now.UnixMilli(), time.Time{}, now)
	if err != nil {
		t.Fatalf("NewEnrollmentCertificate: %v", err)
	}
	certBytes, err := CanonicalBytes(cert)
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "secure-session.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	if _, err := database.UpsertSecureConnectionDevice(context.Background(), db.SecureConnectionDeviceRecord{
		DeviceID:               cert.DeviceID,
		HostID:                 cert.HostID,
		DisplayName:            "Phone",
		Platform:               PlatformIOS,
		Role:                   RoleOperator,
		Capabilities:           []string{CapabilityChat, CapabilityTerminal},
		TrustLevel:             TrustNativeSoftware,
		DeviceSigningPublicKey: cert.DeviceSigningPublicKey,
		DeviceNoisePublicKey:   cert.DeviceNoisePublicKey,
		EnrollmentCertificate:  certBytes,
		EnrollmentEpoch:        cert.EnrollmentEpoch,
		Status:                 StatusActive,
		CreatedAt:              now.UnixMilli(),
		AccountID:              "acct-1",
	}); err != nil {
		t.Fatalf("UpsertSecureConnectionDevice: %v", err)
	}
	hash, err := EnrollmentCertificateHash(cert)
	if err != nil {
		t.Fatalf("EnrollmentCertificateHash: %v", err)
	}
	manager := &SessionManager{DB: database, Identity: identity, Now: func() time.Time { return now }}
	claims, _, err := manager.StartVerifiedSession(context.Background(), SessionStartInput{
		DeviceID:                  "device-session",
		EnrollmentCertificateHash: hash,
		AccountID:                 "acct-1",
		StepUpAt:                  now,
		TTL:                       time.Minute,
	})
	if err != nil {
		t.Fatalf("StartVerifiedSession: %v", err)
	}
	frameBytes, err := EncodeSecureFrame(SecureFrameV1{
		Kind:          FrameControl,
		SessionID:     claims.SessionID,
		Sequence:      1,
		CorrelationID: "corr-1",
		Body:          []byte("opaque"),
	}, now)
	if err != nil {
		t.Fatalf("EncodeSecureFrame: %v", err)
	}
	if _, err := manager.ValidateFrame(context.Background(), claims.SessionID, frameBytes, NewReplayWindow(4)); err != nil {
		t.Fatalf("ValidateFrame: %v", err)
	}
	decision := AuthorizeAction(claims, ClassifyAction(httpMethodPost, "/internal/v1/terminal/input", ""), now)
	if !decision.Allowed {
		t.Fatalf("expected terminal action with step-up to be allowed: %#v", decision)
	}
	claims.StepUpAtUnixMs = 0
	decision = AuthorizeAction(claims, ClassifyAction(httpMethodPost, "/internal/v1/terminal/input", ""), now)
	if decision.Allowed || decision.Code != ErrorStepUpRequired {
		t.Fatalf("expected missing step-up denial, got %#v", decision)
	}
	webClaims := claims
	webClaims.TrustLevel = TrustWebLimited
	webClaims.Capabilities = []string{CapabilityFiles}
	webClaims.StepUpAtUnixMs = 0
	decision = AuthorizeAction(webClaims, ClassifyAction(httpMethodPost, "/internal/v1/files/write", ""), now)
	if decision.Allowed || decision.Code != ErrorStepUpRequired {
		t.Fatalf("expected web-limited mutation to require step-up, got %#v", decision)
	}
	if !ShouldRekey(RekeyPolicy{CreatedAt: now.Add(-DefaultRekeyAfter)}, now) {
		t.Fatal("expected duration-based rekey")
	}
	manager.Now = func() time.Time { return now.Add(2 * time.Minute) }
	expired, err := manager.ExpireStaleSessions(context.Background())
	if err != nil {
		t.Fatalf("ExpireStaleSessions: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expected one expired session, got %d", expired)
	}
}

func TestHostIdentityRecoveryBlocksSecureSessions(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "identity-recovery.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	if err := database.UpsertSecureConnectionHostIdentity(context.Background(), db.SecureConnectionHostIdentityRecord{
		HostID:               identity.HostID,
		HostSigningPublicKey: identity.HostSigningPublicKey,
		HostNoisePublicKey:   identity.HostNoisePublicKey,
		Fingerprint:          identity.Public().Fingerprint,
		Status:               ErrorHostIdentityChanged,
		CreatedAt:            now.UnixMilli(),
		RecoveryRequired:     true,
	}); err != nil {
		t.Fatalf("UpsertSecureConnectionHostIdentity: %v", err)
	}
	manager := &SessionManager{DB: database, Identity: identity, Now: func() time.Time { return now }}
	_, _, err = manager.StartVerifiedSession(context.Background(), SessionStartInput{DeviceID: "device"})
	if err == nil {
		t.Fatal("expected recovery-required host identity to block secure sessions")
	}
	if !strings.Contains(err.Error(), ErrorHostIdentityChanged) {
		t.Fatalf("expected host identity changed error, got %v", err)
	}
}

func TestEnrollmentApprovalRequiresMatchingAccountBinding(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "binding.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-binding",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: mustRandomBase64URL(t, 32),
		DeviceNoisePublicKey:   mustRandomBase64URL(t, 32),
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		AccountBinding:         map[string]any{"accountId": "acct-a"},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	store := &TrustStore{DB: database, Identity: identity, Now: func() time.Time { return now }}
	if _, _, err := store.ApproveEnrollment(context.Background(), proposal, RoleOperator, []string{CapabilityChat}, TrustNativeSoftware, "acct-b", time.Time{}); err == nil {
		t.Fatal("expected mismatched account binding to be rejected")
	}
	if _, _, err := store.ApproveEnrollment(context.Background(), proposal, RoleOperator, []string{CapabilityChat}, TrustNativeSoftware, "acct-a", time.Time{}); err != nil {
		t.Fatalf("expected matching account binding to be approved: %v", err)
	}
}

func TestWebEnrollmentRestrictionsAndPrivacyHelpers(t *testing.T) {
	now := time.Now().UTC()
	caps, expiresAt, trust := ApplyWebEnrollmentRestrictions(PlatformWeb, []string{CapabilityChat, CapabilityTerminal, CapabilitySecrets}, now.Add(7*24*time.Hour), now)
	if trust != TrustWebLimited {
		t.Fatalf("expected web-limited trust, got %q", trust)
	}
	if len(caps) != 1 || caps[0] != CapabilityChat {
		t.Fatalf("expected high-risk web capabilities to be stripped, got %#v", caps)
	}
	if expiresAt.After(now.Add(25 * time.Hour)) {
		t.Fatalf("expected web enrollment expiry to be capped, got %s", expiresAt)
	}
	if LegacyPairingAllowedForRemote(true, false) {
		t.Fatal("expected remote legacy pairing to require explicit override")
	}
	redacted := RedactSecureConnectionLogValue(map[string]any{
		"route_id":        "route-1",
		"pairing_secret":  "secret",
		"nested":          map[string]any{"terminal_output": "pwd"},
		"normal_metadata": "ok",
	}).(map[string]any)
	if redacted["pairing_secret"] != RedactedValue {
		t.Fatalf("expected pairing secret redaction, got %#v", redacted)
	}
	if redacted["route_id"] != "route-1" || redacted["normal_metadata"] != "ok" {
		t.Fatalf("expected non-sensitive metadata to remain, got %#v", redacted)
	}
	nested := redacted["nested"].(map[string]any)
	if nested["terminal_output"] != RedactedValue {
		t.Fatalf("expected nested terminal output redaction, got %#v", nested)
	}
	event := BuildTelemetryEvent("pairing", "failed", "expired", map[string]any{
		"host_id_hash": "host-hash",
		"device_hash":  "device-hash",
		"payload":      "plaintext",
	}, now)
	if event.Metadata["payload"] != RedactedValue {
		t.Fatalf("expected telemetry payload redaction, got %#v", event)
	}
	discovery := CurrentCapabilityDiscovery()
	if !discovery.QRPairingV2 || !discovery.EnrollmentCertificates || discovery.LegacyPairingRemote {
		t.Fatalf("unexpected discovery flags: %#v", discovery)
	}
}

func mustRandomBase64URL(t *testing.T, n int) string {
	t.Helper()
	value, err := RandomBase64URL(n)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	return value
}

const httpMethodPost = "POST"
