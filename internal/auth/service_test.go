package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"

	"github.com/go-webauthn/webauthn/protocol"
	libwebauthn "github.com/go-webauthn/webauthn/webauthn"
)

func TestServiceWebAuthnCeremonyState(t *testing.T) {
	ctx := context.Background()
	svc, database := newAuthTestService(t)
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }

	session := &libwebauthn.SessionData{
		Challenge:        "challenge-1",
		RelyingPartyID:   "localhost",
		UserID:           []byte(DefaultUserID),
		Expires:          now.Add(time.Minute),
		UserVerification: protocol.VerificationRequired,
	}
	if err := svc.storeCeremony(ctx, "ceremony-1", CeremonyTypeStepUp, DefaultUserID, "device-1", "session-1", "test", session); err != nil {
		t.Fatalf("storeCeremony: %v", err)
	}
	stored, err := database.GetWebAuthnCeremony(ctx, "ceremony-1")
	if err != nil {
		t.Fatalf("GetWebAuthnCeremony: %v", err)
	}
	if stored.ChallengeHash == "" || strings.Contains(stored.ChallengeHash, "challenge-1") {
		t.Fatalf("expected hashed challenge only, got %#v", stored)
	}
	if _, _, err := svc.consumeCeremony(ctx, "ceremony-1", CeremonyTypeStepUp); err != nil {
		t.Fatalf("consumeCeremony first use: %v", err)
	}
	if _, _, err := svc.consumeCeremony(ctx, "ceremony-1", CeremonyTypeStepUp); !errors.Is(err, ErrInvalidCeremony) {
		t.Fatalf("expected replay prevention error, got %v", err)
	}

	expired := *session
	expired.Expires = now.Add(-time.Second)
	if err := svc.storeCeremony(ctx, "ceremony-expired", CeremonyTypeLogin, DefaultUserID, "device-1", "", "expired", &expired); err != nil {
		t.Fatalf("store expired ceremony: %v", err)
	}
	if _, _, err := svc.consumeCeremony(ctx, "ceremony-expired", CeremonyTypeLogin); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected expired ceremony error, got %v", err)
	}
}

func TestServiceWebAuthnLoginOptionsRequireUserVerificationAndUnknownCredentialFails(t *testing.T) {
	ctx := context.Background()
	svc, database := newAuthTestService(t)

	if _, err := svc.BeginLogin(ctx, BeginLoginRequest{}); !errors.Is(err, ErrPasskeyRequired) {
		t.Fatalf("expected login to require paired device identity, got %v", err)
	}
	begin, err := svc.BeginLogin(ctx, BeginLoginRequest{DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	ceremony, err := database.GetWebAuthnCeremony(ctx, begin.CeremonyID)
	if err != nil {
		t.Fatalf("GetWebAuthnCeremony: %v", err)
	}
	var session libwebauthn.SessionData
	if err := json.Unmarshal([]byte(ceremony.SessionDataJSON), &session); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if session.UserVerification != protocol.VerificationRequired {
		t.Fatalf("expected required user verification, got %q", session.UserVerification)
	}
	if _, err := svc.lookupDiscoverableUser(ctx)([]byte("missing-credential"), nil); err == nil {
		t.Fatal("expected unknown credential lookup to fail")
	}
}

func TestServicePasskeyCredentialCounterUpdates(t *testing.T) {
	ctx := context.Background()
	_, database := newAuthTestService(t)
	if _, err := database.UpsertAuthUser(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}); err != nil {
		t.Fatalf("UpsertAuthUser: %v", err)
	}
	if _, err := database.UpsertPairedDevice(ctx, db.PairedDeviceRecord{DeviceID: "device-1", Role: approval.RoleAdmin, DisplayName: "Phone", TokenHash: []byte("paired-token-hash"), Status: approval.StatusActive, CreatedAt: 1, LastSeenAt: 1}); err != nil {
		t.Fatalf("UpsertPairedDevice: %v", err)
	}
	record, err := database.UpsertPasskeyCredential(ctx, db.PasskeyCredentialRecord{
		ID:           "credential-record-1",
		UserID:       DefaultUserID,
		DeviceID:     "device-1",
		CredentialID: []byte("credential-id-1"),
		PublicKey:    []byte("public-key"),
		SignCount:    1,
		CreatedAt:    1,
	})
	if err != nil {
		t.Fatalf("UpsertPasskeyCredential: %v", err)
	}
	if err := database.UpdatePasskeyCredentialUsage(ctx, record.ID, 7, 0x10, 123); err != nil {
		t.Fatalf("UpdatePasskeyCredentialUsage: %v", err)
	}
	updated, err := database.GetPasskeyCredential(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetPasskeyCredential: %v", err)
	}
	if updated.SignCount != 7 || !updated.BackupState || updated.LastUsedAt != 123 {
		t.Fatalf("expected counter and metadata update, got %#v", updated)
	}
}

func TestServicePasskeyRegistrationRequiresRecentStepUpAfterBootstrap(t *testing.T) {
	ctx := context.Background()
	svc, database := newAuthTestService(t)
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }
	user := db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}
	if _, err := database.UpsertAuthUser(ctx, user); err != nil {
		t.Fatalf("UpsertAuthUser: %v", err)
	}
	if _, err := database.UpsertPairedDevice(ctx, db.PairedDeviceRecord{DeviceID: "device-1", Role: approval.RoleAdmin, DisplayName: "Phone", TokenHash: []byte("paired-token-hash"), Status: approval.StatusActive, CreatedAt: now.UnixMilli(), LastSeenAt: now.UnixMilli()}); err != nil {
		t.Fatalf("UpsertPairedDevice: %v", err)
	}

	if err := svc.requireRegistrationAuthorization(ctx, user.ID, ""); err != nil {
		t.Fatalf("first registration should bootstrap without session: %v", err)
	}
	if _, err := database.UpsertPasskeyCredential(ctx, db.PasskeyCredentialRecord{
		ID:           "credential-record-1",
		UserID:       DefaultUserID,
		DeviceID:     "device-1",
		CredentialID: []byte("credential-id-1"),
		PublicKey:    []byte("public-key"),
		CreatedAt:    now.UnixMilli(),
	}); err != nil {
		t.Fatalf("UpsertPasskeyCredential: %v", err)
	}
	if err := svc.requireRegistrationAuthorization(ctx, user.ID, ""); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("expected session requirement for additional passkey, got %v", err)
	}
	issued, err := svc.issueSession(ctx, user, approval.RoleAdmin, "credential-record-1", "device-1", "", "")
	if err != nil {
		t.Fatalf("issueSession: %v", err)
	}
	if err := svc.requireRegistrationAuthorization(ctx, user.ID, issued.SessionToken); !errors.Is(err, ErrRecentStepUp) {
		t.Fatalf("expected recent step-up requirement, got %v", err)
	}
	if err := database.UpdateAuthSessionStepUp(ctx, issued.Session.ID, now.UnixMilli(), "credential-record-1", "register-passkey"); err != nil {
		t.Fatalf("UpdateAuthSessionStepUp: %v", err)
	}
	if err := svc.requireRegistrationAuthorization(ctx, user.ID, issued.SessionToken); err != nil {
		t.Fatalf("recently stepped-up session should be allowed: %v", err)
	}
}

