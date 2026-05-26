package db

import (
	"bytes"
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
	ID                   int64
	Type                 string
	SubjectHash          string
	SubjectJSON          string
	RequesterAgentID     string
	RequesterSessionID   string
	RequesterContextJSON string
	ExecutionHostID      string
	Status               string
	PolicyMode           string
	RequestedAt          int64
	ExpiresAt            int64
	ResolvedAt           int64
	ResolverActorID      string
	ResolutionKind       string
	ResolutionNote       string
}

type ApprovalAllowlistRecord struct {
	ID                  int64
	Domain              string
	ScopeJSON           string
	MatcherJSON         string
	CreatedBy           string
	CreatedAt           int64
	ExpiresAt           int64
	DisabledAt          int64
	ScopeHostID         string
	ScopeTool           string
	ScopeProfile        string
	ScopeAgent          string
	MatchExecutablePath string
	MatchWorkingDir     string
	MatchScriptHash     string
	MatchSkillID        string
	MatchPlanHash       string
	MatchRunnerID       string
	MatchTargetPath     string
	MatchPathPrefix     string
	MatchFingerprint    string
}

// ApprovalAllowlistMatchQuery scopes candidate allowlist rows for indexed matching.
type ApprovalAllowlistMatchQuery struct {
	Domain              string
	NowMS               int64
	ScopeHostID         string
	ScopeTool           string
	ScopeProfile        string
	ScopeAgent          string
	MatchExecutablePath string
	MatchWorkingDir     string
	MatchScriptHash     string
	MatchSkillID        string
	MatchPlanHash       string
	MatchRunnerID       string
	MatchTargetPath     string
	MatchPathPrefix     string
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
	metadataJSON, err := marshalJSONMap(input.Metadata)
	if err != nil {
		return PairingRequestRecord{}, err
	}
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

func (d *DB) ListPairingRequests(ctx context.Context, status string, limit int, nowMS int64) ([]PairingRequestRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT id, device_id, role, display_name, origin, pairing_code_hash, requested_at, expires_at, status, approver_id, approved_at, denied_at, metadata_json FROM pairing_requests`
	args := []any{}
	clauses := make([]string, 0, 2)
	if strings.TrimSpace(status) != "" {
		clauses = append(clauses, "status=?")
		args = append(args, status)
	}
	if strings.TrimSpace(status) == "pending" && nowMS > 0 {
		clauses = append(clauses, "(expires_at<=0 OR expires_at>=?)")
		args = append(args, nowMS)
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
	metadataJSON, err := marshalJSONMap(metadata)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, `UPDATE pairing_requests SET status=?, approver_id=?, approved_at=?, denied_at=?, metadata_json=? WHERE id=?`, status, approverID, approvedAt, deniedAt, metadataJSON, id)
	return err
}

func (d *DB) ResolvePairingRequestStatus(ctx context.Context, id int64, fromStatus, toStatus, approverID string, approvedAt, deniedAt int64, metadata map[string]any) (bool, error) {
	metadataJSON, err := marshalJSONMap(metadata)
	if err != nil {
		return false, err
	}
	res, err := d.SQL.ExecContext(ctx, `UPDATE pairing_requests SET status=?, approver_id=?, approved_at=?, denied_at=?, metadata_json=? WHERE id=? AND status=?`, toStatus, approverID, approvedAt, deniedAt, metadataJSON, id, fromStatus)
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
	metadataJSON, err := marshalJSONMap(input.Metadata)
	if err != nil {
		return PairedDeviceRecord{}, err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return PairedDeviceRecord{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	previous, prevErr := scanPairedDevice(tx.QueryRowContext(ctx, `SELECT id, device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json FROM paired_devices WHERE device_id=?`, strings.TrimSpace(input.DeviceID)))
	if prevErr != nil && !errors.Is(prevErr, sql.ErrNoRows) {
		return PairedDeviceRecord{}, prevErr
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paired_devices(device_id, role, display_name, token_hash, status, created_at, last_seen_at, revoked_at, metadata_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET role=excluded.role, display_name=excluded.display_name, token_hash=excluded.token_hash, status=excluded.status, last_seen_at=excluded.last_seen_at, revoked_at=excluded.revoked_at, metadata_json=excluded.metadata_json`, input.DeviceID, input.Role, input.DisplayName, input.TokenHash, input.Status, input.CreatedAt, input.LastSeenAt, input.RevokedAt, metadataJSON)
	if err != nil {
		return PairedDeviceRecord{}, err
	}
	shouldRevokeSessions := false
	if prevErr == nil {
		shouldRevokeSessions = input.RevokedAt > 0 || !bytes.Equal(previous.TokenHash, input.TokenHash)
	}
	if shouldRevokeSessions {
		reason := "device-token-rotated"
		revokedAt := input.LastSeenAt
		if revokedAt <= 0 {
			revokedAt = NowMS()
		}
		if input.RevokedAt > 0 {
			reason = "device-revoked"
			revokedAt = input.RevokedAt
		}
		if _, err := revokeAuthSessionsByDeviceExec(ctx, tx, input.DeviceID, reason, revokedAt); err != nil {
			return PairedDeviceRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
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

func (d *DB) FindActivePairedDeviceByChannelIdentity(ctx context.Context, channel, identity string) (bool, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	identity = strings.ToLower(strings.TrimSpace(identity))
	row := d.SQL.QueryRowContext(ctx, `SELECT 1 FROM paired_devices WHERE status='active' AND revoked_at=0 AND LOWER(json_extract(metadata_json, '$.channel'))=? AND (LOWER(json_extract(metadata_json, '$.identity'))=? OR LOWER(json_extract(metadata_json, '$.sender'))=? OR LOWER(json_extract(metadata_json, '$.user_id'))=? OR LOWER(json_extract(metadata_json, '$.chat_id'))=? OR LOWER(json_extract(metadata_json, '$.from'))=?) LIMIT 1`, channel, identity, identity, identity, identity, identity)
	var found int
	if err := row.Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (d *DB) CreateApprovalRequest(ctx context.Context, input ApprovalRequestRecord) (ApprovalRequestRecord, error) {
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO approval_requests(type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.Type, input.SubjectHash, input.SubjectJSON, input.RequesterAgentID, input.RequesterSessionID, input.RequesterContextJSON, input.ExecutionHostID, input.Status, input.PolicyMode, input.RequestedAt, input.ExpiresAt, input.ResolvedAt, input.ResolverActorID, input.ResolutionKind, input.ResolutionNote)
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
	row := d.SQL.QueryRowContext(ctx, `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note FROM approval_requests WHERE id=?`, id)
	return scanApprovalRequest(row)
}

func (d *DB) FindPendingApprovalRequest(ctx context.Context, approvalType, subjectHash, hostID string, nowMS int64) (ApprovalRequestRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note
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
	query := `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note FROM approval_requests`
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
	_, count, err := d.ExpireApprovalRequestsReturning(ctx, nowMS, actor, note)
	return count, err
}

func (d *DB) ExpirePairingRequestsReturning(ctx context.Context, nowMS int64, actor string) ([]int64, int64, error) {
	rows, err := d.SQL.QueryContext(ctx, `UPDATE pairing_requests
		SET status=?, denied_at=?
		WHERE status=? AND expires_at>0 AND expires_at<=?
		RETURNING id`, "expired", nowMS, "pending", nowMS)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return ids, int64(len(ids)), nil
}

func (d *DB) ExpireApprovalRequestsReturning(ctx context.Context, nowMS int64, actor, note string) ([]int64, int64, error) {
	rows, err := d.SQL.QueryContext(ctx, `UPDATE approval_requests
		SET status=?, resolved_at=?, resolver_actor_id=?, resolution_kind=?, resolution_note=?
		WHERE status=? AND expires_at>0 AND expires_at<=?
		RETURNING id`, "expired", nowMS, actor, "expired", note, "pending", nowMS)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return ids, int64(len(ids)), nil
}

func (d *DB) CountApprovalRequests(ctx context.Context, status, approvalType string) (int64, error) {
	query := `SELECT COUNT(*) FROM approval_requests`
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
	var count int64
	if err := d.SQL.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (d *DB) CreateOrGetPendingApprovalRequest(ctx context.Context, input ApprovalRequestRecord, nowMS int64) (ApprovalRequestRecord, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalRequestRecord{}, false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	existing, ok, err := findPendingApprovalRequestTx(ctx, tx, input.Type, input.SubjectHash, input.ExecutionHostID, nowMS)
	if err != nil {
		return ApprovalRequestRecord{}, false, err
	}
	if ok {
		if err := tx.Commit(); err != nil {
			return ApprovalRequestRecord{}, false, err
		}
		return existing, true, nil
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO approval_requests(type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.Type, input.SubjectHash, input.SubjectJSON, input.RequesterAgentID, input.RequesterSessionID, input.RequesterContextJSON, input.ExecutionHostID, input.Status, input.PolicyMode, input.RequestedAt, input.ExpiresAt, input.ResolvedAt, input.ResolverActorID, input.ResolutionKind, input.ResolutionNote)
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, ok, findErr := findPendingApprovalRequestTx(ctx, tx, input.Type, input.SubjectHash, input.ExecutionHostID, nowMS)
			if findErr != nil {
				return ApprovalRequestRecord{}, false, findErr
			}
			if ok {
				if commitErr := tx.Commit(); commitErr != nil {
					return ApprovalRequestRecord{}, false, commitErr
				}
				return existing, true, nil
			}
		}
		return ApprovalRequestRecord{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ApprovalRequestRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalRequestRecord{}, false, err
	}
	rec, err := d.GetApprovalRequest(ctx, id)
	return rec, false, err
}

func findPendingApprovalRequestTx(ctx context.Context, tx *sql.Tx, approvalType, subjectHash, hostID string, nowMS int64) (ApprovalRequestRecord, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note
		FROM approval_requests WHERE type=? AND subject_hash=? AND execution_host_id=? AND status='pending' AND expires_at>? ORDER BY id DESC LIMIT 1`, approvalType, subjectHash, hostID, nowMS)
	rec, err := scanApprovalRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalRequestRecord{}, false, nil
	}
	return rec, err == nil, err
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed")
}

func (d *DB) ListExpiredPendingApprovalRequestIDs(ctx context.Context, nowMS int64) ([]int64, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id FROM approval_requests WHERE status=? AND expires_at>0 AND expires_at<=? ORDER BY id ASC`, "pending", nowMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
	rec, _, err := d.CreateOrGetApprovalAllowlist(ctx, input)
	return rec, err
}

func (d *DB) CreateOrGetApprovalAllowlist(ctx context.Context, input ApprovalAllowlistRecord) (ApprovalAllowlistRecord, bool, error) {
	if strings.TrimSpace(input.MatchFingerprint) != "" {
		row := d.SQL.QueryRowContext(ctx, `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
			scope_host_id, scope_tool, scope_profile, scope_agent,
			match_executable_path, match_working_dir, match_script_hash,
			match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
			FROM approval_allowlists WHERE domain=? AND match_fingerprint=? AND disabled_at=0 LIMIT 1`,
			input.Domain, input.MatchFingerprint)
		rec, err := scanApprovalAllowlist(row)
		if err == nil {
			return rec, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ApprovalAllowlistRecord{}, false, err
		}
	}
	res, err := d.SQL.ExecContext(ctx, `INSERT INTO approval_allowlists(
			domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
			scope_host_id, scope_tool, scope_profile, scope_agent,
			match_executable_path, match_working_dir, match_script_hash,
			match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.Domain, input.ScopeJSON, input.MatcherJSON, input.CreatedBy, input.CreatedAt, input.ExpiresAt, input.DisabledAt,
		input.ScopeHostID, input.ScopeTool, input.ScopeProfile, input.ScopeAgent,
		input.MatchExecutablePath, input.MatchWorkingDir, input.MatchScriptHash,
		input.MatchSkillID, input.MatchPlanHash, input.MatchRunnerID, input.MatchTargetPath, input.MatchPathPrefix, input.MatchFingerprint)
	if err != nil {
		if isUniqueConstraintErr(err) && strings.TrimSpace(input.MatchFingerprint) != "" {
			row := d.SQL.QueryRowContext(ctx, `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
				scope_host_id, scope_tool, scope_profile, scope_agent,
				match_executable_path, match_working_dir, match_script_hash,
				match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
				FROM approval_allowlists WHERE domain=? AND match_fingerprint=? AND disabled_at=0 LIMIT 1`,
				input.Domain, input.MatchFingerprint)
			rec, findErr := scanApprovalAllowlist(row)
			if findErr == nil {
				return rec, true, nil
			}
		}
		return ApprovalAllowlistRecord{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ApprovalAllowlistRecord{}, false, err
	}
	rec, err := d.GetApprovalAllowlist(ctx, id)
	return rec, false, err
}

func (d *DB) ListApprovalAllowlistCandidates(ctx context.Context, query ApprovalAllowlistMatchQuery, limit, offset int) ([]ApprovalAllowlistRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	sqlQuery := `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
		scope_host_id, scope_tool, scope_profile, scope_agent,
		match_executable_path, match_working_dir, match_script_hash,
		match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
		FROM approval_allowlists
		WHERE domain=? AND disabled_at=0 AND (expires_at=0 OR expires_at>?)`
	args := []any{query.Domain, query.NowMS}
	appendScopeFilter := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		sqlQuery += ` AND (` + column + `='' OR ` + column + `=?)`
		args = append(args, value)
	}
	appendScopeFilter("scope_host_id", query.ScopeHostID)
	appendScopeFilter("scope_tool", query.ScopeTool)
	appendScopeFilter("scope_profile", query.ScopeProfile)
	appendScopeFilter("scope_agent", query.ScopeAgent)
	appendScopeFilter("match_executable_path", query.MatchExecutablePath)
	appendScopeFilter("match_working_dir", query.MatchWorkingDir)
	appendScopeFilter("match_script_hash", query.MatchScriptHash)
	appendScopeFilter("match_skill_id", query.MatchSkillID)
	appendScopeFilter("match_plan_hash", query.MatchPlanHash)
	appendScopeFilter("match_runner_id", query.MatchRunnerID)
	appendScopeFilter("match_target_path", query.MatchTargetPath)
	appendScopeFilter("match_path_prefix", query.MatchPathPrefix)
	sqlQuery += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := d.SQL.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ApprovalAllowlistRecord, 0)
	for rows.Next() {
		rec, err := scanApprovalAllowlist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) GetApprovalAllowlist(ctx context.Context, id int64) (ApprovalAllowlistRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
		scope_host_id, scope_tool, scope_profile, scope_agent,
		match_executable_path, match_working_dir, match_script_hash,
		match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
		FROM approval_allowlists WHERE id=?`, id)
	return scanApprovalAllowlist(row)
}

func (d *DB) ListApprovalAllowlists(ctx context.Context, domain string, limit int) ([]ApprovalAllowlistRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
		scope_host_id, scope_tool, scope_profile, scope_agent,
		match_executable_path, match_working_dir, match_script_hash,
		match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
		FROM approval_allowlists`
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

func (d *DB) DisableApprovalAllowlist(ctx context.Context, id int64, disabledAt int64) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE approval_allowlists SET disabled_at=? WHERE id=? AND disabled_at=0`, disabledAt, id)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// ApproveRequestArtifacts bundles the side effects of approving a request.
type ApproveRequestArtifacts struct {
	Request     ApprovalRequestRecord
	AllowlistID int64
	TokenRecord ApprovalTokenRecord
}

func (d *DB) ApproveRequestWithArtifacts(ctx context.Context, requestID int64, actor string, alwaysAllow bool, resolutionKind, note string, nowMS int64, tokenExpiresAt int64, buildAllowlist func(ApprovalRequestRecord) (ApprovalAllowlistRecord, error)) (ApproveRequestArtifacts, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return ApproveRequestArtifacts{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	req, err := getApprovalRequestTx(ctx, tx, requestID)
	if err != nil {
		return ApproveRequestArtifacts{}, err
	}
	if req.Status != "pending" {
		return ApproveRequestArtifacts{}, fmt.Errorf("approval request is not pending")
	}
	if req.ExpiresAt > 0 && req.ExpiresAt < nowMS {
		return ApproveRequestArtifacts{}, fmt.Errorf("approval request expired")
	}
	res, err := tx.ExecContext(ctx, `UPDATE approval_requests SET status=?, resolved_at=?, resolver_actor_id=?, resolution_kind=?, resolution_note=? WHERE id=? AND status=?`,
		"approved", nowMS, strings.TrimSpace(actor), resolutionKind, strings.TrimSpace(note), requestID, "pending")
	if err != nil {
		return ApproveRequestArtifacts{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return ApproveRequestArtifacts{}, err
	}
	if rows != 1 {
		return ApproveRequestArtifacts{}, fmt.Errorf("approval request is not pending")
	}
	if _, err := updateSkillRunPlansByApprovalRequestTx(ctx, tx, requestID, []string{"pending_approval"}, "approved", "", nowMS); err != nil {
		return ApproveRequestArtifacts{}, err
	}
	out := ApproveRequestArtifacts{Request: req}
	if alwaysAllow && buildAllowlist != nil {
		allowlistInput, err := buildAllowlist(req)
		if err != nil {
			return ApproveRequestArtifacts{}, err
		}
		if strings.TrimSpace(allowlistInput.Domain) == "" {
			goto issueToken
		}
		allowlistRec, _, err := createOrGetApprovalAllowlistTx(ctx, tx, allowlistInput)
		if err != nil {
			return ApproveRequestArtifacts{}, err
		}
		out.AllowlistID = allowlistRec.ID
	}
issueToken:
	tokenRes, err := tx.ExecContext(ctx, `INSERT INTO approval_tokens(approval_request_id, subject_hash, issued_at, expires_at, issuer, revoked_at) VALUES(?, ?, ?, ?, ?, 0)`,
		requestID, req.SubjectHash, nowMS, tokenExpiresAt, strings.TrimSpace(actor))
	if err != nil {
		return ApproveRequestArtifacts{}, err
	}
	tokenID, err := tokenRes.LastInsertId()
	if err != nil {
		return ApproveRequestArtifacts{}, err
	}
	out.TokenRecord = ApprovalTokenRecord{
		ID:                tokenID,
		ApprovalRequestID: requestID,
		SubjectHash:       req.SubjectHash,
		IssuedAt:          nowMS,
		ExpiresAt:         tokenExpiresAt,
		Issuer:            strings.TrimSpace(actor),
	}
	if err := tx.Commit(); err != nil {
		return ApproveRequestArtifacts{}, err
	}
	return out, nil
}

func getApprovalRequestTx(ctx context.Context, tx *sql.Tx, id int64) (ApprovalRequestRecord, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, type, subject_hash, subject_json, requester_agent_id, requester_session_id, requester_context_json, execution_host_id, status, policy_mode, requested_at, expires_at, resolved_at, resolver_actor_id, resolution_kind, resolution_note FROM approval_requests WHERE id=?`, id)
	return scanApprovalRequest(row)
}

func createOrGetApprovalAllowlistTx(ctx context.Context, tx *sql.Tx, input ApprovalAllowlistRecord) (ApprovalAllowlistRecord, bool, error) {
	if strings.TrimSpace(input.MatchFingerprint) != "" {
		row := tx.QueryRowContext(ctx, `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
			scope_host_id, scope_tool, scope_profile, scope_agent,
			match_executable_path, match_working_dir, match_script_hash,
			match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
			FROM approval_allowlists WHERE domain=? AND match_fingerprint=? AND disabled_at=0 LIMIT 1`,
			input.Domain, input.MatchFingerprint)
		rec, err := scanApprovalAllowlist(row)
		if err == nil {
			return rec, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ApprovalAllowlistRecord{}, false, err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO approval_allowlists(
			domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
			scope_host_id, scope_tool, scope_profile, scope_agent,
			match_executable_path, match_working_dir, match_script_hash,
			match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.Domain, input.ScopeJSON, input.MatcherJSON, input.CreatedBy, input.CreatedAt, input.ExpiresAt, input.DisabledAt,
		input.ScopeHostID, input.ScopeTool, input.ScopeProfile, input.ScopeAgent,
		input.MatchExecutablePath, input.MatchWorkingDir, input.MatchScriptHash,
		input.MatchSkillID, input.MatchPlanHash, input.MatchRunnerID, input.MatchTargetPath, input.MatchPathPrefix, input.MatchFingerprint)
	if err != nil {
		return ApprovalAllowlistRecord{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ApprovalAllowlistRecord{}, false, err
	}
	row := tx.QueryRowContext(ctx, `SELECT id, domain, scope_json, matcher_json, created_by, created_at, expires_at, disabled_at,
		scope_host_id, scope_tool, scope_profile, scope_agent,
		match_executable_path, match_working_dir, match_script_hash,
		match_skill_id, match_plan_hash, match_runner_id, match_target_path, match_path_prefix, match_fingerprint
		FROM approval_allowlists WHERE id=?`, id)
	rec, err := scanApprovalAllowlist(row)
	return rec, false, err
}

func updateSkillRunPlansByApprovalRequestTx(ctx context.Context, tx *sql.Tx, approvalRequestID int64, fromStatuses []string, toStatus, lastError string, updatedAt int64) (int64, error) {
	if len(fromStatuses) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(fromStatuses))
	args := []any{strings.TrimSpace(toStatus), strings.TrimSpace(lastError), updatedAt, approvalRequestID}
	for i, status := range fromStatuses {
		placeholders[i] = "?"
		args = append(args, status)
	}
	query := `UPDATE skill_run_plans SET status=?, last_error=?, updated_at=? WHERE approval_request_id=? AND status IN (` + strings.Join(placeholders, ",") + `)`
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	return rows, err
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

func (d *DB) ConsumeApprovalToken(ctx context.Context, id int64, revokedAt int64) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE approval_tokens SET revoked_at=? WHERE id=? AND revoked_at=0`, revokedAt, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func marshalJSONMap(value map[string]any) (string, error) {
	if len(value) == 0 {
		return "{}", nil
	}
	blob, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(blob), nil
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
	if err := scanner.Scan(&rec.ID, &rec.Type, &rec.SubjectHash, &rec.SubjectJSON, &rec.RequesterAgentID, &rec.RequesterSessionID, &rec.RequesterContextJSON, &rec.ExecutionHostID, &rec.Status, &rec.PolicyMode, &rec.RequestedAt, &rec.ExpiresAt, &rec.ResolvedAt, &rec.ResolverActorID, &rec.ResolutionKind, &rec.ResolutionNote); err != nil {
		return ApprovalRequestRecord{}, err
	}
	return rec, nil
}

func scanApprovalAllowlist(scanner interface{ Scan(dest ...any) error }) (ApprovalAllowlistRecord, error) {
	var rec ApprovalAllowlistRecord
	if err := scanner.Scan(
		&rec.ID, &rec.Domain, &rec.ScopeJSON, &rec.MatcherJSON, &rec.CreatedBy, &rec.CreatedAt, &rec.ExpiresAt, &rec.DisabledAt,
		&rec.ScopeHostID, &rec.ScopeTool, &rec.ScopeProfile, &rec.ScopeAgent,
		&rec.MatchExecutablePath, &rec.MatchWorkingDir, &rec.MatchScriptHash,
		&rec.MatchSkillID, &rec.MatchPlanHash, &rec.MatchRunnerID, &rec.MatchTargetPath, &rec.MatchPathPrefix, &rec.MatchFingerprint,
	); err != nil {
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
