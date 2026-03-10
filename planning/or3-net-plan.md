# OR3 Net Plan for `or3-intern`

> This file describes the work that belongs **inside the `or3-intern` repo** as part of the broader OR3 Network initiative.
> For the full network plan see `or3-net/plan.md` and `or3-net/planning/`.

---

## Context — Why does `or3-intern` need changes?

`or3-intern` is currently a CLI-first Go application. It runs agent turns, tool loops, subagents, memory retrieval, and audit — all in-process. There is no HTTP listener that external programs can use to submit work programmatically.

`or3-net` needs to orchestrate agent execution on behalf of remote clients (`or3-chat`, CLI, third-party SDK users). Rather than reimplementing execution logic in TypeScript, `or3-net` will call `or3-intern` through a narrow **internal service API**.

This means `or3-intern` needs to expose a small authenticated HTTP surface that `or3-net` can call. The rest of `or3-intern` — config loading, tool registry, memory, quotas, channels, cron, hardening — stays exactly as it is.

---

## What changes in `or3-intern`

### 1. New internal service mode

A new entry path (either a `service` subcommand or a `--service` flag on `serve`) that starts an HTTP listener alongside the existing channel/heartbeat workers.

**Where it fits in the codebase:**

- Entry point: new file `cmd/or3-intern/service.go` (or added to `main.go` alongside the existing `chat`/`serve`/`agent` command dispatch).
- Config: new `ServiceConfig` struct in `internal/config/config.go`, similar to the existing `HeartbeatConfig` and `SubagentsConfig`. Fields: `Enabled bool`, `Listen string` (default `127.0.0.1:9100`), `Secret string` (shared HMAC secret for auth).
- Environment overrides: `OR3_SERVICE_ENABLED`, `OR3_SERVICE_LISTEN`, `OR3_SERVICE_SECRET`, following the same pattern as other env overrides in `config.go`.

The service listener reuses the same `agent.Builder`, tool registry, memory store, DB, subagent manager, and provider client that the CLI and channel commands already build. It does **not** create a second runtime — it shares the one that `main.go` constructs.

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

---

## What does NOT change

- **CLI commands** — `chat`, `serve`, `agent`, `init`, `doctor`, `secrets`, `skills`, `migrate-jsonl`, `audit` all remain exactly as they are.
- **Channel integrations** — Telegram, Slack, Discord, WhatsApp, Email channels are unaffected.
- **Config format** — the existing `config.json` structure is extended (new `service` block), not replaced. Existing configs continue to load without changes.
- **SQLite schema** — no new tables or migrations needed for the service API. `or3-intern` continues using the same DB for history, memory, artifacts, secrets, and audit.
- **Tool safety** — the service API does not bypass hardening, quotas, or sandbox policies.

---

## Design decisions

| Decision | Rationale |
|---|---|
| Shared runtime, not a second engine | Avoids duplicating tool registry, memory, quota, and audit logic. One runtime, multiple entry paths. |
| In-memory job registry, not SQLite | `or3-net` owns durable job state. `or3-intern` only needs transient routing for streams and abort. |
| Shared-secret auth, not JWT/OAuth | This is an internal API on localhost/private-network. Shared HMAC is simple, auditable, and sufficient. |
| SSE for streaming, not WebSocket | Matches the existing streaming patterns in the repo and what `or3-net` expects to relay. |
| Fail closed on missing secret | Prevents accidental exposure of an unauthenticated execution endpoint. |

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

- [ ] **Config** — Add `ServiceConfig` to `internal/config/config.go` with `Enabled`, `Listen`, `Secret` fields. Add env overrides `OR3_SERVICE_ENABLED`, `OR3_SERVICE_LISTEN`, `OR3_SERVICE_SECRET`. Add defaults (`false`, `127.0.0.1:9100`, empty).
- [ ] **Service entry** — Add `cmd/or3-intern/service.go` that starts an HTTP listener using the shared runtime (same `agent.Builder`, tool registry, DB, provider, subagent manager). Wire it into `main.go` command dispatch.
- [ ] **Auth middleware** — Add HMAC-based shared-secret middleware. Refuse to start if `Secret` is empty. Reject requests with invalid or missing auth headers.
- [ ] **Job registry** — Add `internal/agent/job_registry.go` with in-memory job tracking: register, lookup, fan-out subscribe, cancel, and cleanup. Keep it bounded (max tracked jobs, TTL for completed entries).
- [ ] **Turn endpoint** — Add `POST /internal/v1/turns` handler that calls `agent.Builder.Run()` with the provided session key, message, and optional tool policy. Stream events via SSE or return JSON based on Accept header.
- [ ] **Subagent endpoint** — Add `POST /internal/v1/subagents` handler that submits work to the existing `SubagentManager` and returns a job handle.
- [ ] **Stream endpoint** — Add `GET /internal/v1/jobs/:id/stream` handler that subscribes to the job registry's fan-out channel and writes SSE events.
- [ ] **Abort endpoint** — Add `POST /internal/v1/jobs/:id/abort` handler that cancels the job context and returns confirmation.
- [ ] **Tests** — Add tests for: auth rejection (missing/invalid secret), successful turn execution and SSE event ordering, subagent policy inheritance, abort cancellation, stream fan-out to multiple consumers, job registry cleanup.
- [ ] **Doctor check** — Add a `doctor` finding that warns if the service is enabled but the secret is weak or the listen address is publicly routable.
- [ ] **Docs** — Document service-mode config, startup, security expectations, and the internal API contract in the README or a dedicated doc file.