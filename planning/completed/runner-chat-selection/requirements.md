# Runner Chat Selection Requirements

## Overview

Add a first-class way for `or3-app` users to choose which agent runtime handles a chat thread, revisit old chat sessions, and fork a session from a specific message point. The default remains the existing `or3-intern` `/internal/v1/turns` flow. External runners such as Codex, Claude Code, Gemini CLI, and OpenCode become selectable from the composer only when the `or3-intern` service reports that they are available.

Scope assumptions:

- `or3-intern` remains the service boundary and owns runner execution, persistence, safety checks, and event normalization.
- The app should not shell out directly to Codex, Claude, Gemini, or OpenCode.
- External runner chat is added through a new chat-specific backend API, not by overloading the existing job-centric `/internal/v1/agent-runs` API.
- Initial production path should support replay-mode continuity for every available runner and native-session continuity only for runners with a verified stable session reference and resume command.
- Existing local chat sessions, approvals, job history, cron runner jobs, and `/internal/v1/turns` behavior must keep working.
- Session history and forks must work across `or3-intern` and all external runners by using one normalized persisted message timeline. Native runner session forks are optional and per-runner; replay forks are the universal fallback.

Current external CLI assumptions verified on 2026-05-07:

- Claude Code documents non-interactive continuation with `--continue --print` and conversation resume with `--resume`.
- Gemini CLI documents `--resume` and structured `--output-format json` / `stream-json`.
- OpenCode documents `run --session`, `run --continue`, JSON event output, `session list`, export/import, and an optional headless server.
- Codex CLI supports `exec --json` and resume-related commands, but current public issues indicate programmatic session ID extraction and exec/resume behavior are less stable than the others. Treat Codex native-session support as optional until verified by adapter tests.

Sources:

- https://docs.anthropic.com/en/docs/claude-code/tutorials
- https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/configuration.md
- https://opencode.ai/docs/cli/
- https://help.openai.com/en/articles/11096431-openai-codex-ci-getting-started
- https://github.com/openai/codex/issues/8923

## Requirements

1. The app can discover chat-capable runners.
   - Acceptance criteria:
     - `GET /internal/v1/chat-runners` returns `or3-intern` plus detected external runners.
     - Each runner includes `id`, `display_name`, `status`, `auth_status`, `supports`, `chat_capabilities`, and default `model`, `mode`, `isolation`, and `cwd` hints when known.
     - Disabled, missing, unauthenticated, and errored runners are returned with clear status and are not selectable as active chat transports.
     - Existing `GET /internal/v1/agent-runners` keeps its current response shape.

2. The app can bind each chat thread to one runner transport.
   - Acceptance criteria:
     - New `ChatSession` metadata records `runnerId`, `runnerLabel`, `runnerChatSessionId`, `runnerContinuationMode`, `runnerModel`, `runnerMode`, `runnerIsolation`, and `runnerCwd`.
     - Newly created sessions default to `runnerId: "or3-intern"`.
     - Changing runners in an empty session is immediate.
     - Changing runners in a session that already has messages requires explicit user confirmation and creates a new app chat session or forks metadata so transcripts do not silently mix incompatible runtimes.

3. Existing `or3-intern` chat behavior remains the default and unchanged.
   - Acceptance criteria:
     - When `runnerId` is `or3-intern`, `useAssistantStream` continues to call `/internal/v1/turns`.
     - Tool policy modes `ask`, `work`, and `admin`, approval retry, `/internal/v1/jobs/:id/stream` recovery, and current message rendering continue to pass existing tests.
     - No external-runner tables or APIs are required for plain `or3-intern` chat.

4. External runner chat sessions are persisted by `or3-intern`.
   - Acceptance criteria:
     - Creating an external runner chat session writes a `runner_chat_sessions` row linked to the app session key.
     - Each user turn writes a `runner_chat_turns` row with status, prompt, selected runner configuration, associated job/run ID, final text, error, and timestamps.
     - Normalized stream events for runner turns are persisted and replayable after reconnect.
     - Service restart marks in-flight external runner chat turns as aborted without corrupting completed transcript data.

