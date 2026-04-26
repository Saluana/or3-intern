package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type PairingRequestRecord struct {
	ID              int64
	DeviceID        string
	Role            string
	DisplayName     string
	Origin          string
	PairingCodeHash []byte
	RequestedAt     int64
	ExpiresAt       int64
	Status          string
	ApproverID      string
	ApprovedAt      int64
	DeniedAt        int64
	Metadata        map[string]any
}

type PairedDeviceRecord struct {
	ID          int64
	DeviceID    string
	Role        string
	DisplayName string
	TokenHash   []byte
	Status      string
	CreatedAt   int64
	LastSeenAt  int64
	RevokedAt   int64
	Metadata    map[string]any
}

type ApprovalRequestRecord struct {
	ID                 int64
	Type               string
	SubjectHash        string
	SubjectJSON        string
	RequesterAgentID   string
	RequesterSessionID string
	ExecutionHostID    string
	Status             string
	PolicyMode         string
	RequestedAt        int64
	ExpiresAt          int64
	ResolvedAt         int64
	ResolverActorID    string
	ResolutionKind     string
	ResolutionNote     string
}

type ApprovalAllowlistRecord struct {
	ID          int64
	Domain      string
	ScopeJSON   string
	MatcherJSON string
	CreatedBy   string
	CreatedAt   int64
	ExpiresAt   int64
	DisabledAt  int64
}

type ApprovalTokenRecord struct {
	ID                int64
	ApprovalRequestID int64
	SubjectHash       string
	IssuedAt          int64
	ExpiresAt         int64
	Issuer            string
	RevokedAt         int64
}

