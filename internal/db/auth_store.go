package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	libwebauthn "github.com/go-webauthn/webauthn/webauthn"
)

type AuthUserRecord struct {
	ID          string
	DisplayName string
	CreatedAt   int64
	UpdatedAt   int64
	DisabledAt  int64
}

type PasskeyCredentialRecord struct {
	ID                   string
	UserID               string
	DeviceID             string
	CredentialID         []byte
	PublicKey            []byte
	SignCount            uint32
	Transports           []string
	AAGUID               string
	AttestationType      string
	AttestationFormat    string
	BackupEligible       bool
	BackupState          bool
	Flags                byte
	UserVerifiedRequired bool
	Nickname             string
	CredentialJSON       string
	CreatedAt            int64
	LastUsedAt           int64
	RevokedAt            int64
	RevokedReason        string
}

type WebAuthnCeremonyRecord struct {
	ID              string
	Type            string
	UserID          string
	DeviceID        string
	SessionID       string
	ChallengeHash   string
	SessionDataJSON string
	RPID            string
	Origin          string
	Reason          string
	CreatedAt       int64
	ExpiresAt       int64
	ConsumedAt      int64
	FailedAt        int64
	FailureReason   string
	FailureCount    int
}

type AuthSessionRecord struct {
	ID                     string
	UserID                 string
	DeviceID               string
	CredentialID           string
	TokenHash              []byte
	Role                   string
	CreatedAt              int64
	LastSeenAt             int64
	IdleExpiresAt          int64
	AbsoluteExpiresAt      int64
	RevokedAt              int64
	RevokedReason          string
	LastStepUpAt           int64
	LastStepUpCredentialID string
	LastStepUpReason       string
	UserAgentHash          string
	RemoteAddrHash         string
}

type AuthRecoveryCodeRecord struct {
	ID        string
	UserID    string
	CodeHash  string
	CreatedAt int64
	UsedAt    int64
	RevokedAt int64
}

func (r PasskeyCredentialRecord) ToWebAuthnCredential() (libwebauthn.Credential, error) {
	if strings.TrimSpace(r.CredentialJSON) != "" {
		var credential libwebauthn.Credential
		if err := json.Unmarshal([]byte(r.CredentialJSON), &credential); err == nil {
			return credential, nil
		}
	}
	return libwebauthn.Credential{
		ID:                append([]byte(nil), r.CredentialID...),
		PublicKey:         append([]byte(nil), r.PublicKey...),
		AttestationType:   strings.TrimSpace(r.AttestationType),
		AttestationFormat: strings.TrimSpace(r.AttestationFormat),
		Transport:         toAuthenticatorTransports(r.Transports),
		Flags:             libwebauthn.NewCredentialFlags(protocol.AuthenticatorFlags(r.Flags)),
		Authenticator: libwebauthn.Authenticator{
			AAGUID:    mustDecodeHex(r.AAGUID),
			SignCount: r.SignCount,
		},
	}, nil
}

func (d *DB) UpsertAuthUser(ctx context.Context, input AuthUserRecord) (AuthUserRecord, error) {
	if strings.TrimSpace(input.ID) == "" {
		return AuthUserRecord{}, fmt.Errorf("auth user id required")
	}
	now := input.UpdatedAt
	if now <= 0 {
		now = NowMS()
	}
	createdAt := input.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		input.DisplayName = "OR3 Owner"
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO auth_users(id, display_name, created_at, updated_at, disabled_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name, updated_at=excluded.updated_at, disabled_at=excluded.disabled_at`,
		strings.TrimSpace(input.ID), strings.TrimSpace(input.DisplayName), createdAt, now, input.DisabledAt)
	if err != nil {
		return AuthUserRecord{}, err
	}
	return d.GetAuthUser(ctx, input.ID)
}

func (d *DB) GetAuthUser(ctx context.Context, id string) (AuthUserRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, display_name, created_at, updated_at, disabled_at FROM auth_users WHERE id=?`, strings.TrimSpace(id))
	return scanAuthUser(row)
}

