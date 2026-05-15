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

`/internal/v1` request bodies use snake_case as the canonical field convention. Existing camelCase aliases remain accepted for compatibility. When both canonical and alias fields are present with different values, the snake_case value wins and the response includes `X-Or3-Request-Warning` with a non-fatal conflict message. A future `/internal/v2` can reject conflicting duplicate fields once app clients have migrated.

All non-2xx JSON service errors include these stable fields:

```json
{
  "error": "human-readable message",
  "code": "validation_failed",
  "request_id": "req_..."
}
```

Lowercase `code` values are app-facing service error codes. Current generic codes are:

| Code | Meaning |
| --- | --- |
| `validation_failed` | Request body, query, route parameter, or requested option is invalid. |
| `method_not_allowed` | The route exists but does not support the HTTP method. |
| `not_found` | The route or requested resource was not found. |
| `forbidden` | The authenticated actor lacks the required role or capability. |
| `unauthorized` | Authentication failed or no acceptable bearer credential was supplied. |
| `rate_limited` | The request was deferred by service or auth rate limiting. |
| `capability_unavailable` | A backing subsystem or configured capability is unavailable. |
| `request_too_large` | The request body exceeded the endpoint limit. |
| `conflict` | The request conflicts with current resource state. |
| `timeout` | The operation timed out. |
| `request_failed` | Generic fallback for failures that do not fit a narrower code. |

Auth policy challenges keep their uppercase challenge codes (`SESSION_REQUIRED`, `SESSION_EXPIRED`, `PASSKEY_REQUIRED`, `STEP_UP_REQUIRED`, `AUTH_UNSUPPORTED`) so existing app challenge handling remains compatible.

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
| `GET` | `/internal/v1/agent-runners` | Admin or Operator | Enumerate installed external agent CLIs and their readiness states. |
| `POST` | `/internal/v1/agent-runs` | Admin or Operator | Enqueue a background run on an external agent CLI (OpenCode, Codex, Claude, or Gemini). |
| `GET` | `/internal/v1/agent-runs` | Admin or Operator | List recent agent CLI runs. |
| `GET` | `/internal/v1/agent-runs/{id}` | Admin or Operator | Read a persisted CLI run by run ID (acr_…) or job ID. |
| `GET` | `/internal/v1/agent-runs/{id}/events?after_seq=N` | Admin or Operator | Fetch durable events for a CLI run (supports reconnect). |
| `GET` | `/internal/v1/health` | Admin or Operator | Lightweight runtime/job health. |
| `GET` | `/internal/v1/readiness` | Admin or Operator | Startup/readiness evaluation. |
| `GET` | `/internal/v1/capabilities` | Admin or Operator | Effective machine-readable runtime posture. |
| `GET` | `/internal/v1/app/bootstrap` | Admin or Operator | Return an app-shaped host overview with pairing/auth/status/count/action summaries. |
| `POST` | `/internal/v1/actions/restart-service` | Operator | Request a structured service restart without opening a terminal session. |
| `GET` | `/internal/v1/mcp/servers` | Operator | List configured MCP servers with current runtime status. |
| `POST` | `/internal/v1/mcp/servers` | Operator | Add or update one MCP server in config. |
| `DELETE` | `/internal/v1/mcp/servers/{name}` | Operator | Remove one MCP server from config. |
| `POST` | `/internal/v1/mcp/servers/{name}/test` | Operator | Test one saved MCP server config with a temporary manager. |
| `GET` | `/internal/v1/cron/status` | Operator | Return scheduler availability, job count, and next wake time. |
| `GET` | `/internal/v1/cron/jobs` | Operator | List scheduled jobs. |
| `POST` | `/internal/v1/cron/jobs` | Operator | Create a scheduled job. |
| `GET` | `/internal/v1/cron/jobs/{id}` | Operator | Fetch one scheduled job. |
| `PATCH` | `/internal/v1/cron/jobs/{id}` | Operator | Replace one scheduled job definition. |
| `POST` | `/internal/v1/cron/jobs/{id}/run` | Operator | Run one scheduled job immediately. |
| `POST` | `/internal/v1/cron/jobs/{id}/pause` | Operator | Pause one scheduled job. |
| `POST` | `/internal/v1/cron/jobs/{id}/resume` | Operator | Resume one scheduled job. |
| `DELETE` | `/internal/v1/cron/jobs/{id}` | Operator | Delete one scheduled job. |
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
- includes `X-Or3-Job-Id` on both JSON and SSE responses so clients can persist the turn job ID immediately
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

### MCP server management

MCP management routes require operator access and write the primary config file. Runtime MCP tools are loaded at startup, so successful writes return `restartRequired: true`.

`GET /internal/v1/mcp/servers` response:

```json
{
  "servers": [
    {
      "name": "filesystem",
      "config": {
        "enabled": true,
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]
      },
      "status": {
        "connected": true,
        "toolCount": 3,
        "tools": ["mcp_filesystem_read_file"]
      }
    }
  ]
}
```

