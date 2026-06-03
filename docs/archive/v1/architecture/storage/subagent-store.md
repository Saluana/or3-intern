# Subagent Store

The subagent store manages the lifecycle of subagent jobs — background tasks that run in child sessions.

Source: `internal/db/subagent_store.go`

## Status Lifecycle

Subagent jobs move through these statuses (`subagent_store.go:12-17`):

```
queued → running → succeeded
                 → failed
                 → interrupted
```

Additionally, queued jobs can be directly interrupted (aborted) without ever running.

## Data Model

### SubagentJob (`subagent_store.go:24-40`)

```go
type SubagentJob struct {
    ID               string   // unique job ID
    ParentSessionKey string   // session that requested the subagent
    ChildSessionKey  string   // session the subagent runs in
    Channel          string   // communication channel
    ReplyTo          string   // reply target
    Task             string   // task description
    Status           string   // queued|running|succeeded|failed|interrupted
    ResultPreview    string   // preview of result
    ArtifactID       string   // associated artifact
    ErrorText        string   // error message if failed/interrupted
    RequestedAt      int64    // when requested
    StartedAt        int64    // when started
    FinishedAt       int64    // when completed
    Attempts         int      // number of start attempts
    MetadataJSON     string   // additional metadata
}
```

### SubagentJobFilter (`subagent_store.go:48-52`)

```go
type SubagentJobFilter struct {
    Status           string  // specific status, "active", "terminal", or "" for all
    ParentSessionKey string  // filter by parent session
    Limit            int     // default: 50, max: 100
}
```

Status filter supports:
- Exact statuses: `queued`, `running`, `succeeded`, `failed`, `interrupted`
- `"active"` — `queued OR running`
- `"terminal"` — `succeeded OR failed OR interrupted`
- `""` — no filter (all statuses)

## Operations

| Function | Purpose |
|----------|---------|
| `EnqueueSubagentJob()` | Enqueue without queue limit |
| `EnqueueSubagentJobLimited()` | Enqueue with a max queued limit. Uses `WHERE maxQueued <= 0 OR (SELECT COUNT(*) ...) < maxQueued` pattern to reject when the queue is full |
| `GetSubagentJob()` | Retrieves by ID, returns `(job, found, error)` |
| `ListQueuedSubagentJobs()` | All queued jobs ordered by requested_at |
| `ListRunningSubagentJobs()` | All running jobs ordered by requested_at |
| `ListSubagentJobs()` | Filtered listing with status, parent, limit |
| `MarkSubagentRunning()` | Transitions queued → running, increments attempts |
| `ClaimNextSubagentJob()` | Atomically claims the oldest queued job (SELECT + UPDATE in transaction) |
| `AbortQueuedSubagentJob()` | Transitions a queued job to interrupted |
| `MarkSubagentSucceeded()` | Sets succeeded with result preview and artifact ID |
| `MarkSubagentFailed()` | Sets failed with error text |
| `MarkSubagentInterrupted()` | Sets interrupted with error text |
| `MarkRunningSubagentsInterrupted()` | Bulk-interrupts all running jobs (used on restart) |
| `FinalizeSubagentJob()` | Finalizes a running job and optionally appends a result message to the parent session |

## Queue Bounds

- Default list limit: 50 (`SubagentJobListDefaultLimit`)
- Maximum list limit: 100 (`SubagentJobListMaxLimit`)
- Queue capacity is checked during `EnqueueSubagentJobLimited()` using a subquery count

## Key Design Patterns

- **Atomic claiming** — `ClaimNextSubagentJob()` uses a transaction with SELECT + conditional UPDATE to prevent two workers from claiming the same job.
- **Status-gated updates** — Transitions check `WHERE status=?` to prevent double-processing.
- **Parent message on finalize** — `FinalizeSubagentJob()` appends the result as an assistant message in the parent session within the same transaction.
