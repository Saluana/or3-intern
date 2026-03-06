# Overview

Add background subagent support as an in-process, SQLite-backed job system that can be invoked from the tool loop and report back later through the existing channel delivery path. The design stays within the current CLI/runtime architecture: the main process owns scheduling, persistence, and execution; jobs run with bounded concurrency and bounded tool execution; and results are persisted before any outbound delivery attempt.

This fits the current architecture because the repo already has:
- a single-process SQLite runtime with deterministic access patterns in `internal/db`
- a bus and worker model in `cmd/or3-intern/main.go`
- a reusable agent/tool loop in `internal/agent/runtime.go`
- existing channel delivery fanout through `internal/channels`
- bounded tool execution and artifact spilling already enforced in the runtime and tool packages

# Affected areas

- `cmd/or3-intern/main.go`
  - Construct and start a background-job manager alongside cron and channel startup.
  - Register the new spawn tool in the default registry.
  - Reconcile queued jobs on startup and stop the manager on shutdown.
- `internal/config/config.go`
  - Add a `SubagentsConfig` section with safe defaults and env overrides.
  - Keep the feature disabled by default to preserve current behavior.
- `internal/db/db.go`
  - Add SQLite schema for background jobs and indexes.
  - Add restart-reconciliation migration/update for jobs left in `running` state.
- `internal/db/store.go`
  - Add CRUD/claim/update helpers for job lifecycle.
  - Keep all transitions explicit and testable.
- `internal/agent/runtime.go`
  - Refactor the tool loop so foreground turns and background runs can share the same bounded execution path.
  - Ensure background runs do not take the parent session lock for their full duration.
- `internal/agent`
  - Add a small manager/runner implementation, for example `subagents.go`, responsible for enqueue, claim, execute, finalize, and notify.
  - Add a background-task prompt wrapper and result summarization rules.
- `internal/tools`
  - Add a new `spawn_subagent` tool implementation and tests.
  - Ensure subagent runs receive a reduced registry that excludes recursive spawning.
- `internal/artifacts`
  - Reuse the existing artifact spill path for oversized background results; no package changes are required unless tests reveal a missing helper.
- Tests
  - `internal/db/db_test.go`
  - `internal/agent/runtime_test.go`
  - `internal/tools/*_test.go`
  - Possibly a new `internal/agent/subagents_test.go`

# Control flow / architecture

```mermaid
flowchart TD
    A[Foreground agent tool loop] -->|spawn_subagent(task, channel?, to?)| B[Subagent manager enqueue]
    B --> C[SQLite job row status=queued]
    C --> D[Bounded manager worker claims oldest queued job]
    D --> E[Background runner builds parent-context snapshot]
    E --> F[Run bounded agent/tool loop in child session]
    F --> G[Persist terminal state result or error]
    G --> H[Append summary to parent session]
    G --> I[Deliver completion via existing channel manager]
```

End-to-end runtime behavior:

1. The foreground runtime exposes a `spawn_subagent` tool.
2. When the model calls it, the tool validates the request, records a job row in SQLite, and submits the job ID to an in-memory manager queue.
3. The tool returns immediately with a job ID and a short acknowledgement so the foreground assistant can continue without waiting for task completion.
4. A bounded worker pool inside the same process claims queued jobs from SQLite and marks one job `running` transactionally.
5. The worker constructs a child session key derived from the parent session and job ID, then executes a background agent run using:
   - the existing provider client
   - the existing builder output from the parent session as context seed
   - a reduced tool registry with `spawn_subagent` removed
   - the same artifact spill and max tool loop protections already used by the foreground runtime
6. On completion, the worker persists the final status (`succeeded`, `failed`, or `interrupted`), stores a preview and optional artifact ID, appends a concise completion message into the parent session, and attempts channel delivery back to the original target.
7. If delivery fails, the terminal job state remains persisted and the parent-session message still provides an audit trail for later retrieval.

The key refactor is to separate “run one bounded LLM/tool exchange sequence” from “handle one inbound bus event”. That keeps the foreground event handler small while allowing the background runner to reuse the same safety logic.

# Data and persistence

## SQLite

Add a new table for background jobs, for example:

```sql
CREATE TABLE IF NOT EXISTS subagent_jobs(
    id TEXT PRIMARY KEY,
    parent_session_key TEXT NOT NULL,
    child_session_key TEXT NOT NULL,
    channel TEXT NOT NULL,
    reply_to TEXT NOT NULL,
    task TEXT NOT NULL,
    status TEXT NOT NULL,
    result_preview TEXT NOT NULL DEFAULT '',
    artifact_id TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    requested_at INTEGER NOT NULL,
    started_at INTEGER NOT NULL DEFAULT 0,
    finished_at INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS subagent_jobs_status_requested_at
    ON subagent_jobs(status, requested_at);
CREATE INDEX IF NOT EXISTS subagent_jobs_parent_session
    ON subagent_jobs(parent_session_key, requested_at);
```

Recommended status set:
- `queued`
- `running`
- `succeeded`
- `failed`
- `interrupted`

Restart behavior:
- On startup, convert any stale `running` rows to `interrupted` with an explanatory error message instead of auto-resuming them.
- After reconciliation, enqueue only `queued` jobs.
- This avoids duplicate side effects after a crash or manual restart.

## Config

Add a nested config section such as:

```go
type SubagentsConfig struct {
    Enabled              bool `json:"enabled"`
    MaxConcurrent        int  `json:"maxConcurrent"`
    MaxQueued            int  `json:"maxQueued"`
    TaskTimeoutSeconds   int  `json:"taskTimeoutSeconds"`
}
```

Defaults:
- `Enabled=false`
- `MaxConcurrent=1`
- `MaxQueued=32`
- `TaskTimeoutSeconds=300`

Env overrides should follow the existing pattern, for example:
- `OR3_SUBAGENTS_ENABLED`
- `OR3_SUBAGENTS_MAX_CONCURRENT`
- `OR3_SUBAGENTS_MAX_QUEUED`
- `OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS`

No changes are needed to channel config formats, memory schema, or artifact schema.

## Session and memory scope

- Child runs should use a dedicated child session key, not the parent session key, to avoid lock contention and history pollution.
- Parent context should be provided as a snapshot when the job starts, not via live shared mutation during execution.
- Memory tools should continue to read/write according to the child session context unless the task explicitly uses global memory scope.
- The manager should append a short completion/failure note into the parent session so the main thread has durable awareness of background outcomes.

# Interfaces and types

Suggested Go-facing additions:

```go
// internal/db/store.go
type SubagentJob struct {
    ID               string
    ParentSessionKey string
    ChildSessionKey  string
    Channel          string
    ReplyTo          string
    Task             string
    Status           string
    ResultPreview    string
    ArtifactID       string
    ErrorText        string
    RequestedAt      int64
    StartedAt        int64
    FinishedAt       int64
    Attempts         int
    MetadataJSON     string
}

func (d *DB) EnqueueSubagentJob(ctx context.Context, job SubagentJob) error
func (d *DB) ClaimNextSubagentJob(ctx context.Context) (*SubagentJob, error)
func (d *DB) MarkSubagentSucceeded(ctx context.Context, id, preview, artifactID string) error
func (d *DB) MarkSubagentFailed(ctx context.Context, id, errText string) error
func (d *DB) MarkRunningSubagentsInterrupted(ctx context.Context, reason string) error
func (d *DB) ListQueuedSubagentJobs(ctx context.Context) ([]SubagentJob, error)
```

```go
// internal/agent/subagents.go
type SubagentManager struct {
    DB             *db.DB
    Runtime        *Runtime
    Deliver        Deliverer
    MaxConcurrent  int
    TaskTimeout    time.Duration
}

func (m *SubagentManager) Start(ctx context.Context) error
func (m *SubagentManager) Stop(ctx context.Context) error
func (m *SubagentManager) Enqueue(ctx context.Context, req EnqueueRequest) (SubagentJob, error)
```

```go
// internal/tools/spawn.go
type SpawnSubagent struct {
    Base
    Manager        interface {
        Enqueue(context.Context, EnqueueRequest) (SubagentJob, error)
    }
    DefaultChannel string
    DefaultTo      string
}
```

Execution refactor suggestion:
- Extract the LLM/tool loop in `internal/agent/runtime.go` into a helper like `runLoop(ctx, sessionKey string, seed []providers.ChatMessage) (finalText string, artifactID string, err error)`.
- Foreground `turn` can keep its current DB append + delivery behavior around that helper.
- Background jobs can call the same helper with a parent-derived seed and child session key.

# Failure modes and safeguards

- Invalid config
  - If subagents are disabled, the spawn tool should not register or should return a clear disabled error.
  - Non-positive concurrency/timeout values should be normalized during config load.
- Queue saturation
  - When queued jobs reach the configured cap, `spawn_subagent` should fail fast with a user-readable error and persist nothing new.
- Crash or restart during execution
  - Jobs left `running` are marked `interrupted` on startup; the system does not retry automatically.
- Delivery failure
  - Terminal job state is still persisted; the parent session receives a completion/error note; logs capture the delivery error.
- Tool misuse
  - Background runs use the same exec/file/web safeguards as foreground runs.
  - The background registry excludes `spawn_subagent` to prevent recursive fanout.
- Oversized outputs
  - Use existing artifact spilling and store only a bounded preview in the job row and parent-session note.
- Session isolation mistakes
  - Child work persists into `child_session_key`; only a summary is written back to the parent session.
- Provider/API failures
  - Mark the job `failed`, persist the provider error text, and notify the user concisely without retry loops.

# Testing strategy

Use Go’s `testing` package with focused coverage first, then runtime integration tests.

- Unit tests: `internal/db`
  - Migration creates `subagent_jobs` schema.
  - Claim/update lifecycle transitions behave correctly.
  - Startup reconciliation marks stale `running` rows as `interrupted`.
- Unit tests: `internal/tools`
  - `spawn_subagent` validates required fields, uses defaults for channel/recipient, and returns the queued job ID.
  - Recursive spawn is not available inside the reduced background registry.
- Integration-style tests: `internal/agent`
  - A queued background job executes without blocking a foreground session turn.
  - Completion writes a parent-session summary and triggers one outbound delivery.
  - Failed jobs persist failure state and do not deadlock the manager.
- Regression tests
  - Existing runtime tool-loop tests continue to pass with the refactor.
  - Channel-facing behavior remains unchanged for normal `send_message` and foreground assistant replies.
- SQLite-backed tests
  - Prefer temp-file SQLite tests, matching the repo’s current test pattern in `internal/db` and `internal/agent`.