func (d *DB) CreatePairingRequest(ctx context.Context, input PairingRequestRecord) (PairingRequestRecord, error) {
	metadataJSON := mustJSONMap(input.Metadata)
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO pairing_requests(device_id, role, display_name, origin, pairing_code_hash, requested_at, expires_at, status, approver_id, approved_at, denied_at, metadata_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.DeviceID, input.Role, input.DisplayName, input.Origin, input.PairingCodeHash, input.RequestedAt, input.ExpiresAt, input.Status, input.ApproverID, input.ApprovedAt, input.DeniedAt, metadataJSON)
	if err != nil {
		return PairingRequestRecord{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return PairingRequestRecord{}, err
	}
	return d.GetPairingRequest(ctx, id)
}

func (d *DB) GetPairingRequest(ctx context.Context, id int64) (PairingRequestRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, device_id, role, display_name, origin, pairing_code_hash, requested_at, expires_at, status, approver_id, approved_at, denied_at, metadata_json FROM pairing_requests WHERE id=?`, id)
	return scanPairingRequest(row)
}

func (d *DB) ListPairingRequests(ctx context.Context, status string, limit int) ([]PairingRequestRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT id, device_id, role, display_name, origin, pairing_code_hash, requested_at, expires_at, status, approver_id, approved_at, denied_at, metadata_json FROM pairing_requests`
	args := []any{}
	if strings.TrimSpace(status) != "" {
		query += ` WHERE status=?`
		args = append(args, status)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PairingRequestRecord{}
	for rows.Next() {
		rec, err := scanPairingRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) UpdatePairingRequestStatus(ctx context.Context, id int64, status, approverID string, approvedAt, deniedAt int64, metadata map[string]any) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE pairing_requests SET status=?, approver_id=?, approved_at=?, denied_at=?, metadata_json=? WHERE id=?`, status, approverID, approvedAt, deniedAt, mustJSONMap(metadata), id)
	return err
}

func (d *DB) ResolvePairingRequestStatus(ctx context.Context, id int64, fromStatus, toStatus, approverID string, approvedAt, deniedAt int64, metadata map[string]any) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE pairing_requests SET status=?, approver_id=?, approved_at=?, denied_at=?, metadata_json=? WHERE id=? AND status=?`, toStatus, approverID, approvedAt, deniedAt, mustJSONMap(metadata), id, fromStatus)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) CompareAndSwapPairingRequestStatus(ctx context.Context, id int64, fromStatus, toStatus string) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE pairing_requests SET status=? WHERE id=? AND status=?`, toStatus, id, fromStatus)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) FindPairingRequestByCodeHash(ctx context.Context, id int64, codeHash []byte) (PairingRequestRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, device_id, role, display_name, origin, pairing_code_hash, requested_at, expires_at, status, approver_id, approved_at, denied_at, metadata_json FROM pairing_requests WHERE id=? AND pairing_code_hash=?`, id, codeHash)
	rec, err := scanPairingRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PairingRequestRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) FindPairingRequestsByCodeHash(ctx context.Context, codeHash []byte, status string, nowMS int64, limit int) ([]PairingRequestRecord, error) {
	if limit <= 0 || limit > 10 {
		limit = 10
	}
	query := `SELECT id, device_id, role, display_name, origin, pairing_code_hash, requested_at, expires_at, status, approver_id, approved_at, denied_at, metadata_json FROM pairing_requests WHERE pairing_code_hash=?`
	args := []any{codeHash}
	if strings.TrimSpace(status) != "" {
		query += ` AND status=?`
		args = append(args, strings.TrimSpace(status))
	}
	if nowMS > 0 {
		query += ` AND (expires_at<=0 OR expires_at>=?)`
		args = append(args, nowMS)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PairingRequestRecord{}
	for rows.Next() {
		rec, err := scanPairingRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) UpsertPairedDevice(ctx context.Context, input PairedDeviceRecord) (PairedDeviceRecord, error) {
	if strings.TrimSpace(input.DeviceID) == "" {
		return PairedDeviceRecord{}, fmt.Errorf("device ID required")
	}
	metadataJSON := mustJSONMap(input.Metadata)
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO paired_devices(device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET role=excluded.role, display_name=excluded.display_name, token_hash=excluded.token_hash, status=excluded.status, last_seen_at=excluded.last_seen_at, revoked_at=excluded.revoked_at, metadata_json=excluded.metadata_json`, input.DeviceID, input.Role, input.DisplayName, input.TokenHash, input.Status, input.CreatedAt, input.LastSeenAt, input.RevokedAt, metadataJSON)
	if err != nil {
		return PairedDeviceRecord{}, err
	}
	return d.GetPairedDevice(ctx, input.DeviceID)
}

func (d *DB) GetPairedDevice(ctx context.Context, deviceID string) (PairedDeviceRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json FROM paired_devices WHERE device_id=?`, strings.TrimSpace(deviceID))
	return scanPairedDevice(row)
}

func (d *DB) ListPairedDevices(ctx context.Context, limit int) ([]PairedDeviceRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return d.listPairedDevicesPage(ctx, limit, 0)
}

func (d *DB) ListPairedDevicesPage(ctx context.Context, limit, offset int) ([]PairedDeviceRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return d.listPairedDevicesPage(ctx, limit, offset)
}

func (d *DB) listPairedDevicesPage(ctx context.Context, limit, offset int) ([]PairedDeviceRecord, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json FROM paired_devices ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PairedDeviceRecord{}
	for rows.Next() {
		rec, err := scanPairedDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) FindPairedDeviceByToken(ctx context.Context, rawToken string) (PairedDeviceRecord, bool, error) {
	hash := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	row := d.SQL.QueryRowContext(ctx, `SELECT id, device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json FROM paired_devices WHERE token_hash=?`, hash[:])
	rec, err := scanPairedDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PairedDeviceRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) TouchPairedDevice(ctx context.Context, deviceID string, lastSeenAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE paired_devices SET last_seen_at=? WHERE device_id=?`, lastSeenAt, strings.TrimSpace(deviceID))
	return err
}

func (d *DB) CreateApprovalRequest(ctx context.Context, input ApprovalRequestRecord) (ApprovalRequestRecord, error) {
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO approval_requests(type, subject_hash, subject_json, requester_agent_id, requester_session_id, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.Type, input.SubjectHash, input.SubjectJSON, input.RequesterAgentID, input.RequesterSessionID, input.ExecutionHostID, input.Status, input.PolicyMode, input.RequestedAt, input.ExpiresAt, input.ResolvedAt, input.ResolverActorID, input.ResolutionKind, input.ResolutionNote)
	if err != nil {
		return ApprovalRequestRecord{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ApprovalRequestRecord{}, err
	}
	return d.GetApprovalRequest(ctx, id)
}

func (d *DB) GetApprovalRequest(ctx context.Context, id int64) (ApprovalRequestRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note FROM approval_requests WHERE id=?`, id)
	return scanApprovalRequest(row)
}

func (d *DB) FindPendingApprovalRequest(ctx context.Context, approvalType, subjectHash, hostID string, nowMS int64) (ApprovalRequestRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note
		FROM approval_requests WHERE type=? AND subject_hash=? AND execution_host_id=? AND status='pending' AND expires_at>? ORDER BY id DESC LIMIT 1`, approvalType, subjectHash, hostID, nowMS)
	rec, err := scanApprovalRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalRequestRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) ListApprovalRequests(ctx context.Context, status string, limit int) ([]ApprovalRequestRecord, error) {
	return d.ListApprovalRequestsFiltered(ctx, status, "", limit)
}

func (d *DB) ListApprovalRequestsFiltered(ctx context.Context, status, approvalType string, limit int) ([]ApprovalRequestRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note FROM approval_requests`
	args := []any{}
	clauses := make([]string, 0, 2)
	if strings.TrimSpace(status) != "" {
		clauses = append(clauses, "status=?")
		args = append(args, status)
	}
	if strings.TrimSpace(approvalType) != "" {
		clauses = append(clauses, "type=?")
		args = append(args, approvalType)
	}
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, ` AND `)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApprovalRequestRecord{}
	for rows.Next() {
		rec, err := scanApprovalRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) ExpireApprovalRequests(ctx context.Context, nowMS int64, actor, note string) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE approval_requests
		SET status=?, resolved_at=?, resolver_actor_id=?, resolution_kind=?, resolution_note=?
		WHERE status=? AND expires_at>0 AND expires_at<=?`, "expired", nowMS, actor, "expired", note, "pending", nowMS)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

func (d *DB) UpdateApprovalRequestResolution(ctx context.Context, id int64, status string, resolvedAt int64, actor, kind, note string) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE approval_requests SET status=?, resolved_at=?, resolver_actor_id=?, resolution_kind=?, resolution_note=? WHERE id=?`, status, resolvedAt, actor, kind, note, id)
	return err
}

