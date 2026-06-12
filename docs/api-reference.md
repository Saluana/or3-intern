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
| `POST` | `/internal/v1/runner-chat/sessions` | Create or continue a runner chat session. |
| `POST` | `/internal/v1/runner-chat/sessions/{id}/turns` | Submit a foreground turn through a runner. |
| `POST` | `/internal/v1/runner-runs` | Enqueue a persisted runner run. |
| `GET` | `/internal/v1/runner-runs/{id}` | Read a persisted runner run by run ID or job ID. |
| `POST` | `/internal/v1/runner-memory/search` | Search scoped memory for a runner session. |
| `POST` | `/internal/v1/runner-memory/notes` | Add a durable memory note. |
| `GET` | `/internal/v1/runner-memory/pinned` | Read pinned memory. |
| `POST` | `/internal/v1/runner-memory/pinned` | Replace pinned memory. |

Runner-run job IDs use `job-runner-...`, run IDs use `rr_...`, and job kinds use `runner:<runner_id>`.

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
