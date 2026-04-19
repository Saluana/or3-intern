package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"or3-intern/internal/scope"
)

// MemoryKind classifies the content type of a memory_notes row.
type MemoryKind = string

const (
	MemoryKindNote       MemoryKind = "note"
	MemoryKindSummary    MemoryKind = "summary"
	MemoryKindFact       MemoryKind = "fact"
	MemoryKindPreference MemoryKind = "preference"
	MemoryKindGoal       MemoryKind = "goal"
	MemoryKindProcedure  MemoryKind = "procedure"
	MemoryKindEpisode    MemoryKind = "episode"
)

// MemoryStatus tracks the lifecycle state of a memory_notes row.
type MemoryStatus = string

const (
	MemoryStatusActive     MemoryStatus = "active"
	MemoryStatusStale      MemoryStatus = "stale"
	MemoryStatusSuperseded MemoryStatus = "superseded"
)

// maxImportance caps the importance value stored on a memory note.
const maxImportance = 1.0

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

// TypedNoteInput holds the data for a single typed memory note write.
type TypedNoteInput struct {
	Text        string
	Embedding   []byte
	SourceMsgID sql.NullInt64
	Tags        string
	Kind        string
	Status      string
	Importance  float64
}

type ConsolidationWrite struct {
	SessionKey string
	ScopeKey   string
	// Primary summary note (optional).
	NoteText    string
	Embedding   []byte
	SourceMsgID sql.NullInt64
	NoteTags    string
	NoteKind    string // defaults to MemoryKindSummary when NoteText is set
	// Additional typed notes (facts, preferences, goals, procedures).
	ExtraNotes    []TypedNoteInput
	CanonicalKey  string
	CanonicalText string
	CursorMsgID   int64
}

const (
	SubagentStatusQueued      = "queued"
	SubagentStatusRunning     = "running"
	SubagentStatusSucceeded   = "succeeded"
	SubagentStatusFailed      = "failed"
	SubagentStatusInterrupted = "interrupted"
)

var ErrSubagentQueueFull = errors.New("subagent queue is full")

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

func (d *DB) EnsureSession(ctx context.Context, key string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func (d *DB) AppendMessage(ctx context.Context, sessionKey, role, content string, payload any) (int64, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	id, err := appendMessageTx(ctx, tx, sessionKey, role, content, payload)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
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

// InsertMemoryNote inserts a plain (untyped) memory note. It is a convenience
// wrapper around InsertMemoryNoteTyped that uses safe defaults for the new metadata fields.
func (d *DB) InsertMemoryNote(ctx context.Context, sessionKey, text string, embedding []byte, sourceMsgID sql.NullInt64, tags string) (int64, error) {
	return d.InsertMemoryNoteTyped(ctx, sessionKey, TypedNoteInput{
		Text:        text,
		Embedding:   embedding,
		SourceMsgID: sourceMsgID,
		Tags:        tags,
		Kind:        MemoryKindNote,
		Status:      MemoryStatusActive,
	})
}

// InsertMemoryNoteTyped inserts a memory note with explicit metadata fields.
func (d *DB) InsertMemoryNoteTyped(ctx context.Context, sessionKey string, input TypedNoteInput) (int64, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = MemoryKindNote
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = MemoryStatusActive
	}
	importance := input.Importance
	if importance < 0 {
		importance = 0
	} else if importance > maxImportance {
		importance = maxImportance
	}
	if err := d.validateMemoryEmbeddingDims(ctx, input.Embedding); err != nil {
		return 0, err
	}
	emb := input.Embedding
	if emb == nil {
		emb = make([]byte, 0)
	}
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		sessionKey, input.Text, emb, input.SourceMsgID, input.Tags, NowMS(),
		kind, status, importance)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := d.upsertMemoryVec(ctx, id, sessionKey, input.Text, emb); err != nil {
		return id, err
	}
	return id, nil
}

func (d *DB) validateMemoryEmbeddingDims(ctx context.Context, embedding []byte) error {
	if len(embedding) < 4 || len(embedding)%4 != 0 {
		return nil
	}
	want := len(embedding) / 4
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if dims == 0 {
		return d.EnsureMemoryVecIndexWithDim(ctx, want)
	}
	if dims != want {
		return fmt.Errorf("memory vector dims mismatch: have %d want %d", dims, want)
	}
	return nil
}

