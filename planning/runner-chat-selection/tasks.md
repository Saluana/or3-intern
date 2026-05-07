# Runner Chat Selection Tasks

## 1. Backend Capability And Discovery

- [ ] Extend `internal/agentcli/runners.go` with `RunnerChatCapabilities` and add it to `RunnerSupports`. Requirements: 1, 5, 6.
- [ ] Update `internal/agentcli/registry.go` runner specs with conservative chat capabilities: replay selectable for available external runners, native disabled until verified per adapter. Requirements: 1, 6.
- [ ] Add optional `RunnerChatAdapter` and `NativeRunnerChatAdapter` interfaces without changing the existing `RunnerAdapter` contract. Requirements: 5, 6.
- [ ] Add `GET /internal/v1/chat-runners` handler, response builder, and route registration. Requirements: 1, 9.
- [ ] Add tests for runner discovery responses, disabled runner handling, and `or3-intern` default inclusion. Requirements: 1, 13.

## 2. Backend Persistence

- [ ] Add `runner_chat_sessions`, `runner_chat_turns`, and `runner_chat_events` migrations in `internal/db/db.go`. Requirements: 4, 8, 11.
- [ ] Add `chat_session_meta` migration and indexes for session history, runner binding, archive state, and fork metadata. Requirements: 14, 16, 20, 21.
- [ ] Create `internal/db/runner_chat_store.go` with structs and methods for session create/get/update, turn create/get/finalize, active turn lookup, event append/list, and restart reconciliation. Requirements: 4, 8.
- [ ] Create `internal/db/chat_session_store.go` with methods for list sessions, read paginated messages, upsert metadata, rename, archive, and fork-by-message. Requirements: 14, 16, 18, 20, 21.
- [ ] Add indexes for app session lookup, active/status lookup, and event replay by turn sequence. Requirements: 4, 8.
- [ ] Add safe payload-copy logic for forks that strips approval tokens, secret-like metadata, raw child env, and raw runner output. Requirements: 16, 18, 21.
- [ ] Add SQLite-backed tests in `internal/db/runner_chat_store_test.go`. Requirements: 4, 8, 13.
- [ ] Add SQLite-backed tests in `internal/db/chat_session_store_test.go`. Requirements: 14, 16, 18, 20, 21.

## 3. Replay Prompt Support

- [ ] Add `internal/agentcli/chat_prompt.go` to build bounded replay prompts from previous completed `runner_chat_turns`. Requirements: 5, 10.
- [ ] Make replay bounds configurable through `AgentCLIConfig` only if existing defaults are insufficient; otherwise use constants near the prompt builder. Requirements: 5, 10.
- [ ] Preserve newest turns and mark deterministic truncation in the generated prompt. Requirements: 5.
- [ ] Add tests for ordering, byte limits, turn limits, empty history, and oversized single turns. Requirements: 5, 13.

## 4. Runner Chat Manager

- [ ] Create `internal/agentcli/chat_manager.go` with a `ChatManager` that owns start, stream, abort, and reconcile behavior for runner chat turns. Requirements: 4, 7, 8, 10.
- [ ] Reuse existing validation paths: runner detection, disabled runner checks, `ValidateRunPolicy`, `resolveAgentCLICwd`, timeout bounds, and `BuildAgentCLIEnv`. Requirements: 1, 10.
- [ ] Enforce one active turn per `runner_chat_sessions` row. Requirements: 8.
- [ ] Run external processes through existing `ProcessManager` and normalize output into runner chat events. Requirements: 7, 10.
- [ ] Persist all normalized events to `runner_chat_events` and final text/error to `runner_chat_turns`. Requirements: 4, 7, 8.
- [ ] Append normalized user and assistant timeline messages to the shared `messages` table for external runner turns. Requirements: 15, 16, 17.
- [ ] Update `chat_session_meta` after each turn with title fallback, runner binding, message count, preview, and updated timestamp. Requirements: 14, 15, 20.
- [ ] Reconcile running turns to aborted on service startup. Requirements: 8.
- [ ] Add unit tests with fake adapters/process output for success, failure, timeout, abort, and restart reconciliation. Requirements: 4, 7, 8, 13.
- [ ] Add tests that external runner turns produce forkable common messages without persisting raw process output into the transcript. Requirements: 15, 16, 21.

