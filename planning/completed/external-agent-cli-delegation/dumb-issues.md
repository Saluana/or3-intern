# Dumb Issues — External Agent CLI Delegation

---

## `KillProcessGroup` sends SIGTERM and SIGKILL without a grace period

`internal/agentcli/process_unix.go:15-27`

```go
func KillProcessGroup(cmd *exec.Cmd) error {
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return err
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}
```

**Why this is bad:** The design doc explicitly specifies a 2-second grace period between SIGTERM and SIGKILL (`design.md:387-390`). This code sends SIGKILL immediately after SIGTERM, giving the child process zero time to flush buffers, close files, or clean up. The result is that every cancelled run will corrupt any in-flight file writes from the external CLI and lose the last chunk of stdout.

**Consequence:** Users who cancel a running agent will get truncated output in the preview. Codex or Claude could leave half-written files if they were mid-edit when the cancel arrived.

**Fix:**
```go
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Process already gone; nothing to kill.
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	// Wait up to 2 seconds for graceful exit
	time.Sleep(2 * time.Second)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}
```

---

## `readStream` goroutines are not waited on before `Run()` returns

`internal/agentcli/process.go:82-86`

```go
go readStream(stdoutPipe, doneCh, "stdout", ...)
go readStream(stderrPipe, doneCh, "stderr", ...)

waitErr := cmd.Wait()
close(doneCh)

var exitCode int
// ... collects collector.stdout.String() immediately
out := ProcessOutput{
    StdoutPreview: collector.stdout.String(), // RACE
    StderrPreview: collector.stderr.String(), // RACE
```

**Why this is bad:** There is no `sync.WaitGroup` to confirm the readStream goroutines have finished writing to the collector ring buffers. `cmd.Wait()` returning only means the OS process exited; it does not guarantee that the pipe has been drained to EOF. The `doneCh` is closed but `readStream` ignores it (it blocks on `scanner.Scan()` which returns false when the pipe EOFs, not when `doneCh` closes). The closing of the write-end of the pipe (which happens in `Wait()`) does trigger EOF on the read end eventually, but there is no guarantee the kernel buffer has been fully read before `collector.stdout.String()` is called.

**Consequence:** Last few lines of stdout/stderr from the external CLI are routinely lost. The `StdoutPreview` and `FinalTextPreview` will be truncated relative to the actual output.

**Fix:**
```go
var wg sync.WaitGroup
wg.Add(2)
go func() { defer wg.Done(); readStream(stdoutPipe, ...) }()
go func() { defer wg.Done(); readStream(stderrPipe, ...) }()
waitErr := cmd.Wait()
wg.Wait() // drain everything before collecting previews
```

---

## `bufio.Scanner` buffer and max token are the same size — lines at the limit are dropped

`internal/agentcli/stream.go:60-61`

```go
scanner.Buffer(make([]byte, chunkMaxBytes), chunkMaxBytes)
```

**Why this is bad:** `bufio.Scanner`'s `Buffer(buf, max)` sets a maximum token size. If a token (line) is `>= max`, the scanner returns `bufio.ErrTooLong` and the line is **silently dropped**. The buffer needs to be 1 byte larger than the max token. With a 16 KiB chunk limit, a line that is exactly 16 KiB (or longer) disappears from both the event stream and the preview.

**Consequence:** Any long line from Codex or Claude (e.g. a single JSON object spread across one long line in `stream-json` mode) that exceeds 16384 bytes is silently swallowed.

**Fix:**
```go
scanner.Buffer(make([]byte, chunkMaxBytes+1), chunkMaxBytes+1)
```

Or better, since chunk splitting happens per-line anyway, just allocate a larger buffer:
```go
bufSize := chunkMaxBytes * 2
scanner.Buffer(make([]byte, bufSize), bufSize)
```

---

## `atomicIncrement` is not atomic

`internal/agentcli/manager.go:625-630`

```go
func atomicIncrement(i *int64) int64 {
	*i++
	return *i
}
```

