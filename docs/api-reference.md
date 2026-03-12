# Internal service API reference

This page documents the authenticated HTTP API exposed by:

```bash
go run ./cmd/or3-intern service
```

## Intended use

`or3-intern service` is a loopback/private-network API intended for integrations such as OR3 Net. It uses the same runtime, tool registry, memory system, quotas, and subagent manager as the CLI and channel entrypoints.

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

### `GET /internal/v1/jobs/:jobId/stream`

Attaches to a live SSE stream for a turn or background job.

### `POST /internal/v1/jobs/:jobId/abort`

Requests cancellation of a running job when cancellation is possible.

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