5. External runner chat supports replay-mode continuity.
   - Acceptance criteria:
     - For runners without verified native resume support, each new turn builds a bounded transcript prompt from `runner_chat_turns` plus the new user message.
     - Replay history is bounded by configurable byte, turn, and message limits.
     - Oversized history is summarized or trimmed deterministically, with the newest turns preserved.
     - The generated prompt clearly separates system context, previous user/assistant turns, and the new user message.

6. External runner chat supports native-session mode where safe.
   - Acceptance criteria:
     - Adapter capabilities can report `chat_native_session`, `chat_resume`, `chat_session_ref_extractable`, and supported output event formats.
     - Native mode is enabled per adapter only after tests prove the session ref can be extracted and resumed deterministically.
     - Native session refs are stored in `runner_chat_sessions.native_session_ref`.
     - If native resume fails with a known unrecoverable error, the turn fails clearly instead of silently falling back to replay inside the same native session.

7. External runner turns stream into the same chat UI event model as `or3-intern` turns.
   - Acceptance criteria:
     - Backend emits normalized events: `queued`, `started`, `text_delta`, `assistant`, `reasoning_delta`, `tool_call`, `tool_result`, `completion`, `error`, and optional raw `runner_output`.
     - `completion` includes `status`, `final_text`, `runner_id`, `runner_chat_session_id`, and `runner_chat_turn_id`.
     - Output chunks and raw events are bounded by existing `AgentCLIConfig` preview/event limits or new chat-specific equivalents.
     - The app can render external runner output as assistant text, activity, tool activity, or failure using the existing `ChatMessage` surface.

8. External runner chat can be aborted and recovered.
   - Acceptance criteria:
     - `POST /internal/v1/runner-chat/sessions/:id/abort` cancels the active turn when one exists.
     - Reloading the app while an external runner turn is active reconnects to the backend turn stream or fetches the latest persisted turn snapshot.
     - If the service restarted during a turn, the app shows an aborted/error state with retry available.
     - Recovery is keyed by `runnerChatSessionId` and active turn ID, not only the generic job ID.

9. The composer exposes runner selection without cluttering normal chat.
   - Acceptance criteria:
     - `AssistantComposer.vue` shows a runner picker inside `or3-composer-menu`.
     - The current runner is visible in compact form near the mode selector or composer controls.
     - Runner unavailable/auth-missing states are shown in the picker with disabled options and concise status text.
     - Advanced runner settings are behind a secondary panel or expandable area, not in the default composer row.

10. Runner settings preserve safety defaults.
    - Acceptance criteria:
      - External runner chat defaults to `safe_edit` + `host_workspace_write` or the existing configured defaults.
      - `sandbox_auto` / dangerous bypass options are only selectable when `AgentCLIConfig.AllowSandboxAuto` permits them.
      - `cwd` is resolved through the existing `resolveAgentCLICwd` restriction path.
      - Child runner environments continue to use `BuildAgentCLIEnv` and do not expose OR3 internal secrets.

11. App local cache migration is backward compatible.
    - Acceptance criteria:
      - Existing `or3-app:v1:state` entries without runner fields load successfully.
      - Missing runner metadata is normalized to `or3-intern`.
      - Draft keys and existing message IDs do not change.
      - A corrupted or obsolete runner binding cannot prevent the app from opening the existing session.

12. Observability and diagnostics are sufficient for support.
    - Acceptance criteria:
      - Backend logs include runner ID, chat session ID, turn ID, mode, isolation, and terminal status.
      - API errors distinguish runner missing, auth missing, unsupported native chat, invalid session, active turn conflict, timeout, and aborted.
      - Activity and job history can link from a chat turn to the underlying `agent_cli_runs` row when one exists.

13. Tests cover backend persistence, adapter behavior, API contracts, app transport selection, and recovery.
    - Acceptance criteria:
      - Go tests cover migrations, store methods, chat API handlers, replay prompt construction, native capability gating, abort, and restart reconciliation.
     - TypeScript/Vue tests cover local cache normalization, transport adapter choice, composer runner picker behavior, and event normalization.
     - Existing tests for `/internal/v1/turns`, `/internal/v1/agent-runs`, jobs, approvals, and runner detection continue to pass.

