package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"

	"github.com/go-webauthn/webauthn/protocol"
	libwebauthn "github.com/go-webauthn/webauthn/webauthn"
)

const (
	DefaultUserID          = "owner"
	DefaultUserDisplayName = "OR3 Owner"

	CeremonyTypeRegistration = "registration"
	CeremonyTypeLogin        = "login"
	CeremonyTypeStepUp       = "step-up"

	CodeAuthDisabled    = "AUTH_UNSUPPORTED"
	CodeSessionRequired = "SESSION_REQUIRED"
	CodeSessionExpired  = "SESSION_EXPIRED"
	CodePasskeyRequired = "PASSKEY_REQUIRED"
	CodeStepUpRequired  = "STEP_UP_REQUIRED"
)

var (
	ErrAuthDisabled     = &Error{Code: CodeAuthDisabled, Message: "passkey auth is disabled", Status: http.StatusNotImplemented}
	ErrSessionRequired  = &Error{Code: CodeSessionRequired, Message: "session required", Status: http.StatusUnauthorized}
	ErrSessionExpired   = &Error{Code: CodeSessionExpired, Message: "session expired", Status: http.StatusUnauthorized}
	ErrPasskeyRequired  = &Error{Code: CodePasskeyRequired, Message: "passkey required", Status: http.StatusUnauthorized}
	ErrRecentStepUp     = &Error{Code: CodeStepUpRequired, Message: "recent passkey verification required", Status: http.StatusForbidden}
	ErrInvalidCeremony  = errors.New("invalid ceremony")
	ErrRecoveryRequired = errors.New("cannot remove final passkey without a recovery path")
)

