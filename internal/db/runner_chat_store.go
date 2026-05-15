package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Runner chat session/turn statuses.
const (
	RunnerChatTurnStatusQueued           = "queued"
	RunnerChatTurnStatusRunning          = "running"
	RunnerChatTurnStatusSucceeded        = "succeeded"
	RunnerChatTurnStatusApprovalRequired = "approval_required"
	RunnerChatTurnStatusFailed           = "failed"
	RunnerChatTurnStatusAborted          = "aborted"
	RunnerChatTurnStatusTimedOut         = "timed_out"
)

// ErrRunnerChatTurnActive is returned when a session already has a queued or
// running turn and a new turn is requested.
var ErrRunnerChatTurnActive = errors.New("runner chat turn already active")

// ErrRunnerChatSessionNotFound is returned when a session lookup misses.
var ErrRunnerChatSessionNotFound = errors.New("runner chat session not found")

// ErrRunnerChatTurnNotFound is returned when a turn lookup misses.
var ErrRunnerChatTurnNotFound = errors.New("runner chat turn not found")

// RunnerChatSession is a row in runner_chat_sessions.
type RunnerChatSession struct {
	ID               string
	AppSessionKey    string
	RunnerID         string
	ContinuationMode string
	NativeSessionRef string
	Model            string
	Mode             string
	Isolation        string
	Cwd              string
	MaxTurns         int
	MetaJSON         string
	CreatedAt        int64
	UpdatedAt        int64
}

// RunnerChatTurn is a row in runner_chat_turns.
type RunnerChatTurn struct {
	ID                 string
	SessionID          string
	Sequence           int64
	Status             string
	UserMessage        string
	FinalText          string
	ErrorMessage       string
	AgentCLIRunID      string
	AgentCLIJobID      string
	Model              string
	Mode               string
	Isolation          string
	Cwd                string
	ContinuationMode   string
	UserMessageID      int64
	AssistantMessageID int64
	RequestedAt        int64
	StartedAt          int64
	CompletedAt        int64
	MetaJSON           string
}

// RunnerChatEvent is a row in runner_chat_events.
type RunnerChatEvent struct {
	ID          int64
	TurnID      string
	SessionID   string
	JobID       string
	Seq         int64
	TS          int64
	Type        string
	Stream      string
	Text        string
	PayloadJSON string
}

// RunnerChatTurnFinalize captures terminal state when finalizing a turn.
type RunnerChatTurnFinalize struct {
	Status             string
	FinalText          string
	ErrorMessage       string
	AssistantMessageID int64
	CompletedAt        int64
}

