package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const (
	defaultDiagnosticLogMaxEvents = 1000
	defaultDiagnosticLogMaxAgeMS  = int64(7 * 24 * 60 * 60 * 1000)
	defaultDiagnosticLogMaxBytes  = int64(4 * 1024 * 1024)
)

type SettingsChangePlanRecord struct {
	ID               string
	Status           string
	ConversationID   string
	AcceptedCardID   string
	CreatedBy        string
	PlanJSON         string
	ApprovalJSON     string
	LiveReloadJSON   string
	RollbackID       string
	PostCheckPending bool
	ErrorText        string
	CreatedAt        int64
	UpdatedAt        int64
	AppliedAt        int64
}

type DoctorCheckpointRecord struct {
	ID             string
	PlanID         string
	ConversationID string
	AcceptedCardID string
	Status         string
	ChecksJSON     string
	ResultsJSON    string
	CreatedAt      int64
	UpdatedAt      int64
}

type SettingsChangeRollbackRecord struct {
	ID           string
	PlanID       string
	Status       string
	RollbackJSON string
	ChangesJSON  string
	ErrorText    string
	CreatedAt    int64
	UpdatedAt    int64
	AppliedAt    int64
}

type DiagnosticLogEvent struct {
	ID            int64
	Source        string
	Level         string
	CorrelationID string
	EventType     string
	Payload       json.RawMessage
	SizeBytes     int64
	CreatedAt     int64
}

type DiagnosticLogQuery struct {
	Source        string
	Level         string
	CorrelationID string
	EventType     string
	Pattern       string
	SinceUnixMS   int64
	UntilUnixMS   int64
	Limit         int
}

func (d *DB) CreateSettingsChangePlan(ctx context.Context, record SettingsChangePlanRecord) error {
	now := NowMS()
	createdAt := record.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	updatedAt := record.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = createdAt
	}
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO settings_change_plans(id, status, conversation_id, accepted_card_id, created_by, plan_json, approval_json, live_reload_json, rollback_id, post_check_pending, error_text, created_at, updated_at, applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(record.ID), strings.TrimSpace(record.Status), strings.TrimSpace(record.ConversationID), strings.TrimSpace(record.AcceptedCardID), strings.TrimSpace(record.CreatedBy),
		defaultJSONObject(record.PlanJSON), defaultJSONObject(record.ApprovalJSON), defaultJSONArray(record.LiveReloadJSON), strings.TrimSpace(record.RollbackID), settingsBoolToInt(record.PostCheckPending), strings.TrimSpace(record.ErrorText),
		createdAt, updatedAt, record.AppliedAt)
	return err
}

func (d *DB) GetSettingsChangePlan(ctx context.Context, id string) (SettingsChangePlanRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, status, conversation_id, accepted_card_id, created_by, plan_json, approval_json, live_reload_json, rollback_id, post_check_pending, error_text, created_at, updated_at, applied_at
		 FROM settings_change_plans WHERE id=?`, strings.TrimSpace(id))
	record, err := scanSettingsChangePlanRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SettingsChangePlanRecord{}, false, nil
		}
		return SettingsChangePlanRecord{}, false, err
	}
	return record, true, nil
}

func (d *DB) ListPendingSettingsChangePlans(ctx context.Context, limit int) ([]SettingsChangePlanRecord, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, status, conversation_id, accepted_card_id, created_by, plan_json, approval_json, live_reload_json, rollback_id, post_check_pending, error_text, created_at, updated_at, applied_at
		 FROM settings_change_plans
		 WHERE post_check_pending=1 OR status IN ('applied','restart_pending','restart_start_failed','restart_approval_required','post_check_failed')
		 ORDER BY updated_at DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SettingsChangePlanRecord{}
	for rows.Next() {
		item, err := scanSettingsChangePlanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) UpdateSettingsChangePlanStatus(ctx context.Context, id, status, rollbackID, errorText string, postCheckPending bool, approvalJSON, liveReloadJSON string, appliedAt int64) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE settings_change_plans
		 SET status=?, rollback_id=?, post_check_pending=?, error_text=?, approval_json=?, live_reload_json=?, updated_at=?, applied_at=?
		 WHERE id=?`,
		strings.TrimSpace(status), strings.TrimSpace(rollbackID), settingsBoolToInt(postCheckPending), strings.TrimSpace(errorText), defaultJSONObject(approvalJSON), defaultJSONArray(liveReloadJSON), NowMS(), appliedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) CreateDoctorCheckpoint(ctx context.Context, record DoctorCheckpointRecord) error {
	now := NowMS()
	createdAt := record.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	updatedAt := record.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = createdAt
	}
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO doctor_checkpoints(id, plan_id, conversation_id, accepted_card_id, status, checks_json, results_json, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(record.ID), strings.TrimSpace(record.PlanID), strings.TrimSpace(record.ConversationID), strings.TrimSpace(record.AcceptedCardID), strings.TrimSpace(record.Status), defaultJSONArray(record.ChecksJSON), defaultJSONArray(record.ResultsJSON), createdAt, updatedAt)
	return err
}

