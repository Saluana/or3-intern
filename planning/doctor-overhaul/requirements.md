# Requirements

## Overview

This plan turns `or3-intern doctor` from a passive warning printer into the project’s central health, readiness, and repair workflow.

Scope assumptions:

- `or3-intern` stays a Go CLI-first runtime with optional service mode, channels, triggers, approvals, and MCP integrations.
- SQLite remains the single local persistence layer; the first implementation does not require a new database or daemon for doctor itself.
- The same health engine should power CLI doctor output, startup gating, configure follow-up checks, and machine-readable CI checks.
- The first implementation should prefer deterministic local inspection and bounded live checks over speculative or long-running automation.

## 1. Doctor becomes the single health engine for readiness and startup

`doctor` must become the canonical source of truth for whether a given config and environment are safe, complete, and runnable.

Acceptance criteria:

- Startup gating in `chat`, `serve`, and `service` reuses the same doctor engine rather than a partially duplicated ruleset.
- Doctor can evaluate at least three policy modes: advisory, strict CLI, and startup-gate.
- Findings that block startup are represented explicitly instead of being inferred from ad hoc warning strings.
- `configure` and related setup flows can invoke doctor after edits and surface the same findings model.

## 2. Findings must be structured, actionable, and stable

Doctor output must stop being free-form warning text only.

Acceptance criteria:

- Each finding has a stable identity, severity, area, summary, detailed explanation, and remediation guidance.
- Findings distinguish at minimum: `info`, `warn`, `error`, and `block`.
- Findings indicate whether they are auto-fixable, require confirmation, or are manual-only.
- Findings include enough structured context for JSON output and for targeted filtering by area, severity, and fixability.

## 3. Doctor must inspect the real local runtime, not just config shape

Doctor must catch issues that only appear when the local machine, filesystem, keys, directories, binaries, DB, or network posture are actually examined.

Acceptance criteria:

- Doctor checks configured paths for existence, readability, writability, and sane permissions where relevant.
- Doctor verifies SQLite can open and that expected runtime directories can be created or written.
- Doctor verifies runtime prerequisites such as Bubblewrap path existence when sandboxing is enabled.
- Doctor validates that configured listen addresses, key files, artifact directories, and workspace restrictions are operational rather than only syntactically present.

## 4. Doctor must provide safe repair workflows

Doctor should not stop at diagnostics when the fix is deterministic and low-risk.

Acceptance criteria:

- `doctor --fix` applies a bounded set of safe automatic remediations.
- Auto-fixable repairs include at least: missing directory creation, missing security key generation, invalid channel ingress default repair, and safe loopback bind correction where policy allows.
- `doctor --fix --interactive` handles ambiguous cases by presenting the user with explicit choices instead of guessing silently.
- Fix operations produce a clear before/after summary and fail safely without leaving partially written config when a repair cannot complete.

## 5. Doctor must be baseline-aware and explain drift clearly

Doctor must help operators move toward a coherent operating posture instead of printing isolated warnings.

Acceptance criteria:

- Doctor can evaluate drift against the active `runtimeProfile`.
- Doctor can explain which baseline controls are satisfied, missing, or contradicted.
- The report can group findings by baseline area such as ingress, secrets, audit, profiles, network, exec, and integrations.
- Setup and docs can point users toward “green” baseline states instead of only telling them what is wrong.

## 6. Doctor must support both human and machine consumers

Doctor should be usable interactively by operators and also reliable in automation.

Acceptance criteria:

- Human-readable output is prioritized by default and groups blockers, warnings, and available fixes.
- `--json` produces a stable machine-readable report suitable for CI and external tooling.
- Doctor supports filtering such as `--area`, `--severity`, and `--fixable-only`.
- `--strict` exits non-zero on policy-relevant findings using the same structured severity model.

## 7. Doctor must cover the project’s real exposure surfaces

Doctor must reason across the same surfaces that make `or3-intern` risky or broken in practice.

Acceptance criteria:

- Checks cover at least: config validation, filesystem/runtime readiness, secret store, audit, approvals, channels, webhook, service mode, profiles, network policy, MCP, exec posture, skill execution, and runtime profiles.
- Checks reason across combined exposure, such as open ingress plus permissive profiles plus privileged tools.
- Checks detect internally contradictory states such as enabled channels with broken ingress policy, service exposure without auth posture, or hosted profiles without required controls.
- Existing validation and doctor-only heuristics are merged into a coherent layered model instead of remaining scattered across config load, startup, and CLI paths.

## 8. Doctor must become a first-class part of setup, operations, and CI

Doctor should be central to the user workflow rather than an optional extra.

Acceptance criteria:

- `configure` ends with a doctor summary and recommends or offers remediation when findings exist.
- Startup failures point users back to `doctor` or directly embed top blocking findings from the doctor engine.
- Documentation treats doctor as the canonical readiness gate before enabling channels, webhook, service mode, or remote MCP.
- CI can run `doctor --strict --json` against fixture configs as a regression contract.

## 9. The first implementation must stay repo-aligned and bounded

This overhaul should materially improve usefulness without inventing a second control plane.

Acceptance criteria:

- The design fits the current Go CLI/runtime structure and prefers extending `cmd/or3-intern`, `internal/config`, `internal/security`, and related internal packages.
- The first implementation does not require a new long-running doctor daemon, background job queue, or web UI.
- Live checks are bounded by timeouts and do not require external services to be available indefinitely.
- Auto-fix remains conservative and does not silently widen privilege, disable protections, or overwrite unrelated user settings.

## Non-functional constraints

- Keep doctor deterministic and cheap by default; more expensive live checks should be opt-in or tightly bounded.
- Preserve backward-compatible config loading and upgrade behavior.
- Avoid destructive repairs and avoid changing unrelated config sections during auto-fix.
- Keep the report format stable enough for tests and documentation.
- Prefer additive changes that reuse existing config semantics, startup wiring, and security helpers.