// CreateOrGetRunnerChatSession creates or returns an existing session row
// for the (app_session_key, runner_id) tuple.
func (d *DB) CreateOrGetRunnerChatSession(ctx context.Context, sess RunnerChatSession) (RunnerChatSession, error) {
	if strings.TrimSpace(sess.ID) == "" {
		return RunnerChatSession{}, errors.New("runner chat session id required")
	}
	if strings.TrimSpace(sess.AppSessionKey) == "" {
		return RunnerChatSession{}, errors.New("app_session_key required")
	}
	if strings.TrimSpace(sess.RunnerID) == "" {
		return RunnerChatSession{}, errors.New("runner_id required")
	}
	if strings.TrimSpace(sess.ContinuationMode) == "" {
		sess.ContinuationMode = "replay"
	}
	if strings.TrimSpace(sess.MetaJSON) == "" {
		sess.MetaJSON = "{}"
	}
	now := NowMS()
	if sess.CreatedAt == 0 {
		sess.CreatedAt = now
	}
	if sess.UpdatedAt == 0 {
		sess.UpdatedAt = now
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return RunnerChatSession{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureSessionTx(ctx, tx, sess.AppSessionKey); err != nil {
		return RunnerChatSession{}, err
	}
	// Try to read existing.
	row := tx.QueryRowContext(ctx,
		`SELECT id, app_session_key, runner_id, continuation_mode, native_session_ref,
			model, mode, isolation, cwd, max_turns, meta_json, created_at, updated_at
		 FROM runner_chat_sessions WHERE app_session_key=? AND runner_id=?`,
		sess.AppSessionKey, sess.RunnerID)
	existing, scanErr := scanRunnerChatSession(row)
	if scanErr == nil {
		return existing, nil
	}
	if !errors.Is(scanErr, sql.ErrNoRows) {
		return RunnerChatSession{}, scanErr
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runner_chat_sessions(
			id, app_session_key, runner_id, continuation_mode, native_session_ref,
			model, mode, isolation, cwd, max_turns, meta_json, created_at, updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sess.ID, sess.AppSessionKey, sess.RunnerID, sess.ContinuationMode, sess.NativeSessionRef,
		sess.Model, sess.Mode, sess.Isolation, sess.Cwd, sess.MaxTurns, sess.MetaJSON,
		sess.CreatedAt, sess.UpdatedAt,
	); err != nil {
		return RunnerChatSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunnerChatSession{}, err
	}
	return sess, nil
}

// GetRunnerChatSession returns a session by ID.
func (d *DB) GetRunnerChatSession(ctx context.Context, id string) (RunnerChatSession, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, app_session_key, runner_id, continuation_mode, native_session_ref,
			model, mode, isolation, cwd, max_turns, meta_json, created_at, updated_at
		 FROM runner_chat_sessions WHERE id=?`, id)
	sess, err := scanRunnerChatSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RunnerChatSession{}, ErrRunnerChatSessionNotFound
	}
	return sess, err
}

// UpdateRunnerChatSessionNativeRef updates the native session reference and
// bumps updated_at.
func (d *DB) UpdateRunnerChatSessionNativeRef(ctx context.Context, id, ref string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_chat_sessions SET native_session_ref=?, updated_at=? WHERE id=?`,
		ref, now, id)
	return err
}

// CreateRunnerChatTurn inserts a new turn for a session. Returns
// ErrRunnerChatTurnActive when an active turn already exists.
func (d *DB) CreateRunnerChatTurn(ctx context.Context, turn RunnerChatTurn) (RunnerChatTurn, error) {
	if strings.TrimSpace(turn.ID) == "" {
		return RunnerChatTurn{}, errors.New("turn id required")
	}
	if strings.TrimSpace(turn.SessionID) == "" {
		return RunnerChatTurn{}, errors.New("session_id required")
	}
	if strings.TrimSpace(turn.Status) == "" {
		turn.Status = RunnerChatTurnStatusQueued
	}
	if strings.TrimSpace(turn.MetaJSON) == "" {
		turn.MetaJSON = "{}"
	}
	if turn.RequestedAt == 0 {
		turn.RequestedAt = NowMS()
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return RunnerChatTurn{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// Verify session exists.
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM runner_chat_sessions WHERE id=?`, turn.SessionID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunnerChatTurn{}, ErrRunnerChatSessionNotFound
		}
		return RunnerChatTurn{}, err
	}
	// Compute next sequence.
	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM runner_chat_turns WHERE session_id=?`, turn.SessionID).Scan(&maxSeq); err != nil {
		return RunnerChatTurn{}, err
	}
	turn.Sequence = maxSeq.Int64 + 1
	_, err = tx.ExecContext(ctx,
		`INSERT INTO runner_chat_turns(
			id, session_id, sequence, status, user_message, final_text, error_message,
			agent_cli_run_id, agent_cli_job_id, model, mode, isolation, cwd, continuation_mode,
			user_message_id, assistant_message_id, requested_at, started_at, completed_at, meta_json
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		turn.ID, turn.SessionID, turn.Sequence, turn.Status, turn.UserMessage, turn.FinalText, turn.ErrorMessage,
		turn.AgentCLIRunID, turn.AgentCLIJobID, turn.Model, turn.Mode, turn.Isolation, turn.Cwd, turn.ContinuationMode,
		turn.UserMessageID, turn.AssistantMessageID, turn.RequestedAt, turn.StartedAt, turn.CompletedAt, turn.MetaJSON,
	)
	if err != nil {
		// Partial unique index constraint failure means an active turn exists.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "unique") && (strings.Contains(msg, "idx_runner_chat_turns_active") || strings.Contains(msg, "runner_chat_turns.session_id")) {
			return RunnerChatTurn{}, ErrRunnerChatTurnActive
		}
		return RunnerChatTurn{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runner_chat_sessions SET updated_at=? WHERE id=?`, NowMS(), turn.SessionID); err != nil {
		return RunnerChatTurn{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunnerChatTurn{}, err
	}
	return turn, nil
}

// GetRunnerChatTurn returns a single turn.
func (d *DB) GetRunnerChatTurn(ctx context.Context, id string) (RunnerChatTurn, error) {
	row := d.SQL.QueryRowContext(ctx, runnerChatTurnSelectSQL+` WHERE id=?`, id)
	t, err := scanRunnerChatTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RunnerChatTurn{}, ErrRunnerChatTurnNotFound
	}
	return t, err
}

// GetActiveRunnerChatTurn returns the active turn for a session, if any.
func (d *DB) GetActiveRunnerChatTurn(ctx context.Context, sessionID string) (RunnerChatTurn, bool, error) {
	row := d.SQL.QueryRowContext(ctx, runnerChatTurnSelectSQL+
		` WHERE session_id=? AND status IN ('queued','running') ORDER BY sequence DESC LIMIT 1`,
		sessionID)
	t, err := scanRunnerChatTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RunnerChatTurn{}, false, nil
	}
	if err != nil {
		return RunnerChatTurn{}, false, err
	}
	return t, true, nil
}

// ListRunnerChatTurns returns the most recent turns for a session in
// chronological (ascending sequence) order. limit <= 0 means no limit.
func (d *DB) ListRunnerChatTurns(ctx context.Context, sessionID string, limit int) ([]RunnerChatTurn, error) {
	q := runnerChatTurnSelectSQL + ` WHERE session_id=? ORDER BY sequence ASC`
	args := []any{sessionID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunnerChatTurn
	for rows.Next() {
		t, err := scanRunnerChatTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkRunnerChatTurnStarted transitions a queued turn to running.
func (d *DB) MarkRunnerChatTurnStarted(ctx context.Context, id string, runID, jobID string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_chat_turns SET status='running', started_at=?, agent_cli_run_id=?, agent_cli_job_id=?
		 WHERE id=? AND status='queued'`,
		now, runID, jobID, id)
	return err
}

// FinalizeRunnerChatTurn writes terminal status/text and timestamps.
func (d *DB) FinalizeRunnerChatTurn(ctx context.Context, id string, in RunnerChatTurnFinalize) error {
	if strings.TrimSpace(in.Status) == "" {
		return errors.New("status required")
	}
	if in.CompletedAt == 0 {
		in.CompletedAt = NowMS()
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_chat_turns
		 SET status=?, final_text=?, error_message=?, assistant_message_id=COALESCE(NULLIF(?,0), assistant_message_id), completed_at=?
		 WHERE id=?`,
		in.Status, in.FinalText, in.ErrorMessage, in.AssistantMessageID, in.CompletedAt, id)
	return err
}

// SetRunnerChatTurnUserMessageID sets the persisted user message ID for a turn.
func (d *DB) SetRunnerChatTurnUserMessageID(ctx context.Context, id string, messageID int64) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_chat_turns SET user_message_id=? WHERE id=?`, messageID, id)
	return err
}

// AppendRunnerChatEvent inserts a new event for a turn.
func (d *DB) AppendRunnerChatEvent(ctx context.Context, ev RunnerChatEvent) error {
	if strings.TrimSpace(ev.TurnID) == "" {
		return errors.New("turn_id required")
	}
	if strings.TrimSpace(ev.Type) == "" {
		return errors.New("type required")
	}
	if ev.TS == 0 {
		ev.TS = NowMS()
	}
	if ev.PayloadJSON == "" {
		ev.PayloadJSON = ""
	}
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO runner_chat_events(turn_id, session_id, job_id, seq, ts, type, stream, text, payload_json)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		ev.TurnID, ev.SessionID, ev.JobID, ev.Seq, ev.TS, ev.Type, ev.Stream, ev.Text, ev.PayloadJSON)
	return err
}

// ListRunnerChatEvents returns events for a turn after a given sequence.
func (d *DB) ListRunnerChatEvents(ctx context.Context, turnID string, afterSeq int64, limit int) ([]RunnerChatEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, turn_id, session_id, job_id, seq, ts, type, stream, text, payload_json
		 FROM runner_chat_events WHERE turn_id=? AND seq>? ORDER BY seq ASC LIMIT ?`,
		turnID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunnerChatEvent
	for rows.Next() {
		var ev RunnerChatEvent
		if err := rows.Scan(&ev.ID, &ev.TurnID, &ev.SessionID, &ev.JobID, &ev.Seq, &ev.TS, &ev.Type, &ev.Stream, &ev.Text, &ev.PayloadJSON); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// MaxRunnerChatEventSeq returns the largest seq currently persisted for a turn.
// Returns 0 when no events exist yet.
func (d *DB) MaxRunnerChatEventSeq(ctx context.Context, turnID string) (int64, error) {
	var max sql.NullInt64
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM runner_chat_events WHERE turn_id=?`, turnID).Scan(&max); err != nil {
		return 0, err
	}
	return max.Int64, nil
}

// ReconcileRunnerChatTurnsOnStartup transitions any in-flight turns to
// aborted. Returns the number of turns that were reconciled.
func (d *DB) ReconcileRunnerChatTurnsOnStartup(ctx context.Context) (int, error) {
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE runner_chat_turns
		 SET status='aborted', error_message=COALESCE(NULLIF(error_message,''),'service restarted'), completed_at=?
		 WHERE status IN ('queued','running')`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

const runnerChatTurnSelectSQL = `SELECT id, session_id, sequence, status, user_message, final_text, error_message,
		agent_cli_run_id, agent_cli_job_id, model, mode, isolation, cwd, continuation_mode,
		user_message_id, assistant_message_id, requested_at, started_at, completed_at, meta_json
	FROM runner_chat_turns`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunnerChatSession(row rowScanner) (RunnerChatSession, error) {
	var s RunnerChatSession
	err := row.Scan(
		&s.ID, &s.AppSessionKey, &s.RunnerID, &s.ContinuationMode, &s.NativeSessionRef,
		&s.Model, &s.Mode, &s.Isolation, &s.Cwd, &s.MaxTurns, &s.MetaJSON,
		&s.CreatedAt, &s.UpdatedAt,
	)
	return s, err
}

func scanRunnerChatTurn(row rowScanner) (RunnerChatTurn, error) {
	var t RunnerChatTurn
	err := row.Scan(
		&t.ID, &t.SessionID, &t.Sequence, &t.Status, &t.UserMessage, &t.FinalText, &t.ErrorMessage,
		&t.AgentCLIRunID, &t.AgentCLIJobID, &t.Model, &t.Mode, &t.Isolation, &t.Cwd, &t.ContinuationMode,
		&t.UserMessageID, &t.AssistantMessageID, &t.RequestedAt, &t.StartedAt, &t.CompletedAt, &t.MetaJSON,
	)
	return t, err
}

// formatRunnerChatTurnDescription provides a short label for logging.
func formatRunnerChatTurnDescription(t RunnerChatTurn) string {
	return fmt.Sprintf("turn=%s session=%s seq=%d status=%s", t.ID, t.SessionID, t.Sequence, t.Status)
}

// Compile-time assertion to silence unused warnings in some build configs.
var _ = formatRunnerChatTurnDescription