func (d *DB) GetLatestDoctorCheckpointForPlan(ctx context.Context, planID string) (DoctorCheckpointRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, plan_id, conversation_id, accepted_card_id, status, checks_json, results_json, created_at, updated_at
		 FROM doctor_checkpoints WHERE plan_id=? ORDER BY updated_at DESC, created_at DESC LIMIT 1`, strings.TrimSpace(planID))
	var record DoctorCheckpointRecord
	if err := row.Scan(&record.ID, &record.PlanID, &record.ConversationID, &record.AcceptedCardID, &record.Status, &record.ChecksJSON, &record.ResultsJSON, &record.CreatedAt, &record.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return DoctorCheckpointRecord{}, false, nil
		}
		return DoctorCheckpointRecord{}, false, err
	}
	return record, true, nil
}

func (d *DB) UpdateDoctorCheckpoint(ctx context.Context, id, status, resultsJSON string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE doctor_checkpoints SET status=?, results_json=?, updated_at=? WHERE id=?`,
		strings.TrimSpace(status), defaultJSONArray(resultsJSON), NowMS(), strings.TrimSpace(id))
	return err
}

func (d *DB) CreateSettingsChangeRollback(ctx context.Context, record SettingsChangeRollbackRecord) error {
	now := NowMS()
	createdAt := record.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	updatedAt := record.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = createdAt
	}
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO settings_change_rollbacks(id, plan_id, status, rollback_json, changes_json, error_text, created_at, updated_at, applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(record.ID), strings.TrimSpace(record.PlanID), strings.TrimSpace(record.Status), defaultJSONObject(record.RollbackJSON), defaultJSONArray(record.ChangesJSON), strings.TrimSpace(record.ErrorText), createdAt, updatedAt, record.AppliedAt)
	return err
}

func (d *DB) UpdateSettingsChangeRollbackStatus(ctx context.Context, id, status, errorText string, appliedAt int64) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE settings_change_rollbacks SET status=?, error_text=?, updated_at=?, applied_at=? WHERE id=?`,
		strings.TrimSpace(status), strings.TrimSpace(errorText), NowMS(), appliedAt, strings.TrimSpace(id))
	return err
}

func (d *DB) ListRecentSettingsChangeRollbacks(ctx context.Context, limit int) ([]SettingsChangeRollbackRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, plan_id, status, rollback_json, changes_json, error_text, created_at, updated_at, applied_at
		 FROM settings_change_rollbacks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SettingsChangeRollbackRecord{}
	for rows.Next() {
		item, err := scanSettingsChangeRollbackRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) GetSettingsChangeRollback(ctx context.Context, id string) (SettingsChangeRollbackRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, plan_id, status, rollback_json, changes_json, error_text, created_at, updated_at, applied_at
		 FROM settings_change_rollbacks WHERE id=?`, strings.TrimSpace(id))
	record, err := scanSettingsChangeRollbackRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SettingsChangeRollbackRecord{}, false, nil
		}
		return SettingsChangeRollbackRecord{}, false, err
	}
	return record, true, nil
}

