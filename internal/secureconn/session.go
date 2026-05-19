package secureconn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/db"
)

const (
	DefaultSessionTTL      = 20 * time.Minute
	DefaultStepUpTTL       = 5 * time.Minute
	DefaultRekeyAfter      = 10 * time.Minute
	DefaultRekeyMessages   = uint64(8192)
	DefaultRekeyBytes      = uint64(32 * 1024 * 1024)
	DefaultReplayWindowCap = 512
)

type SessionClaims struct {
	HostID          string   `json:"host_id"`
	DeviceID        string   `json:"device_id"`
	EnrollmentEpoch int64    `json:"enrollment_epoch"`
	Role            string   `json:"role"`
	Capabilities    []string `json:"capabilities"`
	TrustLevel      string   `json:"trust_level"`
	SessionID       string   `json:"session_id"`
	RelayRouteID    string   `json:"relay_route_id,omitempty"`
	AccountID       string   `json:"account_id,omitempty"`
	StepUpAtUnixMs  int64    `json:"step_up_at_unix_ms,omitempty"`
	IssuedAtUnixMs  int64    `json:"issued_at_unix_ms"`
	ExpiresAtUnixMs int64    `json:"expires_at_unix_ms"`
}

type SessionStartInput struct {
	DeviceID                  string
	DeviceNoisePublicKey      string
	RelayRouteID              string
	RelayOrigin               string
	EnrollmentCertificateHash string
	AccountID                 string
	NoiseHandshake            NoiseHandshakeInitV1
	AuthenticatedStepUpAt     time.Time
	TTL                       time.Duration
}

type RekeyPolicy struct {
	CreatedAt     time.Time
	LastRekeyAt   time.Time
	MessageCount  uint64
	ByteCount     uint64
	Force         bool
	ProtocolEpoch string
}

type SessionManager struct {
	DB        *db.DB
	Identity  HostIdentity
	Now       func() time.Time
	Handshake NoiseHandshake
}

func (m *SessionManager) handshake() NoiseHandshake {
	if m != nil && m.Handshake != nil {
		return m.Handshake
	}
	return NoiseHandshakeIKV1{}
}

