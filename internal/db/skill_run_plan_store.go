package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	sqlite3 "github.com/mattn/go-sqlite3"
)

type SkillRunStatus = string

const (
	SkillRunStatusPlanned         SkillRunStatus = "planned"
	SkillRunStatusPendingApproval SkillRunStatus = "pending_approval"
	SkillRunStatusApproved        SkillRunStatus = "approved"
	SkillRunStatusRunning         SkillRunStatus = "running"
	SkillRunStatusSucceeded       SkillRunStatus = "succeeded"
	SkillRunStatusFailed          SkillRunStatus = "failed"
	SkillRunStatusPreflightFailed SkillRunStatus = "preflight_failed"
	SkillRunStatusBlockedByPolicy SkillRunStatus = "blocked_by_policy"
	SkillRunStatusTimedOut        SkillRunStatus = "timed_out"
	SkillRunStatusCancelled       SkillRunStatus = "cancelled"
	SkillRunStatusDenied          SkillRunStatus = "denied"
	SkillRunStatusExpired         SkillRunStatus = "expired"
	SkillRunStatusStalePlan       SkillRunStatus = "stale_plan"

	legacySkillRunStatusPrepared       SkillRunStatus = "prepared"
	legacySkillRunStatusAwaitingResume SkillRunStatus = "awaiting_resume"
	legacySkillRunStatusBlocked        SkillRunStatus = "blocked"
)

var skillRunActiveStatuses = []string{
	legacySkillRunStatusPrepared,
	SkillRunStatusPlanned,
	SkillRunStatusPendingApproval,
	legacySkillRunStatusAwaitingResume,
	SkillRunStatusApproved,
	SkillRunStatusRunning,
}

var skillRunClaimableStatuses = []string{
	legacySkillRunStatusPrepared,
	SkillRunStatusPlanned,
	SkillRunStatusPendingApproval,
	legacySkillRunStatusAwaitingResume,
	SkillRunStatusApproved,
}

func NormalizeSkillRunStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "", legacySkillRunStatusPrepared:
		return SkillRunStatusPlanned
	case legacySkillRunStatusAwaitingResume:
		return SkillRunStatusApproved
	case legacySkillRunStatusBlocked:
		return SkillRunStatusBlockedByPolicy
	default:
		return strings.TrimSpace(status)
	}
}

func IsTerminalSkillRunStatus(status string) bool {
	switch NormalizeSkillRunStatus(status) {
	case SkillRunStatusSucceeded, SkillRunStatusFailed, SkillRunStatusPreflightFailed, SkillRunStatusBlockedByPolicy, SkillRunStatusTimedOut, SkillRunStatusCancelled, SkillRunStatusDenied, SkillRunStatusExpired, SkillRunStatusStalePlan:
		return true
	default:
		return false
	}
}

type SkillRunPlanRecord struct {
	ID                 string
	SkillID            string
	Version            string
	Origin             string
	TrustState         string
	SkillDir           string
	RelativePath       string
	Entrypoint         string
	ArgsJSON           string
	StdinText          string
	StdinNonce         []byte
	StdinSHA256        string
	TimeoutSeconds     int
	CommandJSON        string
	ScriptHash         string
	EnvBindingHash     string
	PlanHash           string
	SubjectHash        string
	RequesterAgentID   string
	RequesterSessionID string
	ExecutionHostID    string
	ApprovalRequestID  int64
	Status             string
	ResultJSON         string
	LastError          string
	CreatedAt          int64
	UpdatedAt          int64
}

func (d *DB) CreateSkillRunPlan(ctx context.Context, input SkillRunPlanRecord) (SkillRunPlanRecord, error) {
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		input.ID = newSkillRunPlanID()
	}
	if strings.TrimSpace(input.SkillID) == "" {
		return SkillRunPlanRecord{}, fmt.Errorf("skill ID required")
	}
	if strings.TrimSpace(input.SkillDir) == "" {
		return SkillRunPlanRecord{}, fmt.Errorf("skill directory required")
	}
	if strings.TrimSpace(input.ArgsJSON) == "" {
		input.ArgsJSON = "[]"
	}
	if strings.TrimSpace(input.CommandJSON) == "" {
		input.CommandJSON = "[]"
	}
	if input.CreatedAt <= 0 {
		input.CreatedAt = NowMS()
	}
	if input.UpdatedAt <= 0 {
		input.UpdatedAt = input.CreatedAt
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = SkillRunStatusPlanned
	}
	input.Status = NormalizeSkillRunStatus(input.Status)
	var approvalRequestID any
	if input.ApprovalRequestID > 0 {
		approvalRequestID = input.ApprovalRequestID
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO skill_run_plans(
		id, skill_id, version, origin, trust_state, skill_dir, relative_path, entrypoint, args_json, stdin_text, stdin_nonce, stdin_sha256,
		timeout_seconds, command_json, script_hash, env_binding_hash, plan_hash, subject_hash,
		requester_agent_id, requester_session_id, execution_host_id, approval_request_id,
		status, result_json, last_error, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.SkillID, input.Version, input.Origin, input.TrustState, input.SkillDir, input.RelativePath, input.Entrypoint, input.ArgsJSON, input.StdinText, skillRunPlanNonceValue(input.StdinNonce), input.StdinSHA256,
		input.TimeoutSeconds, input.CommandJSON, input.ScriptHash, input.EnvBindingHash, input.PlanHash, input.SubjectHash,
		input.RequesterAgentID, input.RequesterSessionID, input.ExecutionHostID, approvalRequestID,
		input.Status, input.ResultJSON, input.LastError, input.CreatedAt, input.UpdatedAt,
	)
	if err != nil {
		return SkillRunPlanRecord{}, err
	}
	return d.GetSkillRunPlan(ctx, input.ID)
}

