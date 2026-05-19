package secureconn

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/db"

	"golang.org/x/crypto/curve25519"
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
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, _ := mustEd25519KeyPair(t)
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

func TestVerifyEnrollmentProposalSignatureRejectsUnsignedAndTamperedRequests(t *testing.T) {
	now := time.Now().UTC()
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, deviceSignPriv := mustEd25519KeyPair(t)
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-signed",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	if err := VerifyEnrollmentProposalSignature(proposal, now); err == nil {
		t.Fatal("expected unsigned proposal to be rejected")
	}
	proposal = mustSignEnrollmentProposal(t, proposal, deviceSignPriv)
	if err := VerifyEnrollmentProposalSignature(proposal, now); err != nil {
		t.Fatalf("expected signed proposal to verify: %v", err)
	}
	proposal.RequestedRole = RoleAdmin
	if err := VerifyEnrollmentProposalSignature(proposal, now); err == nil {
		t.Fatal("expected tampered signed proposal to be rejected")
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

func TestHostAcceptNoiseIKAndTransport(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	devicePriv, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes device: %v", err)
	}
	clampX25519Scalar(devicePriv)
	devicePub, err := curve25519.X25519(devicePriv, curve25519.Basepoint)
	if err != nil {
		t.Fatalf("device public: %v", err)
	}
	ephemeralPriv, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes ephemeral: %v", err)
	}
	clampX25519Scalar(ephemeralPriv)
	ephemeralPub, err := curve25519.X25519(ephemeralPriv, curve25519.Basepoint)
	if err != nil {
		t.Fatalf("ephemeral public: %v", err)
	}
	prologue := SessionPrologueV1{
		Protocol:                  "or3-secure-runtime",
		Version:                   ProtocolVersion,
		RouteID:                   "route",
		HostID:                    identity.HostID,
		DeviceIDHash:              HashBase64URL([]byte("device")),
		EnrollmentCertificateHash: "cert-hash",
		MinProtocolVersion:        ProtocolVersion,
		MaxProtocolVersion:        ProtocolVersion,
	}
	prologueBytes, _ := CanonicalBytes(prologue)
	result, err := HostAcceptNoiseIK(identity, NoiseHandshakeInitV1{
		Version:                 ProtocolVersion,
		PrologueHash:            HashBase64URL([]byte("OR3-NOISE-PROLOGUE-V1"), prologueBytes),
		DeviceID:                "device",
		DeviceNoisePublicKey:    Base64URL(devicePub),
		DeviceEphemeralKey:      Base64URL(ephemeralPub),
		EnrollmentCertHash:      "cert-hash",
		EncryptedInitialPayload: "",
	}, prologue)
	if err != nil {
		t.Fatalf("HostAcceptNoiseIK: %v", err)
	}
	ciphertext, err := SealNoiseTransport(result.SessionKey, []byte("aad"), []byte("hello"))
	if err != nil {
		t.Fatalf("SealNoiseTransport: %v", err)
	}
	plaintext, err := OpenNoiseTransport(result.SessionKey, []byte("aad"), ciphertext)
	if err != nil {
		t.Fatalf("OpenNoiseTransport: %v", err)
	}
	if string(plaintext) != "hello" {
		t.Fatalf("unexpected plaintext %q", plaintext)
	}
}

