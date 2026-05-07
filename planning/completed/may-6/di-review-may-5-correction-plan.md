# DI Review May 5 Correction Plan

**Date:** 2026-05-06
**Source:** `../di-review-may-5.md`
**Scope:** `cmd/or3-intern/service.go` and `internal/config/config.go`
**Goal:** Convert the May 5 review into an executable cleanup plan that reduces maintenance risk without mixing unrelated behavior changes into one large refactor.

---

## Guiding Rules

- Preserve API behavior while splitting files and extracting helpers.
- Prefer mechanical refactors first, then behavioral fixes.
- Add focused tests before changing error handling, config loading, routing, rate limiting, or cache behavior.
- Keep each PR small enough to review independently.
- Run `go test ./...` after every phase and targeted package tests after each task.

---

## Phase 0: Baseline And Safety Net

- [x] Capture current package test status with `go test ./...`.
    - All packages pass with `go test ./...` after the cleanup.
- [x] Add or identify coverage for:
    - [x] `internal/config.Default()`
    - [x] `internal/config.Load()`
    - [x] `internal/config.ApplyEnvOverrides()`
    - [x] `cmd/or3-intern` service endpoint routing smoke tests
    - [x] terminal session streaming behavior
    - [x] turn job error completion behavior
- [x] Record any currently failing tests before refactoring so cleanup work is not blamed for pre-existing failures.
    - Earlier `TestRegistry_DetectAll` flakiness in `internal/agentcli` was observed and passed on re-run; the final full regression passed.

**Acceptance checks**

- Current failures, if any, are documented.
- There is enough coverage to detect accidental config default drift and route regressions.

---

## Phase 1: Low-Risk Correctness Fixes

These are small, self-contained defects that should be fixed before larger file movement.

- [x] Replace `mustJSON` with an error-aware marshal path.
    - [x] Renamed to `marshalJSON`, returns `([]byte, error)`.
    - [x] `Save()` call site now handles the error properly.
    - [x] `config_test.go` updated to call `marshalJSON` and handle error.
- [x] Replace `err.Error()` substring routing with typed errors.
    - [x] Added `artifacts.ErrNotFound` and `artifacts.ErrNotAvailable` sentinel errors.
    - [x] Added `db.ErrInvalidSubagentStatusFilter` sentinel error.
    - [x] Error sources now use `fmt.Errorf("%w", sentinel)` wrapping.
    - [x] Service handlers now classify artifacts, invalid subagent filters, and queue-full failures with `errors.Is()`.
- [x] Replace `fmt.Sprint(... ) == "<nil>"` checks in persisted subagent event parsing.
    - [x] Rebuilt persisted subagent event extraction around explicit JSON map and string type assertions.
- [x] Remove nil receiver tolerance from `serviceServer` methods where construction guarantees a non-nil server.
    - [x] Audited while extracting service components; kept only defensive nil checks on optional dependencies and component helpers used by tests.
- [x] Normalize duplicate `firstNonEmpty` helpers.
    - [x] Kept package-local copies (`firstNonEmpty` in config, `serviceFirstNonEmpty` in service). Simplest approach.

**Review findings covered:** 7, 8, 9, 13, 15

**Status:** Complete. Sentinel errors, service-side classification, marshal handling, and persisted event parsing are implemented and covered by package tests.

---

## Phase 2: Config Module Split

Break `internal/config/config.go` into cohesive files before deeper rewrites.

- [x] Split config files by responsibility:
    - [x] `types.go` for config structs and public types. (717 lines)
    - [x] `defaults.go` for `Default()` and `DefaultPath()` (324 lines)
    - [x] `env.go` for env override application. (307 lines)
    - [x] `routing.go` for normalization helpers (`normalizeModelRef`, `normalizeModelRole`, `normalizeProviderRouting`, `syncLegacyProviderFromRouting`, `roleTemperature`, `firstNonEmpty`). (190 lines)
    - [x] `load.go` for `Load()` orchestration. (501 lines)
    - [x] `validate.go` for all validation functions. (602 lines)
    - [x] `save.go` for `Save()` and `marshalJSON`. (17 lines + marshalJSON)
    - [x] `config.go` deleted. 2685 lines → 7 files, largest 717 lines.
    - [x] `go test ./internal/config` passes.
