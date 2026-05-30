# Consumer UX Facade Tasks

## 1. Add additive `/app/v1` route foundation

- [ ] 1.1 Create `cmd/or3-intern/service_app_facade.go` with a small dispatcher for `/app/v1/*` routes. Requirements: 1
- [ ] 1.2 Update `cmd/or3-intern/service_routes.go` to register `/app/v1/bootstrap` and the initial facade subtrees without removing or changing `/internal/v1/*`. Requirements: 1
- [ ] 1.3 Implement `/app/v1/bootstrap` by adapting existing `buildAppBootstrap` into consumer sections and action cards. Requirements: 1
- [ ] 1.4 Add route contract tests proving `/app/v1/bootstrap` works and `/internal/v1/app/bootstrap` remains unchanged. Requirements: 1

## 1A. Add realtime as progressive enhancement

- [ ] 1A.1 Decide initial realtime transport: prefer SSE for one-way app updates; reserve WebSocket for terminal I/O or bidirectional controls. Requirements: 1A
- [ ] 1A.2 Add `/app/v1/events` or `/app/v1/ws` using existing service auth/role checks and bounded per-connection buffers. Requirements: 1A
- [ ] 1A.3 Define screen-oriented event envelopes for conversation, task, approval, device, file, terminal, and activity updates. Requirements: 1A
- [ ] 1A.4 Keep HTTP endpoints authoritative for bootstrap, state reads, and actions; realtime events should invalidate/update screens, not replace durable actions. Requirements: 1A
- [ ] 1A.5 Add tests that realtime normal payloads omit raw IDs, tokens, websocket tickets, and secret/config internals. Requirements: 1A, 8
- [ ] 1A.6 Add client-contract guidance that `or3-app` must refetch HTTP state when realtime is unavailable or reconnect misses events. Requirements: 1A

## 2. Add consumer handles for conversations

- [ ] 2.1 Add an additive SQLite migration/table in `internal/db` for `app_conversation_handles(handle, session_key, created_at, updated_at)`. Requirements: 2
- [ ] 2.2 Add DB helpers to get-or-create a conversation handle for an existing `session_key` and resolve a handle back to `session_key`. Requirements: 2
- [ ] 2.3 Add a bounded random handle helper such as `conv_` + random hex using existing crypto/rand patterns. Requirements: 2
- [ ] 2.4 Add SQLite-backed tests for handle uniqueness, idempotent get-or-create, and missing-handle lookup. Requirements: 2

## 3. Build conversation facade endpoints

- [ ] 3.1 Add `GET /app/v1/conversations` using `ListChatSessions`, returning `conversation_id` and screen-ready summaries only. Requirements: 2
- [ ] 3.2 Add `POST /app/v1/conversations` that accepts title/mode, generates the internal `session_key`, upserts chat metadata, and returns a conversation card. Requirements: 2
- [ ] 3.3 Add `GET /app/v1/conversations/:conversationId` and `/messages`, resolving the handle internally and hiding session keys in normal responses. Requirements: 2
- [ ] 3.4 Add `POST /app/v1/conversations/:conversationId/messages` with a small body (`message`, optional predefined `mode`) and backend-owned tool-policy/profile resolution. Requirements: 2
- [ ] 3.5 Add `POST /app/v1/conversations/:conversationId/fork` that accepts selected `message_id`, generates `new_session_key`, calls existing `ForkChatSession`, and returns the new `conversation_id`. Requirements: 2
- [ ] 3.6 Add tests that normal conversation responses omit `session_key`, `parent_session_key`, `runner_id`, `runner_chat_session_id`, `approval_token`, and raw tool policy fields. Requirements: 2, 8

## 4. Convert approvals to inbox cards/actions

- [ ] 4.1 Extend `internal/uxstate.BuildApprovalPrompt` or add a companion builder for app approval cards with action labels. Requirements: 3
- [ ] 4.2 Add `GET /app/v1/approvals/inbox` returning pending approval cards with opaque card/action handles and no tokens or numeric IDs in normal fields. Requirements: 3
- [ ] 4.3 Add `POST /app/v1/approvals/actions` mapping `approve_once`, `remember_for_project`, and `deny` to existing approval broker operations. Requirements: 3
- [ ] 4.4 Ensure “remember” derives allowlist matchers from the reviewed approval request and does not expose blank matcher forms in normal flow. Requirements: 3
- [ ] 4.5 Change `cmd/or3-intern/approvals_cmd.go` so no-arg `approvals` opens the pending inbox. Requirements: 3
- [ ] 4.6 Hide token, allowlist ID, plan ID, subject hash, and subject JSON in default approval CLI output; keep them behind `--advanced`. Requirements: 3, 8
- [ ] 4.7 Add regression tests for approval CLI defaults, advanced output, and facade action behavior. Requirements: 3, 8

## 5. Simplify devices and pairing defaults