func (d *DB) AppendDiagnosticLogEvent(ctx context.Context, event DiagnosticLogEvent) error {
	payload := json.RawMessage(strings.TrimSpace(string(event.Payload)))
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	createdAt := event.CreatedAt
	if createdAt <= 0 {
		createdAt = NowMS()
	}
	sizeBytes := event.SizeBytes
	if sizeBytes <= 0 {
		sizeBytes = int64(len(payload))
	}
	if _, err := d.SQL.ExecContext(ctx,
		`INSERT INTO diagnostic_log_events(source, level, correlation_id, event_type, payload_json, size_bytes, created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		strings.TrimSpace(event.Source), strings.TrimSpace(event.Level), strings.TrimSpace(event.CorrelationID), strings.TrimSpace(event.EventType), string(payload), sizeBytes, createdAt); err != nil {
		return err
	}
	return d.pruneDiagnosticLogEvents(ctx, defaultDiagnosticLogMaxEvents, defaultDiagnosticLogMaxAgeMS, defaultDiagnosticLogMaxBytes)
}

func (d *DB) QueryDiagnosticLogEvents(ctx context.Context, query DiagnosticLogQuery) ([]DiagnosticLogEvent, error) {
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	clauses := []string{"1=1"}
	args := []any{}
	if source := strings.TrimSpace(query.Source); source != "" {
		clauses = append(clauses, "source=?")
		args = append(args, source)
	}
	if level := strings.TrimSpace(query.Level); level != "" {
		clauses = append(clauses, "level=?")
		args = append(args, level)
	}
	if correlationID := strings.TrimSpace(query.CorrelationID); correlationID != "" {
		clauses = append(clauses, "correlation_id=?")
		args = append(args, correlationID)
	}
	if eventType := strings.TrimSpace(query.EventType); eventType != "" {
		clauses = append(clauses, "event_type=?")
		args = append(args, eventType)
	}
	if query.SinceUnixMS > 0 {
		clauses = append(clauses, "created_at>=?")
		args = append(args, query.SinceUnixMS)
	}
	if query.UntilUnixMS > 0 {
		clauses = append(clauses, "created_at<=?")
		args = append(args, query.UntilUnixMS)
	}
	if pattern := strings.TrimSpace(query.Pattern); pattern != "" {
		if len(pattern) > 120 {
			pattern = pattern[:120]
		}
		clauses = append(clauses, "(event_type LIKE ? OR payload_json LIKE ?)")
		like := "%" + strings.ReplaceAll(pattern, "%", `\%`) + "%"
		args = append(args, like, like)
	}
	args = append(args, limit)
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, source, level, correlation_id, event_type, payload_json, size_bytes, created_at
		 FROM diagnostic_log_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DiagnosticLogEvent{}
	for rows.Next() {
		var item DiagnosticLogEvent
		var payload string
		if err := rows.Scan(&item.ID, &item.Source, &item.Level, &item.CorrelationID, &item.EventType, &payload, &item.SizeBytes, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Payload = json.RawMessage(payload)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) pruneDiagnosticLogEvents(ctx context.Context, maxEvents int, maxAgeMS, maxBytes int64) error {
	if maxAgeMS > 0 {
		cutoff := NowMS() - maxAgeMS
		if _, err := d.SQL.ExecContext(ctx, `DELETE FROM diagnostic_log_events WHERE created_at < ?`, cutoff); err != nil {
			return err
		}
	}
	if maxEvents > 0 {
		if _, err := d.SQL.ExecContext(ctx,
			`DELETE FROM diagnostic_log_events
			 WHERE id IN (
				SELECT id FROM diagnostic_log_events ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?
			 )`, maxEvents); err != nil {
			return err
		}
	}
	if maxBytes > 0 {
		for {
			var total sql.NullInt64
			if err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes), 0) FROM diagnostic_log_events`).Scan(&total); err != nil {
				return err
			}
			if !total.Valid || total.Int64 <= maxBytes {
				break
			}
			result, err := d.SQL.ExecContext(ctx,
				`DELETE FROM diagnostic_log_events WHERE id IN (
					SELECT id FROM diagnostic_log_events ORDER BY created_at ASC, id ASC LIMIT 1
				)`)
			if err != nil {
				return err
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				break
			}
		}
	}
	return nil
}

func scanSettingsChangePlanRecord(scanner interface{ Scan(dest ...any) error }) (SettingsChangePlanRecord, error) {
	var record SettingsChangePlanRecord
	var planJSON string
	var approvalJSON string
	var liveReloadJSON string
	var postCheckPending int
	if err := scanner.Scan(&record.ID, &record.Status, &record.ConversationID, &record.AcceptedCardID, &record.CreatedBy, &planJSON, &approvalJSON, &liveReloadJSON, &record.RollbackID, &postCheckPending, &record.ErrorText, &record.CreatedAt, &record.UpdatedAt, &record.AppliedAt); err != nil {
		return SettingsChangePlanRecord{}, err
	}
	record.PlanJSON = planJSON
	record.ApprovalJSON = approvalJSON
	record.LiveReloadJSON = liveReloadJSON
	record.PostCheckPending = postCheckPending == 1
	return record, nil
}

func scanSettingsChangeRollbackRecord(scanner interface{ Scan(dest ...any) error }) (SettingsChangeRollbackRecord, error) {
	var record SettingsChangeRollbackRecord
	var rollbackJSON string
	var changesJSON string
	if err := scanner.Scan(&record.ID, &record.PlanID, &record.Status, &rollbackJSON, &changesJSON, &record.ErrorText, &record.CreatedAt, &record.UpdatedAt, &record.AppliedAt); err != nil {
		return SettingsChangeRollbackRecord{}, err
	}
	record.RollbackJSON = rollbackJSON
	record.ChangesJSON = changesJSON
	return record, nil
}

func settingsBoolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func defaultJSONObject(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	return value
}

func defaultJSONArray(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "[]"
	}
	return value
}
