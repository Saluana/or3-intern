# SQLite Schema

This is the complete database schema as defined in the migration function at `internal/db/db.go:77-583`. All tables are created with `CREATE TABLE IF NOT EXISTS`.

## Core Chat Tables

### sessions
```sql
CREATE TABLE sessions(
    key TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0
)
```

### messages
```sql
CREATE TABLE messages(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
)
CREATE INDEX messages_session_id ON messages(session_key, id)
```

### artifacts
```sql
CREATE TABLE artifacts(
    id TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    mime TEXT NOT NULL,
    path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
)
```

### chat_session_meta
```sql
CREATE TABLE chat_session_meta(
    session_key TEXT PRIMARY KEY,
    host_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    runner_id TEXT NOT NULL DEFAULT '',
    runner_label TEXT NOT NULL DEFAULT '',
    runner_chat_session_id TEXT NOT NULL DEFAULT '',
    runner_continuation_mode TEXT NOT NULL DEFAULT '',
    runner_model TEXT NOT NULL DEFAULT '',
    runner_mode TEXT NOT NULL DEFAULT '',
    runner_isolation TEXT NOT NULL DEFAULT '',
    runner_cwd TEXT NOT NULL DEFAULT '',
    message_count INTEGER NOT NULL DEFAULT 0,
    last_message_preview TEXT NOT NULL DEFAULT '',
    last_message_at INTEGER NOT NULL DEFAULT 0,
    parent_session_key TEXT NOT NULL DEFAULT '',
    fork_anchor_message_id INTEGER NOT NULL DEFAULT 0,
    forked_from_runner_id TEXT NOT NULL DEFAULT '',
    fork_strategy TEXT NOT NULL DEFAULT '',
    archived INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
)
```

## Memory Tables

### memory_pinned
```sql
CREATE TABLE memory_pinned(
    session_key TEXT NOT NULL DEFAULT '<global_scope>',
    key TEXT NOT NULL,
    content TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(session_key, key)
)
```

### memory_notes
```sql
CREATE TABLE memory_notes(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL DEFAULT '<global_scope>',
    text TEXT NOT NULL,
    embedding BLOB NOT NULL,
    embed_fingerprint TEXT NOT NULL DEFAULT '',
    source_message_id INTEGER,
    tags TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
)
```
Additional columns added by migration (`db.go:911-975`): `kind`, `status`, `importance`, `summary`, `source_artifact_id`, `confidence`, `updated_at`, `expires_at`, `supersedes_id`, `use_count`, `last_used_at`.

### memory_fts (FTS5 virtual table)
```sql
CREATE VIRTUAL TABLE memory_fts USING fts5(
    text, content='memory_notes', content_rowid='id'
)
```
With triggers for INSERT, DELETE, and UPDATE on memory_notes to keep FTS in sync.

### memory_docs
```sql
CREATE TABLE memory_docs(
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
)
```

### memory_docs_fts (FTS5 virtual table)
```sql
CREATE VIRTUAL TABLE memory_docs_fts USING fts5(
    title, summary, text, content='memory_docs', content_rowid='id'
)
```

### memory_vec_meta
```sql
CREATE TABLE memory_vec_meta(
    id INTEGER PRIMARY KEY CHECK(id=1),
    dims INTEGER NOT NULL DEFAULT 0,
    embed_fingerprint TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
)
```

### memory_vec (vec0 virtual table)
Created dynamically by `initMemoryVecIndex()` (`db.go:824-829`):
```sql
CREATE VIRTUAL TABLE memory_vec USING vec0(
    note_id integer primary key,
    session_key text partition key,
    embedding float[<dims>] distance_metric=cosine,
    +text text
)
```

## Job Tables

### subagent_jobs
```sql
CREATE TABLE subagent_jobs(
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
)
```

### service_jobs
```sql
CREATE TABLE service_jobs(
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    events_json TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)
```

### agent_cli_runs
```sql
CREATE TABLE agent_cli_runs(
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL UNIQUE,
    parent_session_key TEXT NOT NULL,
    runner_id TEXT NOT NULL,
    task TEXT NOT NULL,
    cwd TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    isolation TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    pid INTEGER NOT NULL DEFAULT 0,
    requested_at INTEGER NOT NULL,
    started_at INTEGER NOT NULL DEFAULT 0,
    completed_at INTEGER NOT NULL DEFAULT 0,
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    exit_code INTEGER,
    stdout_preview TEXT NOT NULL DEFAULT '',
    stderr_preview TEXT NOT NULL DEFAULT '',
    final_text_preview TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    meta_json TEXT NOT NULL DEFAULT '{}'
)
```

### agent_cli_events
```sql
CREATE TABLE agent_cli_events(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    ts TEXT NOT NULL,
    type TEXT NOT NULL,
    stream TEXT NOT NULL DEFAULT '',
    chunk TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '',
    UNIQUE(run_id, seq)
)
```

## Runner Chat Tables

