package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrChatSessionNotFound is returned when a chat session metadata row is not found.
var ErrChatSessionNotFound = errors.New("chat session not found")

// ErrInvalidForkAnchor is returned when fork anchor message ID does not belong
// to the source session.
var ErrInvalidForkAnchor = errors.New("invalid fork anchor")

// ErrForkAnchorIncomplete is returned when fork anchor is on a streaming or
// pending assistant message and explicit fallback was not requested.
var ErrForkAnchorIncomplete = errors.New("fork anchor incomplete")

// ChatSessionMeta is a row in chat_session_meta.
type ChatSessionMeta struct {
	SessionKey             string
	HostID                 string
	Title                  string
	RunnerID               string
	RunnerLabel            string
	RunnerChatSessionID    string
	RunnerContinuationMode string
	RunnerModel            string
	RunnerMode             string
	RunnerIsolation        string
	RunnerCwd              string
	MessageCount           int64
	LastMessagePreview     string
	LastMessageAt          int64
	ParentSessionKey       string
	ForkAnchorMessageID    int64
	ForkedFromRunnerID     string
	ForkStrategy           string
	Archived               bool
	CreatedAt              int64
	UpdatedAt              int64
}

// ChatSessionListFilter parameters for listing.
type ChatSessionListFilter struct {
	HostID         string
	RunnerID       string
	IncludeArchive bool
	OnlyArchived   bool
	Search         string
	Limit          int
}

// ChatMessage is a row in messages relevant to chat history paging.
type ChatMessage struct {
	ID          int64
	SessionKey  string
	Role        string
	Content     string
	PayloadJSON string
	CreatedAt   int64
}

// ChatMessagePage is a page of messages with a continuation cursor.
type ChatMessagePage struct {
	Messages   []ChatMessage
	NextCursor int64 // 0 when no more
}

const chatSessionMetaSelectSQL = `SELECT session_key, host_id, title, runner_id, runner_label,
		runner_chat_session_id, runner_continuation_mode, runner_model, runner_mode,
		runner_isolation, runner_cwd, message_count, last_message_preview, last_message_at,
		parent_session_key, fork_anchor_message_id, forked_from_runner_id, fork_strategy,
		archived, created_at, updated_at
	FROM chat_session_meta`

func scanChatSessionMeta(row rowScanner) (ChatSessionMeta, error) {
	var m ChatSessionMeta
	var archivedInt int64
	err := row.Scan(
		&m.SessionKey, &m.HostID, &m.Title, &m.RunnerID, &m.RunnerLabel,
		&m.RunnerChatSessionID, &m.RunnerContinuationMode, &m.RunnerModel, &m.RunnerMode,
		&m.RunnerIsolation, &m.RunnerCwd, &m.MessageCount, &m.LastMessagePreview, &m.LastMessageAt,
		&m.ParentSessionKey, &m.ForkAnchorMessageID, &m.ForkedFromRunnerID, &m.ForkStrategy,
		&archivedInt, &m.CreatedAt, &m.UpdatedAt,
	)
	m.Archived = archivedInt != 0
	return m, err
}

