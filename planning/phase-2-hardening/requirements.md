# Overview

This plan covers **Phase 2** from `planning/analysis/suggestions.md`: the next lightweight hardening pass after Phase 1, focused on skills, autonomous trigger inputs, a single privileged sandbox path, and a lightweight operator audit command.

Scope covers:

- skill permission manifests and quarantine mode
- structured event inputs for heartbeat, webhook, and file-watch flows
- one Bubblewrap-backed privileged execution path
- an `or3-intern doctor` hardening audit command

Assumptions:

- Phase 1 capability tiers, workspace confinement, safer exec defaults, and basic quotas are already in place or planned as the baseline
- the repo remains a single-process Go CLI with SQLite and optional external channels
- Phase 2 should stay lightweight and avoid introducing a full sandbox matrix or policy engine

# Requirements

## 1. Add skill permission manifests and quarantine mode

The system shall treat installed or workspace skills as permissioned components instead of implicitly trusted scripts.

### Acceptance criteria

- skill metadata can declare whether a skill needs shell/process execution, outbound network access, write access, specific workspace paths, or specific hosts
- skills without explicit approval run in a quarantine mode that denies privileged execution, unrestricted network, and write access
- `skills check` or equivalent operator output shows whether a skill is approved, quarantined, or blocked by missing permissions
- existing skill discovery remains backward compatible, but unpermissioned execution is no longer silently treated as trusted

## 2. Add structured autonomous event inputs

The system shall support a structured event payload for heartbeat, webhook, and file-watch triggers alongside the current plain-text prompt path.

### Acceptance criteria

- heartbeat, webhook, and file-watch events can publish a stable structured payload in event metadata describing source, type, and bounded details
- runtime can inject structured event context into autonomous turns without requiring raw natural-language-only prompts
- structured event handling does not break existing autonomous flows that still rely on plain text messages
- trigger payloads remain bounded in size and preserve session isolation

## 3. Add one privileged Bubblewrap execution path

The system shall support a single optional Bubblewrap-backed sandbox for privileged subprocess execution on supported systems.

### Acceptance criteria

- privileged exec and privileged skill execution can route through Bubblewrap when explicitly enabled in config
- safe and guarded tools continue to run without an external sandbox backend
- when Bubblewrap is unavailable or unsupported, privileged execution is denied by default rather than falling back to unrestricted host execution
- sandbox configuration stays minimal and centered on workspace/filesystem isolation, bounded temp paths, and restricted network/process behavior

## 4. Add a lightweight doctor hardening audit command

The system shall provide a CLI command that checks the current configuration for common unsafe deployment choices.

### Acceptance criteria

- `or3-intern doctor` or an equivalent command reports warnings for unsafe settings such as open external channels, unrestricted privileged tools, unrestricted filesystem access, inherited child environments, missing quotas, or unsafe webhook bind/secret settings
- the command reads current config and emits deterministic, operator-readable results without mutating runtime state
- findings are bounded and grouped so operators can act on them quickly
- the command exits successfully for informational runs and can optionally return a non-zero status when strict mode is requested

## 5. Preserve existing runtime compatibility

The system shall keep current sessions, memory behavior, and CLI/channel flows compatible while adding Phase 2 controls.

### Acceptance criteria

- skill metadata and quarantine state load without breaking existing skill discovery for bundled, managed, or workspace skills
- structured trigger metadata does not change session keys or normal history persistence rules
- the CLI, channels, cron, and subagent flows continue to function when Phase 2 features are disabled
- any SQLite changes used for skill approval or doctor state are additive and migration-safe

# Non-functional constraints

- Favor small changes in `internal/skills`, `internal/tools`, `internal/triggers`, `internal/heartbeat`, `internal/agent`, `internal/config`, and `cmd/or3-intern`
- Keep structured trigger data bounded and cheap to serialize; do not introduce a second event bus or external queue
- Bubblewrap support must remain optional, Linux-first, and isolated to privileged paths only
- Quarantine and doctor checks must be deterministic and safe by default
- Avoid adding multiple sandbox backends, a heavyweight policy DSL, or broad background services in this phase
