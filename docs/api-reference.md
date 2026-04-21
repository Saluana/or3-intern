# Internal service API reference

This page documents the authenticated HTTP API exposed by:

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
    "secret": "replace-with-a-long-random-shared-secret"
  }
}
```

Environment overrides documented in the README:

- `OR3_SERVICE_ENABLED`
- `OR3_SERVICE_LISTEN`
- `OR3_SERVICE_SECRET`

## Authentication

All routes require:

```http
Authorization: Bearer <signed-token>
```

The service refuses to start without `service.secret`.

Keep `service.listen` on loopback or private networking only.

## Endpoints

### `POST /internal/v1/turns`

Submits a foreground turn.

Behavior:

- returns Server-Sent Events when the request sends `Accept: text/event-stream`
- otherwise waits for completion and returns JSON

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

### `POST /internal/v1/subagents`

Queues a background subagent job through the shared subagent manager.

Request fields:

- canonical: `parent_session_key`, `task`, optional `prompt_snapshot`, `tool_policy`, `timeout_seconds`, `meta`, `profile_name`, `channel`, `reply_to`
- compatibility aliases also accepted: `session_key`, `intern_session_key`, `allowed_tools`, `timeout`, and the SDK camelCase forms (`parentSessionKey`, `sessionKey`, `internSessionKey`, `promptSnapshot`, `allowedTools`, `timeoutSeconds`, `profileName`, `replyTo`)

Parent session contract:

- `parent_session_key` is the canonical parent execution identity for subagent work.
- ingress aliases (`session_key`, `intern_session_key`, `parentSessionKey`, `sessionKey`, `internSessionKey`) are compatibility shims only; they normalize to `parent_session_key` and are not used internally after decoding.
- provider-owned metadata such as `request_id`, `workspace_id`, and `network_session_id` may accompany the request, but they do not supersede `parent_session_key`.

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
```

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

### `GET /internal/v1/jobs/{jobId}`

Returns the current job snapshot, including lifecycle events, timestamps, and the latest completion or error payload.

### `GET /internal/v1/jobs/{jobId}/stream`

Attaches to a live SSE stream for a turn or background job.

### `POST /internal/v1/jobs/{jobId}/abort`

Requests cancellation of a running job when cancellation is possible.

### `GET /internal/v1/health`

Returns a lightweight service health report for the shared runtime and job registry.

### `GET /internal/v1/readiness`

Returns the current startup/readiness evaluation for `service`, including the doctor summary and findings. Returns HTTP 503 when blocking startup findings are present.

### `GET /internal/v1/capabilities`

Returns the same machine-readable capabilities report exposed by `or3-intern capabilities --json`. Optional filters:

- `?channel=<name>`
- `?trigger=<name>`

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