func (d *DB) ListAuthUsers(ctx context.Context, limit int) ([]AuthUserRecord, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, display_name, created_at, updated_at, disabled_at FROM auth_users ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuthUserRecord{}
	for rows.Next() {
		rec, err := scanAuthUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) UpsertPasskeyCredential(ctx context.Context, input PasskeyCredentialRecord) (PasskeyCredentialRecord, error) {
	if strings.TrimSpace(input.ID) == "" {
		return PasskeyCredentialRecord{}, fmt.Errorf("passkey id required")
	}
	if strings.TrimSpace(input.UserID) == "" {
		return PasskeyCredentialRecord{}, fmt.Errorf("passkey user id required")
	}
	if len(input.CredentialID) == 0 {
		return PasskeyCredentialRecord{}, fmt.Errorf("credential id required")
	}
	if len(input.PublicKey) == 0 {
		return PasskeyCredentialRecord{}, fmt.Errorf("public key required")
	}
	createdAt := input.CreatedAt
	if createdAt <= 0 {
		createdAt = NowMS()
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO passkey_credentials(
		id, user_id, device_id, credential_id, public_key, sign_count, transports, aaguid, attestation_type, attestation_format,
		backup_eligible, backup_state, flags, user_verified_required, nickname, credential_json, created_at, last_used_at, revoked_at, revoked_reason
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		user_id=excluded.user_id,
		device_id=excluded.device_id,
		public_key=excluded.public_key,
		sign_count=excluded.sign_count,
		transports=excluded.transports,
		aaguid=excluded.aaguid,
		attestation_type=excluded.attestation_type,
		attestation_format=excluded.attestation_format,
		backup_eligible=excluded.backup_eligible,
		backup_state=excluded.backup_state,
		flags=excluded.flags,
		user_verified_required=excluded.user_verified_required,
		nickname=excluded.nickname,
		credential_json=excluded.credential_json,
		last_used_at=excluded.last_used_at,
		revoked_at=excluded.revoked_at,
		revoked_reason=excluded.revoked_reason`,
		input.ID, input.UserID, strings.TrimSpace(input.DeviceID), input.CredentialID, input.PublicKey, input.SignCount,
		strings.Join(compactStringSlice(input.Transports), ","), strings.TrimSpace(input.AAGUID), strings.TrimSpace(input.AttestationType), strings.TrimSpace(input.AttestationFormat),
		boolToInt(input.BackupEligible), boolToInt(input.BackupState), input.Flags, boolToInt(input.UserVerifiedRequired), strings.TrimSpace(input.Nickname), input.CredentialJSON,
		createdAt, input.LastUsedAt, input.RevokedAt, strings.TrimSpace(input.RevokedReason))
	if err != nil {
		return PasskeyCredentialRecord{}, err
	}
	return d.GetPasskeyCredential(ctx, input.ID)
}

func (d *DB) GetPasskeyCredential(ctx context.Context, id string) (PasskeyCredentialRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, device_id, credential_id, public_key, sign_count, transports, aaguid, attestation_type, attestation_format, backup_eligible, backup_state, flags, user_verified_required, nickname, credential_json, created_at, last_used_at, revoked_at, revoked_reason FROM passkey_credentials WHERE id=?`, strings.TrimSpace(id))
	return scanPasskeyCredential(row)
}

func (d *DB) FindPasskeyCredentialByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredentialRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, device_id, credential_id, public_key, sign_count, transports, aaguid, attestation_type, attestation_format, backup_eligible, backup_state, flags, user_verified_required, nickname, credential_json, created_at, last_used_at, revoked_at, revoked_reason FROM passkey_credentials WHERE credential_id=?`, credentialID)
	rec, err := scanPasskeyCredential(row)
	if err == sql.ErrNoRows {
		return PasskeyCredentialRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) ListPasskeyCredentialsByUser(ctx context.Context, userID string, includeRevoked bool) ([]PasskeyCredentialRecord, error) {
	query := `SELECT id, user_id, device_id, credential_id, public_key, sign_count, transports, aaguid, attestation_type, attestation_format, backup_eligible, backup_state, flags, user_verified_required, nickname, credential_json, created_at, last_used_at, revoked_at, revoked_reason FROM passkey_credentials WHERE user_id=?`
	args := []any{strings.TrimSpace(userID)}
	if !includeRevoked {
		query += ` AND revoked_at=0`
	}
	query += ` ORDER BY created_at ASC`
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PasskeyCredentialRecord{}
	for rows.Next() {
		rec, err := scanPasskeyCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) RenamePasskeyCredential(ctx context.Context, id, nickname string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE passkey_credentials SET nickname=? WHERE id=?`, strings.TrimSpace(nickname), strings.TrimSpace(id))
	return err
}

