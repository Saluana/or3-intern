// Package db opens and migrates the SQLite stores used by or3-intern.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/scope"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	_ "modernc.org/sqlite"
)

var sqliteVecAutoOnce sync.Once

// DB holds the primary SQL connection and the sqlite-vec connection.
type DB struct {
	SQL     *sql.DB
	VecSQL  *sql.DB
	auditMu sync.Mutex
}

// Open opens path, configures both SQLite drivers, and runs migrations.
func Open(path string) (*DB, error) {
	primaryDSN := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	s, err := sql.Open("sqlite", primaryDSN)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(4)
	s.SetMaxIdleConns(4)

	sqliteVecAutoOnce.Do(sqlite_vec.Auto)
	vecDSN := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal=WAL&_sync=NORMAL&_fk=1", path)
	vec, err := sql.Open("sqlite3", vecDSN)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	vec.SetMaxOpenConns(2)
	vec.SetMaxIdleConns(2)

	d := &DB{SQL: s, VecSQL: vec}
	if err := d.migrate(context.Background()); err != nil {
		_ = vec.Close()
		_ = s.Close()
		return nil, err
	}
	return d, nil
}

// Close closes both database handles and returns the first close error.
func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	var firstErr error
	if d.VecSQL != nil {
		if closeErr := d.VecSQL.Close(); closeErr != nil {
			firstErr = closeErr
		}
	}
	if d.SQL != nil {
		if closeErr := d.SQL.Close(); closeErr != nil && firstErr == nil {
			firstErr = closeErr
		}
	}
	return firstErr
}

