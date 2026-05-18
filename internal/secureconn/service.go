package secureconn

import (
	"context"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/db"
)

type TrustStore struct {
	DB       *db.DB
	Identity HostIdentity
	Now      func() time.Time
}

type EnrollmentApprovalInput struct {
	RendezvousID  string
	PairingSecret string
	Proposal      DeviceEnrollmentProposalV1
	TrustLevel    string
	ExpiresAt     time.Time
}

func (s *TrustStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *TrustStore) PinHostIdentity(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("secure connection trust store unavailable")
	}
	if err := s.Identity.Validate(); err != nil {
		return err
	}
	existing, ok, err := s.DB.GetSecureConnectionHostIdentity(ctx, s.Identity.HostID)
	if err != nil {
		return err
	}
	if ok {
		expected := HostIdentityPublic{
			HostID:               existing.HostID,
			HostSigningPublicKey: existing.HostSigningPublicKey,
			HostNoisePublicKey:   existing.HostNoisePublicKey,
			Fingerprint:          existing.Fingerprint,
		}
		if err := DetectHostIdentityReplacement(expected, s.Identity.Public()); err != nil {
			_ = s.DB.UpsertSecureConnectionHostIdentity(ctx, db.SecureConnectionHostIdentityRecord{
				HostID:               existing.HostID,
				HostSigningPublicKey: existing.HostSigningPublicKey,
				HostNoisePublicKey:   existing.HostNoisePublicKey,
				Fingerprint:          existing.Fingerprint,
				Status:               ErrorHostIdentityChanged,
				CreatedAt:            existing.CreatedAt,
				RotatedAt:            s.now().UnixMilli(),
				RecoveryRequired:     true,
				Metadata:             existing.Metadata,
			})
			return err
		}
		return nil
	}
	public := s.Identity.Public()
	return s.DB.UpsertSecureConnectionHostIdentity(ctx, db.SecureConnectionHostIdentityRecord{
		HostID:               public.HostID,
		HostSigningPublicKey: public.HostSigningPublicKey,
		HostNoisePublicKey:   public.HostNoisePublicKey,
		Fingerprint:          public.Fingerprint,
		Status:               StatusActive,
		CreatedAt:            public.CreatedAtUnixMs,
		Metadata:             map[string]any{"protocol_version": ProtocolVersion},
	})
}

func (s *TrustStore) CreatePairingIntent(ctx context.Context, input PairingIntent) (PairingIntentResult, error) {
	if s == nil || s.DB == nil {
		return PairingIntentResult{}, fmt.Errorf("secure connection trust store unavailable")
	}
	if err := s.PinHostIdentity(ctx); err != nil {
		return PairingIntentResult{}, err
	}
	now := s.now()
	expiresAt := input.ExpiresAt
	if expiresAt.IsZero() {
		ttl := input.TTL
		if ttl <= 0 {
			ttl = 2 * time.Minute
		}
		if ttl > 10*time.Minute {
			ttl = 10 * time.Minute
		}
		expiresAt = now.Add(ttl)
	}
	secret, err := RandomBase64URL(32)
	if err != nil {
		return PairingIntentResult{}, err
	}
	rendezvousID, err := RandomBase64URL(18)
	if err != nil {
		return PairingIntentResult{}, err
	}
	nonce, err := RandomBase64URL(16)
	if err != nil {
		return PairingIntentResult{}, err
	}
	role := NormalizeRole(input.RequestedRole)
	if role == "" {
		role = RoleOperator
	}
	payload := PairingQRCodeV1{
		Version:              ProtocolVersion,
		RelayOrigin:          strings.TrimSpace(input.RelayOrigin),
		RendezvousID:         rendezvousID,
		HostID:               s.Identity.HostID,
		HostDisplayName:      strings.TrimSpace(input.HostDisplayName),
		HostSigningPublicKey: s.Identity.HostSigningPublicKey,
		HostNoisePublicKey:   s.Identity.HostNoisePublicKey,
		PairingSecret:        secret,
		ExpiresAtUnixMs:      expiresAt.UTC().UnixMilli(),
		RequestedAccountID:   strings.TrimSpace(input.RequestedAccountID),
		Capabilities:         NormalizeCapabilities(input.Capabilities),
		QRNonce:              nonce,
	}
	encoded, err := EncodePairingQR(payload)
	if err != nil {
		return PairingIntentResult{}, err
	}
	commitment, err := RendezvousCommitment(secret)
	if err != nil {
		return PairingIntentResult{}, err
	}
	if err := s.DB.CreateSecureConnectionPairingSession(ctx, db.SecureConnectionPairingSessionRecord{
		RendezvousID:     rendezvousID,
		HostID:           s.Identity.HostID,
		SecretCommitment: commitment,
		Status:           StatusCreated,
		RequestedRole:    role,
		Capabilities:     payload.Capabilities,
		RelayOrigin:      payload.RelayOrigin,
		AccountID:        payload.RequestedAccountID,
		CreatedAt:        now.UnixMilli(),
		ExpiresAt:        payload.ExpiresAtUnixMs,
		Metadata:         map[string]any{"qr_nonce_hash": HashBase64URL([]byte(payload.QRNonce))},
	}); err != nil {
		return PairingIntentResult{}, err
	}
	return PairingIntentResult{Payload: payload, Encoded: encoded, SecretCommitment: commitment}, nil
}