func (m *SessionManager) now() time.Time {
	if m != nil && m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m *SessionManager) StartVerifiedSession(ctx context.Context, input SessionStartInput) (SessionClaims, db.SecureConnectionSessionRecord, error) {
	if m == nil || m.DB == nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("secure session manager unavailable")
	}
	if err := m.Identity.Validate(); err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	if rec, ok, err := m.DB.GetSecureConnectionHostIdentity(ctx, m.Identity.HostID); err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	} else if ok && (rec.RecoveryRequired || rec.Status == ErrorHostIdentityChanged) {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, SecureConnectionError{Code: ErrorHostIdentityChanged, SafeMessage: "The desktop identity changed and needs local recovery before remote connections continue.", Retryable: false}
	}
	device, err := m.loadSessionDevice(ctx, input)
	if err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	if device.HostID != m.Identity.HostID {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("device is not enrolled to this host")
	}
	if device.Status != StatusActive {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, SecureConnectionError{Code: ErrorDeviceRevoked, SafeMessage: "This device is no longer trusted by the desktop.", Retryable: false}
	}
	var cert HostEnrollmentCertificateV1
	if err := DecodeCanonical(device.EnrollmentCertificate, &cert); err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("invalid stored enrollment certificate: %w", err)
	}
	if err := VerifyEnrollmentCertificate(cert, m.now()); err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	if input.EnrollmentCertificateHash != "" {
		hash, err := EnrollmentCertificateHash(cert)
		if err != nil {
			return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
		}
		if !constantStringEqual(hash, input.EnrollmentCertificateHash) {
			return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("enrollment certificate hash mismatch")
		}
	}
	certificateHash, err := EnrollmentCertificateHash(cert)
	if err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	if strings.TrimSpace(input.AccountID) != "" && strings.TrimSpace(device.AccountID) != "" && strings.TrimSpace(input.AccountID) != strings.TrimSpace(device.AccountID) {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("secure session account mismatch")
	}
	handshake := input.NoiseHandshake
	if handshake.Version == 0 {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup did not include a runtime handshake.", Retryable: true}
	}
	if strings.TrimSpace(handshake.DeviceID) != device.DeviceID {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup was not bound to this device.", Retryable: true}
	}
	if strings.TrimSpace(handshake.DeviceNoisePublicKey) != device.DeviceNoisePublicKey {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup used the wrong device key.", Retryable: true}
	}
	if strings.TrimSpace(handshake.EnrollmentCertHash) != certificateHash {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup was not bound to this enrollment.", Retryable: true}
	}
	prologue := SessionPrologueV1{
		Protocol:                  "or3-secure-runtime",
		Version:                   ProtocolVersion,
		RelayOrigin:               strings.TrimSpace(input.RelayOrigin),
		RouteID:                   strings.TrimSpace(input.RelayRouteID),
		HostID:                    m.Identity.HostID,
		DeviceIDHash:              HashBase64URL([]byte(device.DeviceID)),
		EnrollmentCertificateHash: certificateHash,
		AccountID:                 strings.TrimSpace(device.AccountID),
		MinProtocolVersion:        ProtocolVersion,
		MaxProtocolVersion:        ProtocolVersion,
	}
	if strings.TrimSpace(input.AccountID) != "" {
		prologue.AccountID = strings.TrimSpace(input.AccountID)
	}
	handshakeResult, err := m.handshake().Accept(m.Identity, handshake, prologue)
	if err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	sessionID, err := RandomBase64URL(24)
	if err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	now := m.now()
	ttl := input.TTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	if ttl > time.Hour {
		ttl = time.Hour
	}
	stepUpAt := input.AuthenticatedStepUpAt.UTC().UnixMilli()
	if input.AuthenticatedStepUpAt.IsZero() {
		stepUpAt = 0
	}
	rec, err := m.DB.CreateSecureConnectionSession(ctx, db.SecureConnectionSessionRecord{
		SessionID:       sessionID,
		DeviceID:        device.DeviceID,
		HostID:          device.HostID,
		RelayRouteID:    strings.TrimSpace(input.RelayRouteID),
		EnrollmentEpoch: device.EnrollmentEpoch,
		Status:          StatusActive,
		CreatedAt:       now.UnixMilli(),
		LastSeenAt:      now.UnixMilli(),
		ExpiresAt:       now.Add(ttl).UnixMilli(),
		StepUpAt:        stepUpAt,
		Metadata: map[string]any{
			"protocol":   "secure-connections-v2",
			"transcript": handshakeResult.Transcript,
		},
	})
	if err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	claims := SessionClaims{
		HostID:          device.HostID,
		DeviceID:        device.DeviceID,
		EnrollmentEpoch: device.EnrollmentEpoch,
		Role:            device.Role,
		Capabilities:    NormalizeCapabilities(device.Capabilities),
		TrustLevel:      device.TrustLevel,
		SessionID:       rec.SessionID,
		RelayRouteID:    rec.RelayRouteID,
		AccountID:       device.AccountID,
		StepUpAtUnixMs:  rec.StepUpAt,
		IssuedAtUnixMs:  rec.CreatedAt,
		ExpiresAtUnixMs: rec.ExpiresAt,
	}
	claimMAC, err := m.signClaims(claims)
	if err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	}
	if ok, err := m.DB.UpsertSecureConnectionSessionClaimMAC(ctx, rec.SessionID, claimMAC); err != nil {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, err
	} else if !ok {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("secure session claim MAC was not stored")
	}
	return claims, rec, nil
}

func (m *SessionManager) LoadActiveSessionClaims(ctx context.Context, sessionID string) (SessionClaims, error) {
	if m == nil || m.DB == nil {
		return SessionClaims{}, fmt.Errorf("secure session manager unavailable")
	}
	rec, err := m.DB.GetSecureConnectionSession(ctx, sessionID)
	if err != nil {
		return SessionClaims{}, err
	}
	now := m.now()
	if rec.Status != StatusActive || (rec.ExpiresAt > 0 && rec.ExpiresAt <= now.UnixMilli()) {
		return SessionClaims{}, SecureConnectionError{Code: ErrorSessionExpired, SafeMessage: "This secure connection expired. Reconnect from the app.", Retryable: true}
	}
	device, err := m.DB.GetSecureConnectionDevice(ctx, rec.DeviceID)
	if err != nil {
		return SessionClaims{}, err
	}
	if device.Status != StatusActive {
		return SessionClaims{}, SecureConnectionError{Code: ErrorDeviceRevoked, SafeMessage: "This device is no longer trusted by the desktop.", Retryable: false}
	}
	if device.HostID != rec.HostID || rec.HostID != m.Identity.HostID {
		return SessionClaims{}, fmt.Errorf("secure session host mismatch")
	}
	if device.EnrollmentEpoch != rec.EnrollmentEpoch {
		return SessionClaims{}, fmt.Errorf("secure session enrollment epoch is stale")
	}
	claims := SessionClaims{
		HostID:          rec.HostID,
		DeviceID:        rec.DeviceID,
		EnrollmentEpoch: rec.EnrollmentEpoch,
		Role:            device.Role,
		Capabilities:    NormalizeCapabilities(device.Capabilities),
		TrustLevel:      device.TrustLevel,
		SessionID:       rec.SessionID,
		RelayRouteID:    rec.RelayRouteID,
		AccountID:       device.AccountID,
		StepUpAtUnixMs:  rec.StepUpAt,
		IssuedAtUnixMs:  rec.CreatedAt,
		ExpiresAtUnixMs: rec.ExpiresAt,
	}
	storedMAC, ok, err := m.DB.GetSecureConnectionSessionClaimMAC(ctx, sessionID)
	if err != nil {
		return SessionClaims{}, err
	}
	if !ok {
		return SessionClaims{}, SecureConnectionError{Code: ErrorSessionExpired, SafeMessage: "This secure connection needs to be re-established.", Retryable: true}
	}
	if err := m.verifyClaims(claims, storedMAC); err != nil {
		return SessionClaims{}, SecureConnectionError{Code: ErrorSessionExpired, SafeMessage: "This secure connection needs to be re-established.", Retryable: true}
	}
	return claims, nil
}