- [ ] 5.1 Change `cmd/or3-intern/devices_cmd.go` so no-arg `devices` shows the connected-device manager/list. Requirements: 4
- [ ] 5.2 Update default device list output to show visible names, access labels, status, last used, and numbered/visible actions rather than raw device IDs. Requirements: 4, 8
- [ ] 5.3 Keep `devices list|requests|approve|deny|revoke|rotate` working for scripts, but label ID-based forms as advanced in help/copy. Requirements: 4
- [ ] 5.4 Update `cmd/or3-intern/connect_device_cmd.go` to omit `Request ID` in default pairing output and expose it only through advanced details. Requirements: 4, 8
- [ ] 5.5 Update `cmd/or3-intern/pairing_cmd.go` copy to prefer `approve-code` and move request-ID/channel-identity language into advanced help. Requirements: 4
- [ ] 5.6 Add `GET /app/v1/devices` and `POST /app/v1/devices/actions` using device cards and opaque action handles. Requirements: 4
- [ ] 5.7 Add tests for no-arg devices, hidden request ID in connect-device, and preserved advanced/scriptable commands. Requirements: 4, 8

## 6. Split default settings from advanced configuration

- [ ] 6.1 Update `internal/uxstate.BuildSettingsHomeView` so default sections are AI Provider, Workspace Folder, Safety, Connected Devices, Integrations, Memory, and App/Appearance where supported. Requirements: 5
- [ ] 6.2 Keep raw configure sections in `configureSections` for `--section` compatibility, but group or label advanced sections in interactive/default flows. Requirements: 5
- [ ] 6.3 Update `internal/uxcopy` labels and hints to avoid token budgets, MCP, secret store, raw config sections, network policy, and service listener in normal settings copy. Requirements: 5, 8
- [ ] 6.4 Add `GET /app/v1/settings/basic` returning settings cards and actions without raw config keys. Requirements: 5
- [ ] 6.5 Add tests for default settings copy and existing `configure --section` compatibility. Requirements: 5, 8

## 7. Add preset-based integration facade

- [ ] 7.1 Define integration preset descriptors for Local Files, GitHub, Browser, Database, and Custom MCP Server. Requirements: 6
- [ ] 7.2 Add `GET /app/v1/integrations` returning preset cards and current integration status summaries. Requirements: 6
- [ ] 7.3 Add `POST /app/v1/integrations/actions` for supported preset setup actions while reusing existing config validation/save paths. Requirements: 6
- [ ] 7.4 Keep raw MCP add/list/delete/test behavior in `/internal/v1/mcp/servers` and advanced/custom integration mode. Requirements: 6
- [ ] 7.5 Add tests that normal integration responses omit raw MCP command/args/env/header fields unless custom/advanced is requested. Requirements: 6, 8

## 8. Add file and terminal contextual facade slices

- [ ] 8.1 Add `GET /app/v1/files` and `/app/v1/files/list` that wrap existing roots/listing logic with labels, breadcrumbs, and actions while hiding `root_id` in normal fields. Requirements: 7
- [ ] 8.2 Add file action handling that resolves opaque folder/file context back to existing `root_id` + path and reuses path escape checks. Requirements: 7
- [ ] 8.3 Add `GET /app/v1/terminal` or `POST /app/v1/terminal/actions` for contextual terminal creation from workspace/current folder and default shell. Requirements: 7
- [ ] 8.4 Ensure normal terminal facade responses omit `session_id`, websocket ticket, `root_id`, rows, and cols; retain internal terminal endpoints for transport. Requirements: 7, 8
- [ ] 8.5 Add tests for file root hiding, path escape rejection, and terminal response redaction. Requirements: 7, 8

## 9. Add no-raw-IDs UX guard

- [ ] 9.1 Add a targeted Go scanner test, likely `cmd/or3-intern/consumer_ux_no_raw_ids_test.go`, for default CLI/user-copy strings. Requirements: 8
- [ ] 9.2 Seed the banned-term list from the audit: `session_key`, `session key`, `internSessionKey`, `parentSessionKey`, `request_id`, `job_id`, `device-id`, `device_id`, `pairing-request-id`, `approval ID`, `allowlist ID`, `scope-key`, `root_id`, `runner_id`, `anchor_message_id`, `token`, `fingerprint`, `Bubblewrap`, `MCP`, `allowlist`, and `inbound policy`. Requirements: 8
- [ ] 9.3 Add explicit allow patterns for tests, internal API paths, logs, developer docs, and strings containing `--advanced` or advanced/debug labels. Requirements: 8
- [ ] 9.4 Require each scanner exception to include a reason so default UX regressions are intentional. Requirements: 8

## 10. Validate rollout safety

- [ ] 10.1 Run focused Go tests for changed command/service files. Requirements: 1, 2, 3, 4, 5, 6, 7, 8
- [ ] 10.2 Run the existing Go workspace build task after implementation. Requirements: 1
- [ ] 10.3 Review route diffs to confirm no `/internal/v1` route, payload field, config key, or DB source-of-truth was removed or renamed. Requirements: 1
- [ ] 10.4 Smoke-test old `or3-app` calls against `/internal/v1/app/bootstrap`, chat sessions, approvals, files, and terminal routes before migrating app clients. Requirements: 1
- [ ] 10.5 Migrate `or3-app` to `/app/v1` gradually behind client-side capability detection after facade tests pass; treat realtime as optional progressive enhancement with HTTP fallback. Requirements: 1, 1A

## Out of scope

- [ ] Do not remove `/internal/v1` routes or ID-based CLI subcommands in this cleanup.
- [ ] Do not rewrite the agent runtime, channel bridge session-key model, approval broker, file sandboxing, or terminal transport.
- [ ] Do not require `or3-app` to switch all screens to `/app/v1` in the same backend patch.
- [ ] Do not expose raw tokens, websocket tickets, service secrets, or approval tokens in normal facade responses.