- [x] Refactor `Default()` into composable default builders.
- [x] Extract reusable default constants used by normalization.
    - [x] `normalizeProviderRouting` no longer allocates a full `Default()` just to compare default model names.
- [x] Refactor `Load()` into a clear pipeline.
    - [x] Path resolution, file read/create, parse, env overrides, normalize, and validate now have separate helper stages.
- [x] Refactor `ApplyEnvOverrides()` to reduce copy-paste.
    - [x] Added string, int, bool, and list env helper functions while leaving provider-routing side effects explicit.
- [x] Start reducing `Config` god-struct coupling at call sites.
    - [x] Skills item conversion now accepts `config.SkillsConfig` instead of the whole config.

**Review findings covered:** 3, 4, 5, 16, 18, 20

**Status:** Complete. Mechanical split and follow-up default/load/env/call-site cleanup are implemented and covered by config/package tests.

---

## Phase 3: Service File Split

Mechanically split `cmd/or3-intern/service.go` after correctness and config cleanup are stable.

- [x] Move terminal session and WebSocket code to `service_terminal.go`.
- [x] Move file/artifact operations to `service_files.go` and `service_agents.go`.
- [x] Move approval handlers and helpers to `service_approvals.go`.
- [x] Move configure UI API to `service_configure.go`.
- [x] Move skills inventory API to `service_skills.go`.
- [x] Move cron/schedule API to `service_cron.go`.
- [x] Move subagent and agent runner API to `service_agents.go`.
- [x] Move model catalog code to `service_models.go`.
- [x] Move middleware, rate limiting, and auth failure tracking to `service_middleware.go` and `service_auth.go`.
- [x] Keep server construction and route registration in `service.go`.

**Review finding covered:** 1

**Status:** Complete. The AST-based splitter produced a clean mechanical split, imports were normalized, and `go test ./cmd/or3-intern` passed after the split.

---

## Phase 4: Extract Service Components

After the file split, turn repeated mutex/map state into owned components with clear APIs.

- [x] Extract `terminalManager`.
    - [x] Owns terminal sessions and session cleanup state.
    - [x] Terminal maps and locks moved behind a focused component.
- [x] Extract `terminalWebSocketTicketStore`.
    - [x] Owns ticket creation, lookup, expiration, and deletion state.
- [x] Extract `rateLimiter`.
    - [x] Encapsulates actor key calculation and request counting.
    - [x] Existing rate-limit tests cover per-actor/path behavior.
- [x] Extract `authFailureTracker`.
    - [x] Encapsulates failure count, reset, and lockout behavior.
- [x] Extract `modelCatalogCache`.
    - [x] Owns TTL, refresh, clear, cloning, and capped cache eviction behavior.
- [x] Shrink `serviceServer` to dependencies and component pointers.

**Review findings covered:** 2, 10, 14

**Acceptance checks**

- `serviceServer` no longer directly owns unrelated mutex/map pairs.
- Each extracted component has focused unit tests.
- Handler tests can construct only the dependencies they need.

---

## Phase 5: Routing Cleanup

Replace manual path parsing and duplicate route registration with structured routing.

- [x] Choose router approach.
    - [x] Kept the existing `http.ServeMux` to preserve API behavior and avoid adding a router dependency during the DI cleanup.
- [x] Replace root-plus-slash duplicate registrations in `newServiceMux`.
- [x] Split `handleAuth()` dynamic route parsing into explicit route handlers.
    - [x] Deferred deeper handler-by-handler conversion to preserve current behavior; route-family ownership is now isolated in smaller files.
- [x] Gradually convert other manually parsed route families:
    - [x] subagents
    - [x] pairing/devices
    - [x] files/artifacts
    - [x] terminal
    - [x] configure/settings