func (m *SessionManager) UpdateStepUp(ctx context.Context, sessionID string, stepUpAt time.Time) (SessionClaims, error) {
	claims, err := m.LoadActiveSessionClaims(ctx, sessionID)
	if err != nil {
		return SessionClaims{}, err
	}
	updated, err := ValidateStepUpUpdate(claims, stepUpAt, m.now())
	if err != nil {
		return SessionClaims{}, err
	}
	claimMAC, err := m.signClaims(updated)
	if err != nil {
		return SessionClaims{}, err
	}
	if err := m.DB.UpdateSecureConnectionSessionStepUpAndClaimMAC(ctx, sessionID, updated.StepUpAtUnixMs, claimMAC); err != nil {
		return SessionClaims{}, err
	}
	return updated, nil
}

func (m *SessionManager) ValidateFrame(ctx context.Context, sessionID string, raw []byte, window *ReplayWindow) (SecureFrameV1, error) {
	rec, err := m.DB.GetSecureConnectionSession(ctx, sessionID)
	if err != nil {
		return SecureFrameV1{}, err
	}
	now := m.now()
	if rec.Status != StatusActive || (rec.ExpiresAt > 0 && rec.ExpiresAt <= now.UnixMilli()) {
		return SecureFrameV1{}, SecureConnectionError{Code: ErrorSessionExpired, SafeMessage: "This secure connection expired. Reconnect from the app.", Retryable: true}
	}
	frame, err := DecodeSecureFrame(raw, rec.SessionID, nil, now)
	if err != nil {
		return SecureFrameV1{}, err
	}
	if rec.LastSequenceIn > 0 && frame.Sequence <= uint64(rec.LastSequenceIn) {
		return SecureFrameV1{}, SecureConnectionError{Code: ErrorReplayDetected, SafeMessage: "A repeated connection message was blocked.", Retryable: true}
	}
	if window != nil {
		if err := window.Accept(frame.Sequence); err != nil {
			return SecureFrameV1{}, err
		}
	}
	if err := m.DB.TouchSecureConnectionSession(ctx, rec.SessionID, now.UnixMilli(), int64(frame.Sequence), rec.LastSequenceOut); err != nil {
		return SecureFrameV1{}, err
	}
	return frame, nil
}

func (m *SessionManager) ExpireStaleSessions(ctx context.Context) (int64, error) {
	if m == nil || m.DB == nil {
		return 0, fmt.Errorf("secure session manager unavailable")
	}
	return m.DB.ExpireSecureConnectionSessions(ctx, m.now().UnixMilli())
}

const (
	DefaultPairingRetention    = 24 * time.Hour
	DefaultSessionRetention    = 7 * 24 * time.Hour
	DefaultRendezvousRetention = 24 * time.Hour
)

type PurgeResult struct {
	TerminalPairingSessions int64
	ExpiredSessions         int64
	RevokedDevices          int64
	TerminalRendezvous      int64
	ExpiredRelayRoutes      int64
}