func (d *DB) GetSkillRunPlan(ctx context.Context, id string) (SkillRunPlanRecord, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT id, skill_id, version, origin, trust_state, skill_dir, relative_path, entrypoint, args_json, stdin_text, stdin_nonce, stdin_sha256,
		timeout_seconds, command_json, script_hash, env_binding_hash, plan_hash, subject_hash,
		requester_agent_id, requester_session_id, execution_host_id, approval_request_id,
		status, result_json, last_error, created_at, updated_at
		FROM skill_run_plans WHERE id=?`, strings.TrimSpace(id))
	return scanSkillRunPlan(row)
}

func (d *DB) ListSkillRunPlansByApprovalRequest(ctx context.Context, requestID int64, limit int) ([]SkillRunPlanRecord, error) {
	if requestID <= 0 {
		return []SkillRunPlanRecord{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, skill_id, version, origin, trust_state, skill_dir, relative_path, entrypoint, args_json, stdin_text, stdin_nonce, stdin_sha256,
			timeout_seconds, command_json, script_hash, env_binding_hash, plan_hash, subject_hash,
			requester_agent_id, requester_session_id, execution_host_id, approval_request_id,
			status, result_json, last_error, created_at, updated_at
			FROM skill_run_plans WHERE approval_request_id=? ORDER BY created_at DESC LIMIT ?`, requestID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SkillRunPlanRecord{}
	for rows.Next() {
		rec, err := scanSkillRunPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (d *DB) FindActiveSkillRunPlan(ctx context.Context, sessionID, planHash string) (SkillRunPlanRecord, bool, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(planHash) == "" {
		return SkillRunPlanRecord{}, false, nil
	}
	query, args := skillRunPlanStatusQuery(`SELECT id, skill_id, version, origin, trust_state, skill_dir, relative_path, entrypoint, args_json, stdin_text, stdin_nonce, stdin_sha256,
			timeout_seconds, command_json, script_hash, env_binding_hash, plan_hash, subject_hash,
			requester_agent_id, requester_session_id, execution_host_id, approval_request_id,
			status, result_json, last_error, created_at, updated_at
			FROM skill_run_plans
			WHERE requester_session_id=? AND plan_hash=?`, []any{strings.TrimSpace(sessionID), strings.TrimSpace(planHash)}, skillRunActiveStatuses)
	query += ` ORDER BY created_at DESC LIMIT 1`
	row := d.SQL.QueryRowContext(ctx, query, args...)
	rec, err := scanSkillRunPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SkillRunPlanRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) GetOrCreateActiveSkillRunPlan(ctx context.Context, input SkillRunPlanRecord) (SkillRunPlanRecord, bool, error) {
	if existing, ok, err := d.FindActiveSkillRunPlan(ctx, input.RequesterSessionID, input.PlanHash); err != nil || ok {
		return existing, ok, err
	}
	created, err := d.CreateSkillRunPlan(ctx, input)
	if err == nil {
		return created, false, nil
	}
	if !isSQLiteUniqueConstraint(err) {
		return SkillRunPlanRecord{}, false, err
	}
	existing, ok, lookupErr := d.FindActiveSkillRunPlan(ctx, input.RequesterSessionID, input.PlanHash)
	if lookupErr != nil {
		return SkillRunPlanRecord{}, false, lookupErr
	}
	if ok {
		return existing, true, nil
	}
	return SkillRunPlanRecord{}, false, err
}

func (d *DB) ClaimSkillRunPlan(ctx context.Context, id string, updatedAt int64) (bool, error) {
	if updatedAt <= 0 {
		updatedAt = NowMS()
	}
	query, args := skillRunPlanStatusUpdateQuery(`UPDATE skill_run_plans SET status=?, updated_at=? WHERE id=?`, []any{"running", updatedAt, strings.TrimSpace(id)}, skillRunClaimableStatuses)
	res, err := d.SQL.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) UpdateSkillRunPlansByApprovalRequest(ctx context.Context, approvalRequestID int64, fromStatuses []string, toStatus, lastError string, updatedAt int64) (int64, error) {
	if approvalRequestID <= 0 {
		return 0, nil
	}
	if updatedAt <= 0 {
		updatedAt = NowMS()
	}
	toStatus = NormalizeSkillRunStatus(toStatus)
	statuses := append([]string{}, fromStatuses...)
	if len(statuses) == 0 {
		statuses = append(statuses, skillRunActiveStatuses...)
	}
	query, args := skillRunPlanStatusUpdateQuery(`UPDATE skill_run_plans SET status=?, last_error=?, updated_at=? WHERE approval_request_id=?`, []any{strings.TrimSpace(toStatus), strings.TrimSpace(lastError), updatedAt, approvalRequestID}, statuses)
	res, err := d.SQL.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

func (d *DB) UpdateSkillRunPlanApproval(ctx context.Context, id string, approvalRequestID int64, subjectHash, status string, updatedAt int64) error {
	if updatedAt <= 0 {
		updatedAt = NowMS()
	}
	status = NormalizeSkillRunStatus(status)
	_, err := d.SQL.ExecContext(ctx, `UPDATE skill_run_plans
		SET approval_request_id=CASE WHEN ? > 0 THEN ? ELSE approval_request_id END,
			subject_hash=?, status=?, updated_at=?
		WHERE id=?`, approvalRequestID, approvalRequestID, strings.TrimSpace(subjectHash), strings.TrimSpace(status), updatedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) UpdateSkillRunPlanResult(ctx context.Context, id, status, resultJSON, lastError string, updatedAt int64) error {
	if updatedAt <= 0 {
		updatedAt = NowMS()
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE skill_run_plans SET status=?, result_json=?, last_error=?, updated_at=? WHERE id=?`, NormalizeSkillRunStatus(status), strings.TrimSpace(resultJSON), strings.TrimSpace(lastError), updatedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) ClearSkillRunPlanStdin(ctx context.Context, id string, updatedAt int64) error {
	if updatedAt <= 0 {
		updatedAt = NowMS()
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE skill_run_plans SET stdin_text='', stdin_nonce=X'', stdin_sha256='', updated_at=? WHERE id=?`, updatedAt, strings.TrimSpace(id))
	return err
}

func scanSkillRunPlan(scanner interface{ Scan(dest ...any) error }) (SkillRunPlanRecord, error) {
	var rec SkillRunPlanRecord
	var approvalRequestID sql.NullInt64
	err := scanner.Scan(
		&rec.ID,
		&rec.SkillID,
		&rec.Version,
		&rec.Origin,
		&rec.TrustState,
		&rec.SkillDir,
		&rec.RelativePath,
		&rec.Entrypoint,
		&rec.ArgsJSON,
		&rec.StdinText,
		&rec.StdinNonce,
		&rec.StdinSHA256,
		&rec.TimeoutSeconds,
		&rec.CommandJSON,
		&rec.ScriptHash,
		&rec.EnvBindingHash,
		&rec.PlanHash,
		&rec.SubjectHash,
		&rec.RequesterAgentID,
		&rec.RequesterSessionID,
		&rec.ExecutionHostID,
		&approvalRequestID,
		&rec.Status,
		&rec.ResultJSON,
		&rec.LastError,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SkillRunPlanRecord{}, err
	}
	if approvalRequestID.Valid {
		rec.ApprovalRequestID = approvalRequestID.Int64
	}
	rec.Status = NormalizeSkillRunStatus(rec.Status)
	return rec, err
}

func newSkillRunPlanID() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("srp_%d", NowMS())
	}
	return "srp_" + hex.EncodeToString(buf)
}

func skillRunPlanStatusQuery(base string, args []any, statuses []string) (string, []any) {
	if len(statuses) == 0 {
		return base, args
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	query := base + ` AND status IN (` + placeholders + `)`
	for _, status := range statuses {
		args = append(args, strings.TrimSpace(status))
	}
	return query, args
}

func skillRunPlanStatusUpdateQuery(base string, args []any, statuses []string) (string, []any) {
	if len(statuses) == 0 {
		return base, args
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	query := base + ` AND status IN (` + placeholders + `)`
	for _, status := range statuses {
		args = append(args, strings.TrimSpace(status))
	}
	return query, args
}

func isSQLiteUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrConstraint
	}
	return false
}

func skillRunPlanNonceValue(nonce []byte) []byte {
	if len(nonce) == 0 {
		return []byte{}
	}
	return append([]byte{}, nonce...)
}
