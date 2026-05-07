package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const (
	AgentCLIStatusQueued    = "queued"
	AgentCLIStatusStarting  = "starting"
	AgentCLIStatusRunning   = "running"
	AgentCLIStatusSucceeded = "succeeded"
	AgentCLIStatusFailed    = "failed"
	AgentCLIStatusAborted   = "aborted"
	AgentCLIStatusTimedOut  = "timed_out"
)

var ErrAgentCLIQueueFull = errors.New("agent CLI queue is full")

type AgentCLIRun struct {
	ID               string
	JobID            string
	ParentSessionKey string
	RunnerID         string
	Task             string
	Cwd              string
	Model            string
	Mode             string
	Isolation        string
	Status           string
	PID              int
	RequestedAt      int64
	StartedAt        int64
	CompletedAt      int64
	TimeoutSeconds   int
	ExitCode         sql.NullInt64
	StdoutPreview    string
	StderrPreview    string
	FinalTextPreview string
	ErrorMessage     string
	Attempts         int
	MetaJSON         string
}

type AgentCLIEvent struct {
	ID          int64
	RunID       string
	JobID       string
	Seq         int64
	TS          string
	Type        string
	Stream      string
	Chunk       string
	PayloadJSON string
}

type AgentCLIFinalizeInput struct {
	Status           string
	ExitCode         int
	StdoutPreview    string
	StderrPreview    string
	FinalTextPreview string
	ErrorMessage     string
	CompletedAt      int64
}

type AgentCLIRunFilter struct {
	Status           string
	ParentSessionKey string
	Limit            int
}

const AgentCLIRunListDefaultLimit = 50
const AgentCLIRunListMaxLimit = 100

func (d *DB) EnqueueAgentCLIRun(ctx context.Context, run AgentCLIRun) error {
	return d.EnqueueAgentCLIRunLimited(ctx, run, 0)
}

func (d *DB) EnqueueAgentCLIRunLimited(ctx context.Context, run AgentCLIRun, maxQueued int) error {
	if run.RequestedAt == 0 {
		run.RequestedAt = NowMS()
	}
	if strings.TrimSpace(run.Status) == "" {
		run.Status = AgentCLIStatusQueued
	}
	if strings.TrimSpace(run.MetaJSON) == "" {
		run.MetaJSON = "{}"
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := ensureSessionTx(ctx, tx, run.ParentSessionKey); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO agent_cli_runs(
			id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message,
			attempts, meta_json
		)
		SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
		WHERE ? <= 0 OR (SELECT COUNT(*) FROM agent_cli_runs WHERE status=?) < ?`,
		run.ID,
		run.JobID,
		run.ParentSessionKey,
		run.RunnerID,
		run.Task,
		run.Cwd,
		run.Model,
		run.Mode,
		run.Isolation,
		run.Status,
		run.PID,
		run.RequestedAt,
		run.StartedAt,
		run.CompletedAt,
		run.TimeoutSeconds,
		run.ExitCode,
		run.StdoutPreview,
		run.StderrPreview,
		run.FinalTextPreview,
		run.ErrorMessage,
		run.Attempts,
		run.MetaJSON,
		maxQueued,
		AgentCLIStatusQueued,
		maxQueued,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrAgentCLIQueueFull
	}
	return tx.Commit()
}

func (d *DB) GetAgentCLIRun(ctx context.Context, idOrJobID string) (AgentCLIRun, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM agent_cli_runs WHERE id=? OR job_id=?`,
		idOrJobID, idOrJobID)
	run, err := scanAgentCLIRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentCLIRun{}, false, nil
		}
		return AgentCLIRun{}, false, err
	}
	return run, true, nil
}

func (d *DB) ListQueuedAgentCLIRuns(ctx context.Context) ([]AgentCLIRun, error) {
	return d.listAgentCLIRunsByStatus(ctx, AgentCLIStatusQueued)
}

func (d *DB) ListRunningAgentCLIRuns(ctx context.Context) ([]AgentCLIRun, error) {
	return d.listAgentCLIRunsByStatus(ctx, AgentCLIStatusRunning)
}

