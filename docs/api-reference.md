# Internal service REST / HTTP API reference

This page documents the authenticated machine-facing REST / HTTP API exposed by:

```bash
go run ./cmd/or3-intern service
```

## Intended use

`or3-intern service` is a loopback/private-network API intended for integrations such as OR3 Net. It uses the same runtime, tool registry, memory system, quotas, and subagent manager as the CLI and channel entrypoints.

Command boundary:

- `or3-intern serve` hosts channels, triggers, workers, heartbeat, and background orchestration.
- `or3-intern service` hosts the authenticated machine-facing control plane.

## Service configuration

```json
{
  "service": {
    "enabled": true,
    "listen": "127.0.0.1:9100",
    "secret": "replace-with-a-long-random-shared-secret",
    "trustedBrowserOrigins": ["http://100.x.y.z:3060"],
    "trustedBrowserCIDRs": ["100.64.0.0/10"]
  }
}
```

`trustedBrowserOrigins` is an exact-origin CORS allowlist for browser apps that call the authenticated service API from a non-loopback private-network address. `trustedBrowserCIDRs` limits which remote client IPs can use those origins. Both fields accept multiple entries.

Environment overrides documented in the README:

- `OR3_SERVICE_ENABLED`
- `OR3_SERVICE_LISTEN`
- `OR3_SERVICE_SECRET`
- `OR3_SERVICE_TRUSTED_BROWSER_ORIGINS`
- `OR3_SERVICE_TRUSTED_BROWSER_CIDRS`

## Authentication

Most routes require:

```http
Authorization: Bearer <signed-token>
```

Authentication modes:

- shared-secret bearer token → full admin access
- paired-device bearer token with `operator` role → operator/admin control-plane access
- unauthenticated bootstrap is allowed only for:
  - `POST /internal/v1/pairing/requests`
  - `POST /internal/v1/pairing/exchange`

The service refuses to start without `service.secret`.

Keep `service.listen` on loopback or private networking only.

Bearer token notes:

- shared-secret bearer tokens are HMAC-signed and time-bounded
- paired-device tokens come from the pairing flow and are validated by the approval broker
- malformed or missing bearer tokens return `401`
- role failures return `403`

## Conventions

- request and response bodies use JSON unless the route explicitly streams SSE
- unknown JSON fields are rejected
- trailing garbage after JSON bodies is rejected
- validation/decode failures return `400`
- unsupported methods return `405`
- unknown routes return `404`
- routes backed by unavailable subsystems return `503` when the failure is infrastructural rather than user input

