package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strings"

	"or3-intern/internal/scope"
)

type Message struct {
	ID          int64
	SessionKey  string
	Role        string
	Content     string
	PayloadJSON string
	CreatedAt   int64
}

type ConsolidationMessage struct {
	ID      int64
	Role    string
	Content string
}

type ConsolidationWrite struct {
	SessionKey    string
	ScopeKey      string
	NoteText      string
	Embedding     []byte
	SourceMsgID   sql.NullInt64
	NoteTags      string
	CanonicalKey  string
	CanonicalText string
	CursorMsgID   int64
}

func (d *DB) EnsureSession(ctx context.Context, key string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func (d *DB) AppendMessage(ctx context.Context, sessionKey, role, content string, payload any) (int64, error) {
	if err := d.EnsureSession(ctx, sessionKey); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := d.SQL.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	return id, nil
}

func (d *DB) GetLastMessages(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?`, sessionKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// align so first is user (best-effort)
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, rows.Err()
}

func (d *DB) GetPinned(ctx context.Context, sessionKey string) (map[string]string, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT key, content FROM memory_pinned
		 WHERE session_key IN (?, ?)
		 ORDER BY CASE WHEN session_key=? THEN 1 ELSE 0 END, key`,
		scope.GlobalMemoryScope, sessionKey, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, c string
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, rows.Err()
}

func (d *DB) GetPinnedValue(ctx context.Context, sessionKey, key string) (string, bool, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	row := d.SQL.QueryRowContext(ctx,
		`SELECT content FROM memory_pinned WHERE session_key=? AND key=?`,
		sessionKey, key)
	var out string
	if err := row.Scan(&out); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return out, true, nil
}

func (d *DB) UpsertPinned(ctx context.Context, sessionKey, key, content string) error {
	sessionKey = normalizeMemorySession(sessionKey)
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		sessionKey, key, content, NowMS())
	return err
}

func (d *DB) InsertMemoryNote(ctx context.Context, sessionKey, text string, embedding []byte, sourceMsgID sql.NullInt64, tags string) (int64, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?,?)`,
		sessionKey, text, embedding, sourceMsgID, tags, NowMS())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

type MemoryNoteRow struct {
	ID              int64
	Text            string
	Embedding       []byte
	SourceMessageID sql.NullInt64
	Tags            string
	CreatedAt       int64
}

func (d *DB) StreamMemoryNotes(ctx context.Context, sessionKey string) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at FROM memory_notes
		 WHERE session_key IN (?, ?)`,
		scope.GlobalMemoryScope, sessionKey)
}

func (d *DB) StreamMemoryNotesScopeLimit(ctx context.Context, sessionKey string, limit int) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if limit <= 0 {
		return d.SQL.QueryContext(ctx,
			`SELECT id, text, embedding, source_message_id, tags, created_at
			 FROM memory_notes WHERE session_key=?`,
			sessionKey)
	}
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at
		 FROM memory_notes WHERE session_key=? ORDER BY id DESC LIMIT ?`,
		sessionKey, limit)
}

func (d *DB) StreamMemoryNotesLimit(ctx context.Context, sessionKey string, limit int) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if limit <= 0 {
		return d.StreamMemoryNotes(ctx, sessionKey)
	}
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at
		 FROM memory_notes WHERE session_key IN (?, ?) ORDER BY id DESC LIMIT ?`,
		scope.GlobalMemoryScope, sessionKey, limit)
}

type FTSCandidate struct {
	ID   int64
	Text string
	Rank float64
}

