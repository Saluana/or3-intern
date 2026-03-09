# Overview

This plan covers **Phase 3** from `planning/analysis/suggestions.md`: the heavier hardening work that should only land after the lightweight Phase 1 and Phase 2 controls are in place.

Scope covers:

- encrypted secrets at rest
- signed audit trail records for sensitive actions
- per-agent access profiles
- trusted-host outbound network policy

Assumptions:

- Phase 1 capability tiers, workspace confinement, and safer exec defaults already exist or are planned as the baseline
- Phase 2 skill permissions and structured event inputs exist or are available to build on
- this phase must still fit the single-process Go + SQLite runtime and stay deterministic

# Requirements

## 1. Encrypt stored secrets at rest

The system shall avoid persisting plaintext operational secrets in config or SQLite where a protected storage path is available.

### Acceptance criteria

- provider keys, channel tokens/passwords, webhook secrets, and other configured credentials can be stored encrypted at rest instead of plaintext JSON
- startup can decrypt required secrets deterministically using a local master key source or OS-backed key material configured by the operator
- existing plaintext configs continue to load during migration, with a bounded upgrade path to encrypted storage
- logs, errors, and audit records never print decrypted secret values

## 2. Record a tamper-evident audit trail for sensitive actions

The system shall persist a signed or MAC-protected audit record for security-relevant operations.

### Acceptance criteria

- at minimum, secret changes, privileged tool execution, skill install/approval, profile changes, and channel pairing/approval events are written to an audit store
- each audit record includes stable metadata such as timestamp, actor/session, event type, and a bounded payload summary
- records are chained or signed so post-hoc tampering can be detected during verification
- audit writes fail closed for the protected action when the configured audit mode requires durability

## 3. Enforce per-agent access profiles

The system shall support explicit access profiles that constrain what a given agent or subagent may do.

### Acceptance criteria

- a profile can restrict capability tiers, allowed tools, writable paths, outbound hosts, and whether the agent may spawn subagents
- the default interactive agent profile remains backward compatible unless an operator opts into stricter profiles
- subagents inherit a bounded child profile rather than expanding privileges beyond the parent
- profile resolution is deterministic for CLI, channel, trigger, and background/subagent turns

## 4. Apply trusted-host outbound network policy

The system shall enforce explicit outbound host policy for networked tools and integrations.

### Acceptance criteria

- outbound HTTP and MCP client traffic can be limited to configured trusted hosts or host patterns
- default-deny or policy-deny behavior is available for high-risk networked actions without breaking localhost-safe exceptions already required by the repo
- redirects and resolved IPs are validated against policy, not just the initial hostname
- policy violations fail with bounded, non-secret-bearing errors

## 5. Preserve existing runtime compatibility and migration safety

The system shall roll out Phase 3 without breaking existing histories, sessions, or SQLite data.

### Acceptance criteria

- SQLite migrations are additive and backward compatible
- existing config files continue to load, with clear defaults for operators that do not enable Phase 3 features yet
- existing session keys, memory retrieval behavior, and channel routing semantics remain unchanged
- the runtime can start in a mixed mode where plaintext config is still accepted while encrypted secret storage or audit verification is introduced

# Non-functional constraints

- Favor small extensions to `internal/config`, `internal/db`, `internal/tools`, `internal/agent`, `internal/mcp`, and existing channel packages over new services
- Keep secret handling deterministic, low-memory, and local-first; do not require a cloud KMS
- Audit records must stay bounded in size and should avoid storing raw secret material or oversized tool output
- Host policy enforcement must remain SSRF-safe and work across redirects and DNS resolution
- Access-profile checks must be cheap enough to run on every tool call or network action