`POST /internal/v1/mcp/servers` adds or updates one server:

```json
{
  "name": "filesystem",
  "config": {
    "enabled": true,
    "transport": "stdio",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
    "connectTimeoutSeconds": 10,
    "toolTimeoutSeconds": 30
  }
}
```

`DELETE /internal/v1/mcp/servers/{name}` removes one configured server.

`POST /internal/v1/mcp/servers/{name}/test` creates a temporary MCP manager for the saved server config, connects once, reports discovered tools, and closes the manager. It does not hot-reload the runtime manager.

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

### External Agent CLI Delegation

The external agent CLI subsystem allows `or3-intern` to queue and supervise non-interactive runs of OpenCode, Codex, Claude Code, and Gemini CLI from the internal service API. Each run is a child process managed by a dedicated worker pool, with output streamed through the existing job registry and persisted in SQLite.

#### Mode and isolation policy

Every run specifies a **mode** and an **isolation** boundary:

| Mode | Isolation required | Behaviour |
|------|-------------------|-----------|
| `review` | `host_readonly` or `sandbox_workspace_write` | Read-only analysis; no filesystem mutations. |
| `safe_edit` | `host_workspace_write` or `sandbox_workspace_write` | Non-interactive edits with the CLI's built-in safety flags. |
| `sandbox_auto` | `sandbox_dangerous` | Full autonomy inside a sandbox; rejected unless `agentCLI.allowSandboxAuto` is `true`. |

The default mode is `safe_edit` with `host_workspace_write` isolation. `sandbox_auto` is rejected on host machines regardless of config — it requires a true sandbox runtime.

#### Runner readiness

Each external CLI has four possible states:

| Status          | Meaning |
|-----------------|---------|
| `available`     | Binary found, version probe passed, auth is ready. |
| `missing`       | Binary not on `PATH`. |
| `auth_missing`  | Binary found but required auth check failed. |
| `disabled_by_config` | Runner listed in `agentCLI.disabledRunners`. |

### `GET /internal/v1/agent-runners`

Returns the detection status for every registered runner, including `or3-intern` as an always-available default:

```json
{
  "runners": [
    {
      "id": "or3-intern",
      "display_name": "OR3 Intern",
      "status": "available",
      "auth_status": "ready"
    },
    {
      "id": "opencode",
      "display_name": "OpenCode",
      "binary_name": "opencode",
      "binary_path": "/usr/local/bin/opencode",
      "version": "opencode 1.0.0",
      "status": "available",
      "auth_status": "ready"
    }
  ]
}
```

### `POST /internal/v1/agent-runs`

Enqueues a background external CLI run. Request fields (all snake_case, `DisallowUnknownFields`):

| Field                | Type    | Required | Default                  |
|----------------------|---------|----------|--------------------------|
| `parent_session_key` | string  | **yes**  |                          |
| `runner_id`          | string  | **yes**  |                          |
| `task`               | string  | **yes**  |                          |
| `timeout_seconds`    | number  | no       | `agentCLI.defaultTimeoutSeconds` (900) |
| `cwd`                | string  | no       | service working directory |
| `model`              | string  | no       |                          |
| `mode`               | string  | no       | `agentCLI.defaultMode` (`safe_edit`) |
| `isolation`          | string  | no       | `agentCLI.defaultIsolation` (`host_workspace_write`) |
| `max_turns`          | number  | no       |                          |
| `meta`               | object  | no       |                          |

Example:

```json
{
  "parent_session_key": "session-123",
  "runner_id": "codex",
  "task": "fix the failing auth test in src/auth_test.go",
  "timeout_seconds": 600,
  "mode": "safe_edit",
  "isolation": "host_workspace_write",
  "max_turns": 5
}
```

Accepted response:

```json
{
  "job_id": "job-agentcli-abc123def456",
  "run_id": "acr_abc123def456",
  "status": "queued"
}
```

The run is also registered in the in-memory `JobRegistry` with kind `agent_cli:<runner_id>`, so `/internal/v1/jobs/{job_id}/stream` and `/internal/v1/jobs/{job_id}/abort` apply without any additional wiring.

### `GET /internal/v1/agent-runs/{id}`

Returns a persisted CLI run snapshot by run ID (`acr_…`) or job ID:

```json
{
  "job_id": "job-agentcli-abc123def456",
  "run_id": "acr_abc123def456",
  "kind": "agent_cli:codex",
  "runner_id": "codex",
  "parent_session_key": "session-123",
  "task": "fix the failing auth test",
  "mode": "safe_edit",
  "isolation": "host_workspace_write",
  "status": "succeeded",
  "exit_code": 0,
  "output_preview": "Fixed the auth test by...",
  "requested_at": "2025-06-01T12:00:00Z",
  "started_at": "2025-06-01T12:00:01Z",
  "completed_at": "2025-06-01T12:10:30Z",
  "timeout_seconds": 600,
  "attempts": 1
}
```

