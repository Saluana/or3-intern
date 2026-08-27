package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const (
	RunnerRunStatusQueued           = "queued"
	RunnerRunStatusStarting         = "starting"
	RunnerRunStatusRunning          = "running"
	RunnerRunStatusSucceeded        = "succeeded"
	RunnerRunStatusFailed           = "failed"
	RunnerRunStatusAborted          = "aborted"
	RunnerRunStatusTimedOut         = "timed_out"
	RunnerRunStatusApprovalRequired = "approval_required"
)

var ErrRunnerRunQueueFull = errors.New("runner run queue is full")

type RunnerRun struct {
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

type RunnerRunEvent struct {
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

type RunnerRunFinalizeInput struct {
	Status           string
	ExitCode         int
	StdoutPreview    string
	StderrPreview    string
	FinalTextPreview string
	ErrorMessage     string
	CompletedAt      int64
}

type RunnerRunFilter struct {
	Status           string
	ParentSessionKey string
	Limit            int
}

const RunnerRunListDefaultLimit = 50
const RunnerRunListMaxLimit = 100

func (d *DB) EnqueueRunnerRun(ctx context.Context, run RunnerRun) error {
	return d.EnqueueRunnerRunLimited(ctx, run, 0)
}

func (d *DB) EnqueueRunnerRunLimited(ctx context.Context, run RunnerRun, maxQueued int) error {
	if run.RequestedAt == 0 {
		run.RequestedAt = NowMS()
	}
	if strings.TrimSpace(run.Status) == "" {
		run.Status = RunnerRunStatusQueued
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
		`INSERT INTO runner_runs(
			id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message,
			attempts, meta_json
		)
		SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
		WHERE ? <= 0 OR (SELECT COUNT(*) FROM runner_runs WHERE status=?) < ?`,
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
		RunnerRunStatusQueued,
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
		return ErrRunnerRunQueueFull
	}
	return tx.Commit()
}

func (d *DB) GetRunnerRun(ctx context.Context, idOrJobID string) (RunnerRun, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM runner_runs WHERE id=? OR job_id=?`,
		idOrJobID, idOrJobID)
	run, err := scanRunnerRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return RunnerRun{}, false, nil
		}
		return RunnerRun{}, false, err
	}
	return run, true, nil
}

func (d *DB) ListQueuedRunnerRuns(ctx context.Context) ([]RunnerRun, error) {
	return d.listRunnerRunsByStatus(ctx, RunnerRunStatusQueued)
}

func (d *DB) ListRunningRunnerRuns(ctx context.Context) ([]RunnerRun, error) {
	return d.listRunnerRunsByStatus(ctx, RunnerRunStatusRunning)
}

// ListApprovalRequiredRunnerRuns returns native runs paused for approval.
// Startup reconciliation treats these like interrupted work because the live
// native runtime cannot be safely resumed across a service restart.
func (d *DB) ListApprovalRequiredRunnerRuns(ctx context.Context) ([]RunnerRun, error) {
	return d.listRunnerRunsByStatus(ctx, RunnerRunStatusApprovalRequired)
}

func (d *DB) ListRunnerRuns(ctx context.Context, filter RunnerRunFilter) ([]RunnerRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = RunnerRunListDefaultLimit
	}
	if limit > RunnerRunListMaxLimit {
		limit = RunnerRunListMaxLimit
	}

	query := `SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM runner_runs`
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
	var out []RunnerRun
	for rows.Next() {
		run, err := scanRunnerRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (d *DB) listRunnerRunsByStatus(ctx context.Context, status string) ([]RunnerRun, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM runner_runs WHERE status=? ORDER BY requested_at ASC, id ASC`,
		status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunnerRun
	for rows.Next() {
		run, err := scanRunnerRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (d *DB) ClaimNextRunnerRun(ctx context.Context) (*RunnerRun, error) {
	now := NowMS()
	row := d.SQL.QueryRowContext(ctx,
		`UPDATE runner_runs
		 SET status=?, started_at=?, attempts=attempts+1
		 WHERE id=(
		 	SELECT id FROM runner_runs
		 	WHERE status=?
		 	ORDER BY requested_at ASC, id ASC
		 	LIMIT 1
		 )
		 AND status=?
		 RETURNING id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
		 	pid, requested_at, started_at, completed_at, timeout_seconds,
		 	exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json`,
		RunnerRunStatusRunning, now, RunnerRunStatusQueued, RunnerRunStatusQueued)
	run, err := scanRunnerRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (d *DB) AbortQueuedRunnerRun(ctx context.Context, idOrJobID, reason string) (RunnerRun, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return RunnerRun{}, false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(ctx,
		`SELECT id, job_id, parent_session_key, runner_id, task, cwd, model, mode, isolation, status,
			pid, requested_at, started_at, completed_at, timeout_seconds,
			exit_code, stdout_preview, stderr_preview, final_text_preview, error_message, attempts, meta_json
		 FROM runner_runs WHERE id=? OR job_id=?`,
		idOrJobID, idOrJobID)
	run, err := scanRunnerRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return RunnerRun{}, false, nil
		}
		return RunnerRun{}, false, err
	}

	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE runner_runs
		 SET status=?, error_message=?, completed_at=?
		 WHERE id=? AND status=?`,
		RunnerRunStatusAborted, reason, now, run.ID, RunnerRunStatusQueued)
	if err != nil {
		return RunnerRun{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return RunnerRun{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return RunnerRun{}, false, err
	}
	if affected == 0 {
		return run, false, nil
	}
	run.Status = RunnerRunStatusAborted
	run.ErrorMessage = reason
	run.CompletedAt = now
	return run, true, nil
}

func (d *DB) MarkRunningRunnerRunsAborted(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "interrupted during restart"
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_runs
		 SET status=?, error_message=?, completed_at=?
		 WHERE status=?`,
		RunnerRunStatusAborted, reason, NowMS(), RunnerRunStatusRunning)
	return err
}

// MarkRunnerRunApprovalRequired pauses a claimed run until its native
// approval is resolved. The conditional update makes the transition
// idempotent with an abort or timeout racing the approval event.
func (d *DB) MarkRunnerRunApprovalRequired(ctx context.Context, runID string) error {
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_runs SET status=? WHERE id=? AND status=?`,
		RunnerRunStatusApprovalRequired, runID, RunnerRunStatusRunning)
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

// ResumeRunnerRunAfterApproval reopens a paused native run. Runs already
// marked running are accepted for compatibility with older rows created
// before approval-required persistence was introduced.
func (d *DB) ResumeRunnerRunAfterApproval(ctx context.Context, runID string) error {
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_runs SET status=?, completed_at=0, error_message='' WHERE id=? AND status=?`,
		RunnerRunStatusRunning, runID, RunnerRunStatusApprovalRequired)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	var status string
	if err := d.SQL.QueryRowContext(ctx, `SELECT status FROM runner_runs WHERE id=?`, runID).Scan(&status); err != nil {
		return err
	}
	if status == RunnerRunStatusRunning {
		return nil
	}
	return sql.ErrNoRows
}

func (d *DB) AppendRunnerRunEvent(ctx context.Context, event RunnerRunEvent) error {
	_, err := d.SQL.ExecContext(ctx,
		`INSERT OR IGNORE INTO runner_run_events(run_id, job_id, seq, ts, type, stream, chunk, payload_json)
		 VALUES(?,?,?,?,?,?,?,?)`,
		event.RunID, event.JobID, event.Seq, event.TS, event.Type, event.Stream, event.Chunk, event.PayloadJSON)
	return err
}

func (d *DB) ListRunnerRunEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]RunnerRunEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, run_id, job_id, seq, ts, type, stream, chunk, payload_json
		 FROM runner_run_events WHERE job_id=? AND seq > ?
		 ORDER BY seq ASC LIMIT ?`,
		jobID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunnerRunEvent
	for rows.Next() {
		var e RunnerRunEvent
		if err := rows.Scan(&e.ID, &e.RunID, &e.JobID, &e.Seq, &e.TS, &e.Type, &e.Stream, &e.Chunk, &e.PayloadJSON); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) FinalizeRunnerRun(ctx context.Context, runID string, final RunnerRunFinalizeInput) error {
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_runs
		 SET status=?, exit_code=?, stdout_preview=?, stderr_preview=?, final_text_preview=?,
		     error_message=?, completed_at=?
		 WHERE id=? AND status IN (?,?)`,
		final.Status, final.ExitCode, final.StdoutPreview, final.StderrPreview, final.FinalTextPreview,
		final.ErrorMessage, final.CompletedAt,
		runID, RunnerRunStatusRunning, RunnerRunStatusApprovalRequired)
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

func scanRunnerRun(scanner interface{ Scan(dest ...any) error }) (RunnerRun, error) {
	var run RunnerRun
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