func (d *DB) upsertMemoryVec(ctx context.Context, noteID int64, sessionKey, text string, embedding []byte) error {
	if d == nil || d.VecSQL == nil {
		return nil
	}
	if len(embedding) < 4 || len(embedding)%4 != 0 {
		return nil
	}
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if dims == 0 {
		if err := d.EnsureMemoryVecIndexWithDim(ctx, len(embedding)/4); err != nil {
			return err
		}
		dims, err = d.MemoryVectorDims(ctx)
		if err != nil {
			return err
		}
	}
	if dims != len(embedding)/4 {
		return fmt.Errorf("memory vector dims mismatch: have %d want %d", dims, len(embedding)/4)
	}
	_, err = d.VecSQL.ExecContext(ctx,
		`INSERT OR REPLACE INTO memory_vec(note_id, session_key, embedding, text) VALUES(?,?,?,?)`,
		noteID, sessionKey, embedding, text)
	return err
}

type MemoryNoteRow struct {
	ID              int64
	Text            string
	Embedding       []byte
	SourceMessageID sql.NullInt64
	Tags            string
	CreatedAt       int64
	Kind            string
	Status          string
	Importance      float64
	UseCount        int
	LastUsedAt      int64
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
	ID         int64
	Text       string
	Rank       float64
	CreatedAt  int64
	Kind       string
	Status     string
	Importance float64
	UseCount   int
	LastUsedAt int64
}

type VecCandidateRow struct {
	ID         int64
	Text       string
	Distance   float64
	CreatedAt  int64
	Kind       string
	Status     string
	Importance float64
	UseCount   int
	LastUsedAt int64
}

func (d *DB) SearchMemoryVectors(ctx context.Context, sessionKey string, queryVec []byte, k int) ([]VecCandidateRow, error) {
	if d == nil || k <= 0 || len(queryVec) == 0 {
		return nil, nil
	}
	scopes := []string{scope.GlobalMemoryScope}
	if trimmed := strings.TrimSpace(sessionKey); trimmed != "" && trimmed != scope.GlobalMemoryScope {
		scopes = append(scopes, normalizeMemorySession(trimmed))
	}
	seen := make(map[int64]struct{}, k*len(scopes))
	out := make([]VecCandidateRow, 0, k*len(scopes))
	for _, memoryScope := range scopes {
		rows, err := d.SearchVecScope(ctx, memoryScope, queryVec, k)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			rows, err = d.SearchVecScopeFallback(ctx, memoryScope, queryVec, k)
			if err != nil {
				return nil, err
			}
		}
		for _, row := range rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			out = append(out, row)
		}
	}
	return out, nil
}