func (s *TrustStore) ApproveEnrollment(ctx context.Context, proposal DeviceEnrollmentProposalV1, role string, capabilities []string, trustLevel, accountID string, expiresAt time.Time) (HostEnrollmentCertificateV1, db.SecureConnectionDeviceRecord, error) {
	if s == nil || s.DB == nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("secure connection trust store unavailable")
	}
	now := s.now()
	if err := VerifyEnrollmentProposalSignature(proposal, now); err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	if err := validateAccountBinding(proposal, accountID); err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	capabilities, expiresAt, restrictedTrust := ApplyWebEnrollmentRestrictions(proposal.Platform, capabilities, expiresAt, now)
	if restrictedTrust != "" {
		trustLevel = restrictedTrust
	}
	epoch := now.UnixMilli()
	cert, err := NewEnrollmentCertificate(s.Identity, proposal, role, capabilities, trustLevel, accountID, epoch, expiresAt, now)
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	certBytes, err := CanonicalBytes(cert)
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	record, err := s.DB.UpsertSecureConnectionDevice(ctx, db.SecureConnectionDeviceRecord{
		DeviceID:               cert.DeviceID,
		HostID:                 cert.HostID,
		DisplayName:            strings.TrimSpace(proposal.DeviceDisplayName),
		Platform:               NormalizePlatform(proposal.Platform),
		Role:                   cert.Role,
		Capabilities:           cert.Capabilities,
		TrustLevel:             cert.TrustLevel,
		DeviceSigningPublicKey: cert.DeviceSigningPublicKey,
		DeviceNoisePublicKey:   cert.DeviceNoisePublicKey,
		EnrollmentCertificate:  certBytes,
		EnrollmentEpoch:        cert.EnrollmentEpoch,
		Status:                 StatusActive,
		CreatedAt:              now.UnixMilli(),
		LastSeenAt:             now.UnixMilli(),
		AccountID:              cert.AccountID,
		Metadata:               map[string]any{"source": "secure-connections-v2"},
	})
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	return cert, record, nil
}

