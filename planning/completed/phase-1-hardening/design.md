# Overview

Phase 1 hardening fits the current architecture by tightening policy at the existing integration points: config loading, tool registry construction, subprocess launch, and channel ingress. The goal is to make unsafe behavior opt-in without introducing a new service boundary.

# Affected areas

- `cmd/or3-intern/main.go`
  - wire new policy config into channel setup, tool registry construction, and runtime quotas
- `internal/config/config.go` and tests
  - add config for capability policy, exec allowlists, child env allowlists, quotas, and channel trust defaults
- `internal/tools/registry.go`, `internal/tools/exec.go`, `internal/tools/files.go`, `internal/tools/web.go`, `internal/tools/spawn.go`, `internal/tools/skill_exec.go`
  - classify tools, enforce capability checks, switch exec to argv-first, and apply quota accounting
- `internal/mcp/manager.go`
  - apply child env scrubbing and capability gating for remote tools, especially stdio transports
- `internal/channels/*`
  - enforce paired-or-allowlisted inbound access and preserve isolated session keys per trusted peer or chat
- `internal/agent/runtime.go`
  - reject over-quota tool actions deterministically during a session
- `internal/db/db.go`, `internal/db/store.go`, and tests (if pairing state is persisted)
  - add a small table for trusted channel peers and helper methods to read/write pairings

# Control flow / architecture

1. Startup loads new hardening config from `internal/config`.
2. `cmd/or3-intern` builds the tool registry with a capability profile and quota settings.
3. External channel handlers validate the sender/chat against config allowlists and, if implemented, a small SQLite pairing store before publishing a bus event.
4. Runtime executes turns as it does today, but tool execution first checks capability policy and current session quotas.
5. Subprocess-based actions (`exec`, skill runs, stdio MCP) launch with argv-first execution, workspace-confined `cwd`, and a scrubbed child environment.

This keeps enforcement close to the current code paths and avoids adding a second policy service.

# Data and persistence

- **Config changes:** add small structs for capability policy, exec binary allowlist, child env allowlist, quota limits, and channel trust defaults.
- **SQLite changes:** none are required for capability tiers or quotas. If Phase 1 includes dynamic pairing instead of config-only allowlists, add one small table such as `channel_pairings(channel, peer, session_key, paired_at)` plus indexed lookup helpers.
- **Session and memory scope:** no changes to history or memory schema. Channel trust checks must preserve the current per-channel session isolation pattern.

# Interfaces and types

Likely additions:

```go
type CapabilityLevel string

const (
    CapabilitySafe CapabilityLevel = "safe"
    CapabilityGuarded CapabilityLevel = "guarded"
    CapabilityPrivileged CapabilityLevel = "privileged"
)
```

```go
type HardeningConfig struct {
    GuardedEnabled bool
    PrivilegedEnabled bool
    ExecAllowedPrograms []string
    ChildEnvAllowlist []string
    Quotas QuotaConfig
}
```

`ExecTool` should accept argv-style params first, for example `program`, `args`, `cwd`, and `timeoutSeconds`, while any legacy `command` field is treated as privileged.

# Failure modes and safeguards

- invalid config should fail fast during startup with clear errors for malformed allowlists or impossible policy combinations
- missing workspace config should fall back only to the current process workspace, not to unrestricted host access
- unknown channel peers should be denied or placed into a pairing flow without invoking the agent runtime
- over-quota sessions should receive bounded errors without crashing the process or corrupting history
- denied privileged actions should not silently fall back to shell-string execution or full environment inheritance
- if pairing persistence is added, migration failure should stop startup rather than leave channels in a partially trusted state

# Testing strategy

- add config tests for default-safe loading and backward-compatible overrides
- add tool tests for capability enforcement, workspace confinement, argv exec, binary allowlists, and env scrubbing
- extend channel tests for default deny, allowlisted peers, and stable session keys for trusted peers
- add runtime tests for per-session quota enforcement and deterministic denial messages
- if pairing uses SQLite, add DB-backed migration and lookup tests in `internal/db`