### runner_chat_sessions
```sql
CREATE TABLE runner_chat_sessions(
    id TEXT PRIMARY KEY,
    app_session_key TEXT NOT NULL,
    runner_id TEXT NOT NULL,
    continuation_mode TEXT NOT NULL DEFAULT 'replay',
    native_session_ref TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT '',
    isolation TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    max_turns INTEGER NOT NULL DEFAULT 0,
    meta_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(app_session_key, runner_id)
)
```

### runner_chat_turns
```sql
CREATE TABLE runner_chat_turns(
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    status TEXT NOT NULL,
    user_message TEXT NOT NULL DEFAULT '',
    final_text TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    agent_cli_run_id TEXT NOT NULL DEFAULT '',
    agent_cli_job_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT '',
    isolation TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    continuation_mode TEXT NOT NULL DEFAULT 'replay',
    user_message_id INTEGER NOT NULL DEFAULT 0,
    assistant_message_id INTEGER NOT NULL DEFAULT 0,
    requested_at INTEGER NOT NULL,
    started_at INTEGER NOT NULL DEFAULT 0,
    completed_at INTEGER NOT NULL DEFAULT 0,
    meta_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY(session_id) REFERENCES runner_chat_sessions(id) ON DELETE CASCADE
)
```
Has a partial unique index to enforce one active turn per session.

### runner_chat_events
```sql
CREATE TABLE runner_chat_events(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    job_id TEXT NOT NULL DEFAULT '',
    seq INTEGER NOT NULL,
    ts INTEGER NOT NULL,
    type TEXT NOT NULL,
    stream TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '',
    UNIQUE(turn_id, seq),
    FOREIGN KEY(turn_id) REFERENCES runner_chat_turns(id) ON DELETE CASCADE
)
```

## Auth Tables

### auth_users
```sql
CREATE TABLE auth_users(
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    disabled_at INTEGER NOT NULL DEFAULT 0
)
```

### passkey_credentials
Stores WebAuthn passkeys with fields for credential ID, public key, sign count, transports, AAGUID, attestation metadata, backup state, flags, and revocation info.

### webauthn_ceremonies
Tracks WebAuthn registration and authentication ceremonies with type, challenge, session data, and expiry.

### auth_sessions
Stores active authentication sessions with token hash, idle/absolute expiry, revocation, step-up auth info, and hashed user agent/remote address.

### auth_recovery_codes
Stores hashed recovery codes for account recovery.

## Approval Tables

### paired_devices
Stores paired devices with token hash, role, status, and revocation.

### pairing_requests
Tracks device pairing requests with pairing code hash, status, and approval/denial timestamps.

### approval_requests
```sql
CREATE TABLE approval_requests(
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
)
```

### approval_allowlists
Stores domain-scoped allowlist entries with JSON scope and matcher definitions.

### approval_tokens
Issued tokens tied to approval requests, with expiry and revocation.

## Other Tables

### skill_run_plans
Stores skill execution plans with full parameter sets, hashes for deduplication, approval linkage, and terminal status tracking.

### task_state
```sql
CREATE TABLE task_state(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL,
    scope_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    goal TEXT NOT NULL DEFAULT '',
    plan_json TEXT NOT NULL DEFAULT '[]',
    constraints_json TEXT NOT NULL DEFAULT '[]',
    decisions_json TEXT NOT NULL DEFAULT '[]',
    open_questions_json TEXT NOT NULL DEFAULT '[]',
    message_refs_json TEXT NOT NULL DEFAULT '[]',
    memory_refs_json TEXT NOT NULL DEFAULT '[]',
    artifact_refs_json TEXT NOT NULL DEFAULT '[]',
    active_files_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)
```

### context_compactions
```sql
CREATE TABLE context_compactions(
    scope_key TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    cutoff_message_id INTEGER NOT NULL DEFAULT 0,
    message_refs_json TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)
```

### session_links
```sql
CREATE TABLE session_links(
    session_key TEXT PRIMARY KEY,
    scope_key TEXT NOT NULL,
    linked_at INTEGER NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}'
)
```

### secrets
```sql
CREATE TABLE secrets(
    name TEXT PRIMARY KEY,
    ciphertext BLOB NOT NULL,
    nonce BLOB NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    key_version TEXT NOT NULL DEFAULT 'v1',
    updated_at INTEGER NOT NULL
)
```

### audit_events
```sql
CREATE TABLE audit_events(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    session_key TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}',
    prev_hash BLOB NOT NULL,
    record_hash BLOB NOT NULL,
    created_at INTEGER NOT NULL
)
```

### mcp_tool_catalog
```sql
CREATE TABLE mcp_tool_catalog(
    server_name TEXT NOT NULL,
    remote_name TEXT NOT NULL,
    local_name TEXT NOT NULL,
    status TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    discovered_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(server_name, local_name)
)
```

## Timestamp Convention

All timestamp columns store Unix milliseconds (`int64`). The `NowMS()` function (`db.go:617`) returns `time.Now().UnixMilli()`.