14. Users can view and reopen old chat sessions.
    - Acceptance criteria:
      - `GET /internal/v1/chat-sessions` lists recent chat sessions with title, session key, runner binding, message counts, last message preview, fork metadata, created/updated timestamps, and archived status.
      - `GET /internal/v1/chat-sessions/:session_key/messages` returns paginated messages in chronological order for that session.
      - `or3-app` can hydrate its local session cache from the backend list without losing local-only drafts or message IDs.
      - Opening an old session promotes it to the active local session and loads enough messages to render the thread immediately.

15. All runner transports persist a common conversation timeline.
    - Acceptance criteria:
      - `or3-intern` turns continue writing to the existing `messages` table.
      - External runner turns write normalized user and assistant messages to the same `messages` table, with payload metadata linking to `runner_chat_session_id`, `runner_chat_turn_id`, runner ID, continuation mode, and native session ref when present.
      - Runner raw events remain in runner-specific event tables; the common `messages` table stores only user-visible timeline messages and compact metadata.
      - Existing prompt-building and memory behavior can distinguish OR3-native messages from external-runner messages through payload metadata.

16. Users can fork a chat session from a selected message.
    - Acceptance criteria:
      - `POST /internal/v1/chat-sessions/:session_key/fork` accepts an anchor message ID and creates a new session containing the source transcript up to and including that anchor message.
      - The fork records `parent_session_key`, `fork_anchor_message_id`, `forked_from_runner_id`, and `fork_strategy`.
      - Forked sessions preserve visible user, assistant, system, tool, attachment, approval, and runner metadata needed to render the copied prefix.
      - The original session is never modified by forking.

17. Forks work across all agents through replay continuity.
    - Acceptance criteria:
      - A fork from an `or3-intern` thread can continue with `or3-intern` or any available external runner.
      - A fork from an external-runner thread can continue with the same runner, `or3-intern`, or a different runner.
      - If a native runner cannot fork or resume exactly at the selected message point, the backend creates the fork in replay mode and clearly marks `fork_strategy: "replay"`.
      - Native fork/resume is used only when a runner adapter explicitly declares and tests support for forking or resuming a specific session state.

18. Message-point forks are blocked or clarified for unsafe anchors.
    - Acceptance criteria:
      - The API rejects anchors that do not belong to the source session.
      - Forking from a streaming/incomplete message requires completion, cancellation, or explicit fork-from-last-complete behavior.
      - Forking from a pending approval state preserves the visible approval history but does not auto-carry issued approval tokens.
      - Forking from a tool/result pair preserves enough timeline context to avoid malformed replay prompts.

19. The app exposes session history and fork actions ergonomically.
    - Acceptance criteria:
      - The chat screen has a session/history control that opens recent sessions for the current host.
      - A user can search/filter by title, runner, and last-message text.
      - Each message exposes a fork action, disabled while that message is incomplete or the current chat is streaming.
      - After fork creation, the app activates the new session and scrolls to the copied anchor point or bottom of the forked transcript.

20. Session titles and archival are persisted.
    - Acceptance criteria:
      - Backend session metadata stores title, runner ID, archived status, parent/fork metadata, and updated time.
      - The app can rename and archive sessions.
      - Archived sessions are hidden from the default history list but remain discoverable through an explicit archived filter.
      - Existing local sessions without backend metadata are backfilled on first successful send or history sync.

21. Session history and fork APIs preserve privacy and safety boundaries.
    - Acceptance criteria:
      - Session list and message reads are authenticated with the same service auth model as chat.
      - Session listing returns bounded previews, not unbounded transcript bodies.
      - Message page size and request body sizes are bounded.
      - Forking does not copy secrets, approval tokens, or raw child process environment details from payload metadata.

## Non-Functional Constraints

- Keep the design single-process and SQLite-backed. Do not introduce a new service, queue, daemon, or external database.
- Keep external runner execution bounded by existing concurrency, queue, timeout, output, environment, and cwd restrictions.
- Do not persist secrets, bearer tokens, approval tokens, full child environments, or unrestricted command lines.
- Avoid unbounded transcript replay. Bound by turn count and bytes before spawning a child process.
- Preserve deterministic migration behavior for existing SQLite databases.
- Preserve low memory usage by streaming and truncating event output rather than accumulating full process output in memory.
- Treat native runner session semantics as per-runner capabilities, not as a universal contract.
- Treat replay-mode forks as the universal compatibility layer. Native fork/resume must be an optimization, not a dependency for correctness.