## 5. Adapter Implementations

- [ ] Implement replay command building for Codex using `codex exec --json --color never` with the replay prompt as the task. Requirements: 5, 7.
- [ ] Implement replay command building for Claude Code using print/non-interactive mode and stream JSON output. Requirements: 5, 7.
- [ ] Implement replay command building for Gemini CLI using non-interactive prompt and JSON or stream JSON output. Requirements: 5, 7.
- [ ] Implement replay command building for OpenCode using `opencode run --format json`. Requirements: 5, 7.
- [ ] Add per-runner event normalizers that map structured output to `text_delta`, `assistant`, `reasoning_delta`, `tool_call`, `tool_result`, `completion`, and `error` where possible. Requirements: 7.
- [ ] Keep raw `runner_output` or bounded `output` events for unsupported event shapes. Requirements: 7, 12.
- [ ] Add adapter tests for args, cwd, model, mode, isolation, max turns, and output mode. Requirements: 10, 13.

## 6. Native Session Mode

- [ ] Add native-session capability tests before enabling native mode for any runner. Requirements: 6, 13.
- [ ] Start with one runner that exposes a stable session ref and specific-session resume. OpenCode is the likely first candidate. Requirements: 6.
- [ ] Store extracted native session refs in `runner_chat_sessions.native_session_ref`. Requirements: 6.
- [ ] Reject native mode when a runner only supports "continue latest" and not specific-session resume. Requirements: 6, 8.
- [ ] Keep Codex native mode disabled until a stable session ID extraction path is verified in tests. Requirements: 6.

## 7. Backend API

- [ ] Add general chat session request payloads and decoders in `cmd/or3-intern/service_request.go` for session creation/update and fork. Requirements: 14, 16, 18, 20.
- [ ] Add request payloads and decoders in `cmd/or3-intern/service_request.go` for runner chat session creation and turn creation. Requirements: 4, 8.
- [ ] Add `cmd/or3-intern/service_chat_sessions.go` handlers:
  - `GET /internal/v1/chat-sessions`
  - `POST /internal/v1/chat-sessions`
  - `GET /internal/v1/chat-sessions/:session_key`
  - `PATCH /internal/v1/chat-sessions/:session_key`
  - `GET /internal/v1/chat-sessions/:session_key/messages`
  - `POST /internal/v1/chat-sessions/:session_key/fork`
  Requirements: 14, 16, 18, 20, 21.
- [ ] Add `cmd/or3-intern/service_runner_chat.go` handlers:
  - `POST /internal/v1/runner-chat/sessions`
  - `GET /internal/v1/runner-chat/sessions/:id`
  - `POST /internal/v1/runner-chat/sessions/:id/turns`
  - `GET /internal/v1/runner-chat/sessions/:id/turns/:turn_id`
  - `GET /internal/v1/runner-chat/sessions/:id/turns/:turn_id/stream`
  - `POST /internal/v1/runner-chat/sessions/:id/abort`
  Requirements: 1, 4, 7, 8.
- [ ] Register routes in `cmd/or3-intern/service.go` with the same auth sensitivity as chat/job routes. Requirements: 8, 10, 12.
- [ ] Ensure session-history route responses use bounded previews and paginated message reads. Requirements: 14, 21.
- [ ] Add response builders in `internal/controlplane/controlplane.go`. Requirements: 4, 7, 12.
- [ ] Add service tests for JSON responses, SSE streaming, event replay after `after_seq`, active turn conflict, abort, and not-found cases. Requirements: 4, 7, 8, 12, 13.
- [ ] Add service tests for session listing, message pagination, rename/archive, fork, invalid anchors, incomplete anchors, and replay fallback metadata. Requirements: 14, 16, 17, 18, 20, 21.

## 8. Frontend Types And Cache