func (d *DB) UpdatePasskeyCredentialUsage(ctx context.Context, id string, signCount uint32, flags byte, lastUsedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE passkey_credentials SET sign_count=?, flags=?, backup_state=?, last_used_at=? WHERE id=?`, signCount, flags, boolToInt((flags&0x10) == 0x10), lastUsedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) RevokePasskeyCredential(ctx context.Context, id, reason string, revokedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE passkey_credentials SET revoked_at=?, revoked_reason=? WHERE id=?`, revokedAt, strings.TrimSpace(reason), strings.TrimSpace(id))
	return err
}

func (d *DB) CreateWebAuthnCeremony(ctx context.Context, input WebAuthnCeremonyRecord) (WebAuthnCeremonyRecord, error) {
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO webauthn_ceremonies(id, type, user_id, device_id, session_id, challenge_hash, session_data_json, rp_id, origin, reason, created_at, expires_at, consumed_at, failed_at, failure_reason, failure_count)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.Type, strings.TrimSpace(input.UserID), strings.TrimSpace(input.DeviceID), strings.TrimSpace(input.SessionID), strings.TrimSpace(input.ChallengeHash), input.SessionDataJSON, strings.TrimSpace(input.RPID), strings.TrimSpace(input.Origin), strings.TrimSpace(input.Reason), input.CreatedAt, input.ExpiresAt, input.ConsumedAt, input.FailedAt, strings.TrimSpace(input.FailureReason), input.FailureCount)
	if err != nil {
		return WebAuthnCeremonyRecord{}, err
	}
	return d.GetWebAuthnCeremony(ctx, input.ID)
}

