# Native Runner Improvements Requirements

## Overview

Improve OR3's OpenCode and Codex runner implementations by adopting the best transferable ideas from T3 Code: richer runner readiness snapshots, stronger native runtime lifecycle management, typed event normalization, approval continuation, and app-visible model/runtime capabilities.

Assumption: keep OR3's existing Go/SQLite/job-runner architecture. Do not port T3's TypeScript provider registry wholesale.

## Requirements

1. Native runner status is precise and actionable.
   - Acceptance: `/internal/v1/chat-runners` and `/internal/v1/agent-runners` expose runner runtime kind, configured mode, ownership, endpoint, native readiness, fallback reason, version, auth status, models, default model, and user-facing next action.
   - Acceptance: OR3 Intern remains available when external runner discovery fails.

2. Codex native sessions are durable enough for normal chat operations.
   - Acceptance: Codex app-server sessions can be started, reused, interrupted, and stopped through `NativeRunnerRuntime`.
   - Acceptance: `Abort` for Codex cancels an active turn instead of being a no-op.
   - Acceptance: app-server startup or transport failure in auto mode falls back to CLI before a turn mutates files.

3. OpenCode native runtime is lifecycle-safe.
   - Acceptance: managed `opencode serve` startup captures diagnostics, detects readiness from server output or health, cleans up process groups, and stops on manager shutdown.
   - Acceptance: external configured OpenCode servers are never killed by OR3.
   - Acceptance: idle managed servers are stopped after a bounded TTL when no sessions are active.

4. Runner approval requests can resume native turns.
   - Acceptance: Codex and OpenCode native approval/request IDs are persisted with `runner_chat_events`.
   - Acceptance: when the user approves, OR3 sends the decision back to the native runtime when the runtime is still alive.
   - Acceptance: if the runtime cannot be resumed, the existing approval-required retry flow remains available.

5. Runner events are normalized into the existing chat/event vocabulary.
   - Acceptance: Codex/OpenCode events map to canonical chat events for session state, content deltas, reasoning, tool lifecycle, diffs, plans, approvals, request resolution, runtime warnings/errors, model reroutes, and token usage when available.
   - Acceptance: unknown provider events are retained as bounded diagnostic payloads, not displayed as raw noisy assistant text.

6. Model and option metadata is data-driven.
   - Acceptance: Codex model discovery includes reasoning effort and fast-mode support when app-server reports it.
   - Acceptance: OpenCode discovery includes connected providers, models, agents, and variants when available.
   - Acceptance: `or3-app` can render these options without hardcoding runner-specific choices.

7. Native runtime observability is available without leaking secrets.
   - Acceptance: optional per-thread native/canonical event logs are size-bounded, rotated, redacted, and disabled or minimal by default.
   - Acceptance: logs never include auth tokens, raw environment variables, or unbounded command output.

## Non-functional Constraints

- Keep execution local-first, single-process, and compatible with current SQLite storage.
- Add schema changes only as backward-compatible migrations.
- Bound all subprocess startup waits, event payload sizes, logs, and model probes.
- Preserve existing CLI fallback behavior and existing runner-chat sessions.
- Do not weaken OR3 approval, workspace, sandbox, or secret-handling boundaries.