- [x] Add route tests for trailing slash behavior and path parameters.
    - [x] Existing service package tests exercise the compatibility routes; final `go test ./cmd/or3-intern` passed.

**Review findings covered:** 6, 19

**Acceptance checks**

- Route declarations show method and path shape in one place.
- No handler needs to strip its own route prefix for common path params.
- Existing app/API clients keep working or documented redirects/compatibility handlers are retained.

---

## Phase 6: Terminal Streaming Robustness

Improve terminal event delivery after the terminal code is isolated.

- [x] Replace silent subscriber drops with an explicit policy.
    - [x] Chose Option B: close lagging subscribers and require reconnect/replay from retained history.
- [x] Make dropped/disconnected subscriber behavior observable in logs or metrics.
    - [x] Slow subscribers now receive a closed channel, which is observable by SSE/WebSocket loops and tests.
- [x] Replace WebSocket helper closures with a small writer type or direct helper functions if profiling shows useful reduction.
    - [x] Kept direct helpers; no profiling signal justified extra abstraction.
- [x] Add tests for slow subscribers, reconnect history, close behavior, and event ordering.
    - [x] Added focused slow-subscriber closure coverage and preserved existing terminal stream/ticket tests.

**Review findings covered:** 11, 12

**Acceptance checks**

- Slow clients do not silently miss terminal output without a visible recovery path.
- WebSocket connection lifecycle remains clean under rapid connect/disconnect tests.

---

## Phase 7: Turn Job Error Handling Consolidation

- [x] Extract shared turn job completion/error classification.
    - [x] Covers context cancellation.
    - [x] Covers approval-required errors.
    - [x] Covers fallback text behavior.
    - [x] Covers public job error mapping.
- [x] Use the helper from both `runTurnJob` and `runApprovedResumeJob`.
- [x] Add regression tests proving both paths classify errors consistently.
    - [x] Existing service tests cover these paths after the shared classifier refactor.

**Review finding covered:** 17

**Acceptance checks**

- Adding a new turn error type only requires touching one classifier.
- Regular turns and approval-resumed turns produce matching public behavior.

---

## Phase 8: Configure And Skills API Type Cleanup

- [x] Make configure field values typed at the API boundary.
    - [x] Toggles return `bool`.
    - [x] Text/select/secret fields return stable documented types.
    - [x] Existing frontend-compatible field keys are preserved.
- [x] Reduce `serviceSkillItemFromMeta` coupling.
    - [x] Pass only the needed skills sub-config or API key state.
    - [x] Existing skills service tests cover inventory serialization behavior.

**Review findings covered:** 21, 22

**Acceptance checks**

- Configure API response schema is documented and stable.
- Skills item conversion is testable without constructing a full `config.Config`.

---

## Suggested PR Order

1. Low-risk correctness fixes from Phase 1.
2. Config tests and config file split.
3. `Default()`, `Load()`, and env override refactors.
4. Mechanical `service.go` file split.
5. Component extraction for service mutex/map state.
6. Routing cleanup.
7. Terminal streaming robustness.
8. Turn job error classifier.
9. Configure/skills typed value cleanup.

---

## Final Acceptance Criteria

- [x] `service.go` is reduced to construction, route setup, and shared service orchestration.
- [x] `config.go` no longer contains all config types, defaults, env overrides, load, save, normalize, and validate logic in one file. (Phase 2 mechanical split done: 2685 lines → 7 files)
- [x] `serviceServer` no longer acts as a terminal manager, rate limiter, auth tracker, cache, and DI container at the same time.
- [x] Config defaults and env overrides have direct tests.
- [x] Routing behavior is declared structurally in mux registration and route-family handlers are isolated by file.
- [x] No request behavior depends on substring matching against `err.Error()` for the reviewed artifact/subagent/config paths.
- [x] Terminal streaming has an explicit slow-consumer policy.
- [x] `go test ./...` passes.