func (d *DB) GetWebAuthnCeremony(ctx context.Context, id string) (WebAuthnCeremonyRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, type, user_id, device_id, session_id, challenge_hash, session_data_json, rp_id, origin, reason, created_at, expires_at, consumed_at, failed_at, failure_reason, failure_count FROM webauthn_ceremonies WHERE id=?`, strings.TrimSpace(id))
	return scanWebAuthnCeremony(row)
}

func (d *DB) ConsumeWebAuthnCeremony(ctx context.Context, id string, consumedAt int64) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE webauthn_ceremonies SET consumed_at=? WHERE id=? AND consumed_at=0`, consumedAt, strings.TrimSpace(id))
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) MarkWebAuthnCeremonyFailure(ctx context.Context, id, reason string, failedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE webauthn_ceremonies SET failed_at=?, failure_reason=?, failure_count=failure_count+1 WHERE id=?`, failedAt, strings.TrimSpace(reason), strings.TrimSpace(id))
	return err
}

func (d *DB) DeleteExpiredWebAuthnCeremonies(ctx context.Context, nowMS int64) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `DELETE FROM webauthn_ceremonies WHERE expires_at>0 AND expires_at<?`, nowMS)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

func (d *DB) CreateAuthSession(ctx context.Context, input AuthSessionRecord) (AuthSessionRecord, error) {
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO auth_sessions(id, user_id, device_id, credential_id, token_hash, role, created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at, revoked_reason, last_step_up_at, last_step_up_credential_id, last_step_up_reason, user_agent_hash, remote_addr_hash)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, strings.TrimSpace(input.UserID), strings.TrimSpace(input.DeviceID), strings.TrimSpace(input.CredentialID), input.TokenHash, strings.TrimSpace(input.Role), input.CreatedAt, input.LastSeenAt, input.IdleExpiresAt, input.AbsoluteExpiresAt, input.RevokedAt, strings.TrimSpace(input.RevokedReason), input.LastStepUpAt, strings.TrimSpace(input.LastStepUpCredentialID), strings.TrimSpace(input.LastStepUpReason), strings.TrimSpace(input.UserAgentHash), strings.TrimSpace(input.RemoteAddrHash))
	if err != nil {
		return AuthSessionRecord{}, err
	}
	return d.GetAuthSession(ctx, input.ID)
}

func (d *DB) GetAuthSession(ctx context.Context, id string) (AuthSessionRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, device_id, credential_id, token_hash, role, created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at, revoked_reason, last_step_up_at, last_step_up_credential_id, last_step_up_reason, user_agent_hash, remote_addr_hash FROM auth_sessions WHERE id=?`, strings.TrimSpace(id))
	return scanAuthSession(row)
}

func (d *DB) ListAuthSessionsByUser(ctx context.Context, userID string, includeRevoked bool) ([]AuthSessionRecord, error) {
	query := `SELECT id, user_id, device_id, credential_id, token_hash, role, created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at, revoked_reason, last_step_up_at, last_step_up_credential_id, last_step_up_reason, user_agent_hash, remote_addr_hash FROM auth_sessions WHERE user_id=?`
	args := []any{strings.TrimSpace(userID)}
	if !includeRevoked {
		query += ` AND revoked_at=0`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuthSessionRecord{}
	for rows.Next() {
		rec, err := scanAuthSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) FindAuthSessionByTokenHash(ctx context.Context, tokenHash []byte) (AuthSessionRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, device_id, credential_id, token_hash, role, created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at, revoked_reason, last_step_up_at, last_step_up_credential_id, last_step_up_reason, user_agent_hash, remote_addr_hash FROM auth_sessions WHERE token_hash=?`, tokenHash)
	rec, err := scanAuthSession(row)
	if err == sql.ErrNoRows {
		return AuthSessionRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) UpdateAuthSessionActivity(ctx context.Context, id string, lastSeenAt, idleExpiresAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE auth_sessions SET last_seen_at=?, idle_expires_at=? WHERE id=?`, lastSeenAt, idleExpiresAt, strings.TrimSpace(id))
	return err
}

func (d *DB) UpdateAuthSessionStepUp(ctx context.Context, id string, lastStepUpAt int64, credentialID, reason string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE auth_sessions SET last_step_up_at=?, last_step_up_credential_id=?, last_step_up_reason=? WHERE id=?`, lastStepUpAt, strings.TrimSpace(credentialID), strings.TrimSpace(reason), strings.TrimSpace(id))
	return err
}

func (d *DB) RevokeAuthSession(ctx context.Context, id, reason string, revokedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=?, revoked_reason=? WHERE id=?`, revokedAt, strings.TrimSpace(reason), strings.TrimSpace(id))
	return err
}

func (d *DB) RevokeAuthSessionsByDevice(ctx context.Context, deviceID, reason string, revokedAt int64) (int64, error) {
	return revokeAuthSessionsByDeviceExec(ctx, d.SQL, deviceID, reason, revokedAt)
}

