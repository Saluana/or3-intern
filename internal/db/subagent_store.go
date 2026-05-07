package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	SubagentStatusQueued      = "queued"
	SubagentStatusRunning     = "running"
	SubagentStatusSucceeded   = "succeeded"
	SubagentStatusFailed      = "failed"
	SubagentStatusInterrupted = "interrupted"
)

var (
	ErrSubagentQueueFull           = errors.New("subagent queue is full")
	ErrInvalidSubagentStatusFilter = errors.New("invalid subagent status filter")
)

type SubagentJob struct {
	ID               string
	ParentSessionKey string
	ChildSessionKey  string
	Channel          string
	ReplyTo          string
	Task             string
	Status           string
	ResultPreview    string
	ArtifactID       string
	ErrorText        string
	RequestedAt      int64
	StartedAt        int64
	FinishedAt       int64
	Attempts         int
	MetadataJSON     string
}

// SubagentJobFilter narrows ListSubagentJobs results.
//
// Status accepts the database-native subagent statuses
// (queued, running, succeeded, failed, interrupted) plus the convenience
// values "active" (queued or running) and "terminal" (succeeded, failed,
// interrupted). An empty Status returns all rows.
type SubagentJobFilter struct {
	Status           string
	ParentSessionKey string
	Limit            int
}

// SubagentJobListDefaultLimit is the default row count returned when no
// limit is specified.
const SubagentJobListDefaultLimit = 50

// SubagentJobListMaxLimit caps the maximum row count to keep the response
// bounded for mobile clients.
const SubagentJobListMaxLimit = 100

func (d *DB) EnqueueSubagentJob(ctx context.Context, job SubagentJob) error {
	return d.EnqueueSubagentJobLimited(ctx, job, 0)
}

func (d *DB) EnqueueSubagentJobLimited(ctx context.Context, job SubagentJob, maxQueued int) error {
	if job.RequestedAt == 0 {
		job.RequestedAt = NowMS()
	}
	if strings.TrimSpace(job.Status) == "" {
		job.Status = SubagentStatusQueued
	}
	if strings.TrimSpace(job.MetadataJSON) == "" {
		job.MetadataJSON = "{}"
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := ensureSessionTx(ctx, tx, job.ParentSessionKey); err != nil {
		return err
	}
	if err := ensureSessionTx(ctx, tx, job.ChildSessionKey); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO subagent_jobs(
			id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		)
		SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
		WHERE ? <= 0 OR (SELECT COUNT(*) FROM subagent_jobs WHERE status=?) < ?`,
		job.ID,
		job.ParentSessionKey,
		job.ChildSessionKey,
		job.Channel,
		job.ReplyTo,
		job.Task,
		job.Status,
		job.ResultPreview,
		job.ArtifactID,
		job.ErrorText,
		job.RequestedAt,
		job.StartedAt,
		job.FinishedAt,
		job.Attempts,
		job.MetadataJSON,
		maxQueued,
		SubagentStatusQueued,
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
		return ErrSubagentQueueFull
	}
	return tx.Commit()
}

func (d *DB) GetSubagentJob(ctx context.Context, id string) (SubagentJob, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE id=?`, id)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SubagentJob{}, false, nil
		}
		return SubagentJob{}, false, err
	}
	return job, true, nil
}

func (d *DB) ListQueuedSubagentJobs(ctx context.Context) ([]SubagentJob, error) {
	return d.listSubagentJobsByStatus(ctx, SubagentStatusQueued)
}

func (d *DB) ListRunningSubagentJobs(ctx context.Context) ([]SubagentJob, error) {
	return d.listSubagentJobsByStatus(ctx, SubagentStatusRunning)
}