func (s *TrustStore) ApproveEnrollmentFromPairing(ctx context.Context, input EnrollmentApprovalInput) (HostEnrollmentCertificateV1, db.SecureConnectionDeviceRecord, error) {
	if s == nil || s.DB == nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("secure connection trust store unavailable")
	}
	rendezvousID := strings.TrimSpace(input.RendezvousID)
	if rendezvousID == "" {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("rendezvous ID required")
	}
	pairingSecret := strings.TrimSpace(input.PairingSecret)
	if pairingSecret == "" {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("pairing secret proof required")
	}
	session, err := s.DB.GetSecureConnectionPairingSession(ctx, rendezvousID)
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("pairing session not found: %w", err)
	}
	now := s.now()
	if session.HostID != s.Identity.HostID {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("pairing session belongs to a different host")
	}
	if session.ExpiresAt <= now.UnixMilli() {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, SecureConnectionError{Code: ErrorPairingExpired, SafeMessage: "This pairing code expired. Show a new one.", Retryable: true}
	}
	if session.Status != StatusCreated && session.Status != StatusJoined && session.Status != StatusPendingApproval {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("pairing session is not awaiting approval")
	}
	commitment, err := RendezvousCommitment(pairingSecret)
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	if !constantStringEqual(commitment, session.SecretCommitment) {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("pairing secret proof mismatch")
	}
	cert, rec, err := s.ApproveEnrollment(ctx, input.Proposal, session.RequestedRole, session.Capabilities, input.TrustLevel, session.AccountID, input.ExpiresAt)
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	ok, err := s.DB.CompareAndSwapSecureConnectionPairingStatus(ctx, rendezvousID, session.Status, StatusConsumed, now.UnixMilli())
	if err != nil {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, err
	}
	if !ok {
		return HostEnrollmentCertificateV1{}, db.SecureConnectionDeviceRecord{}, fmt.Errorf("pairing session was already consumed")
	}
	return cert, rec, nil
}

func (s *TrustStore) RevokeDevice(ctx context.Context, deviceID, reason string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("secure connection trust store unavailable")
	}
	now := s.now().UnixMilli()
	if err := s.DB.RevokeSecureConnectionDevice(ctx, deviceID, reason, now); err != nil {
		return err
	}
	_, err := s.DB.RevokeSecureConnectionSessionsByDevice(ctx, deviceID, now)
	return err
}

func (s *TrustStore) ListDevices(ctx context.Context, status string, limit int) ([]db.SecureConnectionDeviceRecord, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("secure connection trust store unavailable")
	}
	return s.DB.ListSecureConnectionDevices(ctx, s.Identity.HostID, status, limit)
}

func (s *TrustStore) GetDevice(ctx context.Context, deviceID string) (db.SecureConnectionDeviceRecord, error) {
	if s == nil || s.DB == nil {
		return db.SecureConnectionDeviceRecord{}, fmt.Errorf("secure connection trust store unavailable")
	}
	rec, err := s.DB.GetSecureConnectionDevice(ctx, deviceID)
	if err != nil {
		return db.SecureConnectionDeviceRecord{}, err
	}
	if rec.HostID != s.Identity.HostID {
		return db.SecureConnectionDeviceRecord{}, fmt.Errorf("device is not enrolled to this host")
	}
	return rec, nil
}

func (s *TrustStore) UpdateDeviceTrust(ctx context.Context, deviceID, role string, capabilities []string, trustLevel string) (db.SecureConnectionDeviceRecord, error) {
	rec, err := s.GetDevice(ctx, deviceID)
	if err != nil {
		return db.SecureConnectionDeviceRecord{}, err
	}
	if role = NormalizeRole(role); role == "" {
		role = rec.Role
	}
	if trustLevel = NormalizeTrustLevel(trustLevel, rec.Platform); trustLevel == "" {
		trustLevel = rec.TrustLevel
	}
	if capabilities == nil {
		capabilities = rec.Capabilities
	}
	rec.Role = role
	rec.Capabilities = NormalizeCapabilities(capabilities)
	rec.TrustLevel = trustLevel
	rec.LastSeenAt = s.now().UnixMilli()
	rec.Metadata = RedactSecureConnectionLogValue(rec.Metadata).(map[string]any)
	return s.DB.UpsertSecureConnectionDevice(ctx, rec)
}

func validateAccountBinding(proposal DeviceEnrollmentProposalV1, expectedAccountID string) error {
	expectedAccountID = strings.TrimSpace(expectedAccountID)
	if expectedAccountID == "" {
		return nil
	}
	if proposal.AccountBinding == nil {
		return fmt.Errorf("account binding proof required")
	}
	actual := ""
	for _, key := range []string{"accountId", "account_id", "sub"} {
		if value, ok := proposal.AccountBinding[key]; ok {
			actual = strings.TrimSpace(fmt.Sprint(value))
			break
		}
	}
	if actual == "" {
		return fmt.Errorf("account binding proof missing account ID")
	}
	if actual != expectedAccountID {
		return fmt.Errorf("account binding mismatch")
	}
	return nil
}