func (d *DB) SearchVecScope(ctx context.Context, sessionKey string, queryVec []byte, k int) ([]VecCandidateRow, error) {
	if d == nil || d.VecSQL == nil {
		return nil, nil
	}
	if k <= 0 || len(queryVec) == 0 {
		return nil, nil
	}
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return nil, err
	}
	if dims == 0 || len(queryVec) != dims*4 {
		return nil, nil
	}
	rows, err := d.VecSQL.QueryContext(ctx,
		`SELECT memory_vec.note_id, memory_vec.text, distance,
		        memory_notes.created_at, memory_notes.kind, memory_notes.status,
		        memory_notes.importance, memory_notes.use_count, memory_notes.last_used_at
		 FROM memory_vec
		 JOIN memory_notes ON memory_notes.id = memory_vec.note_id
		 WHERE memory_vec.embedding MATCH ? AND memory_vec.k = ? AND memory_vec.session_key = ?
		 ORDER BY distance`,
		queryVec, k, normalizeMemorySession(sessionKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVecCandidateRows(rows)
}

func (d *DB) SearchVecScopeFallback(ctx context.Context, sessionKey string, queryVec []byte, k int) ([]VecCandidateRow, error) {
	if d == nil || d.VecSQL == nil {
		return nil, nil
	}
	if k <= 0 || len(queryVec) == 0 || len(queryVec)%4 != 0 {
		return nil, nil
	}
	rows, err := d.VecSQL.QueryContext(ctx,
		`SELECT id, text, vec_distance_cosine(embedding, ?) AS distance,
		        created_at, kind, status, importance, use_count, last_used_at
		 FROM memory_notes
		 WHERE session_key=? AND typeof(embedding)='blob' AND length(embedding)=?
		 ORDER BY distance ASC
		 LIMIT ?`,
		queryVec, normalizeMemorySession(sessionKey), len(queryVec), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVecCandidateRows(rows)
}

func scanVecCandidateRows(rows *sql.Rows) ([]VecCandidateRow, error) {
	var out []VecCandidateRow
	for rows.Next() {
		var item VecCandidateRow
		var distance sql.NullFloat64
		if err := rows.Scan(
			&item.ID, &item.Text, &distance, &item.CreatedAt,
			&item.Kind, &item.Status, &item.Importance, &item.UseCount, &item.LastUsedAt,
		); err != nil {
			return nil, err
		}
		if !distance.Valid {
			continue
		}
		item.Distance = distance.Float64
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) SearchFTS(ctx context.Context, sessionKey, query string, k int) ([]FTSCandidate, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	// bm25 lower is better; invert
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT memory_fts.rowid, memory_fts.text, bm25(memory_fts) as rank,
		        memory_notes.created_at, memory_notes.kind, memory_notes.status,
		        memory_notes.importance, memory_notes.use_count, memory_notes.last_used_at
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
		var c FTSCandidate
		if err := rows.Scan(
			&c.ID, &c.Text, &c.Rank, &c.CreatedAt,
			&c.Kind, &c.Status, &c.Importance, &c.UseCount, &c.LastUsedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
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
		if errors.Is(scanErr, sql.ErrNoRows) {
			// Session row not found yet → nothing to consolidate.
			return 0, 0, nil
		}
		return 0, 0, scanErr
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
	if err := d.validateMemoryEmbeddingDims(ctx, w.Embedding); err != nil {
		return 0, err
	}
	for _, en := range w.ExtraNotes {
		if err := d.validateMemoryEmbeddingDims(ctx, en.Embedding); err != nil {
			return 0, err
		}
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	scopeKey := normalizeMemorySession(w.ScopeKey)
	var noteID int64

	// Write the primary summary note if one is provided.
	if strings.TrimSpace(w.NoteText) != "" {
		kind := strings.TrimSpace(w.NoteKind)
		if kind == "" {
			kind = MemoryKindSummary
		}
		emb := w.Embedding
		if emb == nil {
			emb = make([]byte, 0)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			scopeKey, w.NoteText, emb, w.SourceMsgID, w.NoteTags, NowMS(),
			kind, MemoryStatusActive, 0.0)
		if err != nil {
			return 0, err
		}
		noteID, _ = res.LastInsertId()
	}

	// Write any additional typed notes (facts, preferences, goals, procedures).
	type extraNoteID struct {
		id   int64
		text string
		emb  []byte
	}
	extraIDs := make([]extraNoteID, 0, len(w.ExtraNotes))
	for _, en := range w.ExtraNotes {
		if strings.TrimSpace(en.Text) == "" {
			continue
		}
		kind := strings.TrimSpace(en.Kind)
		if kind == "" {
			kind = MemoryKindNote
		}
		status := strings.TrimSpace(en.Status)
		if status == "" {
			status = MemoryStatusActive
		}
		importance := en.Importance
		if importance < 0 {
			importance = 0
		} else if importance > maxImportance {
			importance = maxImportance
		}
		emb := en.Embedding
		if emb == nil {
			emb = make([]byte, 0)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			scopeKey, en.Text, emb, en.SourceMsgID, en.Tags, NowMS(),
			kind, status, importance)
		if err != nil {
			return noteID, err
		}
		eid, _ := res.LastInsertId()
		extraIDs = append(extraIDs, extraNoteID{id: eid, text: en.Text, emb: emb})
	}

	if strings.TrimSpace(w.CanonicalKey) != "" {
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
	// Update vector index outside the transaction (best-effort).
	if noteID > 0 {
		emb := w.Embedding
		if emb == nil {
			emb = make([]byte, 0)
		}
		_ = d.upsertMemoryVec(ctx, noteID, scopeKey, w.NoteText, emb)
	}
	for _, en := range extraIDs {
		_ = d.upsertMemoryVec(ctx, en.id, scopeKey, en.text, en.emb)
	}
	return noteID, nil
}

func (d *DB) ResetSessionHistory(ctx context.Context, sessionKey string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			panic(rollbackErr)
		}
	}()

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

// TouchMemoryNotes increments use_count and sets last_used_at for the given
// note IDs that belong to the resolved scope (global or session). It is
// best-effort: callers should not treat errors as fatal.
func (d *DB) TouchMemoryNotes(ctx context.Context, scopeKey string, ids []int64, usedAt int64) error {
	if len(ids) == 0 {
		return nil
	}
	scopeKey = normalizeMemorySession(scopeKey)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, usedAt)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, scope.GlobalMemoryScope, scopeKey)
	// Placeholders are generated internally as literal "?" strings – not from
	// user input – so this concatenation is safe from SQL injection.
	q := `UPDATE memory_notes SET use_count=use_count+1, last_used_at=?
	      WHERE id IN (` + strings.Join(placeholders, ",") + `)
	      AND session_key IN (?,?)`
	_, err := d.SQL.ExecContext(ctx, q, args...)
	return err
}

// staleMemoryAgeMS is the minimum age (7 days in milliseconds) before a
// never-used summary or episode note may be marked stale by cleanup.
const staleMemoryAgeMS = 7 * 24 * 60 * 60 * 1000

// cleanupStaleBatchLimit caps how many rows may be marked stale in a single
// cleanup pass to keep the write bounded and safe for single-process SQLite.
const cleanupStaleBatchLimit = 50

// CleanupStaleMemoryNotes marks old, never-used summary and episode notes in
// the given scope as stale. It returns the number of rows updated. Only rows
// that are still active, have use_count=0, and were created more than
// staleMemoryAgeMS milliseconds before nowMS are eligible. At most
// cleanupStaleBatchLimit rows are touched per call.
func (d *DB) CleanupStaleMemoryNotes(ctx context.Context, scopeKey string, nowMS int64, limit int) (int, error) {
	scopeKey = normalizeMemorySession(scopeKey)
	if limit <= 0 {
		limit = cleanupStaleBatchLimit
	}
	cutoff := nowMS - staleMemoryAgeMS
	res, err := d.SQL.ExecContext(ctx,
		`UPDATE memory_notes
		 SET status=?
		 WHERE id IN (
		   SELECT id FROM memory_notes
		   WHERE session_key=? AND kind IN (?,?) AND status=? AND use_count=0
		         AND created_at < ?
		   ORDER BY created_at ASC
		   LIMIT ?
		 )`,
		MemoryStatusStale,
		scopeKey,
		MemoryKindSummary, MemoryKindEpisode,
		MemoryStatusActive,
		cutoff,
		limit,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

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

func ensureSessionTx(ctx context.Context, tx *sql.Tx, key string) error {
	now := NowMS()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func appendMessageTx(ctx context.Context, tx *sql.Tx, sessionKey, role, content string, payload any) (int64, error) {
	if err := ensureSessionTx(ctx, tx, sessionKey); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	return id, nil
}

func normalizeMemorySession(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return scope.GlobalMemoryScope
	}
	return sessionKey
}

// LinkSession links a physical session key to a logical scope key.
// If scopeKey is empty, the sessionKey itself is used.
func (d *DB) LinkSession(ctx context.Context, sessionKey, scopeKey string, meta map[string]any) error {
	if strings.TrimSpace(sessionKey) == "" {
		return fmt.Errorf("sessionKey required")
	}
	if strings.TrimSpace(scopeKey) == "" {
		scopeKey = sessionKey
	}
	mb, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if mb == nil {
		mb = []byte("{}")
	}
	_, err = d.SQL.ExecContext(ctx,
		`INSERT INTO session_links(session_key, scope_key, linked_at, metadata_json) VALUES(?,?,?,?)
         ON CONFLICT(session_key) DO UPDATE SET scope_key=excluded.scope_key, linked_at=excluded.linked_at, metadata_json=excluded.metadata_json`,
		sessionKey, scopeKey, NowMS(), string(mb))
	return err
}

// ResolveScopeKey returns the logical scope key for a physical session key.
// If no link exists, it returns the session key itself.
func (d *DB) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT scope_key FROM session_links WHERE session_key=?`, sessionKey)
	var scopeKey string
	if err := row.Scan(&scopeKey); err != nil {
		if err == sql.ErrNoRows {
			return sessionKey, nil
		}
		return sessionKey, err
	}
	return scopeKey, nil
}

// ListScopeSessions returns all physical session keys linked to the given scope key.
func (d *DB) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT session_key FROM session_links WHERE scope_key=? ORDER BY linked_at ASC`, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sk string
		if err := rows.Scan(&sk); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// GetLastMessagesScoped reads history for all sessions linked under the same scope
// as sessionKey, ordered by message id ascending, up to limit messages.
func (d *DB) GetLastMessagesScoped(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	scopeKey, err := d.ResolveScopeKey(ctx, sessionKey)
	if err != nil {
		return d.GetLastMessages(ctx, sessionKey, limit)
	}
	// get all sessions in scope (including the session itself)
	linked, err := d.ListScopeSessions(ctx, scopeKey)
	if err != nil || len(linked) == 0 {
		return d.GetLastMessages(ctx, sessionKey, limit)
	}
	// build IN clause; always include the physical session key itself
	allKeys := linked
	found := false
	for _, k := range linked {
		if k == sessionKey {
			found = true
			break
		}
	}
	if !found {
		allKeys = append(allKeys, sessionKey)
	}
	// build placeholders
	placeholders := make([]string, len(allKeys))
	args := make([]any, len(allKeys)+1)
	for i, k := range allKeys {
		placeholders[i] = "?"
		args[i] = k
	}
	args[len(allKeys)] = limit
	q := `SELECT id, session_key, role, content, payload_json, created_at
          FROM messages WHERE session_key IN (` + strings.Join(placeholders, ",") + `)
          ORDER BY id DESC LIMIT ?`
	rows, err := d.SQL.QueryContext(ctx, q, args...)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// align so first is user
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, nil
}