func TestServiceSessionsHashExpireRevokeAndCascade(t *testing.T) {
	ctx := context.Background()
	svc, database := newAuthTestService(t)
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }
	if _, err := database.UpsertAuthUser(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}); err != nil {
		t.Fatalf("UpsertAuthUser: %v", err)
	}
	if _, err := database.UpsertPairedDevice(ctx, db.PairedDeviceRecord{DeviceID: "device-1", Role: approval.RoleAdmin, DisplayName: "Phone", TokenHash: []byte("paired-token-hash"), Status: approval.StatusActive, CreatedAt: now.UnixMilli(), LastSeenAt: now.UnixMilli()}); err != nil {
		t.Fatalf("UpsertPairedDevice: %v", err)
	}

	issued, err := svc.issueSession(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}, approval.RoleAdmin, "credential-1", "device-1", "", "")
	if err != nil {
		t.Fatalf("issueSession: %v", err)
	}
	if string(issued.Session.TokenHash) == issued.SessionToken || len(issued.Session.TokenHash) != 32 {
		t.Fatalf("session token was not stored as a SHA-256 hash")
	}
	claims, err := svc.ValidateSessionToken(ctx, issued.SessionToken)
	if err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	if claims.Session.LastSeenAt != now.UnixMilli() {
		t.Fatalf("expected last seen to be updated to now, got %d", claims.Session.LastSeenAt)
	}

	svc.now = func() time.Time { return now.Add(time.Duration(svc.cfg.Auth.SessionIdleTTLSeconds+1) * time.Second) }
	if _, err := svc.ValidateSessionToken(ctx, issued.SessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected idle expiry, got %v", err)
	}

	svc.now = func() time.Time { return now }
	issued, err = svc.issueSession(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}, approval.RoleAdmin, "credential-1", "device-1", "", "")
	if err != nil {
		t.Fatalf("issueSession for logout: %v", err)
	}
	if err := svc.RevokeSessionToken(ctx, issued.SessionToken, "logout"); err != nil {
		t.Fatalf("RevokeSessionToken: %v", err)
	}
	if _, err := svc.ValidateSessionToken(ctx, issued.SessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected revoked session to be expired, got %v", err)
	}

	issued, err = svc.issueSession(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}, approval.RoleAdmin, "credential-1", "device-1", "", "")
	if err != nil {
		t.Fatalf("issueSession for absolute expiry: %v", err)
	}
	if err := database.UpdateAuthSessionActivity(ctx, issued.Session.ID, now.UnixMilli(), now.Add(2*time.Hour).UnixMilli()); err != nil {
		t.Fatalf("extend idle expiry: %v", err)
	}
	svc.now = func() time.Time {
		return now.Add(time.Duration(svc.cfg.Auth.SessionAbsoluteTTLSeconds+1) * time.Second)
	}
	if _, err := svc.ValidateSessionToken(ctx, issued.SessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected absolute expiry, got %v", err)
	}

	svc.now = func() time.Time { return now }
	issued, err = svc.issueSession(ctx, db.AuthUserRecord{ID: DefaultUserID, DisplayName: DefaultUserDisplayName}, approval.RoleAdmin, "credential-1", "device-1", "", "")
	if err != nil {
		t.Fatalf("issueSession for cascade: %v", err)
	}
	if _, err := database.UpsertPairedDevice(ctx, db.PairedDeviceRecord{DeviceID: "device-1", Role: approval.RoleAdmin, DisplayName: "Phone", TokenHash: []byte("rotated-token-hash"), Status: approval.StatusActive, CreatedAt: now.UnixMilli(), LastSeenAt: now.Add(time.Second).UnixMilli()}); err != nil {
		t.Fatalf("rotate paired device: %v", err)
	}
	cascaded, err := database.GetAuthSession(ctx, issued.Session.ID)
	if err != nil {
		t.Fatalf("GetAuthSession: %v", err)
	}
	if cascaded.RevokedAt == 0 || cascaded.RevokedReason != "device-token-rotated" {
		t.Fatalf("expected device token rotation to revoke session, got %#v", cascaded)
	}
}

