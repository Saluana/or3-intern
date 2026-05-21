# Runner Chat Selection — Implementation Notes

This document tracks deviations from `design.md` and `tasks.md` made during
implementation, plus a status snapshot for picking up the work.

## Scope status

The backend foundation, frontend transport path, runner picker, session history,
and fork UX are now implemented. The remaining intentionally incomplete area is
**native-session mode**: adapters still advertise native support as disabled
until a specific runner can prove stable session-ref extraction and exact resume
semantics in tests.

### Delivered in this pass

- Phase 1: `RunnerChatCapabilities` + `GET /internal/v1/chat-runners`.
- Phase 2: SQLite migrations and stores for `runner_chat_sessions`,
  `runner_chat_turns`, `runner_chat_events`, and `chat_session_meta`.
- Phase 3: `internal/agentcli/chat_prompt.go` bounded replay prompt builder.
- Phase 4 (core spine only): `internal/agentcli/chat_manager.go` start/stream
  /abort + restart reconciliation. Native-session mode is **disabled by
  capability** in this pass; only replay continuation is wired through.
- Phase 5: `RunnerChatAdapter` replay command builders for Codex, Claude,
   Gemini, and OpenCode, plus generic chat event normalization that maps stdout
   to `text_delta` and preserves unsupported shapes as `runner_output`.
- Phase 7 (core endpoints): `/internal/v1/runner-chat/sessions[...]` (create,
  get, start turn, stream turn events, abort) and `/internal/v1/chat-sessions`
  list/get/messages/fork/rename routes. Authenticated through the same
  serviceAuth middleware as `/internal/v1/turns` and `/internal/v1/agent-runs`.
- Phase 8: `or3-app` API/state types and local cache normalization for runner
   chat, backend message IDs, archive/fork metadata, and runner bindings.
- Phase 9: `useChatRunners`, `useSessionHistory`, and `useChatSessions` helpers
   for runner metadata, backend session binding, history hydration, and forks.
- Phase 10: `useAssistantStream` keeps the existing OR3 `/internal/v1/turns`
   flow and adds a runner-chat branch that creates/reuses backend runner chat
   sessions, streams turn events, recovers by `runnerChatTurnId`, and aborts via
   the runner-chat endpoint.
- Phase 11/12: composer runner picker, disabled runner states, session history
   panel, message fork action, and activity affordance.
- Phase 13: structured API error codes and backend runner/session/turn logging.

### Deferred / not delivered

- Phase 6 native-session mode: capability flags exist, but no adapter advertises
  `chat_native_session = true`. `continuation_mode: "native"` always returns
  HTTP 400 `unsupported_native_session`.
- Service/UI tests for the new HTTP handlers and Vue components are still thin;
   the implemented code is covered by focused Go store/adapter/prompt tests plus
   the existing app test suite and typecheck.

## Deviations from design.md

1. **`chat_session_meta` schema** matches design.md but adds an explicit
   `host_id TEXT NOT NULL DEFAULT ''` column reserved for future multi-host
   indexing. It is currently always written as empty string.

2. **Snake_case wire shapes** are preserved via dedicated controlplane response
   builders rather than tagging the runtime structs. This avoids leaking
   service-shape concerns into `internal/agentcli`.

3. **Native session refs**: `runner_chat_sessions.native_session_ref` exists in
   schema but is never populated in this pass. Adapters must opt in via the
   future `NativeRunnerChatAdapter` interface (declared but not implemented by
   any concrete adapter yet).

4. **Replay prompt limits** are constants in `chat_prompt.go` (`replayMaxTurns
   = 12`, `replayMaxBytes = 48*1024`) per the design doc note that defaults
   live near the prompt builder unless `AgentCLIConfig` overrides are needed.
   No new env vars introduced in this pass.

5. **One-active-turn enforcement** lives in the manager (`StartTurn`) and is
   backed by a SQLite UNIQUE partial index on
   `runner_chat_turns(session_id) WHERE status IN ('queued','running')`. This
   makes the constraint correct even under concurrent requests.

