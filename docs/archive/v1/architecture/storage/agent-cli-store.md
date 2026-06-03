# Agent CLI Store

The agent CLI store manages agent CLI runner jobs and their streaming events. This is the job queue for executing agent CLI commands in isolated processes.

Source: `internal/db/agent_cli_store.go`

## Status Lifecycle

Agent CLI runs move through these statuses (`agent_cli_store.go:10-18`):

```
queued → running → succeeded
                 → failed
                 → aborted
                 → timed_out
```

The `starting` status exists in the constants but is not actively used in the current store methods.

## Data Model

### AgentCLIRun (`agent_cli_store.go:22-45`)

```go
type AgentCLIRun struct {
    ID               string        // unique run ID
    JobID            string        // unique job ID (separate from run ID)
    ParentSessionKey string        // session that requested the run
    RunnerID         string        // runner identifier
    Task             string        // task description
    Cwd              string        // working directory
    Model            string        // model to use
    Mode             string        // agent mode
    Isolation        string        // isolation setting
    Status           string        // current status
    PID              int           // OS process ID when running
    RequestedAt      int64         // when requested
    StartedAt        int64         // when started
    CompletedAt      int64         // when completed
    TimeoutSeconds   int           // timeout in seconds
    ExitCode         sql.NullInt64 // process exit code
    StdoutPreview    string        // stdout preview
    StderrPreview    string        // stderr preview
    FinalTextPreview string        // final text preview
    ErrorMessage     string        // error message
    Attempts         int           // number of start attempts
    MetaJSON         string        // additional metadata
}
```

### AgentCLIEvent (`agent_cli_store.go:47-57`)

Streaming events from a running agent CLI process:

```go
type AgentCLIEvent struct {
    ID          int64
    RunID       string
    JobID       string
    Seq         int64      // sequence number (unique per run_id)
    TS          string     // timestamp string
    Type        string     // event type
    Stream      string     // output stream (stdout, stderr, etc.)
    Chunk       string     // event content
    PayloadJSON string     // additional payload
}
```

### AgentCLIFinalizeInput (`agent_cli_store.go:59-67`)

```go
type AgentCLIFinalizeInput struct {
    Status           string
    ExitCode         int
    StdoutPreview    string
    StderrPreview    string
    FinalTextPreview string
    ErrorMessage     string
    CompletedAt      int64
}
```

## Operations

### Run Operations

| Function | Purpose |
|----------|---------|
| `EnqueueAgentCLIRun()` | Enqueue without queue limit |
| `EnqueueAgentCLIRunLimited()` | Enqueue with max queued limit. Uses same `WHERE ? <= 0 OR (SELECT COUNT(*) ...) < ?` pattern |
| `GetAgentCLIRun()` | Retrieves by ID or JobID |
| `ListQueuedAgentCLIRuns()` | All queued runs ordered by requested_at ASC |
| `ListRunningAgentCLIRuns()` | All running runs ordered by requested_at ASC |
| `ListAgentCLIRuns()` | Filtered listing by status and/or parent session |
| `ClaimNextAgentCLIRun()` | Atomically claims the oldest queued run |
| `AbortQueuedAgentCLIRun()` | Aborts a queued run (queued → aborted) |
| `MarkRunningAgentCLIRunsAborted()` | Bulk-aborts all running runs (used on restart) |
| `FinalizeAgentCLIRun()` | Sets terminal status with exit code and previews. Only transitions from `running` status |

### Event Operations

| Function | Purpose |
|----------|---------|
| `AppendAgentCLIEvent()` | Inserts a new event using `INSERT OR IGNORE` (idempotent by seq) |
| `ListAgentCLIEvents()` | Lists events for a job after a given sequence number |

## Queue Bounds

- Default list limit: 50 (`AgentCLIRunListDefaultLimit`)
- Maximum list limit: 100 (`AgentCLIRunListMaxLimit`)
- Queue capacity is checked during `EnqueueAgentCLIRunLimited()`

## Key Design Patterns

- **Dual ID system** — Each run has both `ID` and `JobID`. `GetAgentCLIRun()` and `AbortQueuedAgentCLIRun()` accept either.
- **Atomic claiming** — `ClaimNextAgentCLIRun()` uses a transaction with SELECT + conditional UPDATE.
- **Idempotent events** — `AppendAgentCLIEvent()` uses `INSERT OR IGNORE` with a `UNIQUE(run_id, seq)` constraint.
- **Status-gated finalization** — `FinalizeAgentCLIRun()` only transitions from `running` status.