func revokeAuthSessionsByDeviceExec(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, deviceID, reason string, revokedAt int64) (int64, error) {
	res, err := exec.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=?, revoked_reason=? WHERE device_id=? AND revoked_at=0`, revokedAt, strings.TrimSpace(reason), strings.TrimSpace(deviceID))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) RevokeAuthSessionsByCredential(ctx context.Context, credentialID, reason string, revokedAt int64) (int64, error) {
	return revokeAuthSessionsByCredentialExec(ctx, d.SQL, credentialID, reason, revokedAt)
}

func revokeAuthSessionsByCredentialExec(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, credentialID, reason string, revokedAt int64) (int64, error) {
	res, err := exec.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=?, revoked_reason=? WHERE credential_id=? AND revoked_at=0`, revokedAt, strings.TrimSpace(reason), strings.TrimSpace(credentialID))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) RevokeAuthSessionsByUser(ctx context.Context, userID, reason string, revokedAt int64) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=?, revoked_reason=? WHERE user_id=? AND revoked_at=0`, revokedAt, strings.TrimSpace(reason), strings.TrimSpace(userID))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) RevokeExpiredAuthSessions(ctx context.Context, nowMS int64) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE auth_sessions
		SET revoked_at=?, revoked_reason=CASE WHEN revoked_reason='' THEN 'expired' ELSE revoked_reason END
		WHERE revoked_at=0 AND (idle_expires_at<? OR absolute_expires_at<?)`, nowMS, nowMS, nowMS)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) CreateAuthRecoveryCode(ctx context.Context, input AuthRecoveryCodeRecord) (AuthRecoveryCodeRecord, error) {
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO auth_recovery_codes(id, user_id, code_hash, created_at, used_at, revoked_at) VALUES(?, ?, ?, ?, ?, ?)`, input.ID, strings.TrimSpace(input.UserID), strings.TrimSpace(input.CodeHash), input.CreatedAt, input.UsedAt, input.RevokedAt)
	if err != nil {
		return AuthRecoveryCodeRecord{}, err
	}
	return d.GetAuthRecoveryCode(ctx, input.ID)
}

func (d *DB) GetAuthRecoveryCode(ctx context.Context, id string) (AuthRecoveryCodeRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, code_hash, created_at, used_at, revoked_at FROM auth_recovery_codes WHERE id=?`, strings.TrimSpace(id))
	return scanAuthRecoveryCode(row)
}

func (d *DB) FindAuthRecoveryCodeByHash(ctx context.Context, codeHash string) (AuthRecoveryCodeRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, user_id, code_hash, created_at, used_at, revoked_at FROM auth_recovery_codes WHERE code_hash=?`, strings.TrimSpace(codeHash))
	rec, err := scanAuthRecoveryCode(row)
	if err == sql.ErrNoRows {
		return AuthRecoveryCodeRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) ListAuthRecoveryCodesByUser(ctx context.Context, userID string, includeUsedOrRevoked bool) ([]AuthRecoveryCodeRecord, error) {
	query := `SELECT id, user_id, code_hash, created_at, used_at, revoked_at FROM auth_recovery_codes WHERE user_id=?`
	args := []any{strings.TrimSpace(userID)}
	if !includeUsedOrRevoked {
		query += ` AND used_at=0 AND revoked_at=0`
	}
	query += ` ORDER BY created_at ASC`
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuthRecoveryCodeRecord{}
	for rows.Next() {
		rec, err := scanAuthRecoveryCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) MarkAuthRecoveryCodeUsed(ctx context.Context, id string, usedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE auth_recovery_codes SET used_at=? WHERE id=?`, usedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) RevokeAuthRecoveryCode(ctx context.Context, id string, revokedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE auth_recovery_codes SET revoked_at=? WHERE id=?`, revokedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) RevokeAuthRecoveryCodesByUser(ctx context.Context, userID string, revokedAt int64) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE auth_recovery_codes SET revoked_at=? WHERE user_id=? AND revoked_at=0`, revokedAt, strings.TrimSpace(userID))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanAuthUser(scanner interface{ Scan(dest ...any) error }) (AuthUserRecord, error) {
	var rec AuthUserRecord
	if err := scanner.Scan(&rec.ID, &rec.DisplayName, &rec.CreatedAt, &rec.UpdatedAt, &rec.DisabledAt); err != nil {
		return AuthUserRecord{}, err
	}
	return rec, nil
}

