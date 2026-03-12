# Overview

This plan tightens `or3-intern` around its real job: agent runtime, memory, tool execution meaning, and the internal service used by `or3-net`. The goal is not to add more features. The goal is to make service mode predictable, split safe local use from hosted execution postures, and close the easy ways to run unsafe configurations by accident.

Scope assumptions:

- `or3-intern` stays a Go CLI-first runtime with optional service mode and channel integrations.
- SQLite remains the single local persistence layer.
- Isolation and hostile-workload execution continue to belong in `or3-sandbox`, not inside `or3-intern`.

# Requirements

## 1. Stable internal service contract

The service API used by `or3-net` must be treated as a versioned compatibility surface.

Acceptance criteria:

- Request and response shapes for turns, subagents, stream attach, and abort are documented and tested as a stable v1 contract.
- Compatibility aliases remain either explicitly supported or explicitly removed with a versioned migration note.
- Cross-repo compatibility checks catch breaking changes before merge.
- Breaking service changes fail CI instead of shipping silently.

## 2. Explicit runtime profiles

Operators must be able to run `or3-intern` in clear postures instead of relying on scattered flags.

Acceptance criteria:

- The repo defines a small supported profile set such as `local-dev`, `single-user-hardened`, `hosted-service`, `hosted-no-exec`, and `hosted-remote-sandbox-only`.
- Each profile maps to existing config surfaces for tools, network policy, service mode, channels, and automation.
- Service mode refuses incompatible or risky config combinations for the selected profile unless an explicit override exists.
- Docs explain what each profile allows and forbids.

## 3. Non-optional hardening gates for serious modes

Hosted or externally exposed modes must fail closed by default.

Acceptance criteria:

- `doctor --strict` becomes a startup gate for service mode, enabled channels, or enabled automation in the relevant profiles.
- Production-grade profiles require valid secret-store and audit configuration before startup succeeds.
- Unsafe outbound network policy or unsafe MCP HTTP posture is rejected in hosted profiles.
- Startup errors are explicit and actionable rather than generic misconfiguration failures.

## 4. Reduced integration blast radius

External channels, skills, and MCP integrations must stop widening risk silently.

Acceptance criteria:

- Channel ingress has duplicate-delivery protection and bounded rate handling.
- Shared-space channel defaults remain conservative, such as mention-only or explicit allow rules.
- Third-party skills and MCP servers are treated as untrusted integrations with stronger defaults and quarantine or approval workflows where already supported.
- Hosted profiles can disable risky local exec or route execution through `or3-sandbox` only.

## 5. Measured retrieval and persistence behavior

The current SQLite and hybrid-memory design must be proven under realistic load.

Acceptance criteria:

- Benchmarks or soak tests exist for session growth, last-N history fetch, scoped retrieval, hybrid memory lookup, and document indexing.
- Hot SQLite paths have regression checks for index usage or query-shape stability.
- Migration, backup/restore, and integrity-check procedures are covered by automated or scripted verification where practical.
- Long-session behavior stays within documented latency and memory budgets.

# Non-functional constraints

- Keep the solution inside the current Go CLI/runtime/config model.
- Preserve SQLite compatibility, session keys, and stored memory/history data.
- Do not turn `or3-intern` into a public control plane or sandbox manager.
- Keep loops, tool output, and external access bounded by existing safety patterns.
- Prefer additive config and migration changes with backward-compatible defaults.
