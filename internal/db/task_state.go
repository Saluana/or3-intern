package db

import (
	"context"
	"database/sql"
)

type TaskStateRow struct {
	ID                int64
	SessionKey        string
	ScopeKey          string
	Status            string
	Goal              string
	PlanJSON          string
	ConstraintsJSON   string
	DecisionsJSON     string
	OpenQuestionsJSON string
	MessageRefsJSON   string
	MemoryRefsJSON    string
	ArtifactRefsJSON  string
	ActiveFilesJSON   string
	MetadataJSON      string
	CreatedAt         int64
	UpdatedAt         int64
}

func (d *DB) UpsertActiveTaskState(ctx context.Context, row TaskStateRow) error {
	now := NowMS()
	if row.UpdatedAt <= 0 {
		row.UpdatedAt = now
	}
	if row.CreatedAt <= 0 {
		row.CreatedAt = now
	}
	_, err := d.SQL.ExecContext(ctx, `
INSERT INTO task_state(
	session_key, scope_key, status, goal, plan_json, constraints_json, decisions_json,
	open_questions_json, message_refs_json, memory_refs_json, artifact_refs_json, active_files_json,
	metadata_json, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
	goal=excluded.goal,
	plan_json=excluded.plan_json,
	constraints_json=excluded.constraints_json,
	decisions_json=excluded.decisions_json,
	open_questions_json=excluded.open_questions_json,
	message_refs_json=excluded.message_refs_json,
	memory_refs_json=excluded.memory_refs_json,
	artifact_refs_json=excluded.artifact_refs_json,
	active_files_json=excluded.active_files_json,
	metadata_json=excluded.metadata_json,
	status=excluded.status,
	scope_key=excluded.scope_key,
	updated_at=excluded.updated_at`,
		row.SessionKey, row.ScopeKey, row.Status, row.Goal, row.PlanJSON, row.ConstraintsJSON, row.DecisionsJSON,
		row.OpenQuestionsJSON, row.MessageRefsJSON, row.MemoryRefsJSON, row.ArtifactRefsJSON, row.ActiveFilesJSON,
		row.MetadataJSON, row.CreatedAt, row.UpdatedAt)
	if err == nil {
		return nil
	}
	// Fallback upsert by logical key (session,status=active) when no id is present.
	_, err = d.SQL.ExecContext(ctx, `
INSERT INTO task_state(
	session_key, scope_key, status, goal, plan_json, constraints_json, decisions_json,
	open_questions_json, message_refs_json, memory_refs_json, artifact_refs_json, active_files_json,
	metadata_json, created_at, updated_at
)
SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
WHERE NOT EXISTS (SELECT 1 FROM task_state WHERE session_key=? AND status='active')`,
		row.SessionKey, row.ScopeKey, row.Status, row.Goal, row.PlanJSON, row.ConstraintsJSON, row.DecisionsJSON,
		row.OpenQuestionsJSON, row.MessageRefsJSON, row.MemoryRefsJSON, row.ArtifactRefsJSON, row.ActiveFilesJSON,
		row.MetadataJSON, row.CreatedAt, row.UpdatedAt,
		row.SessionKey)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, `
UPDATE task_state SET
	scope_key=?, goal=?, plan_json=?, constraints_json=?, decisions_json=?, open_questions_json=?,
	message_refs_json=?, memory_refs_json=?, artifact_refs_json=?, active_files_json=?, metadata_json=?, updated_at=?
WHERE session_key=? AND status='active'`,
		row.ScopeKey, row.Goal, row.PlanJSON, row.ConstraintsJSON, row.DecisionsJSON, row.OpenQuestionsJSON,
		row.MessageRefsJSON, row.MemoryRefsJSON, row.ArtifactRefsJSON, row.ActiveFilesJSON, row.MetadataJSON, row.UpdatedAt,
		row.SessionKey)
	return err
}

func (d *DB) GetActiveTaskState(ctx context.Context, sessionKey string) (TaskStateRow, bool, error) {
	var row TaskStateRow
	err := d.SQL.QueryRowContext(ctx, `
SELECT id, session_key, scope_key, status, goal, plan_json, constraints_json, decisions_json,
	open_questions_json, message_refs_json, memory_refs_json, artifact_refs_json, active_files_json,
	metadata_json, created_at, updated_at
FROM task_state
WHERE session_key=? AND status='active'
ORDER BY updated_at DESC
LIMIT 1`, sessionKey).Scan(
		&row.ID, &row.SessionKey, &row.ScopeKey, &row.Status, &row.Goal, &row.PlanJSON, &row.ConstraintsJSON, &row.DecisionsJSON,
		&row.OpenQuestionsJSON, &row.MessageRefsJSON, &row.MemoryRefsJSON, &row.ArtifactRefsJSON, &row.ActiveFilesJSON,
		&row.MetadataJSON, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return TaskStateRow{}, false, nil
	}
	if err != nil {
		return TaskStateRow{}, false, err
	}
	return row, true, nil
}

func (d *DB) CompleteActiveTaskState(ctx context.Context, sessionKey string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE task_state SET status='completed', updated_at=? WHERE session_key=? AND status='active'`,
		NowMS(), sessionKey)
	return err
}