func scanPasskeyCredential(scanner interface{ Scan(dest ...any) error }) (PasskeyCredentialRecord, error) {
	var rec PasskeyCredentialRecord
	var transports string
	var backupEligible, backupState, uvRequired int
	var signCount int64
	var flags int64
	if err := scanner.Scan(&rec.ID, &rec.UserID, &rec.DeviceID, &rec.CredentialID, &rec.PublicKey, &signCount, &transports, &rec.AAGUID, &rec.AttestationType, &rec.AttestationFormat, &backupEligible, &backupState, &flags, &uvRequired, &rec.Nickname, &rec.CredentialJSON, &rec.CreatedAt, &rec.LastUsedAt, &rec.RevokedAt, &rec.RevokedReason); err != nil {
		return PasskeyCredentialRecord{}, err
	}
	rec.SignCount = uint32(signCount)
	rec.Transports = compactStringSlice(strings.Split(strings.TrimSpace(transports), ","))
	rec.BackupEligible = backupEligible == 1
	rec.BackupState = backupState == 1
	rec.Flags = byte(flags)
	rec.UserVerifiedRequired = uvRequired != 0
	return rec, nil
}

func scanWebAuthnCeremony(scanner interface{ Scan(dest ...any) error }) (WebAuthnCeremonyRecord, error) {
	var rec WebAuthnCeremonyRecord
	if err := scanner.Scan(&rec.ID, &rec.Type, &rec.UserID, &rec.DeviceID, &rec.SessionID, &rec.ChallengeHash, &rec.SessionDataJSON, &rec.RPID, &rec.Origin, &rec.Reason, &rec.CreatedAt, &rec.ExpiresAt, &rec.ConsumedAt, &rec.FailedAt, &rec.FailureReason, &rec.FailureCount); err != nil {
		return WebAuthnCeremonyRecord{}, err
	}
	return rec, nil
}

func scanAuthSession(scanner interface{ Scan(dest ...any) error }) (AuthSessionRecord, error) {
	var rec AuthSessionRecord
	if err := scanner.Scan(&rec.ID, &rec.UserID, &rec.DeviceID, &rec.CredentialID, &rec.TokenHash, &rec.Role, &rec.CreatedAt, &rec.LastSeenAt, &rec.IdleExpiresAt, &rec.AbsoluteExpiresAt, &rec.RevokedAt, &rec.RevokedReason, &rec.LastStepUpAt, &rec.LastStepUpCredentialID, &rec.LastStepUpReason, &rec.UserAgentHash, &rec.RemoteAddrHash); err != nil {
		return AuthSessionRecord{}, err
	}
	return rec, nil
}

func scanAuthRecoveryCode(scanner interface{ Scan(dest ...any) error }) (AuthRecoveryCodeRecord, error) {
	var rec AuthRecoveryCodeRecord
	if err := scanner.Scan(&rec.ID, &rec.UserID, &rec.CodeHash, &rec.CreatedAt, &rec.UsedAt, &rec.RevokedAt); err != nil {
		return AuthRecoveryCodeRecord{}, err
	}
	return rec, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func compactStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func toAuthenticatorTransports(values []string) []protocol.AuthenticatorTransport {
	out := make([]protocol.AuthenticatorTransport, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, protocol.AuthenticatorTransport(value))
		}
	}
	return out
}

func mustDecodeHex(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil
	}
	return decoded
}

func EncodeCredentialIDHex(raw []byte) string {
	return hex.EncodeToString(raw)
}

func DecodeCredentialJSON(raw string, out any) error {
	return json.Unmarshal([]byte(raw), out)
}
