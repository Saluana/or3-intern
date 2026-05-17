package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SecureConnectionHostIdentityRecord struct {
	HostID               string
	HostSigningPublicKey string
	HostNoisePublicKey   string
	Fingerprint          string
	Status               string
	CreatedAt            int64
	RotatedAt            int64
	RecoveryRequired     bool
	Metadata             map[string]any
}

type SecureConnectionDeviceRecord struct {
	DeviceID               string
	HostID                 string
	DisplayName            string
	Platform               string
	Role                   string
	Capabilities           []string
	TrustLevel             string
	DeviceSigningPublicKey string
	DeviceNoisePublicKey   string
	EnrollmentCertificate  []byte
	EnrollmentEpoch        int64
	Status                 string
	CreatedAt              int64
	LastSeenAt             int64
	RevokedAt              int64
	RevokedReason          string
	AccountID              string
	Metadata               map[string]any
}

type SecureConnectionSessionRecord struct {
	SessionID       string
	DeviceID        string
	HostID          string
	RelayRouteID    string
	EnrollmentEpoch int64
	Status          string
	CreatedAt       int64
	LastSeenAt      int64
	ExpiresAt       int64
	StepUpAt        int64
	LastSequenceIn  int64
	LastSequenceOut int64
	Metadata        map[string]any
}

type SecureConnectionPairingSessionRecord struct {
	RendezvousID     string
	HostID           string
	SecretCommitment string
	Status           string
	RequestedRole    string
	Capabilities     []string
	RelayOrigin      string
	AccountID        string
	CreatedAt        int64
	ExpiresAt        int64
	JoinedAt         int64
	ConsumedAt       int64
	Metadata         map[string]any
}