**Why this is bad:** The name says "atomicIncrement" but the implementation is a plain Go `*i++`. This is not atomic. The comment says "only called from one goroutine at a time" but that's a coincidence of the current call site, not a guarantee. The function is exported (visible package-wide) with a name that promises atomicity. Some future caller will use it from multiple goroutines and get a data race.

**Consequence:** Currently benign (only called from `executeRun` which is single-goroutine per run), but the name is a footgun waiting to go off.

**Fix:** Either rename it to `incrementSeq` and remove the misleading comment, or use `sync/atomic.AddInt64`:
```go
func atomicIncrement(i *int64) int64 {
	return atomic.AddInt64(i, 1)
}
```

The `sync/atomic` package is already imported in `stream.go` and used correctly there.

---

## Dead code: `stripPartialJSON`, `chunkAndStream`, `streamResult`, `maxBytes`

Several types and functions are defined but never called:

| Symbol | File | Line |
|--------|------|------|
| `stripPartialJSON` | `process.go` | 124 |
| `chunkAndStream` | `stream.go` | 134 |
| `streamResult` | `stream.go` | 149 |
| `outputCollector.maxBytes` | `stream.go` | 13 |

**Why this is bad:** Dead code rots. It breaks the reader's mental model (they spend time understanding functions that do nothing), bloats the package, and signals incomplete implementation. `stripPartialJSON` even imports `encoding/json` just for `json.Valid` — an import that exists solely to support dead code.

**Consequence:** Every person reading `process.go` has to ask "does anything call this?" and waste time figuring out it doesn't. CI linters (`staticcheck`, `unused`) will flag these. The `json` import in `process.go` becomes unused if you remove `stripPartialJSON`.

**Fix:** Delete all four. If `stripPartialJSON` or `chunkAndStream` are needed later, resurrect them from git history.

---

## `outputMode` parameter in `emitStructuredIfValid` is unused

`internal/agentcli/stream.go:97`

```go
func emitStructuredIfValid(onEvent func(AgentRunEvent), seq *int64, raw string, mode OutputMode) {
```

**Why this is bad:** The parameter `mode` is never read. It suggests the function is supposed to behave differently for `OutputJSON` vs `OutputJSONL` but the current implementation treats both identically (tries to unmarshal the line as JSON — which works for both JSON and JSONL since JSONL is just JSON objects separated by newlines). The function signature implies a capability it doesn't deliver.

**Fix:** Either implement the mode switch (e.g. for `OutputJSON` mode, accumulate partial lines across scanner boundaries) or remove the parameter. Removing the parameter is cleaner until the behaviour diverges.

---

## `Enqueue` calls `Detect` synchronously — blocks the HTTP handler on process execution

`internal/agentcli/manager.go:176-188`

```go
if m.Registry != nil {
    spec, ok := m.Registry.Spec(RunnerID(runnerID))
    if !ok {
        return db.AgentCLIRun{}, fmt.Errorf("unknown runner %q", runnerID)
    }
    if RunnerID(runnerID) != RunnerOR3 {
        info := Detect(ctx, spec, DetectOptions{...})
```

**Why this is bad:** `Detect` runs real commands (`opencode --version`, `codex login status`, etc.) with a 2-3 second timeout. This happens inside `Enqueue`, which is called from the HTTP handler (`handleAgentRunsStart`). A POST to `/internal/v1/agent-runs` will block the HTTP goroutine for up to 5 seconds (version probe + auth check timeouts) before responding. During that time the client has an open TCP connection waiting. This is especially bad when multiple runners are detected or the system is under load.

**Consequence:** During a burst of POST requests, the service's HTTP goroutines get tied up running exec calls. This is an unintentional DoS vector — any authenticated caller can consume service goroutines by just posting run requests rapidly.

**Fix:** Move the readiness check to before the request, not during enqueue. Cache detection results in the `RunnerRegistry` on a 30-second TTL. Enqueue should only check the cached result, not execute processes.

---

## `_max_turns` is stored as `float64` but cast back with `int()`

`internal/agentcli/manager.go:358-363`

