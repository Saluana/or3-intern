# Storage Overview

OR3 Intern uses SQLite as its sole database, accessed through two driver connections: one for general SQL and one for vector operations via sqlite-vec.

## Architecture

The `DB` struct (`internal/db/db.go:23-27`) holds two connections:

```go
type DB struct {
    SQL     *sql.DB   // general SQL operations (modernc.org/sqlite driver)
    VecSQL  *sql.DB   // vector operations (mattn/go-sqlite3 driver with sqlite-vec)
    auditMu sync.Mutex  // audit log serialization
}
```

## Why Two Connections

sqlite-vec requires the CGo-based `github.com/mattn/go-sqlite3` driver (loaded via `github.com/asg017/sqlite-vec-go-bindings/cgo`). The rest of the application uses the pure-Go `modernc.org/sqlite` driver. Both connections point to the same database file.

## WAL Mode

The database opens in WAL (Write-Ahead Logging) mode with these pragmas (`db.go:31`):

```
_pragma=journal_mode(WAL)
_pragma=synchronous(NORMAL)
_pragma=foreign_keys(ON)
_pragma=busy_timeout(5000)
```

WAL mode allows concurrent reads while a write is in progress. The busy timeout of 5000ms means writers will wait up to 5 seconds before failing with a busy error.

## Connection Pooling

- **SQL connection**: Max 4 open, max 4 idle (`db.go:36-37`)
- **VecSQL connection**: Max 2 open, max 2 idle (`db.go:46-47`)

SQLite is single-writer, so connection limits keep contention manageable.

## Tables

The database includes the following groups of tables:

| Group | Tables | Description |
|-------|--------|-------------|
| Chat | `sessions`, `messages`, `artifacts`, `chat_session_meta` | Core chat storage |
| Memory | `memory_notes`, `memory_pinned`, `memory_docs`, `memory_fts`, `memory_docs_fts`, `memory_vec`, `memory_vec_meta` | Memory and vector storage |
| Jobs | `subagent_jobs`, `service_jobs`, `agent_cli_runs`, `agent_cli_events` | Background job storage |
| Auth | `auth_users`, `passkey_credentials`, `webauthn_ceremonies`, `auth_sessions`, `auth_recovery_codes` | Authentication |
| Approval | `paired_devices`, `pairing_requests`, `approval_requests`, `approval_allowlists`, `approval_tokens` | Approval system |
| Runner | `runner_chat_sessions`, `runner_chat_turns`, `runner_chat_events` | Runner chat system |
| Skills | `skill_run_plans` | Skill execution |
| Security | `secrets`, `audit_events` | Encrypted secrets and audit log |
| Other | `session_links`, `context_compactions`, `task_state`, `mcp_tool_catalog` | Supporting tables |

## Key Files

| File | Purpose |
|------|---------|
| `internal/db/db.go` | DB open/close, migrations, vector index management |
| `internal/db/store.go` | Core CRUD: sessions, messages, memory, FTS, vectors |
| `internal/db/security.go` | Encrypted secrets, HMAC-chained audit log |
| `internal/db/chat_session_store.go` | Chat session metadata, forking |
| `internal/db/approval_store.go` | Pairing, approval requests, allowlists, tokens |
| `internal/db/auth_store.go` | Users, passkeys, WebAuthn, auth sessions |
| `internal/db/service_jobs.go` | Service job summaries |
| `internal/db/subagent_store.go` | Subagent job queue |
| `internal/db/agent_cli_store.go` | Agent CLI runner jobs and events |
| `internal/db/runner_chat_store.go` | Runner chat sessions, turns, events |
| `internal/db/skill_run_plan_store.go` | Skill execution plans |
| `internal/db/task_state.go` | Task card state |
| `internal/db/mcp_catalog.go` | MCP tool catalog |
