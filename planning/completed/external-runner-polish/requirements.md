# External Runner Polish Requirements

## Overview

Improve external agent runners so Codex, Claude Code, Gemini CLI, and OpenCode feel like native `or3-intern` chat transports in `or3-app` without replacing the existing Go/SQLite/SSE runner-chat spine. The plan assumes T3Code's best idea is provider-local mess converted into a shared runtime event model, while its persistent WebSocket/SDK/app-server architecture is too much complexity for this pass.

Scope:
- Fill verified native-session continuity gaps for Codex, Claude Code, and Gemini.
- Add a small canonical runtime event vocabulary on top of existing `runner_chat_events`.
- Improve UI-facing activity metadata for text, reasoning, tools, approvals, plans, diffs, errors, and session state.
- Keep replay mode, bounded output, restricted cwd/env, and the current external runner process model as fallbacks.

## Requirements

1. Native resume must become the default when safely available.
   - Acceptance criteria:
  - Native-capable runners default to `ContinuationNative` once their capability flags and adapter tests are enabled.
  - A brand-new native runner session sends only the current user message, not a replay prompt containing prior OR3 chat history.
     - Codex uses explicit `codex exec resume <thread_id>` after the first extracted `thread_id`.
     - Claude Code uses explicit `claude --resume <session_id>` after the first extracted `session_id`.
     - Gemini uses explicit `gemini --resume <session_id>` after the first extracted `session_id`.
     - OpenCode keeps its existing `--session` path.
  - Subsequent native turns send only the new user message plus the provider session ref; they must not include the full previous chat log.
  - Missing refs, unsupported runners, explicit replay mode, and replay-based forks still use the bounded replay prompt without failing the chat.

2. Native mode must preserve provider-side context and input-cache friendliness.
   - Acceptance criteria:
  - The backend does not rebuild and send the full OR3 transcript on every native turn.
  - Provider-native session state is the source of continuity after the first session ref is captured.
  - Regression tests assert that second and later native turns pass `UserMessage` as the prompt/task, not `ReplayPrompt`.
  - Replay prompts remain bounded and are visibly marked as fallback behavior in event/session metadata.

3. Session refs must be extracted deterministically from each runner's structured output.
   - Acceptance criteria:
     - Codex extracts `thread_id` from `thread.started` JSONL events.
     - Claude extracts `session_id` from `system/init` stream-json events.
     - Gemini extracts `session_id` from `init` stream-json events.
     - Extracted refs are persisted in `runner_chat_sessions.native_session_ref` once and reused for later turns.
     - Concurrent sessions never use "continue latest" style resume commands.

4. External runners must emit one shared, UI-friendly runtime event shape.
   - Acceptance criteria:
     - Existing `text_delta` behavior remains backward compatible.
     - New normalized payloads include canonical event types such as `content.delta`, `item.started`, `item.updated`, `item.completed`, `request.opened`, `request.resolved`, `turn.plan.updated`, `turn.diff.updated`, `turn.completed`, `runtime.warning`, and `runtime.error` where provider output supports them.
     - `content.delta` includes `stream_kind` values: `assistant_text`, `reasoning_text`, `reasoning_summary_text`, `plan_text`, `command_output`, `file_change_output`, or `unknown`.
     - Unsupported provider shapes remain visible as bounded `runner_output` diagnostics.

5. Tool calls and model activity must be visible like `or3-intern` activity.
   - Acceptance criteria:
  - Command/shell tool calls render as command activity with started/updated/completed states and bounded output deltas.
  - File edits/patches render as file-change activity, with diff payloads shown through the same app surface used for OR3-generated diffs where available.
  - MCP, web search, subagent/task, and unknown dynamic tools map to canonical item types instead of raw stdout whenever provider output includes enough structure.
  - Approval or user-input requests render as pending/resolved activity rows, even if provider-native response plumbing is deferred.
  - Reasoning, plans, and TodoWrite-style updates render separately from assistant answer text.

6. UI integration must feel native without requiring provider-specific UI logic.
   - Acceptance criteria:
     - `or3-app` can render external runner turns from normalized events into assistant text, activity timeline entries, approvals, plans, diffs, and errors using one adapter path.
     - Runner-specific raw payloads are optional diagnostics, not required for normal rendering.
     - Completion events include enough metadata to reconcile active turn state after reload.

7. Safety and boundedness must stay unchanged.
   - Acceptance criteria:
     - All external runner commands still pass through existing cwd, timeout, isolation, sandbox, env, queue, and output bounds.
     - No provider SDK server, persistent subprocess manager, or app-server transport is added in this phase.
     - Native resume failures degrade to replay mode or a clear turn error without corrupting stored messages.

## Non-functional constraints

- Keep implementation deterministic and single-process friendly with SQLite persistence.
- Prefer small changes in `internal/agentcli`, `internal/controlplane`, `cmd/or3-intern`, and existing tests.
- Avoid unbounded event payloads; preserve truncation/spill behavior for large output.
- Preserve existing API compatibility for `text_delta`, `runner_output`, `completion`, and stored chat transcripts.
- Treat provider protocols as unstable: add parser tests with fixtures rather than broad assumptions.
