# OR3 Net Plan for `or3-intern`

> This file describes the work that belongs **inside the `or3-intern` repo** as part of the broader OR3 Network initiative.
> For the full network plan see `or3-net/plan.md` and `or3-net/planning/`.

---

## Context — Why does `or3-intern` need changes?

`or3-intern` is currently a CLI-first Go application. It runs agent turns, tool loops, subagents, memory retrieval, and audit — all in-process. There is no HTTP listener that external programs can use to submit work programmatically.

`or3-net` needs to orchestrate agent execution on behalf of remote clients (`or3-chat`, CLI, third-party SDK users). Rather than reimplementing execution logic in TypeScript, `or3-net` will call `or3-intern` through a narrow **internal service API**.

This means `or3-intern` needs to expose a small authenticated HTTP surface that `or3-net` can call. The rest of `or3-intern` — config loading, tool registry, memory, quotas, channels, cron, hardening — stays exactly as it is.

`or3-net` now owns a durable `network_session_id` binding layer on top of the execution-facing `session_key`. The important contract for `or3-intern` is unchanged: `or3-net` resolves or creates the binding, then calls the internal service using the canonical `session_key`/`intern_session_key` for history and memory scope.

---

## What changes in `or3-intern`

### 1. New internal service mode

A new entry path (either a `service` subcommand or a `--service` flag on `serve`) that starts an HTTP listener alongside the existing channel/heartbeat workers.

**Where it fits in the codebase:**

- Entry point: new file `cmd/or3-intern/service.go` (or added to `main.go` alongside the existing `chat`/`serve`/`agent` command dispatch).
- Config: new `ServiceConfig` struct in `internal/config/config.go`, similar to the existing `HeartbeatConfig` and `SubagentsConfig`. Fields: `Enabled bool`, `Listen string` (default `127.0.0.1:9100`), `Secret string` (shared HMAC secret for auth).
- Environment overrides: `OR3_SERVICE_ENABLED`, `OR3_SERVICE_LISTEN`, `OR3_SERVICE_SECRET`, following the same pattern as other env overrides in `config.go`.

The service listener reuses the same `agent.Builder`, tool registry, memory store, DB, subagent manager, and provider client that the CLI and channel commands already build. It does **not** create a second runtime — it shares the one that `main.go` constructs.

That means `or3-intern` should continue treating `session_key` as the execution identity and should not learn or persist `network_session_id` directly. Session binding, replay, and operator-visible history belong in `or3-net`.

### 2. Internal API endpoints

Four HTTP endpoints on the service listener, all behind shared-secret auth middleware:

#### `POST /internal/v1/turns`

Submits a full agent turn (model call → tool loop → response).

```
Request:  { session_key, message, tool_policy?, meta? }
Response: SSE stream of events OR JSON result (based on Accept header)
```

- Internally calls the same `agent.Builder.Run()` path that `chat` and `serve` use.
- The `session_key` determines which conversation history and memory scope the turn uses.
- `tool_policy` can override the default tool set for this specific turn.

#### `POST /internal/v1/subagents`

Spawns a bounded subagent turn using the existing `SubagentManager`.

```
Request:  { task, prompt_snapshot, tool_policy, timeout }
Response: { job_id, status: "queued" }
```

- Uses the same `SubagentManager` that `spawn_subagent` tool calls use.
- Inherits the same `MaxConcurrent`, `MaxQueued`, and `TaskTimeoutSeconds` limits from `SubagentsConfig`.

#### `GET /internal/v1/jobs/:jobId/stream`

Streams SSE events for a running turn or subagent job.

```
Response: SSE stream
  event: text_delta    data: { content: "..." }
  event: tool_call     data: { name: "...", arguments: {...} }
  event: tool_result   data: { name: "...", result: "..." }
  event: completion    data: { status: "completed", usage: {...} }
  event: error         data: { message: "..." }
```

- Requires a job registry (see below) that maps `jobId` → active stream.
- Multiple consumers can subscribe to the same stream (fan-out).

#### `POST /internal/v1/jobs/:jobId/abort`

Cancels a running turn or subagent job.

```
Response: { ok: true }
```

- Calls the context cancellation for the in-flight work.
- The stream emits a terminal `error` or `completion` event with `status: "aborted"`.

### 3. Auth middleware

All `/internal/v1/*` endpoints are gated by a shared-secret check:

- The caller sends `Authorization: Bearer <hmac-signed-token>`.
- The middleware validates the HMAC signature against `ServiceConfig.Secret`.
- If the secret is empty or missing, the service refuses to start (fail closed).

This is **not** a public user-facing auth system. It's a machine-to-machine secret between `or3-net` and `or3-intern` running on the same host or private network.

### 4. Job registry

A lightweight in-memory registry that tracks in-flight service-triggered work:

- Maps `jobId` → context, cancel func, output channel.
- Enables stream fan-out (multiple SSE consumers for one job).
- Enables abort (cancel the context by job ID).
- Cleans up completed/aborted jobs after a short retention window.

This is intentionally simple — no SQLite persistence for job state. `or3-net` owns durable job metadata in its own database. The job registry here is purely for routing streams and abort signals during execution.

### 5. Execution parity

Service-triggered turns must follow the **exact same rules** as CLI/channel-triggered turns:

- Same tool registry, same capability tiers, same quota enforcement.
- Same memory retrieval (pinned + vector + FTS).
- Same history scoping via session keys.
- Same hardening profile resolution.
- Same subagent policy inheritance.
- Same audit logging.

This is achieved by reusing the shared runtime that `main.go` already builds, not by creating a parallel execution path.

### 6. Browser dashboard launch is not an `or3-intern` responsibility

Although some services may be started by jobs that run through `or3-intern`, the browser launch and tunnel-exposure flow should remain outside the `or3-intern` service API in v1.

- `or3-intern` may start or supervise a service process inside a node or sandbox-backed environment.
- `or3-net` should own the user-facing `launch service` endpoint, authorization checks, and any integration with `or3-sandbox` tunnel APIs.
- This keeps the internal API focused on execution (`turns`, `subagents`, `stream`, `abort`) rather than browser/session mediation.

---

## What does NOT change

- **CLI commands** — `chat`, `serve`, `agent`, `init`, `doctor`, `secrets`, `skills`, `migrate-jsonl`, `audit` all remain exactly as they are.
- **Channel integrations** — Telegram, Slack, Discord, WhatsApp, Email channels are unaffected.
- **Config format** — the existing `config.json` structure is extended (new `service` block), not replaced. Existing configs continue to load without changes.
- **SQLite schema** — no new tables or migrations needed for the service API. `or3-intern` continues using the same DB for history, memory, artifacts, secrets, and audit.
- **Tool safety** — the service API does not bypass hardening, quotas, or sandbox policies.
- **Browser tunnel mediation** — end-user dashboard launch and signed browser tunnel flows stay in `or3-net`/`or3-sandbox`, not in `or3-intern`.

---

## Design decisions

| Decision | Rationale |
|---|---|
| Shared runtime, not a second engine | Avoids duplicating tool registry, memory, quota, and audit logic. One runtime, multiple entry paths. |
| In-memory job registry, not SQLite | `or3-net` owns durable job state. `or3-intern` only needs transient routing for streams and abort. |
| Shared-secret auth, not JWT/OAuth | This is an internal API on localhost/private-network. Shared HMAC is simple, auditable, and sufficient. |
| SSE for streaming, not WebSocket | Matches the existing streaming patterns in the repo and what `or3-net` expects to relay. |
| Fail closed on missing secret | Prevents accidental exposure of an unauthenticated execution endpoint. |
| Keep browser launch outside the internal API | Prevents the service API from growing into a second control plane for UI/session flows. |

---

## Affected files and areas

| Area | Likely files | Notes |
|---|---|---|
| Service entry | `cmd/or3-intern/service.go` | HTTP listener setup, route registration, shutdown |
| Config | `internal/config/config.go` | New `ServiceConfig` struct, env overrides, defaults |
| Auth middleware | `cmd/or3-intern/service_auth.go` | HMAC validation middleware |
| Job registry | `internal/agent/job_registry.go` | In-memory map of active jobs with stream fan-out |
| Turn handler | `cmd/or3-intern/service_turns.go` | `/internal/v1/turns` handler, SSE writer |
| Subagent handler | `cmd/or3-intern/service_subagents.go` | `/internal/v1/subagents` handler |
| Stream handler | `cmd/or3-intern/service_stream.go` | `/internal/v1/jobs/:id/stream` SSE fan-out |
| Abort handler | `cmd/or3-intern/service_abort.go` | `/internal/v1/jobs/:id/abort` cancellation |
| Command dispatch | `cmd/or3-intern/main.go` | Add `service` to the command switch, wire runtime sharing |

---

## Tasks

- [x] **Config** — Added `ServiceConfig` to `internal/config/config.go` with `Enabled`, `Listen`, `Secret` fields, env overrides `OR3_SERVICE_ENABLED`, `OR3_SERVICE_LISTEN`, `OR3_SERVICE_SECRET`, and defaults (`false`, `127.0.0.1:9100`, empty).
- [x] **Service entry** — Added `cmd/or3-intern/service.go` and wired `service` into `cmd/or3-intern/main.go` using the shared runtime/subagent manager instead of a second execution stack.
- [x] **Auth middleware** — Added HMAC-based shared-secret middleware in `cmd/or3-intern/service_auth.go`; startup now fails closed when `service.secret` is empty.
- [x] **Job registry** — Added `internal/agent/job_registry.go` with transient tracking, fan-out subscriptions, cancellation hooks, and retention-based cleanup.
- [x] **Turn endpoint** — Added `POST /internal/v1/turns` with SSE-or-JSON behavior, per-request tool allowlists, and runtime event streaming.
- [x] **Subagent endpoint** — Added `POST /internal/v1/subagents` backed by the existing `SubagentManager`, including service-originated prompt snapshots/tool allowlists.
- [x] **Stream endpoint** — Added `GET /internal/v1/jobs/:id/stream` SSE fan-out over the shared job registry.
- [x] **Abort endpoint** — Added `POST /internal/v1/jobs/:id/abort` for turn cancellation and queued/running subagent abort handling.
- [x] **Tests** — Added focused coverage for auth rejection, SSE ordering, abort cancellation, fan-out/cleanup in the job registry, and config/doctor wiring.
- [x] **Doctor check** — Added `doctor` warnings for weak service secrets and non-loopback service bind addresses.
- [x] **Docs** — Documented service config, startup, auth expectations, and the internal API contract in `README.md`.