```go
switch v := mt.(type) {
case float64:
    req.MaxTurns = int(v)
case json.Number:
    n, _ := v.Int64()
    req.MaxTurns = int(n)
}
```

**Why this is bad:** `json.Unmarshal` into `map[string]any` always produces `float64` for JSON numbers, not `json.Number` (that only happens with `json.Decoder` + `UseNumber()`). The `case json.Number` branch can never execute. Even if it did, the error from `v.Int64()` is silently ignored (`n, _`). It works by accident because the `float64` case handles the only path that actually runs, but the dead branch suggests the author didn't understand `json.Unmarshal`'s default behaviour.

**Fix:** Delete the `case json.Number` branch. If you need `json.Number` behaviour, use `json.NewDecoder` with `UseNumber()` instead of `json.Unmarshal`.

---

## `KillProcessGroup` in `process_unix.go` returns an error for missing process — should be silent

`internal/agentcli/process_unix.go:19-21`

```go
pgid, err := syscall.Getpgid(cmd.Process.Pid)
if err != nil {
    return err
}
```

**Why this is bad:** If the process has already exited (which is the likely case when cancelling — it may have finished between the cancel signal and the kill call), `Getpgid` returns ESRCH and the function returns an error. The caller (`executeRun`) doesn't even call `KillProcessGroup` — this function is exported but never used anywhere. The actual kill happens via `context.WithTimeout` + `cancel()` which triggers `exec.CommandContext`'s built-in `os.Process.Kill()`, not this function.

**Consequence:** `KillProcessGroup` is dead code (unused by the manager) AND it fails in the common case. If someone does wire it up later, it will return error on every terminated process.

**Fix:** If this is meant to be called by `Run()` when the context is cancelled, wire it up. Otherwise delete it. Either way, make the error handling tolerant of the process already being gone:
```go
pgid, err := syscall.Getpgid(cmd.Process.Pid)
if err != nil {
    return nil // process already gone
}
```

---

## `Manager.Enqueue` reads `m.Cfg` without holding `mu`

`internal/agentcli/manager.go:138,157,161,165`

`m.Cfg` is a `config.AgentCLIConfig` struct. It's read in `Enqueue` (called from HTTP handlers on arbitrary goroutines) and also read by `executeRun` (called from worker goroutines). The `mu` mutex protects `started`, `ctx`, `cancel`, and `notifyCh` — but not `Cfg`. Since `AgentCLIConfig` is a value type (not a pointer), and `Config` is never mutated after startup, this is currently safe. But nothing prevents a future maintainer from adding config hot-reload that writes to `Cfg`.

**Consequence:** No data race today, but a fragile invariant that's not documented or enforced. If config reload is ever added, this will silently corrupt.

**Fix:** Either document that `Cfg` is immutable after `Start()` returns, or protect reads with the mutex, or store config values needed at runtime in local fields (like `MaxConcurrent` already is).

---

## `writePersistedAgentCLIRunSnapshot` discards event list errors

`cmd/or3-intern/service.go:2755`

```go
events, _ := store.ListAgentCLIEvents(r.Context(), run.JobID, 0, 100)
response["events"] = s.agentCLIEventsToJobEvents(events)
```

**Why this is bad:** If `ListAgentCLIEvents` returns an error (database failure), the error is silently dropped and `events` will be `nil`. `agentCLIEventsToJobEvents(nil)` returns an empty slice, so the response gets `"events": []` with no indication that the real events couldn't be loaded. The client sees "0 events" instead of "something went wrong."

**Consequence:** A transient DB error during the `/jobs/{jobId}` fallback makes the response look like a successful run with no events, which is indistinguishable from a run that genuinely produced no events.

**Fix:** Log the error. Optionally include a warning in the response:
```go
events, err := store.ListAgentCLIEvents(r.Context(), run.JobID, 0, 100)
if err != nil {
    log.Printf("agent CLI event list failed: %v", err)
}
response["events"] = s.agentCLIEventsToJobEvents(events)
```
