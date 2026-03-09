# Overview

Phase 3 adds stronger security controls on top of the current CLI-first runtime by extending the same places that already own config, persistence, tool execution, and channel ingress. The design stays local-first: SQLite for durable metadata, Go crypto primitives for protection and verification, and central enforcement in runtime/tool boundaries.

# Affected areas

- `internal/config/config.go` and tests
  - add config for encrypted secret storage, audit mode, access profiles, and outbound host policy
- `cmd/or3-intern/main.go`, `cmd/or3-intern/init.go`, and a new `doctor`-style command path if needed later
  - load secret providers, initialize audit services, and bind profiles into runtime construction
- `internal/db/db.go` and `internal/db/store.go`
  - add additive tables for encrypted secret blobs and audit records if Phase 3 persists them in SQLite
- `internal/agent/runtime.go` and `internal/agent/subagents.go`
  - resolve the active access profile per turn and enforce inheritance for subagents
- `internal/tools/*`, especially `web.go`, `exec.go`, `spawn.go`, and `skill_exec.go`
  - enforce profile- and host-policy-aware checks before risky actions execute
- `internal/mcp/manager.go`
  - apply outbound host policy to HTTP transports and profile/capability checks to remote tools
- `internal/channels/*` and `internal/triggers/*`
  - emit audit events for approvals, pairings, deliveries of protected actions, and trigger-driven privileged operations
- `internal/skills/*`
  - bind installed skills to declared permissions and any agent profile restrictions

# Control flow / architecture

1. Startup loads config and resolves secret sources.
2. If encrypted secrets are enabled, the runtime decrypts only the values needed for configured providers and channels.
3. Runtime builds tool registries and channel integrations with an attached access profile resolver.
4. Each turn resolves an effective profile based on the agent/session/channel context.
5. Risky actions perform checks in this order: capability tier, agent profile, outbound host policy, then execution.
6. Sensitive state changes and protected actions append an audit record, including a chain or signature field for verification.

This keeps policy decisions in the same bounded execution path as the rest of the runtime.

# Data and persistence

- **SQLite changes:** likely two additive tables:
  - `secrets` for named encrypted blobs plus metadata such as key version and updated time
  - `audit_events` for append-only event records including event type, actor/session metadata, payload digest, previous record hash, and signature/MAC
- **Config changes:** add small config structs for:
  - secret backend and key source
  - audit enablement and verification mode
  - named access profiles and profile assignment defaults
  - trusted outbound host allowlists
- **Session/memory impact:** none to existing history or retrieval tables; audit is separate from conversation history

# Interfaces and types

Likely additions:

```go
type SecretStoreConfig struct {
    Enabled bool
    Backend string
    KeyFile string
}
```

```go
type AccessProfile struct {
    Name string
    CapabilityLevel string
    AllowedTools []string
    AllowedHosts []string
    AllowedPaths []string
    AllowSubagents bool
}
```

```go
type AuditEvent struct {
    EventType string
    SessionKey string
    Actor string
    PayloadJSON string
    PrevHash []byte
    RecordHash []byte
    Signature []byte
    CreatedAt int64
}
```

Preferred implementation details:

- use Go standard crypto (`crypto/aes`, `crypto/cipher`, `crypto/ed25519`, or HMAC-SHA256`) rather than a large dependency stack
- use envelope-style versioned secret records so keys can rotate later without rewriting unrelated tables
- make audit verification deterministic by hashing a canonical serialized record plus the previous record hash

# Failure modes and safeguards

- missing or invalid master key should fail startup when encrypted secrets are required, rather than silently falling back to plaintext
- corrupted secret blobs should identify the secret name but never expose decrypted material
- audit-chain verification failures should be surfaced clearly and may block startup or privileged operations in strict mode
- malformed access-profile config should fail config validation before the runtime starts
- host policy checks must validate redirect targets and resolved IPs to avoid hostname-only bypasses
- subagents must never receive a broader profile than the parent turn, even if the requested child profile name is more permissive

# Testing strategy

- add config tests for defaults, validation, and upgrade paths from plaintext config
- add SQLite-backed tests for secret record storage, key rotation metadata, and audit chain verification
- extend tool and MCP tests for trusted-host enforcement on direct requests and redirects
- add runtime/subagent tests for profile resolution, denial behavior, and child-profile inheritance
- add regression tests ensuring audit events do not leak secret values and that history/session behavior remains unchanged
