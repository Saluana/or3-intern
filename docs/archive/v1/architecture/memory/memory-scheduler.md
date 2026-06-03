# Memory Scheduler

The memory scheduler ensures that only one consolidation run happens per session at a time, while not dropping consolidation requests that arrive during an active run.

Source: `internal/memory/scheduler.go`

## The Scheduler

`Scheduler` (`scheduler.go:9-16`) is a simple session-keyed debouncer:

```go
type Scheduler struct {
    timeout  time.Duration          // default: 30s
    run      func(context.Context, string)  // the consolidation function
    baseCtx  context.Context
    mu       sync.Mutex
    sessions map[string]*schedulerState
}

type schedulerState struct {
    running bool
    dirty   bool
}
```

## Construction

`NewScheduler()` (`scheduler.go:23-25`) and `NewSchedulerWithContext()` (`scheduler.go:27-39`) create a new scheduler:

```go
func NewScheduler(timeout time.Duration, run func(context.Context, string)) *Scheduler
```

If `timeout <= 0`, it defaults to 30 seconds. If `baseCtx` is nil, it defaults to `context.Background()`.

## Trigger

`Trigger()` (`scheduler.go:42-62`) is called after each agent turn with the session key:

1. **Locks** the mutex and looks up the session's state.
2. If no state exists, creates one.
3. If a run is already in progress, sets `dirty = true` and returns. The active run will re-run after it finishes.
4. If no run is in progress, sets `running = true`, clears `dirty`, and launches a goroutine with `runLoop`.

## Run Loop

`runLoop()` (`scheduler.go:64-88`) runs in a goroutine:

1. Creates a context with the configured timeout from `baseCtx`.
2. Calls the `run` function (which runs consolidation).
3. After the run completes, locks the mutex and checks `dirty`:
   - If `dirty` is true → clears it and loops back for another run (a new trigger arrived during the run).
   - If `dirty` is false → deletes the session state and returns (no more work).

## Design Properties

- **At most one run per session** — The mutex and `running` flag prevent concurrent runs.
- **No lost triggers** — A trigger during an active run sets `dirty`, causing an immediate re-run after the current one finishes.
- **Bounded runtime** — Each run has a context timeout (default 30s).
- **Self-cleaning** — Session state is removed from the map when no more work is pending.
