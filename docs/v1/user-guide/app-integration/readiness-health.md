# Readiness and Health

OR3 Intern exposes several monitoring and posture routes, but they are not the old `/health`, `/ready`, and `/status` surface.

## Public discovery

```http
GET /internal/v1/auth/capabilities
```

Use this before login/pairing-sensitive UI to learn what auth mechanisms are available.

## Authenticated checks

| Endpoint | Purpose |
| --- | --- |
| `GET /internal/v1/health` | Lightweight service/runtime health |
| `GET /internal/v1/readiness` | Startup/readiness evaluation |
| `GET /internal/v1/capabilities` | Machine-readable runtime posture |
| `GET /internal/v1/app/bootstrap` | App-shaped host overview used by OR3 App |

## Suggested usage

- use `auth/capabilities` before prompting for login or passkeys
- use `health` for quick authenticated liveness checks
- use `readiness` before enabling flows that need the runtime fully available
- use `capabilities` or `app/bootstrap` for richer host-status UI

## Error handling

These routes follow the normal service error envelope:

```json
{
  "error": "human-readable message",
  "code": "validation_failed",
  "request_id": "req_..."
}
```

If readiness is not satisfied, expect a structured non-2xx response rather than the old `/ready` pattern.