func (d *DB) UpsertSecureConnectionHostIdentity(ctx context.Context, rec SecureConnectionHostIdentityRecord) error {
	if strings.TrimSpace(rec.HostID) == "" {
		return fmt.Errorf("host ID required")
	}
	recoveryRequired := 0
	if rec.RecoveryRequired {
		recoveryRequired = 1
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO secure_connection_host_identity(host_id, host_signing_public_key, host_noise_public_key, fingerprint, status, created_at, rotated_at, recovery_required, metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(host_id) DO UPDATE SET host_signing_public_key=excluded.host_signing_public_key, host_noise_public_key=excluded.host_noise_public_key, fingerprint=excluded.fingerprint, status=excluded.status, rotated_at=excluded.rotated_at, recovery_required=excluded.recovery_required, metadata_json=excluded.metadata_json`,
		strings.TrimSpace(rec.HostID), rec.HostSigningPublicKey, rec.HostNoisePublicKey, rec.Fingerprint, rec.Status, rec.CreatedAt, rec.RotatedAt, recoveryRequired, mustJSONMap(rec.Metadata))
	return err
}

func (d *DB) GetSecureConnectionHostIdentity(ctx context.Context, hostID string) (SecureConnectionHostIdentityRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT host_id, host_signing_public_key, host_noise_public_key, fingerprint, status, created_at, rotated_at, recovery_required, metadata_json FROM secure_connection_host_identity WHERE host_id=?`, strings.TrimSpace(hostID))
	rec, err := scanSecureConnectionHostIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SecureConnectionHostIdentityRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) UpsertSecureConnectionDevice(ctx context.Context, rec SecureConnectionDeviceRecord) (SecureConnectionDeviceRecord, error) {
	if strings.TrimSpace(rec.DeviceID) == "" || strings.TrimSpace(rec.HostID) == "" {
		return SecureConnectionDeviceRecord{}, fmt.Errorf("device ID and host ID required")
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO secure_connection_devices(device_id, host_id, display_name, platform, role, capabilities_json, trust_level, device_signing_public_key, device_noise_public_key, enrollment_certificate, enrollment_epoch, status, created_at, last_seen_at, revoked_at, revoked_reason, account_id, metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET display_name=excluded.display_name, platform=excluded.platform, role=excluded.role, capabilities_json=excluded.capabilities_json, trust_level=excluded.trust_level, device_signing_public_key=excluded.device_signing_public_key, device_noise_public_key=excluded.device_noise_public_key, enrollment_certificate=excluded.enrollment_certificate, enrollment_epoch=excluded.enrollment_epoch, status=excluded.status, last_seen_at=excluded.last_seen_at, revoked_at=excluded.revoked_at, revoked_reason=excluded.revoked_reason, account_id=excluded.account_id, metadata_json=excluded.metadata_json`,
		rec.DeviceID, rec.HostID, rec.DisplayName, rec.Platform, rec.Role, mustJSONStringSlice(rec.Capabilities), rec.TrustLevel, rec.DeviceSigningPublicKey, rec.DeviceNoisePublicKey, rec.EnrollmentCertificate, rec.EnrollmentEpoch, rec.Status, rec.CreatedAt, rec.LastSeenAt, rec.RevokedAt, rec.RevokedReason, rec.AccountID, mustJSONMap(rec.Metadata))
	if err != nil {
		return SecureConnectionDeviceRecord{}, err
	}
	return d.GetSecureConnectionDevice(ctx, rec.DeviceID)
}

func (d *DB) GetSecureConnectionDevice(ctx context.Context, deviceID string) (SecureConnectionDeviceRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT device_id, host_id, display_name, platform, role, capabilities_json, trust_level, device_signing_public_key, device_noise_public_key, enrollment_certificate, enrollment_epoch, status, created_at, last_seen_at, revoked_at, revoked_reason, account_id, metadata_json FROM secure_connection_devices WHERE device_id=?`, strings.TrimSpace(deviceID))
	return scanSecureConnectionDevice(row)
}

func (d *DB) FindSecureConnectionDeviceByNoiseKey(ctx context.Context, hostID, deviceNoisePublicKey string) (SecureConnectionDeviceRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT device_id, host_id, display_name, platform, role, capabilities_json, trust_level, device_signing_public_key, device_noise_public_key, enrollment_certificate, enrollment_epoch, status, created_at, last_seen_at, revoked_at, revoked_reason, account_id, metadata_json FROM secure_connection_devices WHERE host_id=? AND device_noise_public_key=?`, strings.TrimSpace(hostID), strings.TrimSpace(deviceNoisePublicKey))
	rec, err := scanSecureConnectionDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SecureConnectionDeviceRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) ListSecureConnectionDevices(ctx context.Context, hostID, status string, limit int) ([]SecureConnectionDeviceRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT device_id, host_id, display_name, platform, role, capabilities_json, trust_level, device_signing_public_key, device_noise_public_key, enrollment_certificate, enrollment_epoch, status, created_at, last_seen_at, revoked_at, revoked_reason, account_id, metadata_json FROM secure_connection_devices`
	args := []any{}
	clauses := []string{}
	if strings.TrimSpace(hostID) != "" {
		clauses = append(clauses, "host_id=?")
		args = append(args, strings.TrimSpace(hostID))
	}
	if strings.TrimSpace(status) != "" {
		clauses = append(clauses, "status=?")
		args = append(args, strings.TrimSpace(status))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SecureConnectionDeviceRecord
	for rows.Next() {
		rec, err := scanSecureConnectionDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) RevokeSecureConnectionDevice(ctx context.Context, deviceID, reason string, revokedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE secure_connection_devices SET status='revoked', revoked_at=?, revoked_reason=? WHERE device_id=?`, revokedAt, strings.TrimSpace(reason), strings.TrimSpace(deviceID))
	return err
}

func (d *DB) CreateSecureConnectionPairingSession(ctx context.Context, rec SecureConnectionPairingSessionRecord) error {
	if strings.TrimSpace(rec.RendezvousID) == "" || strings.TrimSpace(rec.SecretCommitment) == "" {
		return fmt.Errorf("rendezvous ID and secret commitment required")
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO secure_connection_pairing_sessions(rendezvous_id, host_id, secret_commitment, status, requested_role, capabilities_json, relay_origin, account_id, created_at, expires_at, joined_at, consumed_at, metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, rec.RendezvousID, rec.HostID, rec.SecretCommitment, rec.Status, rec.RequestedRole, mustJSONStringSlice(rec.Capabilities), rec.RelayOrigin, rec.AccountID, rec.CreatedAt, rec.ExpiresAt, rec.JoinedAt, rec.ConsumedAt, mustJSONMap(rec.Metadata))
	return err
}

func (d *DB) GetSecureConnectionPairingSession(ctx context.Context, rendezvousID string) (SecureConnectionPairingSessionRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT rendezvous_id, host_id, secret_commitment, status, requested_role, capabilities_json, relay_origin, account_id, created_at, expires_at, joined_at, consumed_at, metadata_json FROM secure_connection_pairing_sessions WHERE rendezvous_id=?`, strings.TrimSpace(rendezvousID))
	return scanSecureConnectionPairingSession(row)
}

func (d *DB) CompareAndSwapSecureConnectionPairingStatus(ctx context.Context, rendezvousID, fromStatus, toStatus string, timestamp int64) (bool, error) {
	field := "joined_at"
	if toStatus == "consumed" || toStatus == "approved" || toStatus == "rejected" || toStatus == "expired" {
		field = "consumed_at"
	}
	query := fmt.Sprintf(`UPDATE secure_connection_pairing_sessions SET status=?, %s=? WHERE rendezvous_id=? AND status=?`, field)
	res, err := d.SQL.ExecContext(ctx, query, strings.TrimSpace(toStatus), timestamp, strings.TrimSpace(rendezvousID), strings.TrimSpace(fromStatus))
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSecureConnectionHostIdentity(row scanner) (SecureConnectionHostIdentityRecord, error) {
	var rec SecureConnectionHostIdentityRecord
	var metadataJSON string
	var recoveryRequired int
	if err := row.Scan(&rec.HostID, &rec.HostSigningPublicKey, &rec.HostNoisePublicKey, &rec.Fingerprint, &rec.Status, &rec.CreatedAt, &rec.RotatedAt, &recoveryRequired, &metadataJSON); err != nil {
		return SecureConnectionHostIdentityRecord{}, err
	}
	rec.RecoveryRequired = recoveryRequired != 0
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func scanSecureConnectionDevice(row scanner) (SecureConnectionDeviceRecord, error) {
	var rec SecureConnectionDeviceRecord
	var capabilitiesJSON, metadataJSON string
	if err := row.Scan(&rec.DeviceID, &rec.HostID, &rec.DisplayName, &rec.Platform, &rec.Role, &capabilitiesJSON, &rec.TrustLevel, &rec.DeviceSigningPublicKey, &rec.DeviceNoisePublicKey, &rec.EnrollmentCertificate, &rec.EnrollmentEpoch, &rec.Status, &rec.CreatedAt, &rec.LastSeenAt, &rec.RevokedAt, &rec.RevokedReason, &rec.AccountID, &metadataJSON); err != nil {
		return SecureConnectionDeviceRecord{}, err
	}
	rec.Capabilities = parseJSONStringSlice(capabilitiesJSON)
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func scanSecureConnectionPairingSession(row scanner) (SecureConnectionPairingSessionRecord, error) {
	var rec SecureConnectionPairingSessionRecord
	var capabilitiesJSON, metadataJSON string
	if err := row.Scan(&rec.RendezvousID, &rec.HostID, &rec.SecretCommitment, &rec.Status, &rec.RequestedRole, &capabilitiesJSON, &rec.RelayOrigin, &rec.AccountID, &rec.CreatedAt, &rec.ExpiresAt, &rec.JoinedAt, &rec.ConsumedAt, &metadataJSON); err != nil {
		return SecureConnectionPairingSessionRecord{}, err
	}
	rec.Capabilities = parseJSONStringSlice(capabilitiesJSON)
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func mustJSONStringSlice(values []string) string {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseJSONStringSlice(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return []string{}
	}
	return out
}
