# Tasks

## 1. Introduce a shared doctor engine

- [ ] (Req 1, 2, 7) Create a focused shared package such as `internal/doctor` that owns findings, reports, policy modes, and check orchestration.
- [ ] (Req 1, 2) Replace the current `doctorFinding` string-only model with structured finding/report types that include stable IDs, severity, summary/detail text, and fix metadata.
- [ ] (Req 1, 6) Keep `cmd/or3-intern/doctor.go` as a thin CLI wrapper that parses flags, runs the engine, and renders human or JSON output.

## 2. Unify startup gating with doctor

- [ ] (Req 1, 7, 8) Refactor `cmd/or3-intern/startup_validation.go` so startup commands consume doctor reports instead of maintaining a partially duplicated findings path.
- [ ] (Req 1, 8) Define command-aware policy modes for at least `doctor advisory`, `doctor strict`, `startup chat`, `startup serve`, `startup service`, and `configure post-save`.
- [ ] (Req 1, 6, 8) Update `cmd/or3-intern/main.go` so startup failures print top blocking findings and point back to `or3-intern doctor` or an exact fix command.

## 3. Migrate existing config-only checks into structured check families

- [ ] (Req 2, 7) Move current warning logic from `cmd/or3-intern/doctor.go` into named check groups such as config, exposure, integrations, runtime-profile, exec, skills, channels, and approvals.
- [ ] (Req 2, 5, 7) Assign stable finding IDs to the current warning families so tests and JSON output do not depend on human prose.
- [ ] (Req 2, 6) Preserve existing advisory semantics during the migration while upgrading the internal model.

## 4. Add real filesystem and local runtime readiness checks

- [ ] (Req 3, 7) Add checks for workspace path existence, DB parent dir existence/creatability, artifacts dir existence/creatability, and key file readiness.
- [ ] (Req 3) Add checks that Bubblewrap exists when sandboxing is enabled and that configured bind addresses parse correctly.
- [ ] (Req 3, 7) Add a bounded SQLite readiness check that verifies the configured DB path can be opened without forcing a full runtime bootstrap.
- [ ] (Req 3) Add permission-oriented checks where they are high-signal, such as obviously unsafe/missing key-file handling.

## 5. Add baseline and runtime-profile drift reporting

- [ ] (Req 5, 7) Teach doctor to explain drift against the active `runtimeProfile`, not only list isolated warnings.
- [ ] (Req 5) Add grouped report sections for baseline areas such as ingress, secrets, audit, network, profiles, exec, and integrations.
- [ ] (Req 5, 8) Make configure and startup surfaces reuse the same baseline/drift explanations when reporting readiness.

## 6. Add human- and machine-friendly report rendering

- [ ] (Req 2, 6) Redesign default doctor output around overall status, blockers, warnings, available fixes, and next steps instead of a flat warning list.
- [ ] (Req 6) Add `--json` output for the structured report.
- [ ] (Req 6) Add filters such as `--area`, `--severity`, and `--fixable-only`.
- [ ] (Req 6) Keep `--strict` but make it operate on the structured severity model.

## 7. Add safe automatic fixers

- [ ] (Req 4, 9) Define a bounded fixer interface and a fix plan/apply flow that stages changes before writing them.
- [ ] (Req 4) Implement deterministic automatic fixers for missing directories, missing security key files, unset quota defaults, and other clearly safe local remediations.
- [ ] (Req 4) Implement safe config repair for broken channel ingress states, defaulting to conservative outcomes when auto-fix is unambiguous.
- [ ] (Req 4, 9) Ensure fixer failures do not leave partial config writes or partially generated state behind.

## 8. Add interactive repair for ambiguous cases

- [ ] (Req 4, 8, 9) Add `doctor --fix --interactive` for cases like enabled channels without valid ingress policy, missing service secret, missing profile mappings, or secret-store posture decisions.
- [ ] (Req 4) Reuse existing CLI prompt/TUI infrastructure where practical instead of inventing a second interaction stack.
- [ ] (Req 4, 8) Make configure able to hand off directly into doctor-guided repair when the saved config is still not runnable.

## 9. Add optional bounded live probes

- [ ] (Req 3, 9) Add an opt-in probe mode such as `--probe` for checks that touch the live local environment beyond pure config inspection.
- [ ] (Req 3) Keep probes bounded by strict timeouts and clear output so users know what was actually tested.
- [ ] (Req 9) Limit first-pass probes to local/runtime-safe operations unless a clearly useful remote probe is justified and tightly bounded.

## 10. Wire doctor into setup and operations

- [ ] (Req 1, 8) Update `cmd/or3-intern/configure.go` so a successful save ends with a structured doctor summary and, when possible, offered repair actions.
- [ ] (Req 8) Update command help and top-level UX so doctor is presented as readiness + repair rather than only audit.
- [ ] (Req 8) Update docs in `README.md`, `docs/getting-started.md`, `docs/cli-reference.md`, and `docs/security-and-hardening.md` so doctor is the canonical gate before exposed ingress or service mode.

## 11. Add regression and fixture coverage

- [ ] (Req 6, 8, 9) Expand `cmd/or3-intern/doctor_test.go` or migrate tests to `internal/doctor/*_test.go` to cover finding IDs, severities, render behavior, and fix availability.
- [ ] (Req 1, 8) Add startup gating tests proving startup modes accept/reject based on doctor reports rather than duplicated logic.
- [ ] (Req 4, 9) Add fixer tests for automatic repairs, no-partial-write guarantees, and interactive-choice plumbing.
- [ ] (Req 6, 8) Add fixture-driven `doctor --strict --json` tests suitable for CI.

## 12. Out of scope for the first implementation pass

- [ ] Do not build a separate doctor daemon, health API, or web dashboard.
- [ ] Do not add persistent doctor state or a second database.
- [ ] Do not attempt unbounded remote infrastructure repair.
- [ ] Do not silently widen privilege or relax security controls as part of auto-fix.
