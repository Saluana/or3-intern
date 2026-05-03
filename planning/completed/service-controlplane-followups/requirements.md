# Overview

This plan covers the deferred follow-up work after the initial `service` control-plane refactor: add the highest-value remaining HTTP parity, keep extracting reusable machine-facing operations out of CLI-only code where that improves reuse, and tighten compatibility coverage for the newer status/control endpoints.

Scope assumptions:

- `or3-intern serve` remains the orchestration/runtime host.
- `or3-intern service` remains the authenticated internal HTTP gateway.
- The existing `internal/controlplane` package is the primary reuse point; extend it instead of creating a parallel application layer.
- Embeddings, audit, and scope HTTP parity should be added only where they are clearly useful for machine clients.

# Requirements

## 1. Finish high-value HTTP parity for machine-facing operator flows

The internal HTTP gateway must expose the deferred operator workflows that are useful to OR3 Net and other non-CLI clients.

Acceptance criteria:

- `service` adds backward-compatible endpoints for embeddings status/rebuild, audit status/verify, and scope link/list/resolve when those actions can be expressed safely over the existing authenticated API.
- New handlers delegate to shared operations below the transport layer rather than duplicating CLI command bodies.
- Existing routes, auth behavior, and request aliases remain unchanged.

## 2. Continue reducing CLI/API drift through shared control-plane operations

CLI and HTTP should call the same reusable operations for operator-facing control-plane actions where practical.

Acceptance criteria:

- Shared operations are added or extended in `internal/controlplane` for the newly exposed embeddings, audit, and scope workflows.
- CLI wrappers for those workflows become thinner where reuse is straightforward.
- Transport-specific concerns stay local to CLI/HTTP entrypoints.

## 3. Improve startup/bootstrap reuse without merging command responsibilities

The runtime/bootstrap path should be easier to share across `chat`, `serve`, and `service` without changing their boundaries.

Acceptance criteria:

- Common bootstrap/wiring seams currently embedded in `cmd/or3-intern/main.go` are extracted into a small internal helper package or helper set.
- The extraction preserves current command behavior and startup validation.
- The change reduces direct command-specific wiring duplication rather than introducing a new runtime stack.

## 4. Strengthen compatibility and regression coverage for the newer control-plane surface

The service should keep behaving like a stable machine-facing gateway as more endpoints are added.

Acceptance criteria:

- New singleton/status/control endpoints get route, auth, method-guard, and decoding tests.
- Frozen fixture or compatibility-style tests are added for stable status-style responses where the shape is intended to remain stable.
- Docs clearly reflect the `serve` vs `service` boundary and any newly added endpoints.

# Non-functional constraints

- Keep the design bounded, deterministic, and low-RAM.
- Reuse current SQLite tables and migrations when possible; do not introduce a new persistence layer.
- Preserve startup validation, auth posture, approval/device security, and loopback/private-network assumptions.
- Keep network/file/tool behavior safe by default; do not expose privileged local-only workflows over HTTP unless the existing security model supports them cleanly.
