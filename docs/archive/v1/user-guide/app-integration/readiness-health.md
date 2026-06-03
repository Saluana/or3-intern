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
| `GET /internal/v1/doctor/status` | Basic Doctor aggregate status and finding cards |
| `POST /internal/v1/doctor/run` | Basic Doctor run with optional app-side client diagnostics |
| `GET /internal/v1/doctor/admin-brain` | Admin Brain availability without exposing runner comparison copy |
| `GET /internal/v1/doctor/config-metadata` | App-facing setting metadata for plan preview and risk display |

## Suggested usage

- use `auth/capabilities` before prompting for login or passkeys
- use `health` for quick authenticated liveness checks
- use `readiness` before enabling flows that need the runtime fully available
- use `capabilities` or `app/bootstrap` for richer host-status UI
- use `doctor/run` for Settings Health so service-side findings and app-side service-down diagnostics share one list
- keep app-side fallback checks when Doctor is unreachable; send those client diagnostics on the next successful Doctor run

## Doctor/Admin repair flow

The app should create or reload a Doctor session with:

```http
POST /internal/v1/doctor/sessions
GET /internal/v1/doctor/sessions/{session}
POST /internal/v1/doctor/sessions/{session}/messages
```

Basic Doctor replies are deterministic. When Admin Brain is available, the service may use the existing runner-chat infrastructure, but it is restricted to diagnostic reasoning, safe diagnostic summaries, and structured settings-plan proposals.

Settings writes should use Doctor plans:

```http
POST /internal/v1/doctor/plans
POST /internal/v1/doctor/plans/{id}/apply
POST /internal/v1/doctor/plans/{id}/rollback
POST /internal/v1/doctor/plans/{id}/post-checks
```

Render the returned plan as cards: diagnostic result, recommended fix, settings change preview, risk/approval warning, exact diff behind an expandable section, restart-required state, post-fix check, undo when rollback is available, and manual fallback if automatic repair is blocked.

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
