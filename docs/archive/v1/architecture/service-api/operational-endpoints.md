# Operational Endpoints

These route families do not fit a single chat/job workflow, but they are part of the app-facing v1 contract.

## Health, Readiness, Capabilities

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/health` | Lightweight service/runtime health |
| `GET /internal/v1/readiness` | Startup-service doctor report; returns 503 when not ready |
| `GET /internal/v1/capabilities` | Runtime posture, tool availability, network policy, MCP, channels, triggers, cron, heartbeat |

`capabilities` accepts optional `channel` and `trigger` query filters.

## App Bootstrap

`GET /internal/v1/app/bootstrap`

Returns an app-shaped host overview: auth posture, pairing/device state, access summaries, warnings, counts, restart action availability, and status cards. This is the main first payload for OR3 App after authentication.

## Host Actions

| Route | Purpose |
| --- | --- |
| `POST /internal/v1/actions/restart-service` | Request service restart via the configured restart script |

The restart action can be disabled, unavailable, conflict because a restart is already in progress, or approval-gated depending on host posture.

## Embeddings

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/embeddings/status` | Report memory vector dims, stored/current embedding fingerprint, and doc-index configuration |
| `POST /internal/v1/embeddings/rebuild` | Rebuild `memory`, `docs`, or `all` embeddings |

The rebuild body can include `target`; the query parameter `target` is also accepted.

## Audit

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/audit` | Audit logger status, strict mode, event count, and latest event summary |
| `POST /internal/v1/audit/verify` | Verify the audit chain and return `verified` plus event count |

## Scope

| Route | Purpose |
| --- | --- |
| `POST /internal/v1/scope/links` | Link a `session_key` to a `scope_key` |
| `GET /internal/v1/scope/sessions?scope_key=...` | List session keys attached to a scope |
| `GET /internal/v1/scope/resolve?session_key=...` | Resolve the scope key for a session |

Scope links let related sessions share history and artifacts without collapsing all app sessions into one identity.

## Skills

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/skills` | List skill inventory, roots, global skill dir, and global-load state |
| `POST` or `PATCH /internal/v1/skills/{name}/settings` | Update skill enablement, API key, env, or config |

Skill updates save config and refresh runtime skill inventory when possible.

## Agent Runs

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/agent-runners` | Detect available external-agent CLIs |
| `GET /internal/v1/agent-runs` | List persisted agent CLI runs |
| `POST /internal/v1/agent-runs` | Queue a background agent CLI run |
| `GET /internal/v1/agent-runs/{runId}` | Read one run |
| `GET /internal/v1/agent-runs/{runId}/events?after_seq=...` | Read durable run events |

Use runner chat when the app needs an interactive external-runner conversation. Use agent runs when it needs queued background work.
