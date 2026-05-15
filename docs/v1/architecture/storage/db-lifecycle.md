# Database Lifecycle

How the database is opened, migrated, used, and closed.

Source: `internal/db/db.go`

## Opening

`Open()` (`db.go:30-56`) is the entry point:

```go
func Open(path string) (*DB, error)
```

It performs these steps:

1. **Opens the primary connection** using the `modernc.org/sqlite` driver (registered as `"sqlite"`) with WAL mode, NORMAL synchronous, foreign keys on, and a 5-second busy timeout.

2. **Initializes sqlite-vec** — Calls `sqlite_vec.Auto()` once (via `sync.Once`) to register the vec0 virtual table extension with the CGo SQLite driver.

3. **Opens the vector connection** using `github.com/mattn/go-sqlite3` (registered as `"sqlite3"`), which supports the sqlite-vec C extension.

4. **Runs migrations** via `d.migrate(context.Background())`.

5. **Returns** the `*DB` with both connections ready.

## Migration

`migrate()` (`db.go:77-613`) runs all schema creation and migration steps in order:

### Phase 1: Core schema (`db.go:78-583`)
Creates all tables with `CREATE TABLE IF NOT EXISTS`, so existing tables are not modified. This includes:
- Core chat: sessions, messages, artifacts
- Memory: memory_pinned, memory_notes, memory_fts (FTS5), memory_docs, memory_docs_fts
- Vector: memory_vec_meta
- Jobs: subagent_jobs, service_jobs, agent_cli_runs, agent_cli_events
- Auth: auth_users, passkey_credentials, webauthn_ceremonies, auth_sessions, auth_recovery_codes
- Approval: paired_devices, pairing_requests, approval_requests, approval_allowlists, approval_tokens
- Runner: chat_session_meta, runner_chat_sessions, runner_chat_turns, runner_chat_events
- Skills: skill_run_plans
- Other: session_links, task_state, context_compactions, secrets, audit_events, mcp_tool_catalog

### Phase 2: Additive migrations
These run after the core schema to handle schema changes over time:

| Step | Function | Purpose |
|------|----------|---------|
| 1 | `migrateMemoryPinned()` (`db.go:619`) | Migrates legacy `memory_pinned` (without session_key) to have a session_key column |
| 2 | `ensureMemoryNotesSessionColumn()` (`db.go:650`) | Adds `session_key` column to `memory_notes` if missing |
| 3 | `migrateLegacyGlobalMemoryScope()` (`db.go:664`) | Migrates data from old global scope alias to new global scope key |
| 4 | `ensureMemoryNotesMetaColumns()` (`db.go:911`) | Adds lifecycle metadata columns (kind, status, importance, confidence, etc.) |
| 5 | `ensureMemoryVecMetaFingerprintColumn()` (`db.go:861`) | Adds `embed_fingerprint` to `memory_vec_meta` |
| 6 | `ensureMemoryDocsEmbedFingerprintColumn()` (`db.go:873`) | Adds `embed_fingerprint` to `memory_docs` |
| 7 | `ensureSkillRunPlanColumns()` (`db.go:885`) | Adds `stdin_nonce` and `stdin_sha256` to skill_run_plans |
| 8 | `ensureMemoryVecIndexForExisting()` (`db.go:694`) | Creates the vector index if embeddings exist but no index is configured |

### Column Detection

`tableHasColumn()` (`db.go:977-996`) uses `PRAGMA table_info(tableName)` to check if a column exists before attempting to add it. This makes migrations safe to run multiple times.

## Using the Database

All store methods accept a `context.Context` and use `d.SQL.ExecContext()` or `d.SQL.QueryContext()`. Vector operations use `d.VecSQL`.

Transactions are used for multi-step operations:
- `AppendMessage()` — ensures session + inserts message + updates timestamp in one tx
- `WriteConsolidation()` — writes notes + pinned memory + updates cursor in one tx
- `ForkChatSession()` — copies messages + creates metadata in one tx
- `FinalizeSubagentJob()` — updates job status + appends result message in one tx

## Closing

`Close()` (`db.go:59-75`) closes both connections:

```go
func (d *DB) Close() error
```

It closes `VecSQL` first, then `SQL`. Returns the first error encountered, but always attempts to close both.

## Notes

- The database file is a single SQLite file at the configured path. WAL mode creates a `-wal` and `-shm` file alongside it.
- All timestamps are Unix milliseconds (int64), obtained via `NowMS()`.
- Foreign keys are enforced (`foreign_keys=ON`).
- The `auditMu` mutex serializes audit event writes to maintain hash chain integrity.