func TestServiceAuditDoesNotRecordRawTokensOrCredentialPayloads(t *testing.T) {
	ctx := context.Background()
	database := openAuthTestDB(t)
	audit := &security.AuditLogger{DB: database, Key: []byte("test-audit-key")}
	svc, err := NewService(authTestConfig(), database, audit)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, _ = svc.ValidateSessionToken(ctx, "raw-token-secret")

	rows, err := database.SQL.QueryContext(ctx, `SELECT payload_json FROM audit_events`)
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected an auth audit event")
	}
	var payload string
	if err := rows.Scan(&payload); err != nil {
		t.Fatalf("scan audit payload: %v", err)
	}
	if strings.Contains(payload, "raw-token-secret") || strings.Contains(strings.ToLower(payload), "clientdatajson") {
		t.Fatalf("audit payload leaked sensitive material: %s", payload)
	}
}

func TestAuthMigrationsAreAdditiveForPairedDevices(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := legacy.ExecContext(ctx, `CREATE TABLE paired_devices(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL UNIQUE,
		role TEXT NOT NULL,
		display_name TEXT NOT NULL,
		token_hash BLOB NOT NULL,
		status TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL,
		revoked_at INTEGER NOT NULL DEFAULT 0,
		metadata_json TEXT NOT NULL DEFAULT '{}'
	)`); err != nil {
		t.Fatalf("create legacy paired_devices: %v", err)
	}
	if _, err := legacy.ExecContext(ctx, `INSERT INTO paired_devices(device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json) VALUES('device-legacy', 'admin', 'Legacy', x'010203', 'active', 1, 2, 0, '{}')`); err != nil {
		t.Fatalf("seed legacy paired device: %v", err)
	}
	_ = legacy.Close()

	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open migrated db: %v", err)
	}
	defer database.Close()
	device, err := database.GetPairedDevice(ctx, "device-legacy")
	if err != nil {
		t.Fatalf("GetPairedDevice: %v", err)
	}
	if device.DisplayName != "Legacy" || device.Status != approval.StatusActive || len(device.TokenHash) != 3 {
		t.Fatalf("paired device was destructively changed: %#v", device)
	}
	for _, table := range []string{"auth_users", "passkey_credentials", "webauthn_ceremonies", "auth_sessions"} {
		var name string
		if err := database.SQL.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("expected auth table %s after migration: %v", table, err)
		}
	}
}

func newAuthTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database := openAuthTestDB(t)
	svc, err := NewService(authTestConfig(), database, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, database
}

func openAuthTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "auth-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func authTestConfig() config.Config {
	cfg := config.Default()
	cfg.Auth.Enabled = true
	cfg.Auth.RPID = "localhost"
	cfg.Auth.RPDisplayName = "OR3 Test"
	cfg.Auth.AllowedOrigins = []string{"http://localhost:3000"}
	cfg.Auth.SessionIdleTTLSeconds = 60
	cfg.Auth.SessionAbsoluteTTLSeconds = 3600
	cfg.Auth.StepUpTTLSeconds = 120
	cfg.Auth.FallbackPolicy = config.AuthFallbackPairedTokenPlusWarn
	cfg.Auth.EnforcementMode = config.AuthEnforcementWarn
	cfg.Auth.RequirePasskeyForSensitive = true
	return cfg
}
