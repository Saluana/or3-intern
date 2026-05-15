# Failure Modes

Runner runs can fail for many reasons. OR3 handles each failure mode explicitly. The failure handling is spread across `internal/agentcli/manager.go`, `internal/agentcli/process.go`, and `internal/agentcli/chat_manager.go`.

## Run Statuses

Each run ends in one of these statuses (from `internal/agentcli/manager.go`):

| Status | Meaning |
|--------|---------|
| `succeeded` | Exit code 0, no context errors |
| `failed` | Non-zero exit code |
| `timed_out` | Context deadline exceeded |
| `aborted` | Context cancelled, or interrupted by restart |

## Timeout Handling

Each run has a timeout. When the deadline is exceeded:

1. The context is cancelled
2. The process gets SIGTERM (Unix) or Kill (Windows) via the process group kill
3. After 2 seconds on Unix, SIGKILL follows
4. The status is set to `timed_out`
5. The error message is "timed out"

Default timeout is 900 seconds (`TaskTimeout` in `internal/agentcli/manager.go:80-82`).

## Abort Handling

Users can abort runs via `Manager.Abort()` (`internal/agentcli/manager.go:280-314`):

1. First tries `JobRegistry.Cancel(id)` for running jobs
2. If not running, tries `DB.AbortQueuedAgentCLIRun` for queued jobs
3. Jobs that are already finalized cannot be aborted

When a job is cancelled:
- The context is cancelled, which cancels the running process
- The run is finalized with status `aborted`
- The error message is "aborted"

## Process Failures

Failures during process execution (`internal/agentcli/process.go`):

### Binary Resolution Failure
- Emitted as an error event
- Exit code set to -1
- Status set to `failed`
- Error message contains the resolution error

### Pipeline Failures
- Failure to create stdout/stderr pipes emits an error event
- Exit code -1, status `failed`

### Start Failure
- `cmd.Start()` failure emits an error event
- Exit code -1, status `failed`

### Non-Zero Exit
- Normal completion with non-zero exit code results in status `failed`
- The stderr preview is used as the error message

### Context Errors
- Context errors during `cmd.Wait()` set exit code to -1
- If the context is cancelled → `aborted`
- If the context deadline is exceeded → `timed_out`

## Panic Recovery

Worker panics are recovered in `recoverRunPanic()` (`internal/agentcli/manager.go:489-494`):

```go
func (m *Manager) recoverRunPanic(run db.AgentCLIRun) {
    if recovered := recover(); recovered != nil {
        log.Printf("agent CLI worker recovered panic: run=%s err=%v", run.ID, recovered)
        m.finalizeRun(context.Background(), run, db.AgentCLIStatusFailed,
            "agent CLI worker recovered after an internal failure", ...)
    }
}
```

The run is finalized as `failed` even after a panic.

## Finalization Guarantees

`finalizeRun()` (`internal/agentcli/manager.go:496-542`) uses a 5-second timeout (`agentCLIFinalizeTimeout`) regardless of the original context. The context is detached via `context.WithoutCancel()` so finalization proceeds even if the original context is cancelled.

Database writes in finalization are best-effort. If they fail, the error is logged but not returned.

## Chat Turn Failures

For chat turns, the `ChatManager.finalizeFromSnapshot()` (`internal/agentcli/chat_manager.go:398-479`) handles failures:

- Maps job status to turn status (`succeeded` → `succeeded`, `failed` → `failed`, `aborted` → `aborted`, `timed_out` → `timed_out`)
- Extracts error messages from the job snapshot
- Appends an assistant message even on failure (content is "(error) <error message>" or "(no output)")
- Sets `approval_required` status when a permission was detected that needs approval

## Startup Reconciliation

On restart, the manager reconciles interrupted runs:
- Running runs → aborted with "aborted by service restart"
- Queued runs → picked up by workers normally

The chat manager also reconciles on startup (`ChatManager.ReconcileOnStartup`) by marking any running/queued turns as aborted.