## Route inventory

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| `POST` | `/internal/v1/turns` | Admin or Operator | Run a foreground turn and wait for JSON or attach via SSE-on-submit. |
| `POST` | `/internal/v1/subagents` | Admin or Operator | Queue a background subagent job. |
| `GET` | `/internal/v1/jobs/{jobId}` | Admin or Operator | Fetch current job snapshot. |
| `GET` | `/internal/v1/jobs/{jobId}/stream` | Admin or Operator | Attach to live SSE lifecycle stream. |
| `POST` | `/internal/v1/jobs/{jobId}/abort` | Admin or Operator | Request cancellation. |
| `POST` | `/internal/v1/pairing/requests` | No | Start pairing bootstrap. |
| `GET` | `/internal/v1/pairing/requests` | Operator | List pairing requests. |
| `POST` | `/internal/v1/pairing/requests/{id}/approve` | Operator | Approve pairing request. |
| `POST` | `/internal/v1/pairing/requests/{id}/deny` | Operator | Deny pairing request. |
| `POST` | `/internal/v1/pairing/exchange` | No | Exchange approved pairing code for device token. |
| `GET` | `/internal/v1/devices` | Operator | List paired devices. |
| `POST` | `/internal/v1/devices/{deviceId}/revoke` | Operator | Revoke paired device. |
| `POST` | `/internal/v1/devices/{deviceId}/rotate` | Operator | Rotate paired-device token. |
| `GET` | `/internal/v1/approvals` | Operator | List approval requests. |
| `GET` | `/internal/v1/approvals/{id}` | Operator | Fetch one approval request. |
| `POST` | `/internal/v1/approvals/{id}/approve` | Operator | Approve request and optionally allowlist it. |
| `POST` | `/internal/v1/approvals/{id}/deny` | Operator | Deny request. |
| `POST` | `/internal/v1/approvals/{id}/cancel` | Operator | Cancel request without approving/denying execution. |
| `POST` | `/internal/v1/approvals/expire` | Operator | Expire all pending requests past TTL. |
| `GET` | `/internal/v1/approvals/allowlists` | Operator | List allowlist rules. |
| `POST` | `/internal/v1/approvals/allowlists` | Operator | Create allowlist rule. |
| `POST` | `/internal/v1/approvals/allowlists/{id}/remove` | Operator | Disable allowlist rule. |
| `GET` | `/internal/v1/health` | Admin or Operator | Lightweight runtime/job health. |
| `GET` | `/internal/v1/readiness` | Admin or Operator | Startup/readiness evaluation. |
| `GET` | `/internal/v1/capabilities` | Admin or Operator | Effective machine-readable runtime posture. |
| `GET` | `/internal/v1/embeddings/status` | Operator | Embedding compatibility/reporting. |
| `POST` | `/internal/v1/embeddings/rebuild` | Operator | Rebuild memory/doc embeddings. |
| `GET` | `/internal/v1/audit` | Operator | Audit chain status summary. |
| `POST` | `/internal/v1/audit/verify` | Operator | Verify audit chain integrity. |
| `POST` | `/internal/v1/scope/links` | Operator | Link session key to logical scope. |
| `GET` | `/internal/v1/scope/resolve` | Operator | Resolve scope for one session key. |
| `GET` | `/internal/v1/scope/sessions` | Operator | List sessions within one scope. |
| `GET` | `/internal/v1/configure/sections` | Operator | List configure sections and current status summaries. |
| `GET` | `/internal/v1/configure/fields` | Operator | List editable fields for a section (and channel for `channels`). |
| `POST` | `/internal/v1/configure/apply` | Operator | Apply one or more configure-field mutations and persist config. |
| `GET` | `/internal/v1/files/roots` | Operator | List root-scoped folders available for browsing. |
| `GET` | `/internal/v1/files/list` | Operator | List a directory under a configured file root. |
| `GET` | `/internal/v1/files/stat` | Operator | Return metadata for one file or folder under a root. |
| `GET` | `/internal/v1/files/download` | Operator | Download one file under a root. |
| `POST` | `/internal/v1/files/upload` | Operator | Upload one file into a writable root directory without overwriting. |
| `POST` | `/internal/v1/files/mkdir` | Operator | Create one directory under a writable root. |
| `POST` | `/internal/v1/files/delete` | Operator | Disabled in v1; returns `403` for safety. |
| `POST` | `/internal/v1/terminal/sessions` | Operator | Start a bounded shell session inside a configured root. |
| `GET` | `/internal/v1/terminal/sessions/{sessionId}` | Operator | Fetch terminal session metadata and current status. |
| `GET` | `/internal/v1/terminal/sessions/{sessionId}/stream` | Operator | Attach to live terminal SSE output and lifecycle events. |
| `POST` | `/internal/v1/terminal/sessions/{sessionId}/input` | Operator | Send stdin to a running terminal session. |
| `POST` | `/internal/v1/terminal/sessions/{sessionId}/resize` | Operator | Update remembered terminal size metadata. |
| `POST` | `/internal/v1/terminal/sessions/{sessionId}/close` | Operator | Close an active terminal session. |

## Endpoints

### `POST /internal/v1/turns`

Submits a foreground turn.

Behavior:

- returns Server-Sent Events when the request sends `Accept: text/event-stream`
- otherwise waits for completion and returns JSON
- `X-Request-Id`, `X-Workspace-Id`, and `X-Network-Session-Id` headers are propagated into request/job lifecycle metadata
- `X-Approval-Token` and `X-Or3-Approval-Token` are accepted as approval-token header aliases

Request fields:

- canonical: `session_key`, `message`, optional `tool_policy`, `meta`, `profile_name`
- compatibility aliases also accepted: `intern_session_key`, `allowed_tools`, and the SDK camelCase forms (`sessionKey`, `internSessionKey`, `allowedTools`, `profileName`)

Session identity contract:

- `session_key` is the canonical `or3-intern` execution identity for turns, memory, and persisted messages.
- `or3-net` may bind its own durable `network_session_id` to a `session_key`, but that binding remains external to `or3-intern`; the service accepts it only as metadata/header context and does not replace `session_key` with it.
- if `or3-intern` needs a logical grouping across multiple physical session keys, it uses `session_links.scope_key`; it does not rename the execution-session field.
- aliases are accepted only at the HTTP ingress layer and are normalized immediately to `session_key` before runtime execution.

`tool_policy` uses the OR3 Net shape:

```json
{
  "mode": "allow_all | deny_all | allow_list | deny_list",
  "allowed_tools": ["read_file"],
  "blocked_tools": ["exec"]
}
```

Synchronous JSON response shape:

```json
{
  "job_id": "job_123",
  "kind": "turn",
  "status": "completed",
  "final_text": "hello"
}
```

If the turn fails, the same response includes `error` and typically returns `502`.

### `POST /internal/v1/subagents`

Queues a background subagent job through the shared subagent manager.

Request fields:

- canonical: `parent_session_key`, `task`, optional `prompt_snapshot`, `tool_policy`, `timeout_seconds`, `meta`, `profile_name`, `channel`, `reply_to`
- compatibility aliases also accepted: `session_key`, `intern_session_key`, `allowed_tools`, `timeout`, and the SDK camelCase forms (`parentSessionKey`, `sessionKey`, `internSessionKey`, `promptSnapshot`, `allowedTools`, `timeoutSeconds`, `profileName`, `replyTo`)

Parent session contract:

- `parent_session_key` is the canonical parent execution identity for subagent work.
- ingress aliases (`session_key`, `intern_session_key`, `parentSessionKey`, `sessionKey`, `internSessionKey`) are compatibility shims only; they normalize to `parent_session_key` and are not used internally after decoding.
- provider-owned metadata such as `request_id`, `workspace_id`, and `network_session_id` may accompany the request, but they do not supersede `parent_session_key`.

Accepted response shape:

```json
{
  "job_id": "job_123",
  "child_session_key": "subagent:abc",
  "status": "queued"
}
```

When the subagent queue is full, the route returns `429`.

### Approval and pairing endpoints

These endpoints require the approval broker to be configured (`security.approvals.enabled: true`).

Authentication for these routes accepts either the existing shared-secret bearer token (admin access) or a paired device token with the `operator` role. The `POST /internal/v1/pairing/requests` and `POST /internal/v1/pairing/exchange` endpoints are unauthenticated so that remote clients can initiate the pairing flow.

Audit records for operator-originated actions include `auth_kind: "shared-secret"` or `auth_kind: "paired-device"` to distinguish the authentication method.

#### Pairing

| Method | Path | Auth required | Description |
| --- | --- | --- | --- |
| `POST` | `/internal/v1/pairing/requests` | No | Create a pairing request. Returns `id`, `device_id`, `code`, `role`, `expires_at`. |
| `GET` | `/internal/v1/pairing/requests` | Operator | List pairing requests. Optional `?status=` filter. |
| `POST` | `/internal/v1/pairing/requests/{id}/approve` | Operator | Approve a pending pairing request. |
| `POST` | `/internal/v1/pairing/requests/{id}/deny` | Operator | Deny a pending pairing request. |
| `POST` | `/internal/v1/pairing/exchange` | No | Exchange an approved code for a device token. Returns `device_id`, `role`, `token`. The token is shown once. |

