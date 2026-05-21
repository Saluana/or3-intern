# Runner UX Tasks

## 1. Runtime contract and detection

- [x] 1.1 Add runtime backend types in `internal/agentcli/runners.go`. (Req: 1, 2, 7, 8)
  - Define `RunnerRuntimeKind`, `RunnerRuntimeStatus`, `RunnerRuntimeBackend`, `RunnerRuntimeTurnRequest`, `RunnerRuntimeTurn`, `RunnerRuntimeEvent`, and approval reference structs.
- [x] 1.2 Extend runner capability metadata. (Req: 1, 7)
  - Add preferred/available runtime mode fields to runner responses without removing existing `supports` fields.
- [x] 1.3 Add config normalization for native runtime settings in `internal/config/types.go` and `internal/config/load.go`. (Req: 2, 8)
  - Support `auto|native|cli` per runner, startup timeout, and idle timeout with backward-compatible defaults.
- [x] 1.4 Implement backend selection tests. (Req: 2, 7, 9)
  - Cover disabled runners, forced CLI, forced native, auto native success, auto fallback, and OR3 Intern default.
- [x] 1.5 Define runtime ownership and lazy-start rules. (Req: 8, 9)
  - Add explicit `managed|external|none|unknown` status and document that service scripts boot only `or3-intern`, not runner servers.

## 2. CLI fallback backend

- [x] 2.1 Wrap existing adapters as a `CLIRuntimeBackend`. (Req: 2, 4, 7)
  - Move no behavior initially; delegate to current `BuildChatCommand`, `ProcessManager`, and event normalization.
- [x] 2.2 Preserve current command behavior with regression tests. (Req: 5, 8, 9)
  - Ensure OpenCode, Codex, Claude, and Gemini command previews and output modes remain compatible.
- [x] 2.3 Add explicit fallback warning events. (Req: 2, 4, 6)
  - Emit a bounded `runtime.warning` event when auto mode falls back from native to CLI.

## 3. OpenCode native server backend

- [x] 3.1 Add OpenCode server process manager in `internal/agentcli/opencode_runtime.go`. (Req: 2, 8)
  - Start `opencode serve` on loopback with bounded startup/idle timeouts and parse the server URL.
- [x] 3.1a Add OpenCode attach/reuse checks before spawn. (Req: 8, 9)
  - Probe explicit/existing local server endpoints, classify ownership, and only start a new server when safe reuse is unavailable.
- [x] 3.2 Add OpenCode SDK/API client integration. (Req: 2, 4, 5)
  - Create/resume sessions, send prompts, subscribe to events, and store native session refs.
- [x] 3.3 Normalize OpenCode native events. (Req: 3, 4, 9)
  - Map message deltas, tool updates, file edits, permission requests, session idle, and session errors to canonical runner chat events.
- [x] 3.4 Bridge OpenCode permissions to OR3 approvals. (Req: 3, 6, 8)
  - Create friendly approval requests and reply `once`, `always`, or `reject` through OpenCode when resolved.
- [x] 3.5 Test OpenCode backend with fake server streams. (Req: 2, 3, 4, 9)
  - Avoid real account requirements; fake session/prompt/event/permission reply APIs.

## 4. Codex app-server backend

- [x] 4.1 Add a minimal Codex JSON-RPC client. (Req: 2, 5, 8)
  - Support stdio transport first, request IDs, initialization, notifications, server requests, bounded reads, and shutdown.
- [x] 4.1a Make Codex runtime boot semantics explicit. (Req: 8, 9)
  - Keep `codex app-server` lazy-started per need; do not wire it into `or3-intern service` boot or restart scripts.
- [x] 4.2 Implement Codex thread and turn lifecycle. (Req: 2, 4, 5)
  - Use `thread/start` or `thread/resume`, `turn/start`, `turn/interrupt`, and native thread IDs.
- [x] 4.3 Normalize Codex notifications. (Req: 4, 9)
  - Map item started/completed, agent message deltas, tool progress, errors, and turn completion to canonical events.
- [x] 4.4 Bridge Codex server approval requests. (Req: 3, 6, 8)
  - Handle command execution, file change, tool/user input, and permission request methods with OR3 approvals.
- [x] 4.5 Test Codex backend with fake JSON-RPC transport. (Req: 2, 3, 4, 5, 9)
  - Cover initialization, turn streaming, approval unblock, interrupt, malformed responses, and fallback eligibility.

## 5. Claude SDK bridge path

- [x] 5.1 Define the Claude native bridge boundary. (Req: 2, 7, 8)
  - Deferred by user direction: Claude SDK is intentionally skipped for now; Claude CLI fallback remains.
  - Decide whether the first implementation uses a bundled helper process, optional external helper, or remains design-only behind a feature flag.
