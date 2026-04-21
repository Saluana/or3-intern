# Design

## Overview

`or3-intern doctor` should become the control center for local runtime health:

- the canonical source of startup gating;
- the primary way to understand why a configuration is weak, broken, or incomplete;
- the repair workflow for deterministic local issues; and
- the machine-readable readiness contract for CI and docs.

The key architectural change is to stop treating doctor as a thin CLI command in `cmd/or3-intern` and instead treat it as a reusable health engine consumed by:

- `or3-intern doctor`
- `or3-intern configure`
- startup paths in `chat`, `serve`, and `service`
- tests and fixture validation

This fits the current repo because the main issues are not missing ideas; they are split logic and shallow outputs:

- config validation lives in `internal/config`
- startup blocking logic lives in `cmd/or3-intern/startup_validation.go`
- advisory warnings live in `cmd/or3-intern/doctor.go`
- setup/help flows surface some failures but do not consistently guide repair

The doctor overhaul should collapse those paths into one engine with policy-aware reporting and bounded fixers.

## Affected areas

- `cmd/or3-intern/doctor.go`
  - Replace the current ad hoc warning printer with a thin CLI wrapper over the shared engine.

- `cmd/or3-intern/startup_validation.go`
  - Remove duplicated startup logic and convert startup to consume doctor findings in a startup policy mode.

- `cmd/or3-intern/main.go`
  - Route `doctor`, `serve`, `service`, and `chat` through shared health evaluation entry points.

- `cmd/or3-intern/configure.go`
  - Invoke doctor after save and surface findings plus available repairs.
  - Reuse doctor fixers where setup can safely repair issues immediately.

- `cmd/or3-intern/help.go`
  - Update help text so doctor is described as readiness and repair, not only audit.

- `internal/config/config.go`
  - Remains the source of truth for config semantics and hard validation.
  - Doctor should consume these semantics, not fork them.

- `internal/security/*`
  - Reuse key generation/loading helpers, audit/secret-store readiness, and network policy semantics.

- `internal/db/*`
  - Reuse DB open/migration helpers for runtime readiness checks and safe local fix flows where relevant.

- New shared package such as `internal/doctor`
  - Own finding types, check/fixer interfaces, policy evaluation, and report generation.

- Docs:
  - `README.md`
  - `docs/getting-started.md`
  - `docs/cli-reference.md`
  - `docs/security-and-hardening.md`

## Core architecture

### 1. Shared doctor engine

Introduce a small reusable package, e.g. `internal/doctor`, with three responsibilities:

1. collect findings from named checks
2. apply policy to findings for a given caller context
3. optionally run bounded fixers

Suggested high-level types:

```go
type Severity string

const (
    SeverityInfo  Severity = "info"
    SeverityWarn  Severity = "warn"
    SeverityError Severity = "error"
    SeverityBlock Severity = "block"
)

type FixMode string

const (
    FixModeNone        FixMode = "none"
    FixModeAutomatic   FixMode = "automatic"
    FixModeInteractive FixMode = "interactive"
    FixModeManual      FixMode = "manual"
)

type Finding struct {
    ID          string
    Area        string
    Severity    Severity
    Summary     string
    Detail      string
    Evidence    []string
    FixMode     FixMode
    FixHint     string
    BlockingFor []string
}

type Report struct {
    Findings []Finding
    Summary  ReportSummary
}
```

The CLI should not invent warnings itself. It should render a `Report`.

### 2. Policy-aware evaluation

Doctor needs to answer slightly different questions depending on who is asking.

Recommended policy modes:

- `advisory`
  - default `or3-intern doctor`
  - prints all findings but does not fail unless `--strict`

- `strict`
  - `or3-intern doctor --strict`
  - fails on warnings/errors/blocks according to policy

- `startup-chat`
- `startup-serve`
- `startup-service`
  - use the same findings but elevate or suppress findings depending on the command’s risk surface

- `configure-post-save`
  - allows setup to show “you are still not runnable” versus “you are ready”

This replaces the current split between `doctor.go` and `startup_validation.go`.

### 3. Check families

Checks should be grouped by real operational surface rather than by arbitrary file location.

#### Config checks

Pure config evaluation:

- required field presence
- contradictory settings
- ingress/profile/network contradictions
- runtime profile drift
- existing `internal/config` validation reuse

#### Filesystem checks

Local path and permission checks:

- DB path parent exists or can be created
- artifacts dir exists or can be created
- workspace dir exists when required
- key files exist or are creatable
- configured file paths are readable/writable where relevant

#### Runtime readiness checks

Local machine prerequisites:

- SQLite open succeeds
- bubblewrap path exists when sandboxing is enabled
- bind address parses correctly
- secret store/audit key setup is operational, not just configured

#### Exposure checks

Cross-surface safety checks:

- open ingress plus profiles disabled
- public ingress plus privileged profile
- webhook/service/channel posture versus runtime profile
- approvals plus missing broker/key posture

