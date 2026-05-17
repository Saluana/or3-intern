package secureconn

import (
	"context"
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
	EnrollmentCertificateHash string
	AccountID                 string
	StepUpAt                  time.Time
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
	DB       *db.DB
	Identity HostIdentity
	Now      func() time.Time
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
	if strings.TrimSpace(input.AccountID) != "" && strings.TrimSpace(device.AccountID) != "" && strings.TrimSpace(input.AccountID) != strings.TrimSpace(device.AccountID) {
		return SessionClaims{}, db.SecureConnectionSessionRecord{}, fmt.Errorf("secure session account mismatch")
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
	stepUpAt := input.StepUpAt.UTC().UnixMilli()
	if input.StepUpAt.IsZero() {
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
			"protocol": "secure-connections-v2",
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
	return claims, rec, nil
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
	if window == nil {
		window = NewReplayWindow(DefaultReplayWindowCap)
	}
	frame, err := DecodeSecureFrame(raw, rec.SessionID, window, now)
	if err != nil {
		return SecureFrameV1{}, err
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