- [ ] Extend `../or3-app/app/types/or3-api.ts` with chat runner capabilities, runner chat session, turn, event, and API response types. Requirements: 1, 7.
- [ ] Extend `../or3-app/app/types/or3-api.ts` with general chat session list, message page, session update, and fork request/response types. Requirements: 14, 16, 18, 20.
- [ ] Extend `../or3-app/app/types/app-state.ts` `ChatSession` with runner binding, backend metadata, archive state, and fork fields. Requirements: 2, 11, 14, 16, 20.
- [ ] Extend `../or3-app/app/types/app-state.ts` `ChatMessage` with `backendMessageId`, source/fork IDs, and runner chat IDs. Requirements: 14, 15, 16.
- [ ] Extend `AssistantSendPayload` with optional runner/session overrides only if transport selection needs payload-level data. Requirements: 2.
- [ ] Normalize old local cache sessions in `../or3-app/app/composables/useLocalCache.ts` so missing runner fields become `or3-intern`. Requirements: 11.
- [ ] Normalize old local messages so missing backend IDs do not block rendering, while fork actions require backend IDs. Requirements: 11, 16, 18.
- [ ] Add local cache tests for backward compatibility. Requirements: 11, 13.

## 9. Frontend Runner State

- [ ] Create `../or3-app/app/composables/useChatRunners.ts` to call `GET /internal/v1/chat-runners`, cache by host, and expose selectable runners. Requirements: 1, 9.
- [ ] Create `../or3-app/app/composables/useSessionHistory.ts` to list sessions, hydrate messages, rename/archive, fork, and reconcile backend metadata into local cache. Requirements: 14, 16, 19, 20.
- [ ] Update `../or3-app/app/composables/useChatSessions.ts` with helpers to set runner metadata, bind a backend runner chat session ID, activate old sessions, and handle switch/fork rules. Requirements: 2, 9, 11, 14, 16.
- [ ] Add tests for empty-session switch, non-empty-session switch confirmation/new-session behavior, backend history hydration, session activation, fork activation, and fallback when selected runner becomes unavailable. Requirements: 2, 9, 13, 14, 16, 19.

## 10. Frontend Transport Refactor

- [ ] Extract event application logic from `useAssistantStream.ts` into transport-neutral helpers. Requirements: 3, 7, 8.
- [ ] Add `or3InternTransport` that preserves current `/internal/v1/turns` behavior. Requirements: 3.
- [ ] Add `runnerChatTransport` that creates or reuses backend runner chat sessions, starts turns, streams events, fetches snapshots, and aborts active turns. Requirements: 4, 7, 8.
- [ ] Update recovery logic to branch by `message.jobId` for OR3 and `message.runnerChatSessionId` / `runnerChatTurnId` for external runners. Requirements: 8.
- [ ] Ensure retry payloads preserve runner metadata and do not duplicate user echo during recovery. Requirements: 8, 11.
- [ ] Ensure completed OR3 and runner-chat sends update local messages with backend message IDs when returned by snapshots or session message hydration. Requirements: 14, 15, 16.
- [ ] Add tests around transport selection, fallback, recovery, abort, and event dedupe. Requirements: 3, 7, 8, 13.

## 11. Composer UX

- [ ] Update `../or3-app/app/components/assistant/AssistantComposer.vue` props/emits for selected runner and runner list. Requirements: 2, 9.
- [ ] Add a runner section to `or3-composer-menu` below attachments and above or near mode controls. Requirements: 9.
- [ ] Show disabled states for missing, auth-missing, disabled-by-config, and unsupported runners. Requirements: 1, 9, 12.
- [ ] Keep advanced settings such as model, cwd, isolation, native/replay mode, and max turns in a secondary panel or sheet. Requirements: 9, 10.
- [ ] Update `../or3-app/app/pages/index.vue` to load runners, pass selected runner into composer, and handle runner switch confirmation/new session. Requirements: 2, 9.
- [ ] Add component tests or focused interaction tests for picker rendering and emit behavior. Requirements: 9, 13.

## 12. Session History And Fork UX