func TestSecureSessionLifecycleAuthorizationAndRekey(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, _ := mustEd25519KeyPair(t)
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
	routeID := "route-session"
	handshake := mustNoiseHandshakeInit(t, identity, proposal.DeviceID, deviceNoise, hash, routeID, "https://relay.or3.chat", "acct-1")
	manager := &SessionManager{DB: database, Identity: identity, Now: func() time.Time { return now }}
	claims, _, err := manager.StartVerifiedSession(context.Background(), SessionStartInput{
		DeviceID:                  "device-session",
		RelayRouteID:              routeID,
		RelayOrigin:               "https://relay.or3.chat",
		EnrollmentCertificateHash: hash,
		AccountID:                 "acct-1",
		NoiseHandshake:            handshake,
		AuthenticatedStepUpAt:     now,
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
	if _, err := manager.ValidateFrame(context.Background(), claims.SessionID, frameBytes, nil); err != nil {
		t.Fatalf("ValidateFrame: %v", err)
	}
	if _, err := manager.ValidateFrame(context.Background(), claims.SessionID, frameBytes, nil); err == nil {
		t.Fatal("expected duplicate frame without replay window state to be rejected by persisted sequence tracking")
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

func TestApproveEnrollmentFromPairingRequiresSecretAndConsumesSession(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "pairing-approve.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	secret, err := RandomBase64URL(32)
	if err != nil {
		t.Fatalf("RandomBase64URL: %v", err)
	}
	commitment, err := RendezvousCommitment(secret)
	if err != nil {
		t.Fatalf("RendezvousCommitment: %v", err)
	}
	if err := database.CreateSecureConnectionPairingSession(context.Background(), db.SecureConnectionPairingSessionRecord{
		RendezvousID:     "rv-approve",
		HostID:           identity.HostID,
		SecretCommitment: commitment,
		Status:           StatusJoined,
		RequestedRole:    RoleOperator,
		Capabilities:     []string{CapabilityChat},
		RelayOrigin:      "https://relay.or3.chat",
		AccountID:        "acct-approve",
		CreatedAt:        now.UnixMilli(),
		ExpiresAt:        now.Add(time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("CreateSecureConnectionPairingSession: %v", err)
	}
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, deviceSignPriv := mustEd25519KeyPair(t)
	proposal := mustSignEnrollmentProposal(t, DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-approve",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		AccountBinding:         map[string]any{"accountId": "acct-approve"},
		CreatedAtUnixMs:        now.UnixMilli(),
	}, deviceSignPriv)
	store := &TrustStore{DB: database, Identity: identity, Now: func() time.Time { return now }}
	if _, _, err := store.ApproveEnrollmentFromPairing(context.Background(), EnrollmentApprovalInput{
		RendezvousID:  "rv-approve",
		PairingSecret: mustRandomBase64URL(t, 32),
		Proposal:      proposal,
		TrustLevel:    TrustNativeSoftware,
	}); err == nil {
		t.Fatal("expected wrong pairing secret to be rejected")
	}
	cert, rec, err := store.ApproveEnrollmentFromPairing(context.Background(), EnrollmentApprovalInput{
		RendezvousID:  "rv-approve",
		PairingSecret: secret,
		Proposal:      proposal,
		TrustLevel:    TrustNativeSoftware,
	})
	if err != nil {
		t.Fatalf("expected pairing approval to succeed: %v", err)
	}
	if cert.DeviceID != proposal.DeviceID || rec.DeviceID != proposal.DeviceID {
		t.Fatalf("expected approved device to match proposal, got cert=%q rec=%q", cert.DeviceID, rec.DeviceID)
	}
	pairingSession, err := database.GetSecureConnectionPairingSession(context.Background(), "rv-approve")
	if err != nil {
		t.Fatalf("GetSecureConnectionPairingSession: %v", err)
	}
	if pairingSession.Status != StatusConsumed {
		t.Fatalf("expected pairing session to be consumed, got %q", pairingSession.Status)
	}
	if _, _, err := store.ApproveEnrollmentFromPairing(context.Background(), EnrollmentApprovalInput{
		RendezvousID:  "rv-approve",
		PairingSecret: secret,
		Proposal:      proposal,
		TrustLevel:    TrustNativeSoftware,
	}); err == nil {
		t.Fatal("expected consumed pairing session to reject replayed approval")
	}
}

func TestLoadActiveSessionClaimsRejectsRevokedDevice(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "active-session.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, _ := mustEd25519KeyPair(t)
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-active",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	cert, err := NewEnrollmentCertificate(identity, proposal, RoleOperator, []string{CapabilityChat}, TrustNativeSoftware, "acct-active", now.UnixMilli(), time.Time{}, now)
	if err != nil {
		t.Fatalf("NewEnrollmentCertificate: %v", err)
	}
	certBytes, err := CanonicalBytes(cert)
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	if _, err := database.UpsertSecureConnectionDevice(context.Background(), db.SecureConnectionDeviceRecord{
		DeviceID:               cert.DeviceID,
		HostID:                 cert.HostID,
		DisplayName:            "Phone",
		Platform:               PlatformIOS,
		Role:                   RoleOperator,
		Capabilities:           []string{CapabilityChat},
		TrustLevel:             TrustNativeSoftware,
		DeviceSigningPublicKey: cert.DeviceSigningPublicKey,
		DeviceNoisePublicKey:   cert.DeviceNoisePublicKey,
		EnrollmentCertificate:  certBytes,
		EnrollmentEpoch:        cert.EnrollmentEpoch,
		Status:                 StatusActive,
		CreatedAt:              now.UnixMilli(),
		AccountID:              "acct-active",
	}); err != nil {
		t.Fatalf("UpsertSecureConnectionDevice: %v", err)
	}
	if _, err := database.CreateSecureConnectionSession(context.Background(), db.SecureConnectionSessionRecord{
		SessionID:       "session-active",
		DeviceID:        cert.DeviceID,
		HostID:          identity.HostID,
		RelayRouteID:    "route-active",
		EnrollmentEpoch: cert.EnrollmentEpoch,
		Status:          StatusActive,
		CreatedAt:       now.UnixMilli(),
		LastSeenAt:      now.UnixMilli(),
		ExpiresAt:       now.Add(time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("CreateSecureConnectionSession: %v", err)
	}
	manager := &SessionManager{DB: database, Identity: identity, Now: func() time.Time { return now }}
	if _, err := manager.LoadActiveSessionClaims(context.Background(), "session-active"); err == nil {
		t.Fatal("expected active session without claim MAC to be rejected")
	}
	if err := database.RevokeSecureConnectionDevice(context.Background(), cert.DeviceID, "test revoke", now.UnixMilli()); err != nil {
		t.Fatalf("RevokeSecureConnectionDevice: %v", err)
	}
	if _, err := manager.LoadActiveSessionClaims(context.Background(), "session-active"); err == nil {
		t.Fatal("expected revoked device to block active session claims")
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
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, deviceSignPriv := mustEd25519KeyPair(t)
	proposal := mustSignEnrollmentProposal(t, DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-binding",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		AccountBinding:         map[string]any{"accountId": "acct-a"},
		CreatedAtUnixMs:        now.UnixMilli(),
	}, deviceSignPriv)
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

func mustEd25519KeyPair(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return Base64URL(pub), priv
}

func mustX25519KeyPair(t *testing.T) (string, []byte) {
	t.Helper()
	priv, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	clampX25519Scalar(priv)
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		t.Fatalf("curve25519.X25519: %v", err)
	}
	return Base64URL(pub), priv
}

func mustSignEnrollmentProposal(t *testing.T, proposal DeviceEnrollmentProposalV1, priv ed25519.PrivateKey) DeviceEnrollmentProposalV1 {
	t.Helper()
	encoded, err := EnrollmentProposalSigningBytes(proposal)
	if err != nil {
		t.Fatalf("EnrollmentProposalSigningBytes: %v", err)
	}
	proposal.Signature = Base64URL(ed25519.Sign(priv, encoded))
	return proposal
}

func mustNoiseHandshakeInit(t *testing.T, identity HostIdentity, deviceID, deviceNoisePublicKey, certHash, routeID, relayOrigin, accountID string) NoiseHandshakeInitV1 {
	t.Helper()
	ephemeralPriv, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	clampX25519Scalar(ephemeralPriv)
	ephemeralPub, err := curve25519.X25519(ephemeralPriv, curve25519.Basepoint)
	if err != nil {
		t.Fatalf("curve25519.X25519: %v", err)
	}
	prologue := SessionPrologueV1{
		Protocol:                  "or3-secure-runtime",
		Version:                   ProtocolVersion,
		RelayOrigin:               relayOrigin,
		RouteID:                   routeID,
		HostID:                    identity.HostID,
		DeviceIDHash:              HashBase64URL([]byte(deviceID)),
		EnrollmentCertificateHash: certHash,
		AccountID:                 accountID,
		MinProtocolVersion:        ProtocolVersion,
		MaxProtocolVersion:        ProtocolVersion,
	}
	prologueBytes, err := CanonicalBytes(prologue)
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	return NoiseHandshakeInitV1{
		Version:              ProtocolVersion,
		PrologueHash:         HashBase64URL([]byte("OR3-NOISE-PROLOGUE-V1"), prologueBytes),
		DeviceID:             deviceID,
		DeviceNoisePublicKey: deviceNoisePublicKey,
		DeviceEphemeralKey:   Base64URL(ephemeralPub),
		EnrollmentCertHash:   certHash,
	}
}

const httpMethodPost = "POST"

func TestNoiseSessionKeyDerivationIsDeterministic(t *testing.T) {
	prologueHash := "test-prologue-hash"
	transcript := "test-transcript"
	es := make([]byte, 32)
	ss := make([]byte, 32)
	for i := range es {
		es[i] = byte(i)
		ss[i] = byte(i + 32)
	}
	key1, err := deriveNoiseSessionKey(prologueHash, transcript, es, ss)
	if err != nil {
		t.Fatalf("deriveNoiseSessionKey 1: %v", err)
	}
	key2, err := deriveNoiseSessionKey(prologueHash, transcript, es, ss)
	if err != nil {
		t.Fatalf("deriveNoiseSessionKey 2: %v", err)
	}
	if !bytesEqual(key1, key2) {
		t.Fatal("expected deterministic session key derivation")
	}
	if len(key1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key1))
	}
}

func TestNoiseSessionKeyVerifyMatchesDerivation(t *testing.T) {
	prologueHash := "test-prologue"
	transcript := "test-transcript"
	es := make([]byte, 32)
	ss := make([]byte, 32)
	key, err := deriveNoiseSessionKey(prologueHash, transcript, es, ss)
	if err != nil {
		t.Fatalf("deriveNoiseSessionKey: %v", err)
	}
	if !VerifySessionKeyDerivation(prologueHash, transcript, es, ss, key) {
		t.Fatal("expected VerifySessionKeyDerivation to accept correct key")
	}
	if VerifySessionKeyDerivation(prologueHash, transcript, es, ss, make([]byte, 32)) {
		t.Fatal("expected VerifySessionKeyDerivation to reject wrong key")
	}
}

func TestNoiseSessionKeyDiffersForDifferentInputs(t *testing.T) {
	es := make([]byte, 32)
	ss := make([]byte, 32)
	keyA, _ := deriveNoiseSessionKey("hash-a", "transcript-a", es, ss)
	keyB, _ := deriveNoiseSessionKey("hash-b", "transcript-a", es, ss)
	keyC, _ := deriveNoiseSessionKey("hash-a", "transcript-c", es, ss)
	if bytesEqual(keyA, keyB) {
		t.Fatal("expected different keys for different prologue hashes")
	}
	if bytesEqual(keyA, keyC) {
		t.Fatal("expected different keys for different transcripts")
	}
}

func TestHandshakeInterfaceAcceptsValidInput(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	devicePubStr, devicePriv := mustX25519KeyPair(t)
	devicePub := Base64URL(func() []byte {
		pub, _ := curve25519.X25519(devicePriv, curve25519.Basepoint)
		return pub
	}())
	if devicePub != devicePubStr {
		t.Fatal("test setup: public key mismatch")
	}
	ephemeralPriv, _ := RandomBytes(32)
	clampX25519Scalar(ephemeralPriv)
	ephemeralPub, _ := curve25519.X25519(ephemeralPriv, curve25519.Basepoint)
	prologue := SessionPrologueV1{
		Protocol:                  "or3-secure-runtime",
		Version:                   ProtocolVersion,
		RouteID:                   "route",
		HostID:                    identity.HostID,
		DeviceIDHash:              HashBase64URL([]byte("device")),
		EnrollmentCertificateHash: "cert-hash",
		MinProtocolVersion:        ProtocolVersion,
		MaxProtocolVersion:        ProtocolVersion,
	}
	prologueBytes, _ := CanonicalBytes(prologue)
	init := NoiseHandshakeInitV1{
		Version:              ProtocolVersion,
		PrologueHash:         HashBase64URL([]byte("OR3-NOISE-PROLOGUE-V1"), prologueBytes),
		DeviceID:             "device",
		DeviceNoisePublicKey: devicePub,
		DeviceEphemeralKey:   Base64URL(ephemeralPub),
		EnrollmentCertHash:   "cert-hash",
	}
	var handshake NoiseHandshake = NoiseHandshakeIKV1{}
	result, err := handshake.Accept(identity, init, prologue)
	if err != nil {
		t.Fatalf("NoiseHandshakeIKV1.Accept: %v", err)
	}
	if len(result.SessionKey) != 32 {
		t.Fatalf("expected 32-byte session key, got %d", len(result.SessionKey))
	}
	if result.Transcript == "" {
		t.Fatal("expected non-empty transcript")
	}
}

func TestFrameTimestampRejectsFutureSkewBeyond60Seconds(t *testing.T) {
	now := time.Now().UTC()
	// Frame sent 61 seconds in the FUTURE should be rejected
	frame := SecureFrameV1{
		Version:       ProtocolVersion,
		Kind:          FrameControl,
		SessionID:     "session",
		Sequence:      1,
		CorrelationID: "corr",
		SentAtUnixMs:  now.Add(61 * time.Second).UnixMilli(),
		Body:          []byte("test"),
	}
	if err := ValidateSecureFrame(frame, "session", nil, now); err == nil {
		t.Fatal("expected frame with >60s future skew to be rejected")
	}
	// Frame sent 61 seconds in the PAST should be accepted (within 24h window)
	frame.SentAtUnixMs = now.Add(-61 * time.Second).UnixMilli()
	if err := ValidateSecureFrame(frame, "session", nil, now); err != nil {
		t.Fatalf("expected frame with 61s past skew to be accepted: %v", err)
	}
	// Frame sent 30 seconds in the future should be accepted
	frame.SentAtUnixMs = now.Add(30 * time.Second).UnixMilli()
	if err := ValidateSecureFrame(frame, "session", nil, now); err != nil {
		t.Fatalf("expected frame with 30s future skew to be accepted: %v", err)
	}
	// Frame sent 25 hours in the past should be rejected
	frame.SentAtUnixMs = now.Add(-25 * time.Hour).UnixMilli()
	if err := ValidateSecureFrame(frame, "session", nil, now); err == nil {
		t.Fatal("expected frame with >24h past skew to be rejected")
	}
}

func TestReplayWindowUsesDefaultCapacity(t *testing.T) {
	w := NewReplayWindow(0)
	if w.capacity != DefaultReplayWindowCap {
		t.Fatalf("expected default capacity %d, got %d", DefaultReplayWindowCap, w.capacity)
	}
	w2 := NewReplayWindow(-1)
	if w2.capacity != DefaultReplayWindowCap {
		t.Fatalf("expected default capacity %d for negative input, got %d", DefaultReplayWindowCap, w2.capacity)
	}
}

func TestClaimIntegrityMACTamperDetection(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "claim-mac.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, _ := mustEd25519KeyPair(t)
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-mac",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	cert, err := NewEnrollmentCertificate(identity, proposal, RoleOperator, []string{CapabilityChat}, TrustNativeSoftware, "acct-mac", now.UnixMilli(), time.Time{}, now)
	if err != nil {
		t.Fatalf("NewEnrollmentCertificate: %v", err)
	}
	certBytes, _ := CanonicalBytes(cert)
	if _, err := database.UpsertSecureConnectionDevice(context.Background(), db.SecureConnectionDeviceRecord{
		DeviceID:               cert.DeviceID,
		HostID:                 cert.HostID,
		DisplayName:            "Phone",
		Platform:               PlatformIOS,
		Role:                   RoleOperator,
		Capabilities:           []string{CapabilityChat},
		TrustLevel:             TrustNativeSoftware,
		DeviceSigningPublicKey: cert.DeviceSigningPublicKey,
		DeviceNoisePublicKey:   cert.DeviceNoisePublicKey,
		EnrollmentCertificate:  certBytes,
		EnrollmentEpoch:        cert.EnrollmentEpoch,
		Status:                 StatusActive,
		CreatedAt:              now.UnixMilli(),
		AccountID:              "acct-mac",
	}); err != nil {
		t.Fatalf("UpsertSecureConnectionDevice: %v", err)
	}
	if _, err := database.CreateSecureConnectionSession(context.Background(), db.SecureConnectionSessionRecord{
		SessionID:       "session-mac",
		DeviceID:        cert.DeviceID,
		HostID:          identity.HostID,
		RelayRouteID:    "route-mac",
		EnrollmentEpoch: cert.EnrollmentEpoch,
		Status:          StatusActive,
		CreatedAt:       now.UnixMilli(),
		LastSeenAt:      now.UnixMilli(),
		ExpiresAt:       now.Add(time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("CreateSecureConnectionSession: %v", err)
	}
	manager := &SessionManager{DB: database, Identity: identity, Now: func() time.Time { return now }}
	claims, _, err := manager.StartVerifiedSession(context.Background(), SessionStartInput{
		DeviceID:     "device-mac",
		RelayRouteID: "route-mac",
		RelayOrigin:  "https://relay.or3.chat",
		AccountID:    "acct-mac",
		NoiseHandshake: mustNoiseHandshakeInit(t, identity, "device-mac", deviceNoise, func() string {
			h, _ := EnrollmentCertificateHash(cert)
			return h
		}(), "route-mac", "https://relay.or3.chat", "acct-mac"),
		TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("StartVerifiedSession: %v", err)
	}
	loaded, err := manager.LoadActiveSessionClaims(context.Background(), claims.SessionID)
	if err != nil {
		t.Fatalf("LoadActiveSessionClaims: %v", err)
	}
	if loaded.Role != claims.Role {
		t.Fatalf("expected role %q, got %q", claims.Role, loaded.Role)
	}
	stepUpAt := now.Add(30 * time.Second)
	manager.Now = func() time.Time { return stepUpAt }
	updated, err := manager.UpdateStepUp(context.Background(), claims.SessionID, stepUpAt)
	if err != nil {
		t.Fatalf("UpdateStepUp: %v", err)
	}
	if updated.StepUpAtUnixMs != stepUpAt.UnixMilli() {
		t.Fatalf("expected step-up %d, got %d", stepUpAt.UnixMilli(), updated.StepUpAtUnixMs)
	}
	if _, err := manager.LoadActiveSessionClaims(context.Background(), claims.SessionID); err != nil {
		t.Fatalf("LoadActiveSessionClaims after step-up: %v", err)
	}
	// Tamper with the stored MAC to simulate DB compromise.
	_, err = database.SQL.ExecContext(context.Background(),
		`UPDATE secure_connection_sessions SET metadata_json=json_set(COALESCE(metadata_json,'{}'), '$.claim_mac', 'tampered-mac') WHERE session_id=?`,
		claims.SessionID)
	if err != nil {
		t.Fatalf("tamper metadata: %v", err)
	}
	_, err = manager.LoadActiveSessionClaims(context.Background(), claims.SessionID)
	if err == nil {
		t.Fatal("expected tampered claim MAC to be rejected")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNormalizeRoleEmptyReturnsEmpty(t *testing.T) {
	if NormalizeRole("") != "" {
		t.Fatal("expected empty role to return empty string")
	}
	if NormalizeRole("  ") != "" {
		t.Fatal("expected whitespace role to return empty string")
	}
	if NormalizeRole("unknown") != "" {
		t.Fatal("expected unknown role to return empty string")
	}
	if NormalizeRole("operator") != RoleOperator {
		t.Fatalf("expected operator, got %q", NormalizeRole("operator"))
	}
	if NormalizeRole("viewer") != RoleViewer {
		t.Fatalf("expected viewer, got %q", NormalizeRole("viewer"))
	}
	if NormalizeRole("admin") != RoleAdmin {
		t.Fatalf("expected admin, got %q", NormalizeRole("admin"))
	}
}

func TestClassifyActionUnknownPathFailsClosed(t *testing.T) {
	action := ClassifyAction("", "", "")
	if action.Class != ActionMutate {
		t.Fatalf("expected empty inputs to classify as mutate (restrictive), got %q", action.Class)
	}
	if action.Capability != CapabilityFiles {
		t.Fatalf("expected files capability for empty inputs, got %q", action.Capability)
	}

	action = ClassifyAction("GET", "/api/v1/something/unknown", "")
	if action.Class != ActionView {
		t.Fatalf("expected GET to classify as view, got %q", action.Class)
	}

	action = ClassifyAction("POST", "/api/v1/something/unknown", "")
	if action.Class != ActionMutate {
		t.Fatalf("expected POST to classify as mutate, got %q", action.Class)
	}
}

func TestValidateStepUpUpdateRejectsFutureTimestamps(t *testing.T) {
	now := time.Now().UTC()
	claims := SessionClaims{}

	_, err := ValidateStepUpUpdate(claims, now.Add(10*time.Second), now)
	if err == nil {
		t.Fatal("expected future step-up timestamp to be rejected")
	}

	_, err = ValidateStepUpUpdate(claims, now.Add(6*time.Second), now)
	if err == nil {
		t.Fatal("expected 6s future step-up to be rejected (beyond 5s tolerance)")
	}

	updated, err := ValidateStepUpUpdate(claims, now, now)
	if err != nil {
		t.Fatalf("expected current step-up to succeed: %v", err)
	}
	if updated.StepUpAtUnixMs != now.UnixMilli() {
		t.Fatal("expected step-up timestamp to be set")
	}
}

func TestSessionLifecyclePurge(t *testing.T) {
	now := time.Now().UTC()
	identity, err := NewHostIdentity("Test Host", now)
	if err != nil {
		t.Fatalf("NewHostIdentity: %v", err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "purge-lifecycle.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	deviceNoise, _ := mustX25519KeyPair(t)
	deviceSign, _ := mustEd25519KeyPair(t)
	proposal := DeviceEnrollmentProposalV1{
		Version:                ProtocolVersion,
		DeviceID:               "device-purge",
		DeviceDisplayName:      "Phone",
		Platform:               PlatformIOS,
		DeviceSigningPublicKey: deviceSign,
		DeviceNoisePublicKey:   deviceNoise,
		RequestedRole:          RoleOperator,
		RequestedCapabilities:  []string{CapabilityChat},
		CreatedAtUnixMs:        now.UnixMilli(),
	}
	cert, err := NewEnrollmentCertificate(identity, proposal, RoleOperator, []string{CapabilityChat}, TrustNativeSoftware, "acct-purge", now.UnixMilli(), time.Time{}, now)
	if err != nil {
		t.Fatalf("NewEnrollmentCertificate: %v", err)
	}
	certBytes, _ := CanonicalBytes(cert)
	if _, err := database.UpsertSecureConnectionDevice(context.Background(), db.SecureConnectionDeviceRecord{
		DeviceID:               cert.DeviceID,
		HostID:                 cert.HostID,
		DisplayName:            "Phone",
		Platform:               PlatformIOS,
		Role:                   RoleOperator,
		Capabilities:           []string{CapabilityChat},
		TrustLevel:             TrustNativeSoftware,
		DeviceSigningPublicKey: cert.DeviceSigningPublicKey,
		DeviceNoisePublicKey:   cert.DeviceNoisePublicKey,
		EnrollmentCertificate:  certBytes,
		EnrollmentEpoch:        cert.EnrollmentEpoch,
		Status:                 StatusActive,
		CreatedAt:              now.UnixMilli(),
		AccountID:              "acct-purge",
	}); err != nil {
		t.Fatalf("UpsertSecureConnectionDevice: %v", err)
	}

	hash, _ := EnrollmentCertificateHash(cert)
	manager := &SessionManager{DB: database, Identity: identity, Now: func() time.Time { return now }}
	claims, _, err := manager.StartVerifiedSession(context.Background(), SessionStartInput{
		DeviceID:                  "device-purge",
		RelayRouteID:              "route-purge",
		RelayOrigin:               "https://relay.or3.chat",
		EnrollmentCertificateHash: hash,
		AccountID:                 "acct-purge",
		NoiseHandshake:            mustNoiseHandshakeInit(t, identity, "device-purge", deviceNoise, hash, "route-purge", "https://relay.or3.chat", "acct-purge"),
		TTL:                       time.Minute,
	})
	if err != nil {
		t.Fatalf("StartVerifiedSession: %v", err)
	}
	if claims.Role != RoleOperator {
		t.Fatalf("expected operator role, got %q", claims.Role)
	}

	// Expire the session
	manager.Now = func() time.Time { return now.Add(2 * time.Minute) }
	expired, err := manager.ExpireStaleSessions(context.Background())
	if err != nil {
		t.Fatalf("ExpireStaleSessions: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}

	// Purge should find nothing (session just expired, retention is 7 days)
	manager.Now = func() time.Time { return now.Add(3 * time.Minute) }
	result, err := manager.PurgeStaleRecords(context.Background())
	if err != nil {
		t.Fatalf("PurgeStaleRecords: %v", err)
	}
	if result.ExpiredSessions != 0 {
		t.Fatalf("expected 0 purged sessions (within retention), got %d", result.ExpiredSessions)
	}

	// After retention period, purge should clean up
	manager.Now = func() time.Time { return now.Add(8 * 24 * time.Hour) }
	result, err = manager.PurgeStaleRecords(context.Background())
	if err != nil {
		t.Fatalf("PurgeStaleRecords: %v", err)
	}
	if result.ExpiredSessions != 1 {
		t.Fatalf("expected 1 purged session after retention, got %d", result.ExpiredSessions)
	}
}