func (d *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions(
			key TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS messages(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS messages_session_id ON messages(session_key, id);`,
		`CREATE TABLE IF NOT EXISTS artifacts(
			id TEXT PRIMARY KEY,
			session_key TEXT NOT NULL,
			mime TEXT NOT NULL,
			path TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS memory_pinned(
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(session_key, key)
		);`,
		`CREATE TABLE IF NOT EXISTS memory_notes(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			embed_fingerprint TEXT NOT NULL DEFAULT '',
			source_message_id INTEGER,
			tags TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);`,
		// FTS5
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(text, content='memory_notes', content_rowid='id');`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_ai AFTER INSERT ON memory_notes BEGIN
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_ad AFTER DELETE ON memory_notes BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, text) VALUES('delete', old.id, old.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_au AFTER UPDATE ON memory_notes BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, text) VALUES('delete', old.id, old.text);
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
		`CREATE TABLE IF NOT EXISTS subagent_jobs(
			id TEXT PRIMARY KEY,
			parent_session_key TEXT NOT NULL,
			child_session_key TEXT NOT NULL,
			channel TEXT NOT NULL,
			reply_to TEXT NOT NULL,
			task TEXT NOT NULL,
			status TEXT NOT NULL,
			result_preview TEXT NOT NULL DEFAULT '',
			artifact_id TEXT NOT NULL DEFAULT '',
			error_text TEXT NOT NULL DEFAULT '',
			requested_at INTEGER NOT NULL,
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			attempts INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS subagent_jobs_status_requested_at ON subagent_jobs(status, requested_at);`,
		`CREATE INDEX IF NOT EXISTS subagent_jobs_parent_session ON subagent_jobs(parent_session_key, requested_at);`,
		`CREATE TABLE IF NOT EXISTS session_links(
			session_key TEXT PRIMARY KEY,
			scope_key TEXT NOT NULL,
			linked_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS session_links_scope_key ON session_links(scope_key);`,
		`CREATE TABLE IF NOT EXISTS memory_docs(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope_key TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL,
			embedding BLOB,
			hash TEXT NOT NULL,
			mtime_ms INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			UNIQUE(scope_key, path)
		);`,
		`CREATE INDEX IF NOT EXISTS memory_docs_scope_path ON memory_docs(scope_key, path);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_docs_fts USING fts5(title, summary, text, content='memory_docs', content_rowid='id');`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_ai AFTER INSERT ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(rowid, title, summary, text) VALUES (new.id, new.title, new.summary, new.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_ad AFTER DELETE ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(memory_docs_fts, rowid, title, summary, text) VALUES('delete', old.id, old.title, old.summary, old.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_au AFTER UPDATE ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(memory_docs_fts, rowid, title, summary, text) VALUES('delete', old.id, old.title, old.summary, old.text);
			INSERT INTO memory_docs_fts(rowid, title, summary, text) VALUES (new.id, new.title, new.summary, new.text);
		END;`,
		`CREATE TABLE IF NOT EXISTS memory_vec_meta(
			id INTEGER PRIMARY KEY CHECK(id=1),
			dims INTEGER NOT NULL DEFAULT 0,
			embed_fingerprint TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0
		);`,
		`INSERT INTO memory_vec_meta(id, dims, updated_at)
			VALUES(1, 0, 0)
			ON CONFLICT(id) DO NOTHING;`,
		`CREATE TABLE IF NOT EXISTS secrets(
			name TEXT PRIMARY KEY,
			ciphertext BLOB NOT NULL,
			nonce BLOB NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			key_version TEXT NOT NULL DEFAULT 'v1',
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS audit_events(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			session_key TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			prev_hash BLOB NOT NULL,
			record_hash BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS audit_events_created_at ON audit_events(created_at);`,
		`CREATE TABLE IF NOT EXISTS paired_devices(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL UNIQUE,
			role TEXT NOT NULL,
			display_name TEXT NOT NULL,
			token_hash BLOB NOT NULL,
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			revoked_at INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS paired_devices_status_role ON paired_devices(status, role);`,
		`CREATE TABLE IF NOT EXISTS pairing_requests(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			role TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT '',
			pairing_code_hash BLOB NOT NULL,
			requested_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			status TEXT NOT NULL,
			approver_id TEXT NOT NULL DEFAULT '',
			approved_at INTEGER NOT NULL DEFAULT 0,
			denied_at INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS pairing_requests_status_expires_at ON pairing_requests(status, expires_at);`,
		`CREATE TABLE IF NOT EXISTS approval_requests(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			subject_hash TEXT NOT NULL,
			subject_json TEXT NOT NULL,
			requester_agent_id TEXT NOT NULL DEFAULT '',
			requester_session_id TEXT NOT NULL DEFAULT '',
			execution_host_id TEXT NOT NULL,
			status TEXT NOT NULL,
			policy_mode TEXT NOT NULL,
			requested_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			resolved_at INTEGER NOT NULL DEFAULT 0,
			resolver_actor_id TEXT NOT NULL DEFAULT '',
			resolution_kind TEXT NOT NULL DEFAULT '',
			resolution_note TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS approval_requests_status_type_requested_at ON approval_requests(status, type, requested_at DESC);`,
		`CREATE INDEX IF NOT EXISTS approval_requests_subject_hash_host ON approval_requests(subject_hash, execution_host_id);`,
		`CREATE TABLE IF NOT EXISTS approval_allowlists(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			scope_json TEXT NOT NULL,
			matcher_json TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL DEFAULT 0,
			disabled_at INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS approval_allowlists_domain_created_at ON approval_allowlists(domain, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS approval_tokens(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			approval_request_id INTEGER NOT NULL,
			subject_hash TEXT NOT NULL,
			issued_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			issuer TEXT NOT NULL,
			revoked_at INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(approval_request_id) REFERENCES approval_requests(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS approval_tokens_request_expires_at ON approval_tokens(approval_request_id, expires_at);`,
		`CREATE TABLE IF NOT EXISTS task_state(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL,
			goal TEXT NOT NULL DEFAULT '',
			plan TEXT NOT NULL DEFAULT '',
			constraints TEXT NOT NULL DEFAULT '',
			decisions TEXT NOT NULL DEFAULT '',
			open_questions TEXT NOT NULL DEFAULT '',
			message_refs TEXT NOT NULL DEFAULT '',
			memory_refs TEXT NOT NULL DEFAULT '',
			artifact_refs TEXT NOT NULL DEFAULT '',
			active_files TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS task_state_session_key ON task_state(session_key);`,
	}
	for _, s := range stmts {
		if _, err := d.SQL.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	if err := d.migrateMemoryPinned(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryNotesSessionColumn(ctx); err != nil {
		return err
	}
	if err := d.migrateLegacyGlobalMemoryScope(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryNotesMetaColumns(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryNotesExtendedColumns(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryVecMetaFingerprintColumn(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryDocsEmbedFingerprintColumn(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryVecIndexForExisting(ctx); err != nil {
		return err
	}
	return nil
}

// NowMS returns the current Unix time in milliseconds.
func NowMS() int64 { return time.Now().UnixMilli() }

func (d *DB) migrateMemoryPinned(ctx context.Context) error {
	hasSession, err := d.tableHasColumn(ctx, "memory_pinned", "session_key")
	if err != nil {
		return err
	}
	if hasSession {
		_, err = d.SQL.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS memory_pinned_session_key_key ON memory_pinned(session_key, key);`)
		return err
	}
	stmts := []string{
		`ALTER TABLE memory_pinned RENAME TO memory_pinned_legacy;`,
		`CREATE TABLE memory_pinned(
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(session_key, key)
		);`,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at)
		 SELECT '` + scope.GlobalMemoryScope + `', key, content, updated_at FROM memory_pinned_legacy;`,
		`DROP TABLE memory_pinned_legacy;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS memory_pinned_session_key_key ON memory_pinned(session_key, key);`,
	}
	for _, stmt := range stmts {
		if _, err := d.SQL.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureMemoryNotesSessionColumn(ctx context.Context) error {
	hasSession, err := d.tableHasColumn(ctx, "memory_notes", "session_key")
	if err != nil {
		return err
	}
	if !hasSession {
		if _, err := d.SQL.ExecContext(ctx, `ALTER TABLE memory_notes ADD COLUMN session_key TEXT NOT NULL DEFAULT '`+scope.GlobalMemoryScope+`';`); err != nil {
			return err
		}
	}
	_, err = d.SQL.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS memory_notes_session_id ON memory_notes(session_key, id);`)
	return err
}

func (d *DB) migrateLegacyGlobalMemoryScope(ctx context.Context) error {
	if scope.GlobalMemoryScope == scope.GlobalScopeAlias {
		return nil
	}
	if _, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at)
		 SELECT ?, key, content, updated_at FROM memory_pinned WHERE session_key=?
		 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		scope.GlobalMemoryScope, scope.GlobalScopeAlias); err != nil {
		return err
	}
	if _, err := d.SQL.ExecContext(ctx, `DELETE FROM memory_pinned WHERE session_key=?`, scope.GlobalScopeAlias); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE memory_notes SET session_key=? WHERE session_key=?`, scope.GlobalMemoryScope, scope.GlobalScopeAlias)
	if err != nil {
		return err
	}
	if dims, derr := d.MemoryVectorDims(ctx); derr == nil && dims > 0 && d.VecSQL != nil {
		var hasVec int
		if qerr := d.VecSQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name='memory_vec'`).Scan(&hasVec); qerr != nil {
			return qerr
		}
		if hasVec > 0 {
			_, err = d.VecSQL.ExecContext(ctx, `UPDATE memory_vec SET session_key=? WHERE session_key=?`, scope.GlobalMemoryScope, scope.GlobalScopeAlias)
		}
	}
	return err
}

func (d *DB) ensureMemoryVecIndexForExisting(ctx context.Context) error {
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if dims == 0 {
		dims, err = d.firstMemoryVectorDim(ctx)
		if err != nil {
			return err
		}
	}
	if dims <= 0 {
		return nil
	}
	fingerprint, err := d.MemoryVectorFingerprint(ctx)
	if err != nil {
		return err
	}
	return d.initMemoryVecIndex(ctx, dims, fingerprint, false)
}

// MemoryVectorDims reports the configured memory vector dimensionality.
func (d *DB) MemoryVectorDims(ctx context.Context) (int, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT dims FROM memory_vec_meta WHERE id=1`)
	var dims int
	if err := row.Scan(&dims); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return dims, nil
}

// MemoryVectorFingerprint reports the configured embedding fingerprint for
// persisted memory vectors.
func (d *DB) MemoryVectorFingerprint(ctx context.Context) (string, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT embed_fingerprint FROM memory_vec_meta WHERE id=1`)
	var fingerprint string
	if err := row.Scan(&fingerprint); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return fingerprint, nil
}

func (d *DB) firstMemoryVectorDim(ctx context.Context) (int, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(length(embedding), 0)
		 FROM memory_notes
		 WHERE typeof(embedding)='blob' AND length(embedding) >= 4 AND (length(embedding) % 4)=0
		 ORDER BY id ASC
		 LIMIT 1`)
	var bytes int
	if err := row.Scan(&bytes); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	if bytes <= 0 {
		return 0, nil
	}
	return bytes / 4, nil
}

// EnsureMemoryVecIndexWithDim initializes the vector index when dims is valid.
func (d *DB) EnsureMemoryVecIndexWithDim(ctx context.Context, dims int) error {
	return d.EnsureMemoryVecIndexWithProfile(ctx, dims, "")
}

// EnsureMemoryVecIndexWithProfile initializes the vector index when dims and
// the embedding fingerprint are compatible with any existing metadata.
func (d *DB) EnsureMemoryVecIndexWithProfile(ctx context.Context, dims int, fingerprint string) error {
	if dims <= 0 {
		return nil
	}
	existing, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if existing > 0 && existing != dims {
		return nil
	}
	return d.initMemoryVecIndex(ctx, dims, fingerprint, false)
}

// RebuildMemoryVecIndexWithDim recreates the vector index for a new embedding
// dimensionality and repopulates it from rows whose stored embeddings match.
func (d *DB) RebuildMemoryVecIndexWithDim(ctx context.Context, dims int) error {
	return d.RebuildMemoryVecIndexWithProfile(ctx, dims, "")
}

// RebuildMemoryVecIndexWithProfile recreates the vector index for a new
// dimensionality/embedding fingerprint pair and repopulates it only from rows
// whose stored embeddings match both the dimensionality and fingerprint.
func (d *DB) RebuildMemoryVecIndexWithProfile(ctx context.Context, dims int, fingerprint string) error {
	return d.initMemoryVecIndex(ctx, dims, fingerprint, true)
}

func (d *DB) initMemoryVecIndex(ctx context.Context, dims int, fingerprint string, force bool) error {
	if dims <= 0 {
		return nil
	}
	if d == nil || d.VecSQL == nil {
		return nil
	}
	tx, err := d.VecSQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	var existing int
	var existingFingerprint string
	if err := tx.QueryRowContext(ctx, `SELECT dims, embed_fingerprint FROM memory_vec_meta WHERE id=1`).Scan(&existing, &existingFingerprint); err != nil && err != sql.ErrNoRows {
		return err
	}
	if existing > 0 && existing != dims && !force {
		return fmt.Errorf("memory vector dims mismatch: have %d want %d", existing, dims)
	}
	if existing > 0 && strings.TrimSpace(fingerprint) != "" && strings.TrimSpace(existingFingerprint) != "" && existingFingerprint != fingerprint && !force {
		return fmt.Errorf("memory embedding fingerprint mismatch: have %q want %q", existingFingerprint, fingerprint)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS memory_vec`); err != nil {
		return err
	}
	createSQL := fmt.Sprintf(`CREATE VIRTUAL TABLE memory_vec USING vec0(
			note_id integer primary key,
			session_key text partition key,
			embedding float[%d] distance_metric=cosine,
			+text text
		)`, dims)
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return err
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_vec(note_id, session_key, embedding, text)
			 SELECT id, session_key, embedding, text
			 FROM memory_notes
			 WHERE typeof(embedding)='blob' AND length(embedding)=?`, dims*4); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_vec(note_id, session_key, embedding, text)
			 SELECT id, session_key, embedding, text
			 FROM memory_notes
			 WHERE typeof(embedding)='blob' AND length(embedding)=? AND embed_fingerprint=?`, dims*4, fingerprint); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_vec_meta(id, dims, embed_fingerprint, updated_at)
		 VALUES(1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET dims=excluded.dims, embed_fingerprint=excluded.embed_fingerprint, updated_at=excluded.updated_at`,
		dims, fingerprint, NowMS()); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ensureMemoryVecMetaFingerprintColumn(ctx context.Context) error {
	has, err := d.tableHasColumn(ctx, "memory_vec_meta", "embed_fingerprint")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = d.SQL.ExecContext(ctx, `ALTER TABLE memory_vec_meta ADD COLUMN embed_fingerprint TEXT NOT NULL DEFAULT ''`)
	return err
}

func (d *DB) ensureMemoryDocsEmbedFingerprintColumn(ctx context.Context) error {
	has, err := d.tableHasColumn(ctx, "memory_docs", "embed_fingerprint")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = d.SQL.ExecContext(ctx, `ALTER TABLE memory_docs ADD COLUMN embed_fingerprint TEXT NOT NULL DEFAULT ''`)
	return err
}

// ensureMemoryNotesMetaColumns adds lifecycle/ranking metadata columns to
// memory_notes if they do not yet exist (additive migration), then backfills
// rows that were written by the old consolidation path.
func (d *DB) ensureMemoryNotesMetaColumns(ctx context.Context) error {
	type colDef struct {
		name    string
		ddl     string
		missing bool
	}
	cols := []colDef{
		{name: "embed_fingerprint", ddl: `ALTER TABLE memory_notes ADD COLUMN embed_fingerprint TEXT NOT NULL DEFAULT ''`},
		{name: "kind", ddl: `ALTER TABLE memory_notes ADD COLUMN kind TEXT NOT NULL DEFAULT 'note'`},
		{name: "status", ddl: `ALTER TABLE memory_notes ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`},
		{name: "importance", ddl: `ALTER TABLE memory_notes ADD COLUMN importance REAL NOT NULL DEFAULT 0`},
		{name: "use_count", ddl: `ALTER TABLE memory_notes ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`},
		{name: "last_used_at", ddl: `ALTER TABLE memory_notes ADD COLUMN last_used_at INTEGER NOT NULL DEFAULT 0`},
	}

	for i := range cols {
		has, err := d.tableHasColumn(ctx, "memory_notes", cols[i].name)
		if err != nil {
			return err
		}
		cols[i].missing = !has
	}

	for _, col := range cols {
		if !col.missing {
			continue
		}
		if _, err := d.SQL.ExecContext(ctx, col.ddl); err != nil {
			return err
		}
	}

	// Supporting indexes for cleanup and retrieval queries.
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS memory_notes_scope_kind_status_created_at ON memory_notes(session_key, kind, status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS memory_notes_kind ON memory_notes(kind)`,
		`CREATE INDEX IF NOT EXISTS memory_notes_status ON memory_notes(status)`,
	}
	for _, idx := range indexes {
		if _, err := d.SQL.ExecContext(ctx, idx); err != nil {
			return err
		}
	}

	// Backfill: rows tagged with "consolidation" are rolling summaries.
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE memory_notes SET kind='summary'
		 WHERE kind='note' AND (
		 	tags='consolidation' OR
		 	tags LIKE 'consolidation,%' OR
		 	tags LIKE '%,consolidation' OR
		 	tags LIKE '%,consolidation,%'
		 )`)
	return err
}

func (d *DB) ensureMemoryNotesExtendedColumns(ctx context.Context) error {
	type col struct{ name, def string }
	cols := []col{
		{"summary", "TEXT NOT NULL DEFAULT ''"},
		{"source_artifact_id", "TEXT NOT NULL DEFAULT ''"},
		{"confidence", "REAL NOT NULL DEFAULT 1.0"},
		{"expires_at", "INTEGER NOT NULL DEFAULT 0"},
		{"supersedes_id", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, c := range cols {
		has, err := d.tableHasColumn(ctx, "memory_notes", c.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err := d.SQL.ExecContext(ctx, "ALTER TABLE memory_notes ADD COLUMN "+c.name+" "+c.def); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	rows, err := d.SQL.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}