func (d *DB) ListAgentCLIRuns(ctx context.Context, filter AgentCLIRunFilter) ([]AgentCLIRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = AgentCLIRunListDefaultLimit
	}
	if limit > AgentCLIRunListMaxLimit {
		limit = AgentCLIRunListMaxLimit
	}

	query := `SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM agent_cli_runs`
	var conditions []string
	var args []any
	if status := strings.TrimSpace(filter.Status); status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, status)
	}
	if session := strings.TrimSpace(filter.ParentSessionKey); session != "" {
		conditions = append(conditions, "parent_session_key=?")
		args = append(args, session)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY requested_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentCLIRun
	for rows.Next() {
		run, err := scanAgentCLIRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (d *DB) listAgentCLIRunsByStatus(ctx context.Context, status string) ([]AgentCLIRun, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM agent_cli_runs WHERE status=? ORDER BY requested_at ASC, id ASC`,
		status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentCLIRun
	for rows.Next() {
		run, err := scanAgentCLIRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (d *DB) ClaimNextAgentCLIRun(ctx context.Context) (*AgentCLIRun, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM agent_cli_runs WHERE status=? ORDER BY requested_at ASC, id ASC LIMIT 1`,
		AgentCLIStatusQueued)
	run, err := scanAgentCLIRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE agent_cli_runs SET status=?, started_at=?, attempts=attempts+1 WHERE id=? AND status=?`,
		AgentCLIStatusRunning, now, run.ID, AgentCLIStatusQueued)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, tx.Commit()
	}
	run.Status = AgentCLIStatusRunning
	run.StartedAt = now
	run.Attempts++
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &run, nil
}

func (d *DB) AbortQueuedAgentCLIRun(ctx context.Context, idOrJobID, reason string) (AgentCLIRun, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return AgentCLIRun{}, false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM agent_cli_runs WHERE id=? OR job_id=?`,
		idOrJobID, idOrJobID)
	run, err := scanAgentCLIRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return AgentCLIRun{}, false, nil
		}
		return AgentCLIRun{}, false, err
	}

	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE agent_cli_runs
		 SET status=?, error_message=?, completed_at=?
		 WHERE id=? AND status=?`,
		AgentCLIStatusAborted, reason, now, run.ID, AgentCLIStatusQueued)
	if err != nil {
		return AgentCLIRun{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return AgentCLIRun{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AgentCLIRun{}, false, err
	}
	if affected == 0 {
		return run, false, nil
	}
	run.Status = AgentCLIStatusAborted
	run.ErrorMessage = reason
	run.CompletedAt = now
	return run, true, nil
}

func (d *DB) MarkRunningAgentCLIRunsAborted(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "interrupted during restart"
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE agent_cli_runs
		 SET status=?, error_message=?, completed_at=?
		 WHERE status=?`,
		AgentCLIStatusAborted, reason, NowMS(), AgentCLIStatusRunning)
	return err
}

func (d *DB) AppendAgentCLIEvent(ctx context.Context, event AgentCLIEvent) error {
	_, err := d.SQL.ExecContext(ctx,
		`INSERT OR IGNORE INTO agent_cli_events(run_id, job_id, seq, ts, type, stream, chunk, payload_json)
		 VALUES(?,?,?,?,?,?,?,?)`,
		event.RunID, event.JobID, event.Seq, event.TS, event.Type, event.Stream, event.Chunk, event.PayloadJSON)
	return err
}

func (d *DB) ListAgentCLIEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]AgentCLIEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, run_id, job_id, seq, ts, type, stream, chunk, payload_json
		 FROM agent_cli_events WHERE job_id=? AND seq > ?
		 ORDER BY seq ASC LIMIT ?`,
		jobID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentCLIEvent
	for rows.Next() {
		var e AgentCLIEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.JobID, &e.Seq, &e.TS, &e.Type, &e.Stream, &e.Chunk, &e.PayloadJSON); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) FinalizeAgentCLIRun(ctx context.Context, runID string, final AgentCLIFinalizeInput) error {
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE agent_cli_runs
		 SET status=?, exit_code=?, stdout_preview=?, stderr_preview=?, final_text_preview=?,
		     error_message=?, completed_at=?
		 WHERE id=? AND status=?`,
		final.Status, final.ExitCode, final.StdoutPreview, final.StderrPreview, final.FinalTextPreview,
		final.ErrorMessage, final.CompletedAt,
		runID, AgentCLIStatusRunning)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanAgentCLIRun(scanner interface{ Scan(dest ...any) error }) (AgentCLIRun, error) {
	var run AgentCLIRun
	err := scanner.Scan(
		&run.ID,
		&run.JobID,
		&run.ParentSessionKey,
		&run.RunnerID,
		&run.Task,
		&run.Cwd,
		&run.Model,
		&run.Mode,
		&run.Isolation,
		&run.Status,
		&run.PID,
		&run.RequestedAt,
		&run.StartedAt,
		&run.CompletedAt,
		&run.TimeoutSeconds,
		&run.ExitCode,
		&run.StdoutPreview,
		&run.StderrPreview,
		&run.FinalTextPreview,
		&run.ErrorMessage,
		&run.Attempts,
		&run.MetaJSON,
	)
	return run, err
}
