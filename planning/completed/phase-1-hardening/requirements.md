# Overview

This plan covers **Phase 1** from `planning/analysis/suggestions.md`: a small hardening pass that raises the default safety baseline without adding new services or a heavyweight sandbox.

Scope covers:

- capability tiers for tool access
- workspace-only file and exec defaults
- paired or allowlisted external channel access
- argv-first command execution with scrubbed child environments
- basic per-session quotas

Assumptions:

- this phase should stay inside the current single-process Go runtime
- SQLite remains the only persistence layer
- backward compatibility matters, but new defaults may be stricter when operators have not explicitly opened access

# Requirements

## 1. Add capability tiers for tools and dangerous runtime actions

The system shall classify runtime actions as `safe`, `guarded`, or `privileged` and enforce those tiers centrally.

### Acceptance criteria

- tool registration and runtime execution can determine the capability tier for built-in tools, MCP tools, skill execution, and subagent spawn
- `safe` actions remain available by default
- `guarded` actions require explicit config enablement
- `privileged` actions are disabled by default and require explicit operator opt-in
- denial responses are deterministic and explain which policy blocked the action

## 2. Make workspace confinement the default file boundary

The system shall treat the configured workspace as the default root for file reads, writes, edits, and subprocess working directories.

### Acceptance criteria

- file tools reject paths outside the configured workspace by default
- symlink or canonical-path escapes outside the workspace are rejected
- subprocess `cwd` cannot resolve outside the workspace unless an explicit privileged override is enabled
- existing deployments can retain broader access only through explicit config, not implicit fallback

## 3. Require trusted peers on external channels by default

The system shall process inbound external channel messages only from paired or allowlisted peers by default.

### Acceptance criteria

- Telegram, Slack, Discord, WhatsApp, and Email reject unknown senders or chats by default unless explicitly opened
- direct-message or sender trust can be satisfied by a config allowlist or a lightweight persisted pairing record
- shared-channel traffic still requires mention checks where already supported and must not silently widen session scope
- session keys for paired peers remain stable and isolated from unrelated senders

## 4. Replace shell-string execution as the default exec path

The system shall use argv-based execution as the default subprocess model and keep shell-string execution behind privileged mode only.

### Acceptance criteria

- the `exec` tool supports `program` plus `args` as the default invocation form
- a legacy shell command path, if retained, is disabled unless privileged execution is enabled
- exec uses existing timeout and output limits and keeps execution rooted in the workspace by default
- operators can define a small binary allowlist for non-privileged exec

## 5. Scrub child process environments

The system shall stop passing the full parent environment to child processes by default.

### Acceptance criteria

- exec, skill subprocesses, and stdio MCP servers receive only an allowlisted environment by default
- provider keys and unrelated secrets are not inherited automatically by child processes
- explicitly configured environment entries still work for the child process they were intended for
- environment handling remains deterministic across runs

## 6. Add basic per-session quotas for risky actions

The system shall bound repeated high-risk actions during a session.

### Acceptance criteria

- the runtime tracks configurable per-session limits for at least tool calls, subprocess executions, outbound web/tool calls, and subagent spawns where enabled
- over-limit actions fail fast with a bounded error instead of retrying indefinitely
- quota enforcement does not break normal history persistence or session isolation
- limits default to conservative values and can be raised explicitly by config

# Non-functional constraints

- Keep the implementation inside existing packages such as `internal/tools`, `internal/channels`, `internal/config`, `internal/agent`, `internal/mcp`, and `internal/db`
- Favor deterministic checks and small in-memory counters; avoid background daemons or distributed coordination
- Keep SQLite changes minimal and migration-safe if pairing persistence is added
- Preserve bounded command execution, bounded output, and safe-by-default handling of files, network access, and secrets
- Do not change message history, memory retrieval, or channel session keys except where required to preserve sender isolation