- [ ] Add a session history control to `../or3-app/app/pages/index.vue` that opens a focused history panel for the current host. Requirements: 14, 19.
- [ ] Add `../or3-app/app/components/assistant/SessionHistoryPanel.vue` with recent sessions, search, runner filter, archived filter, fork parent indicators, and open/rename/archive actions. Requirements: 14, 19, 20.
- [ ] Update `../or3-app/app/components/assistant/ChatMessage.vue` with a fork action for complete messages that have `backendMessageId`. Requirements: 16, 18, 19.
- [ ] Disable fork actions while the active chat is streaming, while approval is pending, or when the message has no backend anchor yet. Requirements: 18, 19.
- [ ] After a successful fork, activate the returned session, hydrate copied messages, preserve target runner metadata, and scroll to the anchor or bottom. Requirements: 16, 17, 19.
- [ ] Add UI tests for opening old sessions, filtering session history, fork action disabled states, and successful fork activation. Requirements: 14, 16, 18, 19, 20.

## 13. Activity And Diagnostics

- [ ] Link runner chat turns to underlying `agent_cli_runs` where available through `agent_cli_run_id`. Requirements: 12.
- [ ] Add UI affordance from a runner-backed chat message to the activity/job details when an underlying job exists. Requirements: 12.
- [ ] Add backend logs with runner ID, session ID, turn ID, mode, isolation, terminal status, and duration. Requirements: 12.
- [ ] Add API error codes for `runner_missing`, `runner_auth_missing`, `unsupported_native_session`, `runner_chat_turn_active`, `runner_chat_session_not_found`, `runner_chat_turn_not_found`, and `runner_chat_aborted`. Requirements: 12.
- [ ] Add API error codes for `chat_session_not_found`, `invalid_fork_anchor`, `fork_anchor_incomplete`, and `unsupported_native_fork`. Requirements: 18, 21.

## 14. Rollout Plan

- [ ] Phase 1: Land frontend transport abstraction while keeping only `or3-intern` enabled. Requirements: 3, 13.
- [ ] Phase 2: Land backend session history APIs, common message timeline, and fork-by-message for `or3-intern` only. Requirements: 14, 15, 16, 18, 20, 21.
- [ ] Phase 3: Land backend runner chat persistence/API with fake adapter tests and no composer exposure. Requirements: 4, 7, 8, 13, 15.
- [ ] Phase 4: Enable replay-mode external runner chat for one runner behind a capability flag. Requirements: 5, 7, 10, 17.
- [ ] Phase 5: Add session history panel, message fork actions, composer runner picker, and per-session runner binding. Requirements: 2, 9, 11, 14, 16, 19.
- [ ] Phase 6: Expand replay-mode support to the other detected runners. Requirements: 5, 7, 17.
- [ ] Phase 7: Enable native-session mode and native fork mode for individual runners only after adapter tests prove stable session refs and specific-session resume/fork. Requirements: 6, 17.

## 15. Documentation And Verification

- [ ] Document runner chat configuration and known limitations in the repo docs or service API notes. Requirements: 1, 6, 10, 12.
- [ ] Document session history and fork semantics, especially replay fallback for all agents and native fork limitations. Requirements: 14, 16, 17, 18.
- [ ] Update `.env.example` only if new env vars are introduced. Requirements: 10.
- [ ] Run `go test ./...` in `or3-intern`. Requirements: 13.
- [ ] Run the app's typecheck/test command in `or3-app`. Requirements: 13.
- [ ] Manually verify OR3 default chat, old session reopen, fork from OR3 message, runner replay chat, fork from runner message into OR3 and runner replay, abort, reload recovery, unavailable runner UI, and runner switch behavior. Requirements: 2, 3, 7, 8, 9, 14, 16, 17, 19.

## Out Of Scope For The First Implementation

- [ ] Mapping runner-native approval prompts into OR3 approval requests.
- [ ] Consolidating external runner chat transcripts into OR3 long-term memory.
- [ ] Direct browser-to-runner shell execution from `or3-app`.
- [ ] Multi-runner collaboration inside one chat turn.
- [ ] Native session mode for all runners on day one.
- [ ] Native message-point forks for runners without explicit specific-state fork support.