// UpsertChatSessionMeta inserts or merges chat session metadata. Empty
// fields in the input do not overwrite existing non-empty fields.
func (d *DB) UpsertChatSessionMeta(ctx context.Context, m ChatSessionMeta) (ChatSessionMeta, error) {
	if strings.TrimSpace(m.SessionKey) == "" {
		return ChatSessionMeta{}, errors.New("session_key required")
	}
	now := NowMS()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return ChatSessionMeta{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureSessionTx(ctx, tx, m.SessionKey); err != nil {
		return ChatSessionMeta{}, err
	}
	archivedInt := 0
	if m.Archived {
		archivedInt = 1
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO chat_session_meta(
			session_key, host_id, title, runner_id, runner_label,
			runner_chat_session_id, runner_continuation_mode, runner_model, runner_mode,
			runner_isolation, runner_cwd, message_count, last_message_preview, last_message_at,
			parent_session_key, fork_anchor_message_id, forked_from_runner_id, fork_strategy,
			archived, created_at, updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(session_key) DO UPDATE SET
			host_id=COALESCE(NULLIF(excluded.host_id,''), chat_session_meta.host_id),
			title=COALESCE(NULLIF(excluded.title,''), chat_session_meta.title),
			runner_id=COALESCE(NULLIF(excluded.runner_id,''), chat_session_meta.runner_id),
			runner_label=COALESCE(NULLIF(excluded.runner_label,''), chat_session_meta.runner_label),
			runner_chat_session_id=COALESCE(NULLIF(excluded.runner_chat_session_id,''), chat_session_meta.runner_chat_session_id),
			runner_continuation_mode=COALESCE(NULLIF(excluded.runner_continuation_mode,''), chat_session_meta.runner_continuation_mode),
			runner_model=COALESCE(NULLIF(excluded.runner_model,''), chat_session_meta.runner_model),
			runner_mode=COALESCE(NULLIF(excluded.runner_mode,''), chat_session_meta.runner_mode),
			runner_isolation=COALESCE(NULLIF(excluded.runner_isolation,''), chat_session_meta.runner_isolation),
			runner_cwd=COALESCE(NULLIF(excluded.runner_cwd,''), chat_session_meta.runner_cwd),
			message_count=excluded.message_count,
			last_message_preview=excluded.last_message_preview,
			last_message_at=excluded.last_message_at,
			archived=excluded.archived,
			updated_at=excluded.updated_at`,
		m.SessionKey, m.HostID, m.Title, m.RunnerID, m.RunnerLabel,
		m.RunnerChatSessionID, m.RunnerContinuationMode, m.RunnerModel, m.RunnerMode,
		m.RunnerIsolation, m.RunnerCwd, m.MessageCount, m.LastMessagePreview, m.LastMessageAt,
		m.ParentSessionKey, m.ForkAnchorMessageID, m.ForkedFromRunnerID, m.ForkStrategy,
		archivedInt, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return ChatSessionMeta{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChatSessionMeta{}, err
	}
	return d.GetChatSessionMeta(ctx, m.SessionKey)
}

// GetChatSessionMeta returns the metadata for a session.
func (d *DB) GetChatSessionMeta(ctx context.Context, sessionKey string) (ChatSessionMeta, error) {
	row := d.SQL.QueryRowContext(ctx, chatSessionMetaSelectSQL+` WHERE session_key=?`, sessionKey)
	m, err := scanChatSessionMeta(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatSessionMeta{}, ErrChatSessionNotFound
	}
	return m, err
}

// ListChatSessions returns metadata rows ordered by updated_at DESC.
func (d *DB) ListChatSessions(ctx context.Context, filter ChatSessionListFilter) ([]ChatSessionMeta, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	q := chatSessionMetaSelectSQL + ` WHERE 1=1`
	args := []any{}
	if !filter.IncludeArchive && !filter.OnlyArchived {
		q += ` AND archived=0`
	} else if filter.OnlyArchived {
		q += ` AND archived=1`
	}
	if strings.TrimSpace(filter.HostID) != "" {
		q += ` AND host_id=?`
		args = append(args, filter.HostID)
	}
	if strings.TrimSpace(filter.RunnerID) != "" {
		q += ` AND runner_id=?`
		args = append(args, filter.RunnerID)
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		q += ` AND (title LIKE ? OR last_message_preview LIKE ?)`
		like := "%" + s + "%"
		args = append(args, like, like)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatSessionMeta
	for rows.Next() {
		m, err := scanChatSessionMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// BackfillExternalChannelChatSessionMeta creates chat metadata for persisted
// external-channel sessions that predate chat_session_meta syncing.
func (d *DB) BackfillExternalChannelChatSessionMeta(ctx context.Context) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx, `
		INSERT INTO chat_session_meta(
			session_key, title, runner_id, runner_label,
			message_count, last_message_preview, last_message_at, created_at, updated_at
		)
		SELECT grouped.session_key,
			CASE
				WHEN grouped.session_key LIKE 'telegram:%' THEN 'Telegram ' || substr(grouped.session_key, 10)
				WHEN grouped.session_key LIKE 'discord:%' THEN 'Discord ' || substr(grouped.session_key, 9)
				WHEN grouped.session_key LIKE 'slack:%' THEN 'Slack ' || substr(grouped.session_key, 7)
				WHEN grouped.session_key LIKE 'whatsapp:%' THEN 'WhatsApp ' || substr(grouped.session_key, 10)
				WHEN grouped.session_key LIKE 'email:%' THEN 'Email ' || substr(grouped.session_key, 7)
				ELSE grouped.session_key
			END,
			'or3-intern', 'OR3 Intern', grouped.message_count,
			COALESCE((SELECT m2.content FROM messages m2 WHERE m2.session_key=grouped.session_key ORDER BY m2.id DESC LIMIT 1), ''),
			COALESCE((SELECT m3.created_at FROM messages m3 WHERE m3.session_key=grouped.session_key ORDER BY m3.id DESC LIMIT 1), ?),
			COALESCE((SELECT m4.created_at FROM messages m4 WHERE m4.session_key=grouped.session_key ORDER BY m4.id ASC LIMIT 1), ?),
			COALESCE((SELECT m5.created_at FROM messages m5 WHERE m5.session_key=grouped.session_key ORDER BY m5.id DESC LIMIT 1), ?)
		FROM (
			SELECT m.session_key, COUNT(*) AS message_count
			FROM messages m
			LEFT JOIN chat_session_meta csm ON csm.session_key=m.session_key
			WHERE csm.session_key IS NULL
			  AND (m.session_key LIKE 'telegram:%'
				OR m.session_key LIKE 'discord:%'
				OR m.session_key LIKE 'slack:%'
				OR m.session_key LIKE 'whatsapp:%'
				OR m.session_key LIKE 'email:%')
			GROUP BY m.session_key
		) grouped`, now, now, now)
	return err
}

// SyncChatSessionMessageSummary ensures metadata exists for sessionKey and
// refreshes its message count and latest-message fields from persisted messages.
func (d *DB) SyncChatSessionMessageSummary(ctx context.Context, sessionKey, title, runnerID, runnerLabel string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return errors.New("session_key required")
	}
	now := NowMS()
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensureSessionTx(ctx, tx, sessionKey); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO chat_session_meta(
			session_key, title, runner_id, runner_label,
			message_count, last_message_preview, last_message_at, created_at, updated_at
		)
		SELECT ?, ?, ?, ?, COUNT(*),
			COALESCE((SELECT content FROM messages WHERE session_key=? ORDER BY id DESC LIMIT 1), ''),
			COALESCE((SELECT created_at FROM messages WHERE session_key=? ORDER BY id DESC LIMIT 1), ?),
			?, ?
		FROM messages WHERE session_key=?
		ON CONFLICT(session_key) DO UPDATE SET
			title=COALESCE(NULLIF(excluded.title,''), chat_session_meta.title),
			runner_id=COALESCE(NULLIF(excluded.runner_id,''), chat_session_meta.runner_id),
			runner_label=COALESCE(NULLIF(excluded.runner_label,''), chat_session_meta.runner_label),
			message_count=excluded.message_count,
			last_message_preview=excluded.last_message_preview,
			last_message_at=excluded.last_message_at,
			updated_at=excluded.updated_at`,
		sessionKey, strings.TrimSpace(title), strings.TrimSpace(runnerID), strings.TrimSpace(runnerLabel),
		sessionKey, sessionKey, now, now, now, sessionKey)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RenameChatSession sets the title.
func (d *DB) RenameChatSession(ctx context.Context, sessionKey, title string) error {
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE chat_session_meta SET title=?, updated_at=? WHERE session_key=?`,
		title, now, sessionKey)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// ArchiveChatSession sets archived state.
func (d *DB) ArchiveChatSession(ctx context.Context, sessionKey string, archived bool) error {
	flag := 0
	if archived {
		flag = 1
	}
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE chat_session_meta SET archived=?, updated_at=? WHERE session_key=?`,
		flag, now, sessionKey)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// ListChatMessages returns a chronological page of messages for a session.
// `afterID` >= 0; messages with id > afterID are returned. limit defaults to 100.
func (d *DB) ListChatMessages(ctx context.Context, sessionKey string, afterID int64, limit int) (ChatMessagePage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? AND id>? ORDER BY id ASC LIMIT ?`,
		sessionKey, afterID, limit+1)
	if err != nil {
		return ChatMessagePage{}, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return ChatMessagePage{}, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return ChatMessagePage{}, err
	}
	page := ChatMessagePage{Messages: out}
	if len(out) > limit {
		page.Messages = out[:limit]
		page.NextCursor = page.Messages[len(page.Messages)-1].ID
	}
	return page, nil
}

// ForkChatSessionRequest configures a fork operation.
type ForkChatSessionRequest struct {
	SourceSessionKey string
	NewSessionKey    string
	AnchorMessageID  int64
	TargetRunnerID   string
	Title            string
	// AllowIncompleteAnchor permits forking from an in-progress assistant
	// message by truncating to the last complete message <= anchor.
	AllowIncompleteAnchor bool
	// ForkStrategy is recorded on the new session metadata. Defaults to "replay".
	ForkStrategy string
}

// ForkChatSession copies messages [first..anchor] from the source session
// into a new session. Approval tokens, raw runner output, and child env are
// stripped from copied payloads.
func (d *DB) ForkChatSession(ctx context.Context, req ForkChatSessionRequest) (ChatSessionMeta, []ChatMessage, error) {
	if strings.TrimSpace(req.SourceSessionKey) == "" {
		return ChatSessionMeta{}, nil, errors.New("source_session_key required")
	}
	if strings.TrimSpace(req.NewSessionKey) == "" {
		return ChatSessionMeta{}, nil, errors.New("new_session_key required")
	}
	if req.AnchorMessageID <= 0 {
		return ChatSessionMeta{}, nil, ErrInvalidForkAnchor
	}
	strategy := strings.TrimSpace(req.ForkStrategy)
	if strategy == "" {
		strategy = "replay"
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return ChatSessionMeta{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Verify anchor belongs to source session.
	var anchorRole, anchorPayload string
	if err := tx.QueryRowContext(ctx,
		`SELECT role, payload_json FROM messages WHERE id=? AND session_key=?`,
		req.AnchorMessageID, req.SourceSessionKey).Scan(&anchorRole, &anchorPayload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChatSessionMeta{}, nil, ErrInvalidForkAnchor
		}
		return ChatSessionMeta{}, nil, err
	}
	// Detect incomplete assistant: payload_json.status == "streaming" or "pending".
	if !req.AllowIncompleteAnchor && anchorRole == "assistant" {
		if isIncompletePayload(anchorPayload) {
			return ChatSessionMeta{}, nil, ErrForkAnchorIncomplete
		}
	}
	// Resolve effective anchor when allowing incomplete: walk back to last complete.
	effectiveAnchor := req.AnchorMessageID
	if req.AllowIncompleteAnchor && anchorRole == "assistant" && isIncompletePayload(anchorPayload) {
		row := tx.QueryRowContext(ctx,
			`SELECT id FROM messages
			 WHERE session_key=? AND id<? AND NOT (role='assistant' AND (payload_json LIKE '%"status":"streaming"%' OR payload_json LIKE '%"status":"pending"%'))
			 ORDER BY id DESC LIMIT 1`,
			req.SourceSessionKey, req.AnchorMessageID)
		var prev sql.NullInt64
		if err := row.Scan(&prev); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ChatSessionMeta{}, nil, err
		}
		if prev.Valid {
			effectiveAnchor = prev.Int64
		} else {
			return ChatSessionMeta{}, nil, ErrForkAnchorIncomplete
		}
	}
	// Ensure new session row exists.
	if err := ensureSessionTx(ctx, tx, req.NewSessionKey); err != nil {
		return ChatSessionMeta{}, nil, err
	}
	// Copy messages.
	rows, err := tx.QueryContext(ctx,
		`SELECT role, content, payload_json, created_at FROM messages
		 WHERE session_key=? AND id<=? ORDER BY id ASC`,
		req.SourceSessionKey, effectiveAnchor)
	if err != nil {
		return ChatSessionMeta{}, nil, err
	}
	defer rows.Close()
	var copied []ChatMessage
	var lastPreview string
	var lastAt int64
	for rows.Next() {
		var role, content, payload string
		var createdAt int64
		if err := rows.Scan(&role, &content, &payload, &createdAt); err != nil {
			return ChatSessionMeta{}, nil, err
		}
		safe := sanitizeForkPayload(payload, req.SourceSessionKey)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
			req.NewSessionKey, role, content, safe, createdAt)
		if err != nil {
			return ChatSessionMeta{}, nil, err
		}
		id, _ := res.LastInsertId()
		copied = append(copied, ChatMessage{
			ID:          id,
			SessionKey:  req.NewSessionKey,
			Role:        role,
			Content:     content,
			PayloadJSON: safe,
			CreatedAt:   createdAt,
		})
		lastAt = createdAt
		lastPreview = previewSnippet(content)
	}
	if err := rows.Err(); err != nil {
		return ChatSessionMeta{}, nil, err
	}
	// Read source meta for runner inheritance.
	srcMeta, srcErr := getChatSessionMetaTx(ctx, tx, req.SourceSessionKey)
	now := NowMS()
	newMeta := ChatSessionMeta{
		SessionKey:          req.NewSessionKey,
		Title:               firstNonEmpty(req.Title, fmt.Sprintf("Fork of %s", req.SourceSessionKey)),
		ParentSessionKey:    req.SourceSessionKey,
		ForkAnchorMessageID: effectiveAnchor,
		ForkStrategy:        strategy,
		MessageCount:        int64(len(copied)),
		LastMessagePreview:  lastPreview,
		LastMessageAt:       lastAt,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if srcErr == nil {
		newMeta.HostID = srcMeta.HostID
		newMeta.RunnerLabel = srcMeta.RunnerLabel
		newMeta.RunnerModel = srcMeta.RunnerModel
		newMeta.RunnerMode = srcMeta.RunnerMode
		newMeta.RunnerIsolation = srcMeta.RunnerIsolation
		newMeta.RunnerCwd = srcMeta.RunnerCwd
		newMeta.ForkedFromRunnerID = srcMeta.RunnerID
		// Default the new session to the requested target runner, falling back
		// to the source runner when not provided.
		if strings.TrimSpace(req.TargetRunnerID) != "" {
			newMeta.RunnerID = req.TargetRunnerID
		} else {
			newMeta.RunnerID = srcMeta.RunnerID
		}
		newMeta.RunnerContinuationMode = "replay"
	} else if !errors.Is(srcErr, ErrChatSessionNotFound) {
		return ChatSessionMeta{}, nil, srcErr
	} else if strings.TrimSpace(req.TargetRunnerID) != "" {
		newMeta.RunnerID = req.TargetRunnerID
	}
	archivedInt := 0
	_, err = tx.ExecContext(ctx,
		`INSERT INTO chat_session_meta(
			session_key, host_id, title, runner_id, runner_label,
			runner_chat_session_id, runner_continuation_mode, runner_model, runner_mode,
			runner_isolation, runner_cwd, message_count, last_message_preview, last_message_at,
			parent_session_key, fork_anchor_message_id, forked_from_runner_id, fork_strategy,
			archived, created_at, updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		newMeta.SessionKey, newMeta.HostID, newMeta.Title, newMeta.RunnerID, newMeta.RunnerLabel,
		"", newMeta.RunnerContinuationMode, newMeta.RunnerModel, newMeta.RunnerMode,
		newMeta.RunnerIsolation, newMeta.RunnerCwd, newMeta.MessageCount, newMeta.LastMessagePreview, newMeta.LastMessageAt,
		newMeta.ParentSessionKey, newMeta.ForkAnchorMessageID, newMeta.ForkedFromRunnerID, newMeta.ForkStrategy,
		archivedInt, newMeta.CreatedAt, newMeta.UpdatedAt,
	)
	if err != nil {
		return ChatSessionMeta{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return ChatSessionMeta{}, nil, err
	}
	return newMeta, copied, nil
}

func getChatSessionMetaTx(ctx context.Context, tx *sql.Tx, sessionKey string) (ChatSessionMeta, error) {
	row := tx.QueryRowContext(ctx, chatSessionMetaSelectSQL+` WHERE session_key=?`, sessionKey)
	m, err := scanChatSessionMeta(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatSessionMeta{}, ErrChatSessionNotFound
	}
	return m, err
}

// isIncompletePayload returns true when payload JSON marks status streaming/pending.
func isIncompletePayload(payload string) bool {
	if payload == "" {
		return false
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return false
	}
	if v, ok := p["status"].(string); ok {
		switch v {
		case "streaming", "pending", "queued", "running":
			return true
		}
	}
	return false
}

// sanitizeForkPayload strips approval tokens, raw runner output, and child env
// from a copied payload. Source session key is recorded for traceability.
func sanitizeForkPayload(payload, sourceSessionKey string) string {
	if payload == "" {
		payload = "{}"
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		// Drop unparseable payloads, but keep the source pointer.
		out, _ := json.Marshal(map[string]any{"forked_from_session_key": sourceSessionKey})
		return string(out)
	}
	for _, k := range forkSensitiveKeys {
		delete(p, k)
	}
	p["forked_from_session_key"] = sourceSessionKey
	out, _ := json.Marshal(p)
	return string(out)
}

var forkSensitiveKeys = []string{
	"approval_token",
	"approval_tokens",
	"runner_output",
	"raw_output",
	"child_env",
	"env",
	"secrets",
	"bearer",
	"authorization",
}

func previewSnippet(content string) string {
	const max = 160
	c := strings.TrimSpace(content)
	if len(c) <= max {
		return c
	}
	return c[:max] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