`POST /internal/v1/pairing/requests` request body:

```json
{
  "role": "operator",
  "display_name": "My Laptop",
  "origin": "optional origin metadata",
  "device_id": "optional stable device id"
}
```

`POST /internal/v1/pairing/exchange` request body:

```json
{
  "request_id": 42,
  "code": "123456"
}
```

#### Devices

| Method | Path | Auth required | Description |
| --- | --- | --- | --- |
| `GET` | `/internal/v1/devices` | Operator | List paired devices. |
| `POST` | `/internal/v1/devices/{deviceId}/revoke` | Operator | Revoke a paired device. Returns `{ "device_id": "...", "status": "revoked" }`. |
| `POST` | `/internal/v1/devices/{deviceId}/rotate` | Operator | Rotate a device token. Returns `{ "device_id": "...", "token": "..." }`. The new token is shown once. |

#### Approvals

| Method | Path | Auth required | Description |
| --- | --- | --- | --- |
| `GET` | `/internal/v1/approvals` | Operator | List approval requests. Optional `?status=` and `?type=` filters. |
| `GET` | `/internal/v1/approvals/{id}` | Operator | Get a single approval request with full subject JSON. |
| `POST` | `/internal/v1/approvals/{id}/approve` | Operator | Approve a pending request. Returns `{ "token": "..." }` (shown once) and optional `allowlist_id`. |
| `POST` | `/internal/v1/approvals/{id}/deny` | Operator | Deny a pending request. |
| `POST` | `/internal/v1/approvals/{id}/cancel` | Operator | Cancel a pending request without an approval decision. |
| `POST` | `/internal/v1/approvals/expire` | Operator | Marks all expired pending requests as expired. |
| `GET` | `/internal/v1/approvals/allowlists` | Operator | List allowlist rules. Optional `?domain=` filter. |
| `POST` | `/internal/v1/approvals/allowlists` | Operator | Create a new allowlist rule. |
| `POST` | `/internal/v1/approvals/allowlists/{id}/remove` | Operator | Disable an allowlist rule. |

`POST /internal/v1/approvals/{id}/approve` request body:

```json
{
  "allowlist": false,
  "note": "approved for this session"
}
```

Set `"allowlist": true` to also create a persistent allowlist rule from the subject.

`POST /internal/v1/approvals/{id}/cancel` request body:

