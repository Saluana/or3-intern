package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	"or3-intern/internal/scope"

	_ "modernc.org/sqlite"
)

var sqliteVecAutoOnce sync.Once

type DB struct {
	SQL     *sql.DB
	VecSQL  *sql.DB
	auditMu sync.Mutex
}

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

func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	var err error
	if d.VecSQL != nil {
		if closeErr := d.VecSQL.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if d.SQL != nil {
		if closeErr := d.SQL.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
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
	if err := d.ensureMemoryVecIndexForExisting(ctx); err != nil {
		return err
	}
	return nil
}

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
		_, err = d.VecSQL.ExecContext(ctx, `UPDATE memory_vec SET session_key=? WHERE session_key=?`, scope.GlobalMemoryScope, scope.GlobalScopeAlias)
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
	return d.initMemoryVecIndex(ctx, dims)
}

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

func (d *DB) EnsureMemoryVecIndexWithDim(ctx context.Context, dims int) error {
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
	return d.initMemoryVecIndex(ctx, dims)
}

func (d *DB) initMemoryVecIndex(ctx context.Context, dims int) error {
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
	if err := tx.QueryRowContext(ctx, `SELECT dims FROM memory_vec_meta WHERE id=1`).Scan(&existing); err != nil && err != sql.ErrNoRows {
		return err
	}
	if existing > 0 && existing != dims {
		return nil
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_vec(note_id, session_key, embedding, text)
		 SELECT id, session_key, embedding, text
		 FROM memory_notes
		 WHERE typeof(embedding)='blob' AND length(embedding)=?`, dims*4); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_vec_meta(id, dims, updated_at)
		 VALUES(1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET dims=excluded.dims, updated_at=excluded.updated_at`,
		dims, NowMS()); err != nil {
		return err
	}
	return tx.Commit()
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
