# Consumer UX Facade Requirements

## Overview

This plan turns the audit into an additive, non-breaking consumer UX cleanup for `or3-intern`. The implementation must keep the current internal engine contracts available while adding simpler app/CLI surfaces where normal users choose visible things instead of typing raw internal identifiers.

Scope assumptions:

- `/internal/v1/*` remains the internal and scriptable API contract.
- Existing `or3-app` clients must keep working during rollout; new `/app/v1/*` routes are added before any app migration.
- `/app/v1` should not be WebSocket-only; use HTTP for bootstrap, durable state reads, and actions, with a realtime stream for live updates.
- Internal identifiers still exist in SQLite, logs, API tests, debug output, and explicit advanced/developer views.
- The first implementation should improve/simplify the highest-leak areas without rewriting the agent runtime, channel bridges, approval broker, or storage model.

## Requirements

### Requirement 1: Add a consumer API facade without breaking `/internal/v1`

**Engineering objective:** Add `/app/v1/*` routes that return screen-ready objects and actions while leaving current `/internal/v1/*` routes, request bodies, response bodies, auth behavior, and tests intact.

#### Acceptance Criteria

1. WHEN the service starts THEN existing `/internal/v1/*` routes SHALL remain registered and behave as before.
2. WHEN an app client calls `/app/v1/bootstrap` THEN it SHALL receive consumer mental-model sections for conversations, tasks, approvals, files, devices, settings, integrations, and activity.
3. WHEN `/app/v1/*` wraps internal data THEN it SHALL hide raw internal identifiers from normal fields and expose them only inside an `advanced` object or developer-only endpoint.
4. WHEN `or3-app` has not migrated to `/app/v1` THEN existing app/API behavior SHALL continue to pass current contract tests.
5. WHEN adding facade route tests THEN they SHALL assert additive route registration and no regression in representative `/internal/v1` routes.

### Requirement 1A: Provide realtime updates without making the facade WebSocket-only

**Engineering objective:** Make the app feel responsive with a realtime event channel while keeping request/response HTTP as the reliable baseline for app startup, refresh, actions, and compatibility.

#### Acceptance Criteria

1. WHEN an app needs initial state or refreshes a screen THEN it SHALL be able to use HTTP `/app/v1/*` endpoints without opening a WebSocket.
2. WHEN conversations, tasks, approvals, devices, files, terminal state, or activity change THEN the service SHOULD publish a screen-oriented event on a realtime endpoint such as `/app/v1/events` or `/app/v1/ws`.
3. WHEN the realtime connection drops or is unavailable THEN `or3-app` SHALL recover by refetching HTTP state without losing the ability to use the app.
4. WHEN a user triggers an action THEN the default contract SHALL remain an idempotent HTTP action request; realtime events MAY provide progress, completion, and invalidation updates.
5. WHEN implementing the realtime transport THEN it SHALL reuse existing auth/role checks, bounded event buffers, and no-secret/no-raw-ID redaction rules.

### Requirement 2: Use backend-owned conversation identifiers for app chat flows

**User story:** As an app user, I want to create, open, send messages in, and fork conversations without entering or seeing session keys, runner IDs, approval tokens, replay tool fields, or tool-policy JSON.

#### Acceptance Criteria

1. WHEN an app calls `POST /app/v1/conversations` with optional title and mode THEN the backend SHALL generate any required `session_key` and return a stable `conversation_id` handle.
2. WHEN an app calls conversation list/read/message endpoints THEN response objects SHALL use `conversation_id`, `title`, `mode`, `status`, timestamps, and message summaries instead of `session_key`, `parent_session_key`, `runner_chat_session_id`, or `runner_id` in normal fields.
3. WHEN an app sends a message through the facade THEN it SHALL pass `conversation_id`, `message`, and an optional predefined `mode`; it SHALL NOT pass raw `tool_policy`, `profile_name`, `approval_token`, or replay-tool-call fields.
4. WHEN an app forks from a visible message THEN it SHALL call an action using `conversation_id` and `message_id`; the backend SHALL generate `new_session_key` and preserve the existing fork logic internally.
5. IF a developer needs raw chat internals THEN those values SHALL remain available through `/internal/v1/chat-sessions` or an explicit advanced/debug payload, not default `/app/v1` responses.

### Requirement 3: Convert approvals into inbox cards and action buttons

**User story:** As a normal user, I want to review approval cards with clear allow/deny actions, so that I do not need to copy numeric approval IDs, tokens, allowlist IDs, plan IDs, or raw subject JSON.

#### Acceptance Criteria

1. WHEN a user runs `or3-intern approvals` with no subcommand THEN it SHALL show the pending approval inbox instead of a usage error.
2. WHEN the CLI prints an approval list/card in default mode THEN it SHALL show title, why, action summary, risk, and choices without printing approval tokens, allowlist IDs, plan IDs, subject hashes, or subject JSON.
3. WHEN `/app/v1/approvals/inbox` returns approval cards THEN each card SHALL include display text and action objects such as `approve_once`, `remember_for_project`, and `deny`.
4. WHEN a user chooses “always allow this exact action” THEN allowlist creation SHALL be derived from the reviewed approval request; users SHALL NOT need to fill a blank matcher form in the default flow.
5. IF `--advanced` is supplied or `/internal/v1/approvals` is used THEN existing numeric IDs and advanced details MAY remain available for scripts and debugging.

### Requirement 4: Make device and pairing management visible-name based by default

**User story:** As a user connecting a phone, browser, or app, I want to use a code, QR-style flow, and visible device names rather than request IDs, device IDs, pairing request IDs, or tokens.

#### Acceptance Criteria

