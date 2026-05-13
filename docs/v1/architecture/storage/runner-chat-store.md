# Runner Chat Store

The runner chat store manages runner chat sessions, turns, and streaming events. This is the persistence layer for agent conversations routed through external runners (like Claude Code or other CLI agents).

Source: `internal/db/runner_chat_store.go`

## Data Model

### RunnerChatSession (`runner_chat_store.go:33-47`)

```go
type RunnerChatSession struct {
    ID               string  // unique session ID
    AppSessionKey    string  // linked app session
    RunnerID         string  // runner identifier
    ContinuationMode string  // default: "replay"
    NativeSessionRef string  // native session reference in the runner
    Model            string  // model being used
    Mode             string  // agent mode
    Isolation        string  // isolation setting
    Cwd              string  // working directory
    MaxTurns         int     // maximum turns
    MetaJSON         string  // additional metadata
    CreatedAt        int64
    UpdatedAt        int64
}
```

Unique constraint: `(app_session_key, runner_id)` — one runner chat session per app session per runner.

### RunnerChatTurn (`runner_chat_store.go:50-71`)

```go
type RunnerChatTurn struct {
    ID                 string  // unique turn ID
    SessionID          string  // FK to runner_chat_sessions
    Sequence           int64   // auto-incremented per session
    Status             string  // queued|running|succeeded|approval_required|failed|aborted|timed_out
    UserMessage        string  // the user's input
    FinalText          string  // final assistant response
    ErrorMessage       string  // error if failed
    AgentCLIRunID      string  // linked agent CLI run
    AgentCLIJobID      string  // linked agent CLI job
    Model              string  // model (can differ per turn)
    Mode               string  // mode (can differ per turn)
    Isolation          string  // isolation (can differ per turn)
    Cwd                string  // cwd (can differ per turn)
    ContinuationMode   string  // continuation mode
    UserMessageID      int64   // persisted message ID
    AssistantMessageID int64   // persisted message ID
    RequestedAt        int64
    StartedAt          int64
    CompletedAt        int64
    MetaJSON           string
}
```

Partial unique index enforces one active turn per session: only one row with `status IN ('queued','running')` per `session_id`.

### RunnerChatEvent (`runner_chat_store.go:74-85`)

```go
type RunnerChatEvent struct {
    ID          int64
    TurnID      string
    SessionID   string
    JobID       string
    Seq         int64   // unique per turn
    TS          int64   // timestamp in ms
    Type        string  // event type
    Stream      string  // output stream identifier
    Text        string  // event text
    PayloadJSON string  // additional payload
}
```

### RunnerChatTurnFinalize (`runner_chat_store.go:88-94`)

```go
type RunnerChatTurnFinalize struct {
    Status             string
    FinalText          string
    ErrorMessage       string
    AssistantMessageID int64
    CompletedAt        int64
}
```

## Status Constants (`runner_chat_store.go:12-20`)

```go
RunnerChatTurnStatusQueued           = "queued"
RunnerChatTurnStatusRunning          = "running"
RunnerChatTurnStatusSucceeded        = "succeeded"
RunnerChatTurnStatusApprovalRequired = "approval_required"
RunnerChatTurnStatusFailed           = "failed"
RunnerChatTurnStatusAborted          = "aborted"
RunnerChatTurnStatusTimedOut         = "timed_out"
```

## Error Types

- `ErrRunnerChatTurnActive` — Session already has a queued/running turn
- `ErrRunnerChatSessionNotFound` — Session not found
- `ErrRunnerChatTurnNotFound` — Turn not found

## Operations

### Session Operations

| Function | Purpose |
|----------|---------|
| `CreateOrGetRunnerChatSession()` | Idempotent create/get. Looks up existing by `(app_session_key, runner_id)` first. If found, returns it. If not, creates a new one |
| `GetRunnerChatSession()` | Retrieves by ID |
| `UpdateRunnerChatSessionNativeRef()` | Updates the native session reference |

### Turn Operations

| Function | Purpose |
|----------|---------|
| `CreateRunnerChatTurn()` | Creates a new turn. Auto-increments sequence from previous max. Detects active turn conflicts via the partial unique index |
| `GetRunnerChatTurn()` | Retrieves by ID |
| `GetActiveRunnerChatTurn()` | Gets the active (queued/running) turn for a session |
| `ListRunnerChatTurns()` | Lists turns in chronological order |
| `MarkRunnerChatTurnStarted()` | Transitions queued → running with agent CLI run/job IDs |
| `FinalizeRunnerChatTurn()` | Sets terminal status with final text and timestamps |
| `SetRunnerChatTurnUserMessageID()` | Sets the persisted user message ID |

### Event Operations

| Function | Purpose |
|----------|---------|
| `AppendRunnerChatEvent()` | Inserts a new event. TS defaults to now if 0 |
| `ListRunnerChatEvents()` | Lists events after a given sequence |
| `MaxRunnerChatEventSeq()` | Returns the highest seq for a turn |

### Startup Operations

| Function | Purpose |
|----------|---------|
| `ReconcileRunnerChatTurnsOnStartup()` | Transitions all in-flight (queued/running) turns to aborted with a "service restarted" message |

## Key Design Patterns

- **Idempotent session creation** — `CreateOrGetRunnerChatSession()` first queries, then inserts only if not found (as opposed to upserting).
- **Auto-incremented sequence** — Turn sequences are computed as `MAX(sequence) + 1` within a transaction to avoid gaps.
- **Active turn enforcement** — The partial unique index on `runner_chat_turns(session_id) WHERE status IN ('queued','running')` prevents more than one active turn per session at the database level.
- **Startup reconciliation** — `ReconcileRunnerChatTurnsOnStartup()` ensures no orphaned in-flight turns survive a service restart.
