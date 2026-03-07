package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"or3-intern/internal/scope"

	_ "modernc.org/sqlite"
)

type DB struct {
	SQL *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(1) // deterministic, low-RAM
	d := &DB{SQL: s}
	if err := d.migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.SQL.Close() }

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
	return err
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
