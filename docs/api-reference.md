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

### `POST /internal/v1/subagents`

Queues a background subagent job through the shared subagent manager.

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
