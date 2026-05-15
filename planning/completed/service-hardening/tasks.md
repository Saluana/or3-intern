# 1. Freeze the service contract `or3-net` depends on

- [x] [Req 1] Document the supported v1 service contract in `docs/api-reference.md` and pin request/response fixtures for turns, subagents, job stream attach, and abort.
- [x] [Req 1] Add compatibility tests around `cmd/or3-intern/service.go`, `cmd/or3-intern/service_auth.go`, and related handlers so breaking changes fail CI.
- [x] [Req 1] Add a lightweight cross-repo compatibility check for the current `or3-net` client assumptions around aliases, `tool_policy`, streaming, and abort.

# 2. Add explicit runtime profiles without a new runtime layer

- [x] [Req 2] Extend `internal/config/*` with a small named runtime-profile setting that maps onto existing hardening, service, channel, and automation controls.
- [x] [Req 2] Wire profile validation into `cmd/or3-intern/chat`, `serve`, and `service` startup so risky profile/config combinations fail early.
- [x] [Req 2] Update `docs/security-and-hardening.md`, `docs/configuration-reference.md`, and `README.md` with the supported profile set and intended use.

# 3. Make strict hardening mandatory in serious modes

- [x] [Req 3] Update `cmd/or3-intern/doctor.go` and startup paths so service mode, enabled channels, or enabled automation trigger strict validation in hosted profiles.
- [x] [Req 3] Enforce required secret-store, audit, outbound network policy, and safe MCP HTTP posture in `internal/security/*` and config validation.
- [x] [Req 3, 4] Add clear startup refusal messages that tell operators when to move risky execution to `or3-sandbox` instead of broadening local permissions.

# 4. Narrow external integration risk

- [x] [Req 4] Add duplicate-delivery and rate-handling regressions in `internal/channels/*` for the currently supported external channels.
- [x] [Req 4] Tighten hosted-profile defaults in `internal/skills/*`, `internal/mcp/*`, and tool policy paths so untrusted skills, non-loopback MCP HTTP, and risky exec stay opt-in.
- [x] [Req 4] Add tests showing `hosted-no-exec` and `hosted-remote-sandbox-only` refuse broad local exec while preserving supported remote execution flows.

# 5. Measure retrieval and durability instead of assuming them

- [x] [Req 5] Add benchmarks or soak tests in `internal/db/*`, `internal/memory/*`, and adjacent packages for last-N history load, scoped retrieval, hybrid search, and document indexing.
- [x] [Req 5] Add migration and integrity-check regressions for the current SQLite schema and backup/restore procedures where scripts already exist.
- [x] [Req 5] Document practical latency and memory budgets in the planning notes or operational docs once the measurements exist.

# 6. Out of scope

- [ ] Do not turn `or3-intern` into a second control plane.
- [ ] Do not add a new sandbox manager inside this repo.
- [ ] Do not redesign the runtime around a new server framework or external database.
