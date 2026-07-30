# Internal Service API

`or3-intern service` exposes the authenticated machine-facing API used by OR3 App and local integrations.

## Authentication

Most routes require `Authorization: Bearer <signed-token>`. Secure device enrollment uses:

- `POST /internal/v1/secure-connections/pairing/approve`

Requests and responses use JSON with snake_case field names. Unknown JSON fields and trailing body data are rejected.

## Runner Routes

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/internal/v1/chat-runners` | List available external runners and the service default. |
| `GET` | `/internal/v1/runner-chat/sessions` | List recent runner-chat sessions, optionally scoped by `app_session_key_prefix`; `limit` defaults to 50 and cannot exceed 100. |
| `POST` | `/internal/v1/runner-chat/sessions` | Create or continue a runner chat session. |
| `GET` | `/internal/v1/runner-chat/sessions/{id}/turns` | List turns chronologically; when `limit` is supplied, return the newest N turns rather than the oldest N. |
| `POST` | `/internal/v1/runner-chat/sessions/{id}/turns` | Submit a foreground turn through a runner. |
| `GET` | `/internal/v1/runner-chat/sessions/{id}/turns/{turn}` | Read one canonical turn. |
| `GET` | `/internal/v1/runner-chat/sessions/{id}/turns/{turn}/events` | Read normalized turn events after an optional sequence cursor. |
| `GET` | `/internal/v1/runner-chat/sessions/{id}/turns/{turn}/stream` | Stream normalized turn events with replay-safe sequence cursors. |
| `POST` | `/internal/v1/runner-chat/sessions/{id}/turns/{turn}/abort` | Stop a running turn. |
| `POST` | `/internal/v1/runner-chat/sessions/{id}/turns/{turn}/approve` | Approve a pending runner action and resume the live native turn when possible. |
| `POST` | `/internal/v1/runner-chat/sessions/{id}/turns/{turn}/reject` | Deny a pending runner action and finalize the affected turn. |
| `POST` | `/internal/v1/runner-runs` | Enqueue a persisted runner run. |
| `GET` | `/internal/v1/runner-runs/{id}` | Read a persisted runner run by run ID or job ID. |
| `POST` | `/internal/v1/runner-memory/search` | Search scoped memory for a runner session. |
| `POST` | `/internal/v1/runner-memory/notes` | Add a durable memory note. |
| `GET` | `/internal/v1/runner-memory/pinned` | Read pinned memory. |
| `POST` | `/internal/v1/runner-memory/pinned` | Replace pinned memory. |

Runner-run job IDs use `job-runner-...`, run IDs use `rr_...`, and job kinds use `runner:<runner_id>`.

Runner-chat session listing returns `{ "sessions": [...] }`, ordered by
`updated_at` descending. `app_session_key_prefix` is matched literally (SQL
wildcards have no special meaning), must be non-empty when supplied, and is
limited to 256 UTF-8 bytes. Clients should use a product/workspace-specific
prefix so they do not discover unrelated sessions on the same host.

Each `/internal/v1/chat-runners` item advertises runner-chat action support in
`chat_capabilities`. Clients must gate cancellation on `cancel`, approval
decisions on `approvalDecisions` plus an enabled/available host approval
broker, and free-form working-directory input on `customCwd`. The service still
validates every submitted working directory against its configured runner
root.

Runner entries also expose their native `models` catalog and `default_model`.
Codex discovers this through app-server `model/list`; OpenCode uses its provider
catalog with CLI fallback. Model `id` is the exact value clients submit.
`provider` and `provider_name` are browsing metadata, not a prefix to append to
the ID.

For native Codex and OpenCode approvals, the service records the protected
operation, pauses the turn, and answers the runner's live permission request
after `/approve` or `/reject`. If the live process is gone, approval falls back
to an issued broker token and the response reports that it did not resume
inline. Codex receives one JSON-RPC response with `accept`,
`acceptForSession`, `decline`, or `cancel`. OpenCode receives `once`, `always`,
or `reject` through its current permission reply route, with its legacy
response route retained as a compatibility fallback. Resume mirroring starts
after the original approval event so replay cannot create a second prompt.

## Service Operations

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/internal/v1/health` | Service health. |
| `GET` | `/internal/v1/jobs` | List current jobs. |
| `GET` | `/internal/v1/jobs/{id}` | Read job state. |
| `GET` | `/internal/v1/jobs/{id}/stream` | Stream job events. |
| `POST` | `/internal/v1/jobs/{id}/abort` | Abort a running job. |
| `GET` | `/internal/v1/cron/jobs` | List scheduled jobs. |
| `POST` | `/internal/v1/cron/jobs` | Create a scheduled runner job. |
| `GET` | `/internal/v1/files` | Browse allowed files. |
| `GET` | `/internal/v1/configure` | Read configurable settings. |
| `POST` | `/internal/v1/configure` | Apply canonical settings. |
| `GET` | `/internal/v1/capabilities` | Read runtime posture, approvals, network, runner, and auth capabilities. |
| `GET` | `/internal/v1/secure-connections/discovery` | Read secure connection capabilities. |
| `POST` | `/internal/v1/secure-connections/pairing/approve` | Approve secure device enrollment. |

## Errors

Non-2xx JSON errors include:

```json
{
  "error": "human-readable message",
  "code": "validation_failed",
  "request_id": "req_..."
}
```

Common codes include `validation_failed`, `method_not_allowed`, `not_found`, `forbidden`, `unauthorized`, `rate_limited`, `capability_unavailable`, `request_too_large`, `conflict`, `timeout`, and `request_failed`.