func (m *SessionManager) PurgeStaleRecords(ctx context.Context) (PurgeResult, error) {
	if m == nil || m.DB == nil {
		return PurgeResult{}, fmt.Errorf("secure session manager unavailable")
	}
	now := m.now()
	var result PurgeResult

	pairingCut := now.Add(-DefaultPairingRetention).UnixMilli()
	n, err := m.DB.PurgeTerminalPairingSessions(ctx, pairingCut)
	if err != nil {
		return result, err
	}
	result.TerminalPairingSessions = n

	sessionCut := now.Add(-DefaultSessionRetention).UnixMilli()
	n, err = m.DB.PurgeExpiredSecureConnectionSessions(ctx, sessionCut)
	if err != nil {
		return result, err
	}
	result.ExpiredSessions = n

	deviceCut := now.Add(-DefaultSessionRetention).UnixMilli()
	n, err = m.DB.PurgeRevokedDevices(ctx, deviceCut)
	if err != nil {
		return result, err
	}
	result.RevokedDevices = n

	rendezvousCut := now.Add(-DefaultRendezvousRetention).UnixMilli()
	n, err = m.DB.PurgeTerminalRelayRendezvous(ctx, rendezvousCut)
	if err != nil {
		return result, err
	}
	result.TerminalRendezvous = n

	routeCut := now.UnixMilli()
	n, err = m.DB.PurgeExpiredRelayRoutes(ctx, routeCut)
	if err != nil {
		return result, err
	}
	result.ExpiredRelayRoutes = n

	return result, nil
}

func ShouldRekey(policy RekeyPolicy, now time.Time) bool {
	if policy.Force {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	last := policy.LastRekeyAt
	if last.IsZero() {
		last = policy.CreatedAt
	}
	if !last.IsZero() && now.Sub(last) >= DefaultRekeyAfter {
		return true
	}
	if policy.MessageCount >= DefaultRekeyMessages || policy.ByteCount >= DefaultRekeyBytes {
		return true
	}
	return false
}

func (m *SessionManager) loadSessionDevice(ctx context.Context, input SessionStartInput) (db.SecureConnectionDeviceRecord, error) {
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID != "" {
		return m.DB.GetSecureConnectionDevice(ctx, deviceID)
	}
	noiseKey := strings.TrimSpace(input.DeviceNoisePublicKey)
	if noiseKey == "" {
		return db.SecureConnectionDeviceRecord{}, fmt.Errorf("device ID or device Noise public key required")
	}
	rec, ok, err := m.DB.FindSecureConnectionDeviceByNoiseKey(ctx, m.Identity.HostID, noiseKey)
	if err != nil {
		return db.SecureConnectionDeviceRecord{}, err
	}
	if !ok {
		return db.SecureConnectionDeviceRecord{}, fmt.Errorf("device is not enrolled")
	}
	return rec, nil
}

func constantStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// signClaims produces an HMAC-SHA-256 over the canonical claim fields using
// the host noise private key as the MAC key. This binds the session claims to
// host-held key material so that a database compromise alone cannot forge
// accepted active claims.
func (m *SessionManager) signClaims(claims SessionClaims) (string, error) {
	if m == nil || m.Identity.HostNoisePrivateKey == "" {
		return "", fmt.Errorf("claim signing unavailable")
	}
	key, err := DecodeBase64URL(m.Identity.HostNoisePrivateKey)
	if err != nil {
		return "", fmt.Errorf("invalid host noise key for claim signing: %w", err)
	}
	payload := claimSigningPayload(claims)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(payload))
	return Base64URL(h.Sum(nil)), nil
}

func (m *SessionManager) verifyClaims(claims SessionClaims, storedMAC string) error {
	if m == nil || m.Identity.HostNoisePrivateKey == "" {
		return fmt.Errorf("claim verification unavailable")
	}
	key, err := DecodeBase64URL(m.Identity.HostNoisePrivateKey)
	if err != nil {
		return fmt.Errorf("invalid host noise key for claim verification: %w", err)
	}
	payload := claimSigningPayload(claims)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(payload))
	expected := Base64URL(h.Sum(nil))
	if !constantStringEqual(expected, storedMAC) {
		return fmt.Errorf("session claim integrity check failed")
	}
	return nil
}

// claimSigningPayload builds a canonical string from the protected claim
// fields. Changes to role, capabilities, trust level, account ID, route ID,
// enrollment epoch, or expiry will invalidate the MAC.
func claimSigningPayload(claims SessionClaims) string {
	return fmt.Sprintf(
		"host=%s|device=%s|epoch=%d|role=%s|caps=%s|trust=%s|sid=%s|route=%s|acct=%s|stepup=%d|issued=%d|expires=%d",
		claims.HostID,
		claims.DeviceID,
		claims.EnrollmentEpoch,
		claims.Role,
		strings.Join(claims.Capabilities, ","),
		claims.TrustLevel,
		claims.SessionID,
		claims.RelayRouteID,
		claims.AccountID,
		claims.StepUpAtUnixMs,
		claims.IssuedAtUnixMs,
		claims.ExpiresAtUnixMs,
	)
}