func (d *DB) SearchFTS(ctx context.Context, sessionKey, query string, k int) ([]FTSCandidate, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	// bm25 lower is better; invert
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT memory_fts.rowid, memory_fts.text, bm25(memory_fts) as rank
		 FROM memory_fts
		 JOIN memory_notes ON memory_notes.id = memory_fts.rowid
		 WHERE memory_fts MATCH ? AND memory_notes.session_key IN (?, ?)
		 ORDER BY rank LIMIT ?`,
		query, scope.GlobalMemoryScope, sessionKey, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FTSCandidate
	for rows.Next() {
		var id int64
		var text string
		var rank float64
		if err := rows.Scan(&id, &text, &rank); err != nil {
			return nil, err
		}
		out = append(out, FTSCandidate{ID: id, Text: text, Rank: rank})
	}
	return out, rows.Err()
}

// GetConsolidationRange returns (lastConsolidatedID, oldestActiveID).
// oldestActiveID is the minimum ID among the last historyMax messages,
// or 0 if there are no messages in the session.
// Messages older than oldestActiveID (and newer than lastConsolidatedID)
// may be eligible for consolidation.
func (d *DB) GetConsolidationRange(ctx context.Context, sessionKey string, historyMax int) (lastConsolidatedID int64, oldestActiveID int64, err error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT last_consolidated_msg_id FROM sessions WHERE key=?`, sessionKey)
	if scanErr := row.Scan(&lastConsolidatedID); scanErr != nil {
		// Session row not found yet → nothing to consolidate.
		return 0, 0, nil
	}

	// Oldest ID in the active window (last historyMax messages).
	// If the total number of messages is < historyMax, MIN returns NULL → 0.
	activeRow := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(MIN(id), 0) FROM
		 (SELECT id FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?)`,
		sessionKey, historyMax)
	if scanErr := activeRow.Scan(&oldestActiveID); scanErr != nil {
		return lastConsolidatedID, 0, scanErr
	}
	return lastConsolidatedID, oldestActiveID, nil
}

// GetMessagesForConsolidation returns messages with afterID < id < beforeID
// in chronological order. Used to build the window to summarize.
func (d *DB) GetMessagesForConsolidation(ctx context.Context, sessionKey string, afterID, beforeID int64) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? AND id > ? AND id < ?
		 ORDER BY id ASC`,
		sessionKey, afterID, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) GetConsolidationMessages(ctx context.Context, sessionKey string, afterID, beforeID int64, limit int) ([]ConsolidationMessage, error) {
	if beforeID <= 0 {
		beforeID = math.MaxInt64
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, role, content
		 FROM messages WHERE session_key=? AND id > ? AND id < ?
		 ORDER BY id ASC LIMIT ?`,
		sessionKey, afterID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ConsolidationMessage, 0, limit)
	for rows.Next() {
		var m ConsolidationMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetLastConsolidatedID records the highest message ID that has been
// consolidated into memory notes for this session.
func (d *DB) SetLastConsolidatedID(ctx context.Context, sessionKey string, id int64) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=? WHERE key=?`, id, sessionKey)
	return err
}

func (d *DB) WriteConsolidation(ctx context.Context, w ConsolidationWrite) (int64, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var noteID int64
	if strings.TrimSpace(w.NoteText) != "" {
		scopeKey := normalizeMemorySession(w.ScopeKey)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?,?)`,
			scopeKey, w.NoteText, w.Embedding, w.SourceMsgID, w.NoteTags, NowMS())
		if err != nil {
			return 0, err
		}
		noteID, _ = res.LastInsertId()
	}
	if strings.TrimSpace(w.CanonicalKey) != "" {
		scopeKey := normalizeMemorySession(w.ScopeKey)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO memory_pinned(session_key, key, content, updated_at) VALUES(?,?,?,?)
			 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
			scopeKey, w.CanonicalKey, w.CanonicalText, NowMS())
		if err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=? WHERE key=?`, w.CursorMsgID, w.SessionKey)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return noteID, nil
}

func (d *DB) ResetSessionHistory(ctx context.Context, sessionKey string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_key=?`, sessionKey); err != nil {
		return err
	}
	now := NowMS()
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=0, updated_at=? WHERE key=?`,
		now, sessionKey); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeMemorySession(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return scope.GlobalMemoryScope
	}
	return sessionKey
}