type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Status     int    `json:"status"`
	RetryAfter int    `json:"retryAfterSeconds,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "auth error"
	}
	if strings.TrimSpace(e.Message) == "" {
		return e.Code
	}
	return e.Message
}

type Service struct {
	cfg      config.Config
	db       *db.DB
	audit    *security.AuditLogger
	webauthn *libwebauthn.WebAuthn
	now      func() time.Time
}

type BeginRegistrationRequest struct {
	DeviceID     string
	DisplayName  string
	Reason       string
	SessionToken string
}

type BeginCeremonyResponse struct {
	CeremonyID string `json:"ceremonyId"`
	Options    any    `json:"options"`
}

type FinishRegistrationRequest struct {
	CeremonyID   string
	Body         []byte
	Nickname     string
	SessionToken string
}

type BeginLoginRequest struct {
	DeviceID string
	Reason   string
}

type FinishLoginRequest struct {
	CeremonyID     string
	Body           []byte
	DeviceID       string
	UserAgentHash  string
	RemoteAddrHash string
	FallbackRole   string
}

type BeginStepUpRequest struct {
	SessionToken string
	Reason       string
}

type FinishStepUpRequest struct {
	SessionToken   string
	CeremonyID     string
	Body           []byte
	Reason         string
	UserAgentHash  string
	RemoteAddrHash string
}

type SessionClaims struct {
	Session db.AuthSessionRecord
	User    db.AuthUserRecord
	Role    string
}

type LoginResult struct {
	SessionToken string               `json:"sessionToken"`
	Session      db.AuthSessionRecord `json:"session"`
	User         db.AuthUserRecord    `json:"user"`
	CredentialID string               `json:"credentialId"`
}

func NewService(cfg config.Config, database *db.DB, audit *security.AuditLogger) (*Service, error) {
	svc := &Service{cfg: cfg, db: database, audit: audit, now: time.Now}
	if !cfg.Auth.Enabled {
		return svc, nil
	}
	wa, err := libwebauthn.New(&libwebauthn.Config{
		RPDisplayName:      cfg.Auth.RPDisplayName,
		RPID:               cfg.Auth.RPID,
		RPOrigins:          append([]string{}, cfg.Auth.AllowedOrigins...),
		RPTopOrigins:       append([]string{}, cfg.Auth.RelatedOrigins...),
		RPAllowCrossOrigin: len(cfg.Auth.RelatedOrigins) > 0,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, err
	}
	svc.webauthn = wa
	return svc, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Auth.Enabled && s.webauthn != nil && s.db != nil
}

func (s *Service) BeginRegistration(ctx context.Context, req BeginRegistrationRequest) (*BeginCeremonyResponse, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	user, waUser, err := s.ensureDefaultUser(ctx, req.DisplayName)
	if err != nil {
		return nil, err
	}
	if err := s.requireRegistrationAuthorization(ctx, user.ID, req.SessionToken); err != nil {
		return nil, err
	}
	creation, session, err := s.webauthn.BeginRegistration(
		waUser,
		libwebauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		libwebauthn.WithExclusions(libwebauthn.Credentials(waUser.WebAuthnCredentials()).CredentialDescriptors()),
		libwebauthn.WithExtensions(protocol.AuthenticationExtensions{"credProps": true}),
	)
	if err != nil {
		return nil, err
	}
	ceremonyID, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	if err := s.storeCeremony(ctx, ceremonyID, CeremonyTypeRegistration, user.ID, req.DeviceID, "", req.Reason, session); err != nil {
		return nil, err
	}
	s.auditEvent(ctx, "auth.passkey.registration.begin", user.ID, map[string]any{"device_id": req.DeviceID, "ceremony_id": ceremonyID})
	return &BeginCeremonyResponse{CeremonyID: ceremonyID, Options: creation}, nil
}

func (s *Service) FinishRegistration(ctx context.Context, req FinishRegistrationRequest) (db.PasskeyCredentialRecord, error) {
	if err := s.requireEnabled(); err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	ceremony, session, err := s.consumeCeremony(ctx, req.CeremonyID, CeremonyTypeRegistration)
	if err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	user, waUser, err := s.loadUser(ctx, ceremony.UserID)
	if err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	if err := s.requireRegistrationAuthorization(ctx, user.ID, req.SessionToken); err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	credential, err := s.webauthn.FinishRegistration(waUser, *session, newJSONRequest(req.Body))
	if err != nil {
		_ = s.db.MarkWebAuthnCeremonyFailure(ctx, ceremony.ID, safeErrorReason(err), s.now().UnixMilli())
		s.auditEvent(ctx, "auth.passkey.registration.failed", user.ID, map[string]any{"ceremony_id": ceremony.ID, "reason": safeErrorReason(err)})
		return db.PasskeyCredentialRecord{}, err
	}
	record, err := s.persistCredential(ctx, user.ID, ceremony.DeviceID, req.Nickname, credential, 0)
	if err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	s.auditEvent(ctx, "auth.passkey.registration.finish", user.ID, map[string]any{"credential_id": record.ID, "device_id": record.DeviceID})
	return record, nil
}

func (s *Service) BeginLogin(ctx context.Context, req BeginLoginRequest) (*BeginCeremonyResponse, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return nil, ErrPasskeyRequired
	}
	assertion, session, err := s.webauthn.BeginDiscoverableLogin(libwebauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	ceremonyID, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	if err := s.storeCeremony(ctx, ceremonyID, CeremonyTypeLogin, "", req.DeviceID, "", req.Reason, session); err != nil {
		return nil, err
	}
	s.auditEvent(ctx, "auth.passkey.login.begin", DefaultUserID, map[string]any{"device_id": req.DeviceID, "ceremony_id": ceremonyID})
	return &BeginCeremonyResponse{CeremonyID: ceremonyID, Options: assertion}, nil
}

func (s *Service) FinishLogin(ctx context.Context, req FinishLoginRequest) (LoginResult, error) {
	if err := s.requireEnabled(); err != nil {
		return LoginResult{}, err
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return LoginResult{}, ErrPasskeyRequired
	}
	ceremony, session, err := s.consumeCeremony(ctx, req.CeremonyID, CeremonyTypeLogin)
	if err != nil {
		return LoginResult{}, err
	}
	returnedUser, credential, err := s.webauthn.FinishPasskeyLogin(s.lookupDiscoverableUser(ctx), *session, newJSONRequest(req.Body))
	if err != nil {
		_ = s.db.MarkWebAuthnCeremonyFailure(ctx, ceremony.ID, safeErrorReason(err), s.now().UnixMilli())
		s.auditEvent(ctx, "auth.passkey.login.failed", DefaultUserID, map[string]any{"ceremony_id": ceremony.ID, "reason": safeErrorReason(err)})
		return LoginResult{}, err
	}
	user, ok := returnedUser.(*webauthnUser)
	if !ok {
		return LoginResult{}, fmt.Errorf("unexpected WebAuthn user type")
	}
	deviceID := firstNonEmpty(req.DeviceID, ceremony.DeviceID)
	record, err := s.persistCredential(ctx, user.record.ID, deviceID, "", credential, s.now().UnixMilli())
	if err != nil {
		return LoginResult{}, err
	}
	role := s.resolveRole(ctx, deviceID, req.FallbackRole)
	issued, err := s.issueSession(ctx, user.record, role, record.ID, deviceID, req.UserAgentHash, req.RemoteAddrHash)
	if err != nil {
		return LoginResult{}, err
	}
	s.auditEvent(ctx, "auth.passkey.login.finish", user.record.ID, map[string]any{"credential_id": record.ID, "device_id": deviceID, "role": role})
	return issued, nil
}

func (s *Service) BeginStepUp(ctx context.Context, req BeginStepUpRequest) (*BeginCeremonyResponse, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	claims, err := s.ValidateSessionToken(ctx, req.SessionToken)
	if err != nil {
		return nil, err
	}
	_, waUser, err := s.loadUser(ctx, claims.User.ID)
	if err != nil {
		return nil, err
	}
	assertion, session, err := s.webauthn.BeginLogin(waUser, libwebauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	ceremonyID, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	if err := s.storeCeremony(ctx, ceremonyID, CeremonyTypeStepUp, claims.User.ID, claims.Session.DeviceID, claims.Session.ID, firstNonEmpty(req.Reason, "sensitive-action"), session); err != nil {
		return nil, err
	}
	s.auditEvent(ctx, "auth.stepup.begin", claims.User.ID, map[string]any{"session_id": claims.Session.ID, "ceremony_id": ceremonyID, "reason": req.Reason})
	return &BeginCeremonyResponse{CeremonyID: ceremonyID, Options: assertion}, nil
}

func (s *Service) FinishStepUp(ctx context.Context, req FinishStepUpRequest) (db.AuthSessionRecord, error) {
	if err := s.requireEnabled(); err != nil {
		return db.AuthSessionRecord{}, err
	}
	claims, err := s.ValidateSessionToken(ctx, req.SessionToken)
	if err != nil {
		return db.AuthSessionRecord{}, err
	}
	ceremony, session, err := s.consumeCeremony(ctx, req.CeremonyID, CeremonyTypeStepUp)
	if err != nil {
		return db.AuthSessionRecord{}, err
	}
	if ceremony.SessionID != claims.Session.ID {
		return db.AuthSessionRecord{}, ErrInvalidCeremony
	}
	_, waUser, err := s.loadUser(ctx, claims.User.ID)
	if err != nil {
		return db.AuthSessionRecord{}, err
	}
	credential, err := s.webauthn.FinishLogin(waUser, *session, newJSONRequest(req.Body))
	if err != nil {
		_ = s.db.MarkWebAuthnCeremonyFailure(ctx, ceremony.ID, safeErrorReason(err), s.now().UnixMilli())
		s.auditEvent(ctx, "auth.stepup.failed", claims.User.ID, map[string]any{"ceremony_id": ceremony.ID, "session_id": claims.Session.ID, "reason": safeErrorReason(err)})
		return db.AuthSessionRecord{}, err
	}
	record, err := s.persistCredential(ctx, claims.User.ID, claims.Session.DeviceID, "", credential, s.now().UnixMilli())
	if err != nil {
		return db.AuthSessionRecord{}, err
	}
	stepUpAt := s.now().UnixMilli()
	if err := s.db.UpdateAuthSessionStepUp(ctx, claims.Session.ID, stepUpAt, record.ID, firstNonEmpty(req.Reason, ceremony.Reason)); err != nil {
		return db.AuthSessionRecord{}, err
	}
	updated, err := s.db.GetAuthSession(ctx, claims.Session.ID)
	if err != nil {
		return db.AuthSessionRecord{}, err
	}
	s.auditEvent(ctx, "auth.stepup.finish", claims.User.ID, map[string]any{"session_id": updated.ID, "credential_id": record.ID, "reason": firstNonEmpty(req.Reason, ceremony.Reason)})
	return updated, nil
}

func (s *Service) ValidateSessionToken(ctx context.Context, rawToken string) (SessionClaims, error) {
	if err := s.requireEnabled(); err != nil {
		return SessionClaims{}, err
	}
	tokenHash := hashToken(rawToken)
	session, ok, err := s.db.FindAuthSessionByTokenHash(ctx, tokenHash[:])
	if err != nil {
		return SessionClaims{}, err
	}
	if !ok {
		s.auditEvent(ctx, "auth.session.required", DefaultUserID, map[string]any{"reason": "missing-session"})
		return SessionClaims{}, ErrSessionRequired
	}
	nowMS := s.now().UnixMilli()
	if session.RevokedAt > 0 {
		s.auditEvent(ctx, "auth.session.expired", session.UserID, map[string]any{"session_id": session.ID, "reason": "revoked"})
		return SessionClaims{}, ErrSessionExpired
	}
	if (session.IdleExpiresAt > 0 && session.IdleExpiresAt <= nowMS) || (session.AbsoluteExpiresAt > 0 && session.AbsoluteExpiresAt <= nowMS) {
		_ = s.db.RevokeAuthSession(ctx, session.ID, "expired", nowMS)
		s.auditEvent(ctx, "auth.session.expired", session.UserID, map[string]any{"session_id": session.ID, "reason": "expired"})
		return SessionClaims{}, ErrSessionExpired
	}
	user, err := s.db.GetAuthUser(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.auditEvent(ctx, "auth.session.expired", session.UserID, map[string]any{"session_id": session.ID, "reason": "missing-user"})
			return SessionClaims{}, ErrSessionExpired
		}
		return SessionClaims{}, err
	}
	idleExpiry := nowMS + int64(s.cfg.Auth.SessionIdleTTLSeconds)*1000
	if err := s.db.UpdateAuthSessionActivity(ctx, session.ID, nowMS, idleExpiry); err != nil {
		return SessionClaims{}, err
	}
	session.LastSeenAt = nowMS
	session.IdleExpiresAt = idleExpiry
	return SessionClaims{Session: session, User: user, Role: session.Role}, nil
}

func (s *Service) RevokeSessionToken(ctx context.Context, rawToken, reason string) error {
	claims, err := s.ValidateSessionToken(ctx, rawToken)
	if err != nil {
		return err
	}
	reason = firstNonEmpty(reason, "logout")
	if err := s.db.RevokeAuthSession(ctx, claims.Session.ID, reason, s.now().UnixMilli()); err != nil {
		return err
	}
	s.auditEvent(ctx, "auth.session.revoked", claims.User.ID, map[string]any{"session_id": claims.Session.ID, "reason": reason})
	return nil
}

func (s *Service) ListPasskeys(ctx context.Context, userID string) ([]db.PasskeyCredentialRecord, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	return s.db.ListPasskeyCredentialsByUser(ctx, firstNonEmpty(userID, DefaultUserID), false)
}

func (s *Service) RenamePasskey(ctx context.Context, passkeyID, nickname string) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	return s.db.RenamePasskeyCredential(ctx, passkeyID, nickname)
}

func (s *Service) RevokePasskey(ctx context.Context, sessionToken, passkeyID, reason string) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	claims, err := s.ValidateSessionToken(ctx, sessionToken)
	if err != nil {
		return err
	}
	if !s.hasRecentStepUp(claims.Session) {
		return ErrRecentStepUp
	}
	credential, err := s.db.GetPasskeyCredential(ctx, passkeyID)
	if err != nil {
		return err
	}
	if credential.UserID != claims.User.ID {
		return fmt.Errorf("passkey not found")
	}
	if !s.canRemovePasskey(ctx, claims.User.ID, credential.ID) {
		return ErrRecoveryRequired
	}
	revokedAt := s.now().UnixMilli()
	if err := s.db.RevokePasskeyCredential(ctx, credential.ID, firstNonEmpty(reason, "user-revoked"), revokedAt); err != nil {
		return err
	}
	if _, err := s.db.RevokeAuthSessionsByCredential(ctx, credential.ID, "passkey-revoked", revokedAt); err != nil {
		return err
	}
	s.auditEvent(ctx, "auth.passkey.revoked", claims.User.ID, map[string]any{"credential_id": credential.ID, "reason": firstNonEmpty(reason, "user-revoked")})
	return nil
}

func (s *Service) HasRecentStepUp(session db.AuthSessionRecord) bool {
	return s.hasRecentStepUp(session)
}

func (s *Service) Audit(ctx context.Context, eventType, actor string, payload any) {
	s.auditEvent(ctx, eventType, actor, payload)
}

func (s *Service) requireEnabled() error {
	if s == nil || !s.Enabled() {
		return ErrAuthDisabled
	}
	return nil
}

func (s *Service) ensureDefaultUser(ctx context.Context, displayName string) (db.AuthUserRecord, *webauthnUser, error) {
	if _, err := s.db.UpsertAuthUser(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: firstNonEmpty(displayName, DefaultUserDisplayName)}); err != nil {
		return db.AuthUserRecord{}, nil, err
	}
	return s.loadUser(ctx, DefaultUserID)
}

func (s *Service) loadUser(ctx context.Context, userID string) (db.AuthUserRecord, *webauthnUser, error) {
	record, err := s.db.GetAuthUser(ctx, userID)
	if err != nil {
		return db.AuthUserRecord{}, nil, err
	}
	credentials, err := s.db.ListPasskeyCredentialsByUser(ctx, record.ID, false)
	if err != nil {
		return db.AuthUserRecord{}, nil, err
	}
	return record, &webauthnUser{record: record, credentials: s.mustCredentials(credentials)}, nil
}

func (s *Service) storeCeremony(ctx context.Context, ceremonyID, ceremonyType, userID, deviceID, sessionID, reason string, session *libwebauthn.SessionData) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	_, err = s.db.CreateWebAuthnCeremony(ctx, db.WebAuthnCeremonyRecord{
		ID:              ceremonyID,
		Type:            ceremonyType,
		UserID:          strings.TrimSpace(userID),
		DeviceID:        strings.TrimSpace(deviceID),
		SessionID:       strings.TrimSpace(sessionID),
		ChallengeHash:   hashString(session.Challenge),
		SessionDataJSON: string(raw),
		RPID:            s.cfg.Auth.RPID,
		Reason:          strings.TrimSpace(reason),
		CreatedAt:       s.now().UnixMilli(),
		ExpiresAt:       session.Expires.UnixMilli(),
	})
	return err
}

func (s *Service) consumeCeremony(ctx context.Context, ceremonyID, ceremonyType string) (db.WebAuthnCeremonyRecord, *libwebauthn.SessionData, error) {
	ceremony, err := s.db.GetWebAuthnCeremony(ctx, ceremonyID)
	if err != nil {
		return db.WebAuthnCeremonyRecord{}, nil, err
	}
	if ceremony.Type != ceremonyType {
		return db.WebAuthnCeremonyRecord{}, nil, ErrInvalidCeremony
	}
	nowMS := s.now().UnixMilli()
	if ceremony.ExpiresAt > 0 && ceremony.ExpiresAt <= nowMS {
		return db.WebAuthnCeremonyRecord{}, nil, ErrSessionExpired
	}
	consumed, err := s.db.ConsumeWebAuthnCeremony(ctx, ceremony.ID, nowMS)
	if err != nil {
		return db.WebAuthnCeremonyRecord{}, nil, err
	}
	if !consumed {
		return db.WebAuthnCeremonyRecord{}, nil, ErrInvalidCeremony
	}
	var session libwebauthn.SessionData
	if err := json.Unmarshal([]byte(ceremony.SessionDataJSON), &session); err != nil {
		return db.WebAuthnCeremonyRecord{}, nil, err
	}
	return ceremony, &session, nil
}

func (s *Service) persistCredential(ctx context.Context, userID, deviceID, nickname string, credential *libwebauthn.Credential, lastUsedAt int64) (db.PasskeyCredentialRecord, error) {
	raw, err := json.Marshal(credential)
	if err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	record := db.PasskeyCredentialRecord{
		ID:                   hex.EncodeToString(credential.ID),
		UserID:               strings.TrimSpace(userID),
		DeviceID:             strings.TrimSpace(deviceID),
		CredentialID:         append([]byte(nil), credential.ID...),
		PublicKey:            append([]byte(nil), credential.PublicKey...),
		SignCount:            credential.Authenticator.SignCount,
		Transports:           transportsToStrings(credential.Transport),
		AAGUID:               hex.EncodeToString(credential.Authenticator.AAGUID),
		AttestationType:      strings.TrimSpace(credential.AttestationType),
		AttestationFormat:    strings.TrimSpace(credential.AttestationFormat),
		BackupEligible:       credential.Flags.BackupEligible,
		BackupState:          credential.Flags.BackupState,
		Flags:                byte(credential.Flags.ProtocolValue()),
		UserVerifiedRequired: true,
		Nickname:             strings.TrimSpace(nickname),
		CredentialJSON:       string(raw),
		CreatedAt:            s.now().UnixMilli(),
		LastUsedAt:           lastUsedAt,
	}
	existing, ok, err := s.db.FindPasskeyCredentialByCredentialID(ctx, credential.ID)
	if err != nil {
		return db.PasskeyCredentialRecord{}, err
	}
	if ok {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		record.Nickname = firstNonEmpty(record.Nickname, existing.Nickname)
		record.DeviceID = firstNonEmpty(record.DeviceID, existing.DeviceID)
		record.RevokedAt = existing.RevokedAt
		record.RevokedReason = existing.RevokedReason
	}
	return s.db.UpsertPasskeyCredential(ctx, record)
}

func (s *Service) issueSession(ctx context.Context, user db.AuthUserRecord, role, credentialID, deviceID, userAgentHash, remoteAddrHash string) (LoginResult, error) {
	rawToken, err := randomHex(32)
	if err != nil {
		return LoginResult{}, err
	}
	sessionID, err := randomHex(16)
	if err != nil {
		return LoginResult{}, err
	}
	nowMS := s.now().UnixMilli()
	hash := hashToken(rawToken)
	record, err := s.db.CreateAuthSession(ctx, db.AuthSessionRecord{
		ID:                sessionID,
		UserID:            user.ID,
		DeviceID:          strings.TrimSpace(deviceID),
		CredentialID:      strings.TrimSpace(credentialID),
		TokenHash:         hash[:],
		Role:              firstNonEmpty(role, approval.RoleAdmin),
		CreatedAt:         nowMS,
		LastSeenAt:        nowMS,
		IdleExpiresAt:     nowMS + int64(s.cfg.Auth.SessionIdleTTLSeconds)*1000,
		AbsoluteExpiresAt: nowMS + int64(s.cfg.Auth.SessionAbsoluteTTLSeconds)*1000,
		UserAgentHash:     strings.TrimSpace(userAgentHash),
		RemoteAddrHash:    strings.TrimSpace(remoteAddrHash),
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{SessionToken: rawToken, Session: record, User: user, CredentialID: credentialID}, nil
}

func (s *Service) lookupDiscoverableUser(ctx context.Context) func(rawID, userHandle []byte) (libwebauthn.User, error) {
	return func(rawID, userHandle []byte) (libwebauthn.User, error) {
		record, ok, err := s.db.FindPasskeyCredentialByCredentialID(ctx, rawID)
		if err != nil {
			return nil, err
		}
		if !ok || record.RevokedAt > 0 {
			return nil, fmt.Errorf("credential not found")
		}
		userID := record.UserID
		if len(userHandle) > 0 {
			userID = string(userHandle)
		}
		_, user, err := s.loadUser(ctx, userID)
		return user, err
	}
}

func (s *Service) resolveRole(ctx context.Context, deviceID, fallback string) string {
	if strings.TrimSpace(deviceID) != "" {
		device, err := s.db.GetPairedDevice(ctx, deviceID)
		if err == nil && device.RevokedAt == 0 && strings.TrimSpace(device.Role) != "" {
			return device.Role
		}
	}
	return firstNonEmpty(fallback, approval.RoleAdmin)
}

func (s *Service) requireRegistrationAuthorization(ctx context.Context, userID, sessionToken string) error {
	credentials, err := s.db.ListPasskeyCredentialsByUser(ctx, firstNonEmpty(userID, DefaultUserID), false)
	if err != nil {
		return err
	}
	if len(credentials) == 0 {
		return nil
	}
	claims, err := s.ValidateSessionToken(ctx, sessionToken)
	if err != nil {
		return err
	}
	if claims.User.ID != firstNonEmpty(userID, DefaultUserID) {
		return ErrPasskeyRequired
	}
	if !s.hasRecentStepUp(claims.Session) {
		return ErrRecentStepUp
	}
	return nil
}

func (s *Service) hasRecentStepUp(session db.AuthSessionRecord) bool {
	if session.LastStepUpAt <= 0 {
		return false
	}
	return session.LastStepUpAt+int64(s.cfg.Auth.StepUpTTLSeconds)*1000 > s.now().UnixMilli()
}

func (s *Service) canRemovePasskey(ctx context.Context, userID, removingCredentialID string) bool {
	credentials, err := s.db.ListPasskeyCredentialsByUser(ctx, userID, false)
	if err == nil {
		for _, credential := range credentials {
			if credential.ID != removingCredentialID && credential.RevokedAt == 0 {
				return true
			}
		}
	}
	devices, err := s.db.ListPairedDevices(ctx, 200)
	if err == nil {
		for _, device := range devices {
			if device.RevokedAt == 0 && device.Status == approval.StatusActive && device.Role == approval.RoleAdmin {
				return true
			}
		}
	}
	return strings.TrimSpace(s.cfg.Service.Secret) != "" || s.cfg.Auth.AllowPairedTokenFallback
}

func (s *Service) mustCredentials(records []db.PasskeyCredentialRecord) []libwebauthn.Credential {
	out := make([]libwebauthn.Credential, 0, len(records))
	for _, record := range records {
		if record.RevokedAt > 0 {
			continue
		}
		credential, err := record.ToWebAuthnCredential()
		if err == nil {
			out = append(out, credential)
		}
	}
	return out
}

func (s *Service) auditEvent(ctx context.Context, eventType, actor string, payload any) {
	if s == nil || s.audit == nil {
		return
	}
	_ = s.audit.Record(ctx, eventType, "auth", actor, payload)
}

type webauthnUser struct {
	record      db.AuthUserRecord
	credentials []libwebauthn.Credential
}

func (u *webauthnUser) WebAuthnID() []byte {
	return []byte(u.record.ID)
}

func (u *webauthnUser) WebAuthnName() string {
	return u.record.DisplayName
}

func (u *webauthnUser) WebAuthnDisplayName() string {
	return u.record.DisplayName
}

func (u *webauthnUser) WebAuthnCredentials() []libwebauthn.Credential {
	return append([]libwebauthn.Credential{}, u.credentials...)
}

func newJSONRequest(body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "http://or3.local/internal/v1/auth", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(raw string) [32]byte {
	return sha256.Sum256([]byte(strings.TrimSpace(raw)))
}

func hashString(raw string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(hash[:])
}

func safeErrorReason(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func transportsToStrings(values []protocol.AuthenticatorTransport) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(string(value)); text != "" {
			out = append(out, text)
		}
	}
	return out
}
