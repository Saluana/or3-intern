# Process Management

The `ProcessManager` in `internal/agentcli/process.go` handles launching and monitoring external CLI processes.

## ProcessManager

```go
type ProcessManager struct {
    ChunkMaxBytes   int
    PreviewMaxBytes int
}
```

- `ChunkMaxBytes` — maximum size of a single output chunk event (default 16384 bytes)
- `PreviewMaxBytes` — maximum retained preview for stdout/stderr (default 65536 bytes)

## Run Method

`ProcessManager.Run(ctx, spec, onEvent)` (`internal/agentcli/process.go:48-126`) does the following:

### 1. Resolve Binary

The binary is resolved via `ResolveExecutable(spec.Binary, spec.Env)` against the child process environment. If resolution fails, an error event is emitted and the function returns with exit code -1.

### 2. Create Command

An `exec.CommandContext` is created with the resolved binary and arguments. The command's `Dir` is set to `spec.Cwd` and `Env` to `spec.Env` if provided.

### 3. Set Process Group

On Unix, `setProcessGroup()` (`internal/agentcli/process_unix.go:11-13`) sets `Setpgid: true` on the syscall attributes. This creates a new process group so that cleanup can target the entire group.

On Windows, `setProcessGroup()` (`internal/agentcli/process_windows.go:9-12`) is a no-op with a note that Job Objects should be used in a future version.

### 4. Start and Stream

stdout and stderr pipes are created. Two goroutines read from these pipes concurrently:

- stdout is read through `readStream` with the spec's output mode (JSON, JSONL, or plain)
- stderr is always read in plain mode, regardless of output mode

### 5. Wait

`cmd.Wait()` blocks until the process exits. The wait group ensures both stream-reading goroutines finish before returning.

### 6. Collect Output

The `ProcessOutput` struct captures:

- `ExitCode` — process exit code (or -1 for errors)
- `StdoutPreview` — ring-buffered stdout (up to PreviewMaxBytes)
- `StderrPreview` — ring-buffered stderr (up to PreviewMaxBytes)
- `FinalTextPreview` — extracted final text from stdout (or assistant message content)
- `DurationMS` — wall-clock duration of the run

## Process Killing

When a run is cancelled (timeout or user abort), `KillProcessGroup` handles cleanup:

**Unix** (`internal/agentcli/process_unix.go:16-30`):
1. SIGTERM sent to the negative process group ID
2. After a 2-second grace period, SIGKILL sent to the group

**Windows** (`internal/agentcli/process_windows.go:16-21`):
1. Direct `Process.Kill()` on the child process

## Run Modes

Each adapter builds different command arguments based on the run mode:

### Review mode (`review`)
Read-only access. OpenCode runs with default permissions. Codex uses `--sandbox read-only`. Claude uses `--permission-mode plan`. Gemini uses `--approval-mode default`.

### Safe edit mode (`safe_edit`)
Can write within the workspace only. Codex uses `--sandbox workspace-write`. Claude uses `--permission-mode acceptEdits`. Gemini uses `--approval-mode auto_edit`.

### Sandbox auto mode (`sandbox_auto`)
Full bypass. OpenCode adds `--dangerously-skip-permissions`. Codex adds `--dangerously-bypass-approvals-and-sandbox`. Claude adds `--permission-mode bypassPermissions`. Gemini adds `--approval-mode yolo`.