func (d *DB) listSubagentJobsByStatus(ctx context.Context, status string) ([]SubagentJob, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE status=? ORDER BY requested_at ASC, id ASC`,
		status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubagentJob
	for rows.Next() {
		job, err := scanSubagentJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// ListSubagentJobs returns persisted subagent jobs ordered newest first.
// It uses a stable sort key (max of requested_at/started_at/finished_at)
// so completed jobs appear above older queued ones.
func (d *DB) ListSubagentJobs(ctx context.Context, filter SubagentJobFilter) ([]SubagentJob, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = SubagentJobListDefaultLimit
	}
	if limit > SubagentJobListMaxLimit {
		limit = SubagentJobListMaxLimit
	}

	clauses := []string{}
	args := []any{}

	switch strings.ToLower(strings.TrimSpace(filter.Status)) {
	case "":
		// no filter
	case "active":
		clauses = append(clauses, "status IN (?, ?)")
		args = append(args, SubagentStatusQueued, SubagentStatusRunning)
	case "terminal":
		clauses = append(clauses, "status IN (?, ?, ?)")
		args = append(args, SubagentStatusSucceeded, SubagentStatusFailed, SubagentStatusInterrupted)
	case SubagentStatusQueued, SubagentStatusRunning, SubagentStatusSucceeded, SubagentStatusFailed, SubagentStatusInterrupted:
		clauses = append(clauses, "status=?")
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Status)))
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidSubagentStatusFilter, filter.Status)
	}

	if parent := strings.TrimSpace(filter.ParentSessionKey); parent != "" {
		clauses = append(clauses, "parent_session_key=?")
		args = append(args, parent)
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)

	// Order by the most recent activity timestamp so finished jobs sort
	// alongside their queued/started counterparts in a single timeline.
	query := `SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs ` + where + `
		 ORDER BY MAX(requested_at, started_at, finished_at) DESC, id DESC
		 LIMIT ?`

	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SubagentJob, 0, limit)
	for rows.Next() {
		job, err := scanSubagentJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (d *DB) MarkSubagentRunning(ctx context.Context, id string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END, attempts=attempts+1
		 WHERE id=? AND status=?`,
		SubagentStatusRunning, now, id, SubagentStatusQueued)
	return err
}

func (d *DB) ClaimNextSubagentJob(ctx context.Context) (*SubagentJob, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE status=? ORDER BY requested_at ASC, id ASC LIMIT 1`,
		SubagentStatusQueued)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs SET status=?, started_at=?, attempts=attempts+1 WHERE id=? AND status=?`,
		SubagentStatusRunning, now, job.ID, SubagentStatusQueued)
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
	job.Status = SubagentStatusRunning
	job.StartedAt = now
	job.Attempts++
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

func (d *DB) AbortQueuedSubagentJob(ctx context.Context, id, errText string) (SubagentJob, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return SubagentJob{}, false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE id=?`,
		id)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SubagentJob{}, false, nil
		}
		return SubagentJob{}, false, err
	}

	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=? AND status=?`,
		SubagentStatusInterrupted, errText, now, id, SubagentStatusQueued)
	if err != nil {
		return SubagentJob{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return SubagentJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SubagentJob{}, false, err
	}
	if affected == 0 {
		return job, false, nil
	}
	job.Status = SubagentStatusInterrupted
	job.ErrorText = errText
	job.FinishedAt = now
	return job, true, nil
}

func (d *DB) MarkSubagentSucceeded(ctx context.Context, id, preview, artifactID string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, result_preview=?, artifact_id=?, error_text='', finished_at=?
		 WHERE id=?`,
		SubagentStatusSucceeded, preview, artifactID, NowMS(), id)
	return err
}

func (d *DB) MarkSubagentFailed(ctx context.Context, id, errText string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=?`,
		SubagentStatusFailed, errText, NowMS(), id)
	return err
}

func (d *DB) MarkSubagentInterrupted(ctx context.Context, id, errText string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=?`,
		SubagentStatusInterrupted, errText, NowMS(), id)
	return err
}

func (d *DB) MarkRunningSubagentsInterrupted(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "interrupted during restart"
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE status=?`,
		SubagentStatusInterrupted, reason, NowMS(), SubagentStatusRunning)
	return err
}

func (d *DB) FinalizeSubagentJob(ctx context.Context, job SubagentJob, status, preview, artifactID, errText, parentSummary string, parentPayload any) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, result_preview=?, artifact_id=?, error_text=?, finished_at=?
		 WHERE id=? AND status=?`,
		status, preview, artifactID, errText, NowMS(), job.ID, SubagentStatusRunning)
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
	if strings.TrimSpace(parentSummary) != "" {
		if _, err := appendMessageTx(ctx, tx, job.ParentSessionKey, "assistant", parentSummary, parentPayload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanSubagentJob(scanner interface{ Scan(dest ...any) error }) (SubagentJob, error) {
	var job SubagentJob
	err := scanner.Scan(
		&job.ID,
		&job.ParentSessionKey,
		&job.ChildSessionKey,
		&job.Channel,
		&job.ReplyTo,
		&job.Task,
		&job.Status,
		&job.ResultPreview,
		&job.ArtifactID,
		&job.ErrorText,
		&job.RequestedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&job.Attempts,
		&job.MetadataJSON,
	)
	return job, err
}