#### Integration checks

External integration sanity:

- MCP transport posture
- network policy adequacy for remote MCP
- channel token/config completeness
- webhook secret/listen posture
- service secret strength and bind posture

#### Optional live probes

Bounded probes, likely behind a flag such as `--probe`:

- attempt SQLite open/migration
- check bubblewrap executable resolution
- optionally HEAD/dial configured endpoints with short timeouts
- optionally verify that a configured service bind would be reachable on loopback

These should remain bounded and opt-in when they reach outside the local machine.

## Fixer model

Doctor becomes useful only if it can close the loop for common failures.

### Fix categories

#### Automatic fixers

Safe, deterministic, low-risk:

- create missing directories
- generate missing key files
- normalize invalid or missing channel ingress defaults to a safe state
- tighten listen addresses back to loopback in local/private modes
- populate missing quota defaults

#### Interactive fixers

Needed when multiple safe answers exist:

- enabled Telegram/Slack/Discord/Email with no valid inbound policy
- public ingress with no effective access profile
- missing service secret in a setup flow that could either generate or disable service
- secret store disabled while external integrations are enabled

#### Manual-only fixes

Still surfaced clearly:

- remote endpoint unreachable due to external infrastructure
- broad allowlists that require human narrowing
- profile/tool policy decisions with business impact

### Fix transaction model

Fixes must be conservative:

- stage config changes in memory
- validate after applying staged changes
- write once at the end
- report exactly what changed
- abort without partial mutation if the fix cannot complete

This can reuse the same config-save model already used by `configure`.

## CLI behavior

### Proposed command shape

Keep the current root command but expand flags:

```text
or3-intern doctor
or3-intern doctor --strict
or3-intern doctor --json
or3-intern doctor --fix
or3-intern doctor --fix --interactive
or3-intern doctor --area channels
or3-intern doctor --severity block
or3-intern doctor --probe
```

### Human-readable output

Default output should look like a readiness report, not a warning dump.

Suggested sections:

- overall status
- blockers
- warnings
- fixes available
- next steps

For example:

```text
Status: not ready for serve

Blockers:
- channels.telegram.invalid_ingress: Telegram is enabled without pairing, allowlist, or open access policy.
  Fix available: interactive

Warnings:
- security.audit.disabled: audit logging is disabled.

Suggested commands:
- or3-intern doctor --fix --interactive
- or3-intern configure --section channels
```

### JSON output

`--json` should emit the same report model used internally so tests and CI do not scrape human text.

## Configure and startup integration

### Configure

After a save, `configure` should invoke doctor in `configure-post-save` mode and show:

- ready
- ready with warnings
- not ready, with top blockers and either fix actions or the exact follow-up command

This makes doctor part of the main setup experience.

### Startup

Startup should stop maintaining a separate findings model.

Recommended flow:

1. load config
2. run doctor engine in startup mode for the command
3. if report contains blocking findings, print the top findings and exit
4. continue bootstrap

This makes startup and doctor consistent by construction.

## Baselines and drift

Doctor should become the user-facing explanation layer for `runtimeProfile`.

For each supported profile:

- `local-dev`
- `single-user-hardened`
- `hosted-service`
- `hosted-no-exec`
- `hosted-remote-sandbox-only`

doctor should explain:

- controls satisfied
- controls missing
- controls contradicted

This should remain report-layer logic. It should not replace `internal/config.ValidateProfile`; it should explain it.

## Suggested package layout

```text
internal/doctor/
  engine.go        // orchestration
  finding.go       // finding/report types
  policy.go        // advisory/strict/startup policies
  checks_config.go
  checks_fs.go
  checks_runtime.go
  checks_exposure.go
  checks_integrations.go
  fixers.go
  render.go        // optional shared text/json rendering helpers
```

The CLI entrypoint in `cmd/or3-intern/doctor.go` should stay thin.

## Safeguards and boundaries

- Do not silently weaken protections during auto-fix.
- Do not overwrite unrelated user settings while repairing a narrow issue.
- Keep live probes bounded and off by default where they might surprise the user.
- Do not require a second persistence system for doctor.
- Do not build a background doctor daemon or web dashboard in the first pass.

## Rollout strategy

### Phase 1: unification

- introduce shared finding/report types
- migrate current doctor checks into the engine
- replace startup duplication with doctor policy modes

### Phase 2: real local readiness

- add filesystem/runtime checks
- add baseline drift reporting
- improve human-readable and JSON output

### Phase 3: repair

- add `--fix`
- add interactive repair for ambiguous cases
- wire configure into post-save doctor evaluation

### Phase 4: optional probes and CI contract

- add bounded live probes
- add fixture-driven `--strict --json` tests
- update docs to make doctor central to the project workflow

## Out of scope for the first implementation pass

- A web UI, desktop UI, or background agent for doctor
- Long-running health monitoring
- Cross-host fleet management
- Automatic remote infrastructure repair
- New persistent doctor state, metrics service, or external control plane
