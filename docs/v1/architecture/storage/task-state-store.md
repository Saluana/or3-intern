# Task State Store

The task state store persists structured task card data for agent sessions — goals, plans, decisions, open questions, and file/memory references.

Source: `internal/db/task_state.go`

## Data Model

### TaskStateRow (`task_state.go:8-25`)

```go
type TaskStateRow struct {
    ID                int64
    SessionKey        string
    ScopeKey          string
    Status            string   // default: "active"
    Goal              string
    PlanJSON          string   // JSON array of plan steps
    ConstraintsJSON   string   // JSON array of constraints
    DecisionsJSON     string   // JSON array of decisions
    OpenQuestionsJSON string   // JSON array of open questions
    MessageRefsJSON   string   // JSON array of message references
    MemoryRefsJSON    string   // JSON array of memory references
    ArtifactRefsJSON  string   // JSON array of artifact references
    ActiveFilesJSON   string   // JSON array of active file paths
    MetadataJSON      string   // JSON object for additional metadata
    CreatedAt         int64
    UpdatedAt         int64
}
```

All JSON fields default to `'[]'` (empty array) or `'{}'` (empty object).

## Operations

### UpsertActiveTaskState (`task_state.go:27-62`)

Two-step upsert:
1. First tries to **UPDATE** the existing active row where `session_key=? AND status='active'`.
2. If no rows were affected, **INSERTs** a new row using `WHERE NOT EXISTS (SELECT 1 FROM task_state WHERE session_key=? AND status='active')` to avoid races.

This ensures at most one active task state per session at any time.

### GetActiveTaskState (`task_state.go:64-85`)

Returns the active task state for a session (most recent by `updated_at DESC` where `status='active'`). Returns `(TaskStateRow, false, nil)` when no active state exists.

### CompleteActiveTaskState (`task_state.go:87-92`)

Transitions the active task state to `'completed'`:

```sql
UPDATE task_state SET status='completed', updated_at=? WHERE session_key=? AND status='active'
```

## Key Design Patterns

- **One active task state per session** — The upsert logic ensures only one row has `status='active'` for a given `session_key`.
- **JSON-as-text columns** — Plan, constraints, decisions, references, etc. are stored as JSON strings rather than in normalized tables. This keeps the schema simple and the data self-contained.