```json
{
  "note": "optional operator note"
}

### Configure endpoints

These endpoints expose the same field-level configuration surface used by `or3-intern configure` / settings TUI, so remote operators can automate all section/channel edits over REST.

`GET /internal/v1/configure/sections` response:

```json
{
  "items": [
    {
      "key": "provider",
      "label": "Provider",
      "description": "API endpoint, chat model, embeddings, timeouts, and provider secrets",
      "status": "OpenAI · gpt-4.1-mini · embed=text-embedding-3-small"
    }
  ]
}
```

`GET /internal/v1/configure/fields?section=provider` response includes the field metadata used by the TUI (`key`, `label`, `kind`, `choices`, `value`, `description`). For `section=channels`, pass `channel=<telegram|slack|discord|whatsapp|email>`.

`POST /internal/v1/configure/apply` request:

```json
{
  "changes": [
    { "section": "provider", "field": "provider_model", "op": "set", "value": "gpt-4.1" },
    { "section": "service", "field": "service_enabled", "op": "toggle" },
    { "section": "channels", "channel": "slack", "field": "access", "op": "choose", "value": "allowlist" }
  ]
}
```

`op` values:

- `set` (default): set a text/secret/list field value
- `toggle`: flip a boolean field
- `choose`: select one option for a choice field (for example channel `access`)

`POST /internal/v1/approvals/allowlists` request body:

```json
{
  "domain": "exec",
  "scope": {
    "host_id": "local",
    "tool": "exec"
  },
  "matcher": {
    "executable_path": "/bin/echo"
  },
  "expires_at": 0
}
```

For `skill_execution` rules, `matcher` uses the skill matcher fields (`skill_id`, `version`, `origin`, `trust_state`, `script_hash`, `timeout_seconds`).

#### Approval token lifecycle

1. A tool invocation triggers an approval request in `pending` status.
2. An operator approves the request via CLI or HTTP.
3. An approval token is returned once (never stored in plaintext server-side).
4. The tool retries with the token in context; the broker re-derives the subject hash and verifies the token signature, host ID, and expiry.
5. On match: execution proceeds. On mismatch or expiry: a new approval request is created.

Approval tokens are HMAC-signed and include the request ID, subject hash, host ID, and expiry. They are not reusable across different execution contexts or hosts.

#### Future phases (not in v1)

The following capabilities are compatible with this schema and token format but are **not yet implemented**:

- Chat-channel approval routing (resolve requests by replying in Telegram, Slack, etc.)
- Web browser UI for approval resolution
- Secret-access approval gating (`secret_access` domain)
- Outbound-message approval gating (`message_send` domain)
- File-transfer approval gating (`file_transfer` domain)
- Remote-node and `or3-sandbox` verification using shared signing keys
- Remote-node forwarding of approval decisions

### File endpoints

File endpoints expose a small file-portal-style browser for trusted private-network clients. Every path is resolved under a configured root and traversal outside that root is rejected.

Roots are derived from `allowedDir`, `workspaceDir`, and `artifactsDir`. If none are configured, the service exposes the current working directory as `cwd`.

`GET /internal/v1/files/roots` response:

```json
{
  "items": [
    { "id": "workspace", "label": "Workspace", "path": "/Users/me/project", "writable": true }
  ]
}
```

`GET /internal/v1/files/list?root_id=workspace&path=.` response:

```json
{
  "root_id": "workspace",
  "path": ".",
  "entries": [
    { "name": "README.md", "path": "README.md", "type": "file", "size": 1024, "modified_at": "2026-04-26T10:00:00Z", "mime_type": "text/markdown; charset=utf-8" }
  ]
}
```

`GET /internal/v1/files/stat?root_id=workspace&path=README.md` returns `{ "root_id": "workspace", "item": ... }`.

`GET /internal/v1/files/download?root_id=workspace&path=README.md` streams the file with `http.ServeContent`.

`POST /internal/v1/files/upload` accepts `multipart/form-data` fields `root_id`, `path`, and `file`. Uploads use create-only semantics and return `409` instead of overwriting an existing file.

`POST /internal/v1/files/mkdir` request:

```json
{ "root_id": "workspace", "path": ".", "name": "Notes" }
```

`POST /internal/v1/files/delete` is intentionally disabled in v1 and returns `403`.

### Terminal endpoints

Terminal endpoints expose a bounded shell bridge for trusted private-network operators. Sessions are in-memory, root-scoped, capped to a small concurrent set, and expire automatically after a short TTL.

Availability requirements:

- operator auth
- `hardening.guardedTools=true`
- `hardening.privilegedTools=true`
- `hardening.enableExecShell=true`
- runtime profile must permit shell execution
- runtime profiles that require sandboxed exec do not expose terminal sessions in v1

`POST /internal/v1/terminal/sessions` request:

```json
{
  "root_id": "workspace",
  "path": ".",
  "shell": "sh",
  "rows": 28,
  "cols": 100,
  "approval_token": "optional-approved-token"
}
```

Behavior notes:

- returns `201` with a terminal session snapshot when started
- returns `409` with `requires_approval=true` and `request_id` when exec approval policy requires confirmation
- returns `503` when shell mode is disabled by host posture/config
- create requests run through the same exec approval policy used by other privileged execution paths

`GET /internal/v1/terminal/sessions/{sessionId}/stream` emits SSE events such as:

- `snapshot`
- `status`
- `output`
- `input`
- `resize`
- `error`

Example output event:

```text
event: output
data: {"session_id":"term_171234_1","stream":"stdout","chunk":"hello\n"}
```

`POST /internal/v1/terminal/sessions/{sessionId}/resize` currently updates client-visible metadata only; it does not yet emulate a full PTY resize.

### `GET /internal/v1/jobs/{jobId}`

Returns the current job snapshot, including lifecycle events, timestamps, and the latest completion or error payload.

Typical response fields:

- `job_id`
- `kind`
- `status`
- `created_at`
- `updated_at`
- `events`
- latest completion payload such as `final_text`
- latest failure payload such as `error`

### `GET /internal/v1/jobs/{jobId}/stream`

Attaches to a live SSE stream for a turn or background job.

Lifecycle event types currently emitted include queued/start/completion/error transitions. Unknown job IDs return `404`.

### `POST /internal/v1/jobs/{jobId}/abort`

Requests cancellation of a running job when cancellation is possible.

Behavior notes:

- returns `200` for successful abort requests
- returns `200` for already-completed jobs with final status
- returns `404` for unknown jobs
- returns `409` when a known job is not abortable

### `GET /internal/v1/health`

Returns a lightweight service health report for the shared runtime and job registry.

Current response fields:

- `status`
- `runtimeAvailable`
- `jobRegistryAvailable`
- `subagentManagerEnabled`
- `approvalBrokerAvailable`

### `GET /internal/v1/readiness`

Returns the current startup/readiness evaluation for `service`, including the doctor summary and findings. Returns HTTP 503 when blocking startup findings are present.

Current response fields:

- `status`
- `ready`
- `summary`
- `findings`

### `GET /internal/v1/capabilities`

Returns the same machine-readable capabilities report exposed by `or3-intern capabilities --json`. Optional filters:

- `?channel=<name>`
- `?trigger=<name>`

The report includes runtime profile, hosted posture, approval modes, sandbox/exec availability, network policy, enabled MCP servers, and effective channel/trigger ingress summaries.

### Embeddings endpoints

These routes expose the same embedding compatibility and rebuild operations used by the local CLI command.

| Method | Path | Auth required | Description |
| --- | --- | --- | --- |
| `GET` | `/internal/v1/embeddings/status` | Operator | Returns the current embedding fingerprint/status report for persisted memory vectors and doc-index readiness. |
| `POST` | `/internal/v1/embeddings/rebuild` | Operator | Rebuilds persisted embeddings for `memory`, `docs`, or `all`. |

`POST /internal/v1/embeddings/rebuild` request body:

```json
{
  "target": "memory"
}
```

If `target` is omitted, the service defaults to `memory`.

Request-body alias note:

- `target`
- `targetName`

Current response fields:

- `status`
- `target`
- `fingerprint`
- `memoryNotesRebuilt`
- `docsRebuilt`
- `skipped`

### Audit endpoints

These routes expose the append-only audit chain status and verification flow already used by local operators.

| Method | Path | Auth required | Description |
| --- | --- | --- | --- |
| `GET` | `/internal/v1/audit` | Operator | Returns audit configuration/runtime status plus event-count summary. |
| `POST` | `/internal/v1/audit/verify` | Operator | Verifies the persisted audit chain and returns a machine-readable success response. |

`GET /internal/v1/audit` response fields currently include:

- `status`
- `enabled`
- `available`
- `strict`
- `verifyOnStart`
- `eventCount`
- latest-event summary fields when any records exist

`POST /internal/v1/audit/verify` response:

```json
{
  "verified": true,
  "eventCount": 42
}
```

### Scope endpoints

These routes expose the session-scope linking helpers used by the local `scope` CLI command.

| Method | Path | Auth required | Description |
| --- | --- | --- | --- |
| `POST` | `/internal/v1/scope/links` | Operator | Links a physical `session_key` to a logical `scope_key`. |
| `GET` | `/internal/v1/scope/resolve?session_key=...` | Operator | Resolves the effective logical scope for one physical session key. |
| `GET` | `/internal/v1/scope/sessions?scope_key=...` | Operator | Lists physical session keys linked to a logical scope. |

`POST /internal/v1/scope/links` request body:

```json
{
  "session_key": "telegram:123",
  "scope_key": "customer:acme"
}
```

Accepted aliases:

- `session_key` / `sessionKey`
- `scope_key` / `scopeKey`

Response shapes:

```json
{
  "session_key": "telegram:123",
  "scope_key": "customer:acme"
}
```

```json
{
  "scope_key": "customer:acme",
  "sessions": ["telegram:123", "slack:U42"]
}
```

```json
{
  "session_key": "telegram:123",
  "scope_key": "customer:acme"
}
```

## Operational guidance

- use strong random shared secrets
- do not expose the listener publicly without additional network controls
- run `or3-intern doctor` to catch weak secrets or risky bind addresses
- if `security.network` is enabled, remember that outbound API calls from tools/providers still have to satisfy that policy

## Related documentation

- [Security and hardening](security-and-hardening.md)
- [Agent runtime](agent-runtime.md)
- [CLI reference](cli-reference.md)

## Related code

- `cmd/or3-intern/service.go`
- `cmd/or3-intern/service_auth.go`

## Compatibility contract

The following aliases are part of the stable v1 contract and are covered by CI compatibility tests in `cmd/or3-intern/service_test.go`.
Fixture-pinned request and response shapes live in `cmd/or3-intern/testdata/service_contract/` and are exercised by `TestOr3NetCompatibilityFixtures`.

## Session key ownership

- `session_key` is owned by `or3-intern` and remains the only canonical execution-session field in service requests, DB rows, job events, and runtime APIs.
- `network_session_id` is owned by `or3-net`; when present, it is propagated as request metadata (`X-Network-Session-Id`, request `meta`, and service lifecycle payloads) so `or3-net` can correlate work without changing the `or3-intern` session model.
- alias drift is intentionally contained to the service ingress boundary:
  - turn requests normalize `session_key`, `intern_session_key`, `sessionKey`, `internSessionKey` → `session_key`
  - subagent requests normalize `parent_session_key`, `session_key`, `intern_session_key`, `parentSessionKey`, `sessionKey`, `internSessionKey` → `parent_session_key`
  - no internal package should introduce new aliases such as `session_id` for these service contracts without an explicit compatibility test update.

**`POST /internal/v1/turns` — session key aliases** (all resolve to `session_key`):
`session_key`, `intern_session_key`, `sessionKey`, `internSessionKey`

**`POST /internal/v1/turns` — tool policy aliases**:
`tool_policy` / `toolPolicy`; `allowed_tools` / `allowedTools`; `blocked_tools` / `blockedTools`

**`POST /internal/v1/subagents` — parent session key aliases** (all resolve to `parent_session_key`):
`parent_session_key`, `session_key`, `intern_session_key`, `parentSessionKey`, `sessionKey`, `internSessionKey`

**`POST /internal/v1/subagents` — timeout aliases** (all resolve to `timeout_seconds`):
`timeout_seconds`, `timeoutSeconds`, `timeout`

**Stable job routes:**
- `GET /internal/v1/jobs/{jobId}/stream` — returns 404 for unknown jobs
- `POST /internal/v1/jobs/{jobId}/abort` — returns 404 for unknown, 200 for completed

Any change that removes or renames a supported alias, or alters job route behaviour, will fail the `TestV1*` tests in CI.