- [x] 5.2 Add Claude bridge health detection. (Req: 1, 2, 6)
  - Deferred by user direction: no Claude SDK bridge is loaded or required.
  - Report “SDK bridge unavailable; using CLI fallback” without marking Claude unusable when the CLI is healthy.
- [x] 5.3 Map Claude SDK permissions in tests. (Req: 3, 9)
  - Deferred by user direction: will be covered when Claude SDK bridge scope is reopened.
  - Use fake `can_use_tool`/hook payloads to validate approval summaries and decisions.
- [x] 5.4 Keep current Claude CLI adapter as default fallback. (Req: 2, 5, 8)
  - Do not require Python/Node SDK dependencies for normal `or3-intern` builds.

## 6. Approval and safety integration

- [x] 6.1 Add runner-native approval subject helpers. (Req: 3, 6, 8)
  - Normalize command/file/tool/user-input requests, classify risk, redact secrets, and build user-friendly summaries.
- [x] 6.2 Wire approval decisions back to active runtime backends. (Req: 3, 5)
  - Register pending native request callbacks and resolve them from existing approve/deny flows.
- [x] 6.3 Add timeout and restart reconciliation behavior. (Req: 5, 8)
  - Expire stale pending runtime approvals and mark non-resumable in-flight turns aborted on restart.
- [x] 6.3a Add stale-runtime reconciliation behavior. (Req: 8, 9)
  - Handle stale pid files, stale cached URLs, externally restarted servers, orphaned child processes, and unknown ownership conservatively.
- [x] 6.4 Test allowlist and denial behavior. (Req: 3, 6, 9)
  - Ensure allowlist rules apply to normalized subjects and denial reaches the native backend.

## 7. Service API and persistence

- [x] 7.1 Extend controlplane runner response builders. (Req: 1, 6)
  - Include runtime kind, fallback kind, readiness message, next action, and native capability flags.
- [x] 7.2 Update `/internal/v1/chat-runners`. (Req: 1, 2, 6)
  - Merge binary detection with native runtime detection and keep OR3 Intern fallback on errors.
- [x] 7.3 Persist native refs through existing runner chat rows. (Req: 5)
  - Store native session refs and runtime metadata in `runner_chat_sessions`/`runner_chat_turns` without leaking secrets.
- [x] 7.4 Add service-level regression tests. (Req: 1, 3, 4, 5, 9)
  - Cover discovery, turn start, stream, approval, abort, fallback warning, and restart reconciliation.
- [x] 7.5 Add service script and startup-semantics coverage. (Req: 8, 9)
  - Verify `or3-intern service` and `scripts/restart-service.sh start|restart` do not eagerly start runner servers and do not leak duplicate helper processes under repeated checks.

## 8. or3-app first-class UX

- [x] 8.1 Update runner type definitions and `useChatRunners`. (Req: 1, 6)
  - Normalize runtime readiness, fallback state, next actions, and friendly labels per host.
- [x] 8.2 Upgrade runner selector and agent/runner cards. (Req: 1, 6)
  - Show “Ready”, “Sign in”, “Install”, “Using CLI fallback”, and “Disabled” in non-technical language.
- [x] 8.3 Render native runtime activity consistently. (Req: 4)
  - Ensure text, reasoning, tool, file, approval, warning, and error events use existing chat activity components.
- [x] 8.4 Improve approval detail sheets for runner-native requests. (Req: 3, 6)
  - Show action summary, affected files/commands, risk, remember option, and advanced raw payload toggle.
- [x] 8.5 Add app tests. (Req: 1, 3, 4, 6, 9)
  - Update `chat-runners` and assistant-stream tests for runtime status, fallback warnings, and approval-required events.

## 9. Documentation and rollout

- [x] 9.1 Update runner architecture docs. (Req: 7, 8)
  - Rename the mental model from “external agent CLI” to “runner runtimes” while preserving CLI fallback docs.
- [x] 9.2 Add user-facing setup guidance. (Req: 1, 6)
  - Explain install/sign-in/fallback states for OpenCode, Codex, and Claude Code in plain language.
- [x] 9.3 Add an operator troubleshooting section. (Req: 6, 8)
  - Cover native server startup failure, auth missing, unsupported versions, stale managed processes, already-running external instances, service restart semantics, and fallback behavior.
- [x] 9.4 Gate rollout by runtime. (Req: 2, 8, 9)
  - Ship OpenCode native first, then Codex app-server, then Claude SDK bridge after packaging and recovery are proven.

## Out of scope

- [x] Do not replace OR3 Intern's native assistant/tool runtime with external runners.
- [x] Do not add a hosted runner control plane or remote multi-user runner service.
- [x] Do not require Claude SDK bridge dependencies for normal Go builds until a packaging decision is made.
- [x] Do not expose raw provider payloads, secrets, or command-line details in the default non-technical UI.