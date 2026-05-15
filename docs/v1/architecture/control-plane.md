# Control Plane and ServiceApp

The control plane is the typed management layer behind the service API and several CLI commands. It keeps HTTP handlers from reaching directly into every runtime subsystem.

## Main Types

| Type | Package | Role |
| --- | --- | --- |
| `controlplane.Service` | `internal/controlplane` | Health, readiness, capabilities, approvals, devices, jobs, embeddings, audit, and scope operations |
| `app.ServiceApp` | `internal/app` | Runtime-facing app facade for turns, subagents, approval replay/resume, runner detection, agent runs, auth, and job abort |
| `serviceServer` | `cmd/or3-intern` | HTTP route owner that wires auth, config, ServiceApp, control plane, terminal, cron, MCP, and runner chat |

## Control Plane Responsibilities

`internal/controlplane/controlplane.go` exposes typed methods for:

- health and readiness reports
- runtime capabilities and ingress posture
- approval request list/read/approve/deny/cancel/expire
- allowlist list/add/remove
- paired device list/rotate/revoke
- pairing request create/list/approve/deny/exchange
- job snapshots
- embedding status and rebuilds
- audit status and verification
- session scope links and resolution

It has two constructors:

- `New(...)` for a fully built runtime
- `NewLocal(...)` for commands that need config/database/provider/audit/broker without the whole runtime

## ServiceApp Responsibilities

`internal/app/service_app.go` adapts app requests into runtime operations:

- `RunTurn` builds a service request context and invokes `Runtime.Handle`.
- `ReplayToolCall` and `ResumeApprovedRequest` continue work after approval.
- `StartSubagent` enqueues background subagent work.
- `DetectAgentCLIRunners` and agent-run methods coordinate external CLI runners.
- Auth helpers wrap passkey/session operations.
- Abort helpers route cancellation through the job registry or runner managers.

This keeps service handlers small: decode request, enforce route policy, call ServiceApp/control plane, encode response.

## Context Stamping

ServiceApp stamps runtime contexts with:

- request source: `service`
- session key
- approval token
- requester actor and role
- service capability ceiling
- selected profile name
- tool guard and optional filtered registry
- conversation observers and streamers

That is why service turns, app-driven approvals, runner chat, and CLI work all use the same core agent loop while still respecting service-specific safety boundaries.

## Failure Model

When a dependency is not present, control-plane methods return explicit unavailable errors such as `ErrDatabaseUnavailable`, `ErrProviderUnavailable`, `ErrAuditUnavailable`, `ErrApprovalBrokerUnavailable`, `ErrJobRegistryUnavailable`, and `ErrJobNotFound`. HTTP handlers map those into service error envelopes.
