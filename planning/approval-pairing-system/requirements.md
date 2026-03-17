# Overview

This plan scopes the OR3 approval and pairing system to what fits the current Go, SQLite, CLI-first codebase without introducing a new control plane.

The implementation target for this repo is:

- add a lightweight internal approval broker,
- add explicit pairing for remote operator and service clients that use the existing service listener,
- enforce approval at the host that actually executes `exec` and `run_skill_script`,
- preserve backward compatibility for existing local CLI and current service flows where practical, and
- leave web UI, chat approvals, sandbox/node federation, secret-access approval, and outbound-message approval as compatible follow-on work.

Assumptions:

- local CLI remains a trusted operator surface and does not need device pairing to inspect or resolve approvals;
- v1 enforcement is required for `exec` and skill script execution;
- the schema and API must be extensible to secret access, outbound message send, and remote file transfer later;
- the existing `/internal/v1` service listener is the right place for HTTP approval and pairing APIs.

# Requirements

## 1. Central approval broker

The system must introduce a single internal approval broker that all sensitive runtime checks go through.

Acceptance criteria:

- A single package owns approval request creation, state transitions, allowlist matching, token issuance, expiration, and audit emission.
- `exec` and `run_skill_script` cannot bypass the broker by calling ad hoc approval helpers.
- The broker can be constructed from existing runtime dependencies: SQLite DB handle, audit logger, host ID, and signing key.
- When approvals are disabled for a domain, the broker still returns deterministic allow/deny decisions based on configured mode.

## 2. Scoped approval modes by domain

The system must support small, explicit policy modes per domain rather than one global switch.

Acceptance criteria:

- Config supports exactly `deny`, `ask`, `allowlist`, and `trusted` for at least `pairing`, `exec`, `skill_execution`, `secret_access`, and `message_send`.
- `exec` and `skill_execution` can be configured independently.
- Configuration validation rejects unknown modes.
- Existing config continues to load without requiring approval settings to be present.

## 3. Lightweight pairing for remote clients

The system must support explicit pairing for remote operator and service clients using the existing service transport.

Acceptance criteria:

- A pairing request can be created with a role, display name, and origin metadata.
- Pairing uses a short numeric code with expiration and stores only a hash of the code.
- Successful pairing produces a device record and a role-scoped bearer token whose server-side representation is revocable.
- Supported roles include at least `operator`, `service-client`, `web-ui`, and `node`, even if only `operator` and `service-client` are used in v1 flows.
- Local CLI can list, approve, deny, revoke, and rotate paired devices without requiring the HTTP API.

## 4. Backward-compatible service authentication

The service listener must add paired-device authentication without breaking the current shared-secret service model.

Acceptance criteria:

- Existing service bearer-token authentication remains supported for current automation and compatibility paths.
- Approval and device-management endpoints can authenticate with paired device tokens and enforce role checks.
- Revoked or expired device tokens are rejected immediately for new API requests.
- The system cleanly distinguishes bootstrap/admin auth from paired-device auth in audit records.

## 5. Canonical subject binding for exec

Approval for command execution must bind to the exact execution context the local host will run.

Acceptance criteria:

- The canonical exec subject includes host ID, sandbox ID when present, executable path, argv, cwd, env binding, script hash when applicable, agent ID, session ID, tool name, access profile, and expiration.
- The subject hash is deterministic for semantically identical input.
- Changing argv, cwd, host ID, profile, or script hash changes the subject hash.
- A broker-issued approval token is scoped to the subject hash and host ID.

## 6. Canonical subject binding for skill execution

Approval for skill script execution must bind to the exact skill asset and execution context.

Acceptance criteria:

- The canonical skill subject includes skill ID, version when known, origin, trust state, script content hash, host ID, env binding, timeout, agent ID, and session ID.
- Skill approval does not rely solely on current `PermissionState`; it uses the broker for runtime execution approval.
- Changing script content, host ID, timeout, or skill identity changes the subject hash.

## 7. Host-local enforcement

The final approval check must happen in the host-local execution path, not only in planning or prompting.

Acceptance criteria:

- `exec` enforcement happens in or immediately before `internal/tools.ExecTool` execution.
- `run_skill_script` enforcement happens in or immediately before `internal/tools.RunSkillScript` execution.
- The enforcement path validates allowlist match and/or approval token match against the canonical subject.
- When verification fails or required approval is missing, execution is blocked with a structured reason and no subprocess starts.

## 8. Scoped allowlists

The system must support small, durable allowlist rules that fit OR3’s existing access-profile model.

Acceptance criteria:

- Allowlist rules can be scoped by host, tool, profile, and agent, with sandbox and channel/account fields available in the schema for future use.
- Exec matching supports exact executable path, argument templates, cwd constraints, optional path wildcards, and optional env-name constraints.
- Operators can create allowlists from CLI and HTTP API, including an `approve and always allow matching` action.
- Allowlist evaluation is deterministic and performed before prompting when the domain mode is `allowlist`.

## 9. Audit lifecycle coverage

Approval and pairing actions must extend the existing append-only audit chain instead of creating a second audit system.

Acceptance criteria:

- Pairing request, pairing approval/denial, device revocation, token rotation, approval request, approval resolution, allowlist changes, approval token issuance, execution start, execution block, execution completion, and execution failure are recorded through the current audit logger.
- Audit payloads include actor, subject hash when applicable, host ID, outcome, and request or device identifiers.
- Approval-specific audit records survive restart because they reuse the current SQLite audit table.

## 10. Expiration and fail-closed behavior

All temporary approval state must expire and execution must fail closed.

Acceptance criteria:

- Pairing codes expire automatically and cannot be redeemed after expiry.
- Pending approval requests expire automatically and become unusable.
- Approval tokens are short-lived and rejected after expiry, subject mismatch, host mismatch, or revocation.
- If the approval broker or signing key is unavailable for an `ask` or `allowlist` domain, execution is denied rather than silently allowed.
- CLI management remains available even when the HTTP service listener is not running.

## 11. Persistence and migration compatibility

The feature must remain compatible with the repo’s existing single-process SQLite model.

Acceptance criteria:

- New tables are additive migrations only; no existing session, message, memory, or audit tables are broken.
- No network service other than the existing optional service listener is required.
- Pairing and approval state survive restart in the main SQLite database.
- Migrations can run on an existing install without manual data conversion.

## 12. Operator surfaces for v1

The system must be operable from CLI first and from HTTP second.

Acceptance criteria:

- CLI provides approval list/show/approve/deny/allowlist management and device list/approve/revoke/rotate flows.
- HTTP API provides pairing request create/exchange, approval list/detail/resolve, allowlist create/remove, and device list/revoke/rotate routes under the existing service server.
- There is no requirement to ship a web UI in v1.
- Headless deployments can use the full v1 system with CLI and HTTP only.

## 13. Future-compatibility without overcommitting v1

The design must leave room for future nodes, sandbox verification, message approvals, and secret approvals without forcing them into the first implementation.

Acceptance criteria:

- The persisted domain/type model includes `secret_access`, `message_send`, and `file_transfer` as valid approval request types even if the first enforcement pass only wires `exec` and `skill_execution`.
- Approval tokens encode host-scoped subject hashes in a way that future `or3-sandbox` validation can reuse.
- No v1 API or schema requires a browser session, desktop app, or external policy engine.

# Non-functional constraints

- Deterministic behavior: subject hashing, allowlist matching, and expiration decisions must be stable and testable.
- Low memory usage: approval logic must operate from SQLite rows and small canonical JSON payloads; no background worker or cache is required for correctness.
- Bounded runtime: CLI/API listing endpoints must page or bound result counts; token TTLs and pending request TTLs must be short.
- SQLite safety: all writes must remain compatible with the current single-process WAL model and existing migration pattern.
- Secure handling: pairing codes are stored hashed, device tokens are stored hashed or by revocable reference, approval-token verification uses constant-time comparisons where relevant, and no secrets are written into subject JSON or audit payloads.
- Backward compatibility: existing config loading, service auth, session keys, audit tables, and skill permission metadata must continue to work while approvals are introduced incrementally.
