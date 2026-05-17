package secureconn

import (
	"testing"
	"time"
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
