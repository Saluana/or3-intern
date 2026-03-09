# Overview

Phase 2 extends the existing runtime in four narrow places: skill metadata loading, trigger event publication, privileged subprocess launch, and CLI subcommands. This fits the current architecture because the repo already centralizes skills in `internal/skills`, autonomous triggers in `internal/triggers` and `internal/heartbeat`, tool execution in `internal/tools`, and command dispatch in `cmd/or3-intern`.

# Affected areas

- `internal/skills/skills.go` and related tests
  - extend skill frontmatter or manifest parsing to include permission declarations and approval/quarantine state
- `cmd/or3-intern/skills_cmd.go`
  - expose approval/quarantine status in `skills list`, `skills info`, and `skills check`, and optionally add approve/quarantine subcommands if needed
- `internal/tools/skill_exec.go` and related runtime policy code
  - enforce skill permissions and quarantine restrictions before running skill entrypoints or scripts
- `internal/triggers/filewatch.go`, `internal/triggers/webhook.go`, `internal/heartbeat/service.go`, and `internal/bus/bus.go`
  - add stable structured event metadata and keep bounded textual fallbacks for existing runtime behavior
- `internal/agent/prompt.go` and `internal/agent/runtime.go`
  - surface structured autonomous context to the model and make tool decisions depend on trusted event metadata where appropriate
- `internal/tools/exec.go` and privileged execution helpers
  - route privileged execution through an optional Bubblewrap wrapper
- `cmd/or3-intern/main.go` and config/tests
  - wire sandbox config and add a `doctor` subcommand

# Control flow / architecture

1. Skills are scanned as they are today from `SKILL.md` and optional `skill.json`.
2. Additional permission metadata is loaded from frontmatter or manifest fields and normalized into skill runtime metadata.
3. When a skill executes, runtime checks the declared permissions plus approval/quarantine state before allowing shell, network, or write-capable actions.
4. Trigger producers publish existing bus events, but include a structured payload in `Meta` describing the event source and bounded details.
5. Prompt building and runtime use the structured event payload as authoritative context while retaining the current text seed for backward compatibility.
6. Privileged subprocess actions optionally use a Bubblewrap launcher when enabled and available.
7. `or3-intern doctor` runs a set of deterministic config/runtime checks and prints findings without starting channels or workers.

# Data and persistence

- **Config changes:** add small config for:
  - skill approval/quarantine defaults
  - structured event mode enablement if needed
  - Bubblewrap executable path and minimal sandbox options
  - doctor strict mode defaults if desired
- **SQLite changes:** none are strictly required if skill approval state can live in config or skill install metadata. If operator approvals need durable runtime storage, add one small additive table such as `skill_permissions(skill_name, approved, mode, updated_at)`.
- **Session/memory impact:** structured autonomous event data should stay in event metadata and prompt context; existing message history and memory tables do not need schema changes.

# Interfaces and types

Likely additions:

```go
type SkillPermissions struct {
    NeedsShell bool
    NeedsNetwork bool
    NeedsWrite bool
    AllowedPaths []string
    AllowedHosts []string
}
```

```go
type StructuredTriggerEvent struct {
    EventType string
    Source string
    Path string
    Route string
    Trusted bool
    Details map[string]any
}
```

```go
type SandboxConfig struct {
    Enabled bool
    BubblewrapPath string
    WorkspaceOnly bool
}
```

The existing `bus.Event` type can likely carry structured trigger data in `Meta` without requiring a new bus abstraction.

# Failure modes and safeguards

- malformed skill permission metadata should not crash skill discovery; the skill should become blocked or quarantined with a visible reason
- quarantined skills must fail closed when they request shell, network, or write access beyond their approved manifest
- oversized webhook bodies or file-watch metadata must stay bounded before structured event metadata is attached
- Bubblewrap lookup or execution failure must deny privileged execution rather than silently running outside the sandbox
- `doctor` should continue reporting partial results even if one check fails, while still surfacing config parse errors clearly
- structured event mode must not trust inbound webhook payload contents as already-approved tool instructions

# Testing strategy

- extend `internal/skills` tests for permission parsing, quarantine defaults, and operator-visible status output
- add tool tests for skill-exec denial and approval behavior
- add trigger and heartbeat tests verifying structured metadata shape, bounded payloads, and backward-compatible text messages
- add privileged exec tests for Bubblewrap enabled/disabled/unavailable behavior
- add CLI tests for `doctor` output and strict-mode exit behavior under safe and unsafe configs