6. **Restart reconciliation** runs from `serviceServer.start` after the manager
   is constructed, marking any `queued`/`running` turns as `aborted` with
   `error_message = 'service restarted'`. This matches design.md §"Recovery"
   and §"Service restart".

7. **Abort semantics**: abort is best-effort. The manager attaches a
   `context.CancelFunc` to the active turn keyed by turn ID (in-memory map);
   POST .../abort cancels and writes `status='aborted'`. If the manager process
   restarted, the abort endpoint will mark the row aborted directly.

8. **Common message timeline mirroring** (Phase 4 task: append normalized
   user/assistant rows into `messages`) is wired so external runner turns write
   a `user` message before execution and an `assistant` message on completion,
   with `payload_json.transport = "runner_chat"` plus runner/session/turn IDs.

9. **Frontend transport refactor is branch-based, not a new class hierarchy.**
   `useAssistantStream` still owns event application, but it now branches at
   transport time: OR3 uses `/internal/v1/turns`; external runners use
   `/internal/v1/runner-chat/sessions`. This preserves current OR3 behavior and
   minimizes churn while satisfying selectable runner transport.

10. **Native settings are present but not surfaced as enabled controls.** Model,
    cwd, isolation, continuation mode, and max turns are represented in types and
    request payloads, but the first UI exposes only runner selection to avoid
    implying native session support before adapter tests exist.

## Picking up the work

1. Add service tests for `/internal/v1/chat-runners`, `/internal/v1/chat-sessions`,
   and `/internal/v1/runner-chat/sessions` HTTP/SSE behavior.
2. Add focused Vue/composable tests for `useChatRunners`, `useSessionHistory`,
   composer runner selection, and the session history panel.
3. Implement true native-session mode for one runner (likely OpenCode) only
   after tests prove stable session-ref extraction and specific-session resume.
4. Replace generic structured event normalization with runner-specific JSON
   mapping as each CLI output contract is verified.

## Test coverage delivered

- `internal/agentcli/chat_prompt_test.go` — ordering, turn limits, byte limits,
   incomplete-turn filtering, and truncation markers.
- `internal/agentcli/chat_adapters_test.go` — replay command args and generic
   event normalization for stdout/stderr output.
- `internal/db/runner_chat_store_test.go` — session/turn lifecycle,
   active-turn uniqueness, event append/list, and startup reconciliation.
- `internal/db/chat_session_store_test.go` — session list/rename/archive,
   message pagination, and fork payload sanitization.
- App verification: `bun run typecheck` and `bun run test` pass in `or3-app`.
- Go verification: focused new packages pass; `go test ./...` is blocked by an
   unrelated existing `internal/skills` test that expects local binary `gws` to
   be available on PATH.

## Phase 7 implementation notes

- **Request payloads live in the handler files, not `service_request.go`.**
  The plan asked for payloads in `service_request.go` for symmetry with turns/
  subagents. Chat payloads are simpler (no tool-policy resolution, no replay
  tool calls) so they live as small structs at the top of
  `service_chat_sessions.go` and `service_runner_chat.go`. Move them later if
  the symmetry pays off.

- **`decodeServiceJSONLoose` (chat-only) tolerates unknown fields**, unlike
  `decodeServiceRequestBody`. Chat payloads from the frontend may evolve
  faster than the Go structs; trailing data is still rejected.

- **SSE turn streaming polls `runner_chat_events` rather than tapping
  `JobRegistry`**. Because `ChatManager.mirrorJobEvents` writes every job
  event to the store synchronously, polling is correctness-equivalent and
  much smaller than re-implementing the channel-fanout pattern in
  `streamJob`. Latency is bounded by the 500 ms tick; lower-latency direct
  pub-sub is a follow-up if needed.

- **`controlplane.BuildChatRunner` defaults**: discovery currently passes
  empty strings for `defaultModel/Mode/Isolation/Cwd`. When per-runner config
  defaults become first-class in `AgentCLIConfig`, plumb them through here.
