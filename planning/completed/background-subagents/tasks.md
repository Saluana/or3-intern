# 1. Config and startup wiring

- [x] (R5, R6) Add `SubagentsConfig` to `internal/config/config.go` with safe defaults, JSON fields, and env overrides; keep the feature disabled by default.
- [x] (R2, R3, R5) Update `cmd/or3-intern/main.go` to construct a `SubagentManager`, start it after DB/runtime initialization, stop it during shutdown, and skip all wiring when disabled.
- [x] (R1, R5) Register a new `spawn_subagent` tool in the default registry only when the manager is available.

# 2. SQLite schema and DB helpers

- [x] (R3) Extend `internal/db/db.go` migrations with a `subagent_jobs` table and indexes for status and parent session lookup.
- [x] (R3) Add startup reconciliation in `internal/db/db.go` or `internal/db/store.go` that marks stale `running` jobs as `interrupted` before new workers start.
- [x] (R3) Implement file-oriented helpers in `internal/db/store.go` for enqueue, claim, list queued, mark running, mark success, and mark failure.
- [x] (R3) Add SQLite tests in `internal/db/db_test.go` covering migration, lifecycle transitions, and restart reconciliation.

# 3. Background execution manager

- [x] (R2, R3, R4, R5) Add `internal/agent/subagents.go` with an in-process manager that owns a bounded worker pool, job claim loop, and graceful shutdown.
- [x] (R2, R5) Use per-job child session keys so background execution does not hold the parent session lock for the full task duration.
- [x] (R2, R5) Enforce bounded concurrency and per-job timeout using config-driven limits and `context.WithTimeout`.
- [x] (R4, R5) Persist terminal state before any outbound delivery attempt, including preview/error fields and optional artifact IDs.

# 4. Runtime refactor for reusable bounded runs

- [x] (R2, R5) Refactor `internal/agent/runtime.go` to extract the bounded LLM/tool loop into a reusable helper that can serve both foreground events and background jobs.
- [x] (R2, R4) Add a background-run path that accepts a seeded prompt snapshot from the parent session, executes in the child session, and returns final text plus optional artifact metadata.
- [x] (R5) Build a reduced background registry that excludes `spawn_subagent` and any other recursion-prone entry points.
- [x] (R4) Append a concise success/failure note into the parent session once a job finishes.

# 5. Tool surface

- [x] (R1, R5) Add `internal/tools/spawn.go` implementing `spawn_subagent` with parameters for `task` and optional `channel`/`to` overrides.
- [x] (R1) Make the tool return immediately with a stable job ID and a short queued acknowledgement.
- [x] (R5) Validate empty task text, disabled feature state, and queue-cap overflow with explicit user-readable errors.
- [x] (R1, R5) Add tests in `internal/tools` for schema, validation, defaulting, and enqueue behavior.

# 6. Completion and notification behavior

- [x] (R4) Route completion delivery through the existing channel manager path instead of introducing a new notifier abstraction.
- [x] (R4) Reuse existing artifact spill behavior for oversized background results and include only bounded previews in notifications and DB rows.
- [x] (R4) Ensure delivery failures do not roll back persisted terminal job state.
- [x] (R4) Add integration tests in `internal/agent` verifying success and failure notifications.

# 7. Regression coverage

- [x] (R2, R5, R6) Extend `internal/agent/runtime_test.go` with coverage that foreground turns still work after the runtime refactor.
- [x] (R2, R4, R5, R6) Add manager-focused tests, likely in `internal/agent/subagents_test.go`, for queue processing, timeout handling, and non-blocking foreground operation.
- [x] (R6) Run the focused Go test packages for `internal/db`, `internal/tools`, and `internal/agent` before broader validation.

# 8. Out of scope

- [ ] No multi-process worker service, distributed queue, or external scheduler.
- [ ] No job cancellation, pause/resume, or interactive streaming updates in the first pass.
- [ ] No new channel types or attachment-specific behavior beyond existing text delivery and artifact previews.