func (d *DB) ResolveApprovalRequest(ctx context.Context, id int64, fromStatus, toStatus string, resolvedAt int64, actor, kind, note string) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE approval_requests SET status=?, resolved_at=?, resolver_actor_id=?, resolution_kind=?, resolution_note=? WHERE id=? AND status=?`, toStatus, resolvedAt, actor, kind, note, id, fromStatus)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) CreateApprovalAllowlist(ctx context.Context, input ApprovalAllowlistRecord) (ApprovalAllowlistRecord, error) {
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO approval_allowlists(domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, input.Domain, input.ScopeJSON, input.MatcherJSON, input.CreatedBy, input.CreatedAt, input.ExpiresAt, input.DisabledAt)
	if err != nil {
		return ApprovalAllowlistRecord{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ApprovalAllowlistRecord{}, err
	}
	return d.GetApprovalAllowlist(ctx, id)
}

func (d *DB) GetApprovalAllowlist(ctx context.Context, id int64) (ApprovalAllowlistRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at FROM approval_allowlists WHERE id=?`, id)
	return scanApprovalAllowlist(row)
}

func (d *DB) ListApprovalAllowlists(ctx context.Context, domain string, limit int) ([]ApprovalAllowlistRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at FROM approval_allowlists`
	args := []any{}
	if strings.TrimSpace(domain) != "" {
		query += ` WHERE domain=?`
		args = append(args, domain)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApprovalAllowlistRecord{}
	for rows.Next() {
		rec, err := scanApprovalAllowlist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) DisableApprovalAllowlist(ctx context.Context, id int64, disabledAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE approval_allowlists SET disabled_at=? WHERE id=?`, disabledAt, id)
	return err
}