1. WHEN `or3-intern connect-device` creates a pairing code THEN default output SHALL omit the pairing request ID and token values.
2. WHEN `or3-intern devices` runs with no subcommand THEN it SHALL show a connected-device manager/list instead of requiring `list`.
3. WHEN device management actions are offered in default CLI output THEN they SHALL refer to visible device names or numbered selections, not raw device IDs.
4. WHEN `/app/v1/devices` returns device cards THEN normal fields SHALL include name, access label, status, last used, and actions; raw device IDs SHALL be present only in `advanced`.
5. WHEN advanced/scriptable commands such as `devices revoke <device-id>` or `pairing approve <request-id>` remain available THEN help/copy SHALL label them advanced and continue to support automation.

### Requirement 5: Split normal settings from advanced configuration concepts

**User story:** As a consumer user, I want the default settings experience to show understandable categories while keeping advanced runtime and security knobs available separately.

#### Acceptance Criteria

1. WHEN a normal user opens settings/configure surfaces THEN the default categories SHALL be AI Provider, Workspace Folder, Safety, Connected Devices, Integrations, Memory, and App/Appearance where applicable.
2. WHEN advanced concepts appear, including MCP, token budgets, embedding dimensions, secret store, audit chain, inbound policy, network policy hosts, raw config export, Bubblewrap, and service listener, THEN they SHALL be hidden behind `--advanced`, advanced settings, or developer docs.
3. WHEN `configure --section` is used by scripts THEN existing section keys SHALL remain accepted for backward compatibility.
4. WHEN `uxcopy` or `uxstate` exposes settings labels THEN default labels SHALL prefer consumer language while preserving precise advanced labels.

### Requirement 6: Add preset-based integration setup while keeping raw MCP support

**User story:** As a user adding integrations, I want to choose common presets such as Local Files, GitHub, Browser, or Database before seeing raw custom MCP command fields.

#### Acceptance Criteria

1. WHEN `/app/v1/integrations` or CLI integration setup lists options THEN it SHALL show preset cards with plain descriptions and access levels.
2. WHEN a supported preset is selected THEN the UI/API SHALL ask only for user-understandable fields needed by that preset.
3. WHEN “Custom MCP Server” is selected or advanced mode is enabled THEN raw command, args, URL, headers, env, allowlist, and timeout fields MAY be shown.
4. WHEN the backend saves integration config THEN it SHALL continue using the existing config structures and validation paths.

### Requirement 7: Hide file root IDs and terminal session IDs behind contextual objects

**User story:** As an app user, I want files and terminal sessions to feel like workspace browser/actions rather than a raw API requiring root IDs, absolute paths, websocket tickets, or session IDs.

#### Acceptance Criteria

1. WHEN `/app/v1/files` returns roots or file listings THEN normal fields SHALL use labels, breadcrumbs, display paths, writable state, and action objects; `root_id` SHALL be advanced-only.
2. WHEN a file write/upload/mkdir action is exposed in the facade THEN the destination SHALL be represented by an opaque handle or previously selected folder context, not a user-entered raw root ID.
3. WHEN `/app/v1/terminal` creates a terminal THEN normal input SHALL be location choice and shell choice; rows, cols, websocket tickets, root IDs, and terminal session IDs SHALL stay internal or advanced.
4. WHEN path entry is necessary THEN it SHALL be labeled advanced path entry and still pass through existing workspace/path escape checks.

### Requirement 8: Add a no-raw-IDs guard for default UX copy

**Engineering objective:** Add a lightweight regression guard that catches raw internal terms in default user-facing copy while allowing internal API, logs, tests, docs, and explicit advanced/debug contexts.

#### Acceptance Criteria

1. WHEN the UX guard runs THEN it SHALL scan selected default CLI/user-copy files for banned raw terms such as `session_key`, `internSessionKey`, `request_id`, `job_id`, `device_id`, `pairing-request-id`, `approval ID`, `allowlist ID`, `scope-key`, `root_id`, `runner_id`, `anchor_message_id`, `token`, `fingerprint`, `Bubblewrap`, `MCP`, `allowlist`, and `inbound policy`.
2. WHEN a term appears only in allowed advanced/debug/internal contexts THEN the guard SHALL not fail.
3. WHEN a default user-facing string introduces a banned term outside an allowed context THEN the guard SHALL fail with the file path and term.
4. WHEN the allowlist is updated THEN tests SHALL require a reason/comment so the guard does not become a catch-all exception list.

## Non-functional constraints

- **Backward compatibility:** Do not remove or rename existing `/internal/v1` routes, JSON fields, CLI subcommands used by scripts, config keys, SQLite tables, or session keys.
- **Additive rollout:** Introduce `/app/v1` HTTP endpoints and default CLI improvements first; add realtime updates as progressive enhancement, then migrate `or3-app` only after facade tests pass.
- **SQLite safety:** Any new handle/mapping tables must be additive migrations with idempotent creation and no rewrite of existing chat/session/device/approval data.
- **Deterministic single-process behavior:** Keep facade resolution local to the existing service process and SQLite store; do not add external services.
- **Low memory usage:** Build screen-ready responses from bounded list queries and existing limits; do not cache unbounded conversation, approval, file, or terminal state.
- **Security:** Keep current service auth/role checks, file path restrictions, terminal role requirements, approval broker checks, secret redaction, and bounded request-body limits.
- **No secret leakage:** Tokens, service secrets, approval tokens, websocket tickets, fingerprints, and raw config values must not appear in default facade or CLI output.
- **or3-app safety:** The plan must be deployable while `or3-app` still uses current internal endpoints; app migration should be incremental and reversible.