### `GET /internal/v1/agent-runs/{id}/events?after_seq=N`

Fetches durable persisted events for a CLI run. The `after_seq` parameter supports reconnect after the in-memory job registry has expired.

Event types emitted:

| Type | Contents |
|------|----------|
| `started` | `job_id`, `runner_id`, `argv_preview`, `cwd` |
| `output` | `seq`, `stream` (`stdout` or `stderr`), `chunk` |
| `structured` | `seq`, `payload` (JSON/JSONL line parsed from stdout) |
| `completion` | `exit_code`, `status`, `stdout_preview`, `stderr_preview`, `duration_ms` |
| `error` | `message` |

Events carry monotonic sequence numbers and RFC 3339 timestamps.

### Cancellation and abort

External CLI runs are cancellable through two paths:

- **Running jobs:** `POST /internal/v1/jobs/{job_id}/abort` triggers the registered cancel function. On Unix the process group receives SIGTERM followed by SIGKILL after a 2-second grace period; on Windows only the direct process is killed.
- **Queued jobs:** The abort falls through to the persisted DB layer and marks the run as `aborted` before a worker ever claims it.

After cancellation the run status is `aborted` and the job registry emits a `completion` event with status `aborted`.

### Job fallback chain

`GET /internal/v1/jobs/{job_id}` resolves in this order:

1. In-memory `JobRegistry` snapshot.
2. Persisted `subagent_jobs` row.
3. Persisted `agent_cli_runs` row.

This means clients can look up external CLI runs through the same `/internal/v1/jobs/{job_id}` endpoint that serves subagent and turn jobs — no dedicated route required.

### `GET /internal/v1/app/bootstrap`

Returns an app-shaped summary that composes pairing, auth, runtime state, counts, and action descriptors for `or3-app`.

Response fields include:

- `host` with host identity summary
- `pairing` with paired device state for the current caller
- `auth` with session and step-up status plus auth capability hints
- `status` with health, readiness, capabilities, summary, and warning list
- `counts` with pending approvals and active job/terminal counts
- `actions` with app action descriptors such as `restart-service`
- `features` with app-surface capability flags

This route is summary-oriented and complements, rather than replaces, the more detailed health, readiness, capabilities, approvals, and job routes.

### `POST /internal/v1/actions/restart-service`

Requests a service restart through a structured action route.

Behavior:

- follows the same authorization model as the rest of the service surface
- uses the sensitive-route auth policy for paired-device sessions (`session` + `step-up`)
- preserves approval behavior by checking whether terminal-style exec access would require approval first
- returns `202` with `{ "action_id": "restart-service", "status": "accepted" }` when the restart script was launched
- returns `409` with `approval_id` when approval is required first
- returns `503` when shell access or the restart script is unavailable on the host

### Cron job endpoints

These routes manage the same persisted cron store and live scheduler used by the `cron` tool. They are intended for UI clients such as `or3-app`.

Schedule shapes:

```json
{ "kind": "at", "at_ms": 1788300000000 }
{ "kind": "every", "every_ms": 3600000 }
{ "kind": "cron", "expr": "0 9 * * 1-5", "tz": "America/Los_Angeles" }
```

Create/update job shape:

```json
{
  "name": "Morning summary",
  "enabled": true,
  "schedule": { "kind": "cron", "expr": "0 9 * * 1-5" },
  "payload": {
    "kind": "agent_turn",
    "message": "Summarize overnight changes.",
    "session_key": "cron:default"
  },
  "delete_after_run": false
}
```

External agent CLI scheduled job shape:

```json
{
  "name": "Weekly Codex review",
  "enabled": true,
  "schedule": { "kind": "cron", "expr": "0 9 * * 1" },
  "payload": {
    "kind": "agent_cli_run",
    "session_key": "cron:agents",
    "agent_run": {
      "runner_id": "codex",
      "task": "Review the repository for regressions and summarize risks.",
      "mode": "review",
      "isolation": "host_readonly"
    }
  }
}
```

For `agent_cli_run`, `agent_run.runner_id` and `agent_run.task` are required. Missing `mode` defaults to `review`; missing `isolation` defaults to `host_readonly`. A cron run is marked `ok` once the external agent job is enqueued; completion status is tracked through `/internal/v1/agent-runs/{id}` and job stream APIs.

Job responses include scheduler state:

```json
{
  "job": {
    "id": "abc123",
    "name": "Morning summary",
    "enabled": true,
    "state": {
      "next_run_at_ms": 1788300000000,
      "last_run_at_ms": 1788213600000,
      "last_status": "ok",
      "last_enqueued_job_id": "job-agentcli-abc123def456",
      "last_enqueued_run_id": "acr_abc123def456"
    }
  }
}
```

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
- `agentCLIManagerEnabled`
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
- `internal/agentcli/` (runner registry, adapters, process manager, worker pool)
- `internal/controlplane/controlplane.go` (response builders)
- `internal/app/service_app.go` (ServiceApp delegation)

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
