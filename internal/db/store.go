package db

import (
	"context"
	"database/sql"
	"encoding/json"
)

type Message struct {
	ID        int64
	SessionKey string
	Role      string
	Content   string
	PayloadJSON string
	CreatedAt int64
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
	if err := d.EnsureSession(ctx, sessionKey); err != nil { return 0, err }
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil { return 0, err }
	id, _ := res.LastInsertId()
	_, _ = d.SQL.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey)
	return id, nil
}

func (d *DB) GetLastMessages(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?`, sessionKey, limit)
	if err != nil { return nil, err }
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
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { out[i], out[j] = out[j], out[i] }
	// align so first is user (best-effort)
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, rows.Err()
}

func (d *DB) GetPinned(ctx context.Context) (map[string]string, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT key, content FROM memory_pinned ORDER BY key`)
	if err != nil { return nil, err }
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, c string
		if err := rows.Scan(&k, &c); err != nil { return nil, err }
		out[k] = c
	}
	return out, rows.Err()
}

func (d *DB) UpsertPinned(ctx context.Context, key, content string) error {
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(key, content, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		key, content, NowMS())
	return err
}

func (d *DB) InsertMemoryNote(ctx context.Context, text string, embedding []byte, sourceMsgID sql.NullInt64, tags string) (int64, error) {
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?)`,
		text, embedding, sourceMsgID, tags, NowMS())
	if err != nil { return 0, err }
	id, _ := res.LastInsertId()
	return id, nil
}

type MemoryNoteRow struct {
	ID int64
	Text string
	Embedding []byte
	SourceMessageID sql.NullInt64
	Tags string
	CreatedAt int64
}

func (d *DB) StreamMemoryNotes(ctx context.Context) (*sql.Rows, error) {
	return d.SQL.QueryContext(ctx, `SELECT id, text, embedding, source_message_id, tags, created_at FROM memory_notes`)
}

type FTSCandidate struct {
	ID int64
	Text string
	Rank float64
}

func (d *DB) SearchFTS(ctx context.Context, query string, k int) ([]FTSCandidate, error) {
	// bm25 lower is better; invert
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT rowid, text, bm25(memory_fts) as rank FROM memory_fts WHERE memory_fts MATCH ? ORDER BY rank LIMIT ?`, query, k)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []FTSCandidate
	for rows.Next() {
		var id int64
		var text string
		var rank float64
		if err := rows.Scan(&id, &text, &rank); err != nil { return nil, err }
		out = append(out, FTSCandidate{ID: id, Text: text, Rank: rank})
	}
	return out, rows.Err()
}