func (d *DB) CreateApprovalToken(ctx context.Context, input ApprovalTokenRecord) (ApprovalTokenRecord, error) {
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO approval_tokens(approval_request_id, subject_hash, issued_at, expires_at, issuer, revoked_at) VALUES(?, ?, ?, ?, ?, ?)`, input.ApprovalRequestID, input.SubjectHash, input.IssuedAt, input.ExpiresAt, input.Issuer, input.RevokedAt)
	if err != nil {
		return ApprovalTokenRecord{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ApprovalTokenRecord{}, err
	}
	return d.GetApprovalToken(ctx, id)
}

func (d *DB) GetApprovalToken(ctx context.Context, id int64) (ApprovalTokenRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, approval_request_id, subject_hash, issued_at, expires_at, issuer, revoked_at FROM approval_tokens WHERE id=?`, id)
	return scanApprovalToken(row)
}

func (d *DB) RevokeApprovalToken(ctx context.Context, id int64, revokedAt int64) error {
	_, err := d.SQL.ExecContext(ctx, `UPDATE approval_tokens SET revoked_at=? WHERE id=?`, revokedAt, id)
	return err
}

func mustJSONMap(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	blob, _ := json.Marshal(value)
	return string(blob)
}

func scanPairingRequest(scanner interface{ Scan(dest ...any) error }) (PairingRequestRecord, error) {
	var rec PairingRequestRecord
	var metadataJSON string
	if err := scanner.Scan(&rec.ID, &rec.DeviceID, &rec.Role, &rec.DisplayName, &rec.Origin, &rec.PairingCodeHash, &rec.RequestedAt, &rec.ExpiresAt, &rec.Status, &rec.ApproverID, &rec.ApprovedAt, &rec.DeniedAt, &metadataJSON); err != nil {
		return PairingRequestRecord{}, err
	}
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func scanPairedDevice(scanner interface{ Scan(dest ...any) error }) (PairedDeviceRecord, error) {
	var rec PairedDeviceRecord
	var metadataJSON string
	if err := scanner.Scan(&rec.ID, &rec.DeviceID, &rec.Role, &rec.DisplayName, &rec.TokenHash, &rec.Status, &rec.CreatedAt, &rec.LastSeenAt, &rec.RevokedAt, &metadataJSON); err != nil {
		return PairedDeviceRecord{}, err
	}
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func scanApprovalRequest(scanner interface{ Scan(dest ...any) error }) (ApprovalRequestRecord, error) {
	var rec ApprovalRequestRecord
	if err := scanner.Scan(&rec.ID, &rec.Type, &rec.SubjectHash, &rec.SubjectJSON, &rec.RequesterAgentID, &rec.RequesterSessionID, &rec.ExecutionHostID, &rec.Status, &rec.PolicyMode, &rec.RequestedAt, &rec.ExpiresAt, &rec.ResolvedAt, &rec.ResolverActorID, &rec.ResolutionKind, &rec.ResolutionNote); err != nil {
		return ApprovalRequestRecord{}, err
	}
	return rec, nil
}

func scanApprovalAllowlist(scanner interface{ Scan(dest ...any) error }) (ApprovalAllowlistRecord, error) {
	var rec ApprovalAllowlistRecord
	if err := scanner.Scan(&rec.ID, &rec.Domain, &rec.ScopeJSON, &rec.MatcherJSON, &rec.CreatedBy, &rec.CreatedAt, &rec.ExpiresAt, &rec.DisabledAt); err != nil {
		return ApprovalAllowlistRecord{}, err
	}
	return rec, nil
}

func scanApprovalToken(scanner interface{ Scan(dest ...any) error }) (ApprovalTokenRecord, error) {
	var rec ApprovalTokenRecord
	if err := scanner.Scan(&rec.ID, &rec.ApprovalRequestID, &rec.SubjectHash, &rec.IssuedAt, &rec.ExpiresAt, &rec.Issuer, &rec.RevokedAt); err != nil {
		return ApprovalTokenRecord{}, err
	}
	return rec, nil
}

func decodeJSONMap(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}
