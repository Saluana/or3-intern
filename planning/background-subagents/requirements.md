# Overview

Add background subagent support to `or3-intern` so the foreground agent can delegate a longer task, return immediately, and report the result later through the existing channel delivery path. The implementation should stay inside the current Go CLI runtime, use SQLite for durability, and preserve the repo’s bounded/single-process operating model.

Scope assumptions:
- Background execution runs inside the same `or3-intern` process; no external worker service is introduced.
- The initial feature surface is a tool-triggered spawn flow, not a full job-management UI.
- Completion is reported by appending a summary to the parent session and attempting normal outbound delivery to the original channel target.
- Automatic retry, cancellation, and streaming progress updates are out of scope for the first pass.

# Requirements

1. **Foreground agent can queue a background task**
	 - The tool registry exposes a new background-task tool, expected to be named `spawn_subagent`, when the feature is enabled.
	 - The tool accepts a task description and may accept optional delivery overrides (`channel`, `to`).
	 - The tool returns immediately with a stable job identifier and a short acknowledgement instead of blocking on task completion.
	 - Acceptance criteria:
		 - Given a valid task, the tool call completes without waiting for provider/tool execution of the delegated task.
		 - The returned payload includes the created job ID.
		 - Given an empty task, the tool returns a validation error.

2. **Background execution is asynchronous, bounded, and isolated from the parent session**
	 - Queued jobs execute on a bounded worker pool separate from foreground event handling.
	 - Background execution must not monopolize the parent session lock while the job is running.
	 - Each background job runs under a derived child session key so its scratch history is isolated from the parent conversation.
	 - Acceptance criteria:
		 - A foreground session can continue processing new turns after spawning a job, without waiting for the spawned task to finish.
		 - Background jobs use a configured concurrency limit and per-job timeout.
		 - Messages/tool outputs created by the background run are stored under the child session key, not mixed into the parent session history.

3. **Job lifecycle is durable in SQLite and restart-safe**
	 - Background job state is stored in SQLite with enough data to recover visibility after process restart.
	 - The persisted lifecycle includes at least queued, running, and terminal states, timestamps, parent/child session keys, task text, and terminal result/error summary.
	 - Jobs left in `running` state after an unclean shutdown must be reconciled deterministically on next startup.
	 - Acceptance criteria:
		 - Enqueueing a task inserts a durable job row before the tool reports success.
		 - On normal completion, the row transitions to a terminal success/failure state with timestamps.
		 - On restart, stale `running` jobs are marked terminally interrupted/failed rather than silently disappearing or re-running automatically.

4. **Users receive completion feedback through existing session and channel paths**
	 - When a background job finishes, `or3-intern` appends a concise completion note to the parent session.
	 - The system attempts to deliver the final summary to the original channel/recipient using the existing channel manager.
	 - Delivery failure must not erase or roll back the persisted job outcome.
	 - Acceptance criteria:
		 - Successful jobs produce one durable parent-session summary message.
		 - Successful jobs attempt one outbound delivery using the recorded channel target.
		 - Failed delivery still leaves the job in a completed terminal state and the parent-session summary present.

5. **Background runs inherit existing safety boundaries and avoid recursive fanout**
	 - Background execution must reuse existing bounded tool-loop behavior, tool output size limits, artifact spill behavior, workspace restrictions, and command timeouts.
	 - The background tool registry must not allow unbounded recursive spawning.
	 - Queue size and concurrency must be capped by config.
	 - Acceptance criteria:
		 - Oversized background outputs are handled with the existing artifact spill mechanism and only a bounded preview is stored/delivered.
		 - The background registry does not expose `spawn_subagent`.
		 - When the queue is full, a new spawn attempt fails fast with a clear error.

6. **The feature is configurable and backward compatible**
	 - Background subagent support is controlled by config with safe defaults that preserve current behavior when unset.
	 - Config loading and env override behavior follow the existing patterns in `internal/config`.
	 - Existing sessions, history, memory, and channel integrations remain compatible.
	 - Acceptance criteria:
		 - Existing configs that do not mention subagents continue to load successfully.
		 - The feature is disabled by default unless explicitly enabled.
		 - No existing DB tables or message/session records require destructive migration.

7. **Regression coverage is added for the new execution mode**
	 - The implementation includes focused tests for DB lifecycle, tool behavior, runtime reuse, and completion delivery.
	 - Acceptance criteria:
		 - SQLite-backed tests cover enqueue, claim, terminal transitions, and restart reconciliation.
		 - Tool tests cover schema, validation, and immediate acknowledgement behavior.
		 - Agent/runtime tests cover non-blocking foreground behavior and completion reporting.

# Non-functional constraints

- **Deterministic behavior**
	- Keep the existing single-process SQLite model and deterministic state transitions.
	- Avoid automatic retries or duplicate execution after restart.
- **Low memory usage**
	- Reuse the current bounded history/prompt/tool-loop approach.
	- Limit concurrent background jobs and store large outputs in artifacts instead of RAM or oversized DB rows.
- **Bounded loops, output, and history**
	- Background runs must obey the same maximum tool loops, tool output bounds, and session-history controls as foreground runs.
- **SQLite safety and migration compatibility**
	- Schema additions must be additive and migration-safe for existing user data.
	- All job-state transitions should be explicit and test-covered.
- **Secure handling of files, network access, and secrets**
	- Reuse existing tool restrictions for file access, command execution, and web access.
	- Do not expose secret values in persisted previews, parent-session summaries, or delivery payloads.
