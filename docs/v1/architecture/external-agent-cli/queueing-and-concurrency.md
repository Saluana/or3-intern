# Queueing and Concurrency

Runner jobs are queued in the database and processed by a pool of background workers. The queueing system is in `internal/agentcli/manager.go`.

## Manager Configuration

```go
type Manager struct {
    MaxConcurrent int
    MaxQueued     int
    TaskTimeout   time.Duration
    // ...
}
```

- `MaxConcurrent` — how many runner processes can run at once (default 1)
- `MaxQueued` — how many jobs can wait in the queue (default 16)
- `TaskTimeout` — maximum time for a single run (default 900 seconds)

## Worker Pool

When `Manager.Start()` is called (`internal/agentcli/manager.go:59-112`):

1. Defaults are applied for missing configuration values
2. A `ProcessManager` is created if one was not provided
3. A `RunnerRegistry` is created with default specs and adapters if not provided
4. `MaxConcurrent` goroutines are started, each running `workerLoop()`

### Worker Loop

Each worker (`internal/agentcli/manager.go:316-335`) loops forever:

1. Call `runOnce()` to claim and execute a job
2. If a job was run, immediately try another
3. If no job was available, wait for a signal on `notifyCh` or retry after 25ms (`agentCLIClaimRetryDelay`)

### Claiming

`runOnce()` (`internal/agentcli/manager.go:337-344`) calls `DB.ClaimNextAgentCLIRun(ctx)`. This atomically selects and locks the next queued job in the database using SQLite's transaction isolation.

## Enqueue Flow

`Manager.Enqueue()` (`internal/agentcli/manager.go:147-277`) processes a new run request:

1. Validate the request (parent session, task, runner ID)
2. Validate mode/isolation against policy
3. Check runner readiness from the detection cache
4. Enforce `MaxQueued` limit via `DB.EnqueueAgentCLIRunLimited`
5. Register the job with the `JobRegistry` for status updates
6. Signal a worker by sending on `notifyCh`

## Concurrency Guarantees

- At most `MaxConcurrent` runner processes run simultaneously
- At most `MaxQueued` jobs wait in the database
- Each run has a timeout (from `run.TimeoutSeconds` or `Manager.TaskTimeout`)
- Database `ClaimNextAgentCLIRun` uses SQLite write-locking for atomic claim
- Each worker handles one run at a time

## Startup Reconciliation

On startup, the manager loads any runs that were marked `running` from the previous process. These are reconciled by marking them as `aborted` with the message "aborted by service restart" (`internal/agentcli/manager.go:105-107`).

Similarly, queued runs from a previous process are picked up by workers immediately after startup.

## Job Registry Integration

Each run is tracked in the `JobRegistry` with a unique job ID. The registry provides:

- Status snapshots (`queued`, `running`, `succeeded`, `failed`, `aborted`, `timed_out`)
- Event streaming to subscribers (used by the chat manager for live updates)
- Cancellation support (used by `Manager.Abort()`)
- Event publishing at each lifecycle stage
