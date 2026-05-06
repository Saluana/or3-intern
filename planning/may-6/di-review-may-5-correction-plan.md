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

- [ ] Capture current package test status with `go test ./...`.
- [ ] Add or identify coverage for:
  - [ ] `internal/config.Default()`
  - [ ] `internal/config.Load()`
  - [ ] `internal/config.ApplyEnvOverrides()`
  - [ ] `cmd/or3-intern` service endpoint routing smoke tests
  - [ ] terminal session streaming behavior
  - [ ] turn job error completion behavior
- [ ] Record any currently failing tests before refactoring so cleanup work is not blamed for pre-existing failures.

**Acceptance checks**

- Current failures, if any, are documented.
- There is enough coverage to detect accidental config default drift and route regressions.

---

## Phase 1: Low-Risk Correctness Fixes

These are small, self-contained defects that should be fixed before larger file movement.

- [ ] Replace `mustJSON` with an error-aware marshal path.
  - [ ] Rename to `marshalJSON` if callers can return errors.
  - [ ] Or make it genuinely panic if the call site is intentionally unrecoverable.
  - [ ] Ensure save/write callers do not silently write empty or nil JSON.
- [ ] Replace `err.Error()` substring routing with typed errors.
  - [ ] Add sentinel errors or custom error types for artifact not found, artifact unavailable, and invalid subagent status filter.
  - [ ] Use `errors.Is()` / `errors.As()` at service boundaries.
- [ ] Replace `fmt.Sprint(... ) == "<nil>"` checks in persisted subagent event parsing.
  - [ ] Type-assert known JSON shapes.
  - [ ] Treat missing, null, and non-string values explicitly.
- [ ] Remove nil receiver tolerance from `serviceServer` methods where construction guarantees a non-nil server.
  - [ ] Audit each `if s == nil` case.
  - [ ] Move validity checks to service construction or dependency initialization.
- [ ] Normalize duplicate `firstNonEmpty` helpers.
  - [ ] Keep package-local copies if that is simplest.
  - [ ] If shared, place generic string helpers in an internal utility package without leaking them through `config`.

**Review findings covered:** 7, 8, 9, 13, 15

**Acceptance checks**

- New tests prove config marshaling failures cannot silently produce empty config output.
- Error classification no longer depends on English error text.
- No behavior regression in artifact, subagent, and config save flows.

---

## Phase 2: Config Module Split

Break `internal/config/config.go` into cohesive files before deeper rewrites.

- [ ] Split config files by responsibility:
  - [ ] `types.go` for config structs and public types.
  - [ ] `defaults.go` for `Default()` and default sub-builders.
  - [ ] `env.go` for env override application.
  - [ ] `load.go` for file read, parse, load orchestration.
  - [ ] `normalize.go` for normalization helpers.
  - [ ] `validate.go` for validation helpers.
  - [ ] `save.go` for save and marshal helpers.
- [ ] Refactor `Default()` into composable default builders:
  - [ ] `defaultPaths()`
  - [ ] `defaultProvider()`
  - [ ] `defaultModelRouting()`
  - [ ] `defaultChannels()`
  - [ ] `defaultContext()`
  - [ ] `defaultAuth()`
  - [ ] `defaultService()`
  - [ ] Any other existing sub-config with meaningful boundaries.
- [ ] Extract reusable default constants used by normalization.
  - [ ] Stop calling `Default()` inside `normalizeProviderRouting`.
  - [ ] Use named constants or package variables for default model names.
- [ ] Refactor `Load()` into a clear pipeline:
  - [ ] `readConfigFile(path)`
  - [ ] `parseConfig(data)`
  - [ ] `applyDefaults(cfg)`
  - [ ] `normalizeConfig(cfg)`
  - [ ] `validateConfig(cfg)`
- [ ] Refactor `ApplyEnvOverrides()` to reduce copy-paste.
  - [ ] Add helper functions for string, int, bool, duration, and list env vars.
  - [ ] Use table-driven overrides where it stays readable.
  - [ ] Keep special-case routing side effects explicit and tested.
- [ ] Start reducing `Config` god-struct coupling at call sites.
  - [ ] Prefer passing sub-configs where functions only need one concern.
  - [ ] Do not redesign the entire config schema in this phase.

**Review findings covered:** 3, 4, 5, 16, 18, 20

**Acceptance checks**

- `internal/config` tests pass.
- Golden/default tests show no accidental JSON/default changes unless intentionally documented.
- `Load()` reads as orchestration, not validation logic.
- Env override tests cover representative string, int, bool, and routing side-effect values.

---

## Phase 3: Service File Split

Mechanically split `cmd/or3-intern/service.go` after correctness and config cleanup are stable.

- [ ] Move terminal session and WebSocket code to `service_terminal.go`.
- [ ] Move file/artifact operations to `service_files.go`.
- [ ] Move approval handlers and helpers to `service_approvals.go`.
- [ ] Move configure UI API to `service_configure.go`.
- [ ] Move skills inventory API to `service_skills.go`.
- [ ] Move cron/schedule API to `service_cron.go`.
- [ ] Move subagent and agent runner API to `service_agents.go`.
- [ ] Move model catalog code to `service_models.go`.
- [ ] Move middleware, rate limiting, and auth failure tracking to `service_middleware.go` or narrower files.
- [ ] Keep server construction and route registration in `service.go`.

**Review finding covered:** 1

**Acceptance checks**

- No public API behavior changes.
- File movement produces small, logical files.
- `go test ./cmd/or3-intern ./internal/...` passes.

---

## Phase 4: Extract Service Components

After the file split, turn repeated mutex/map state into owned components with clear APIs.

- [ ] Extract `terminalManager`.
  - [ ] Own terminal sessions and session cleanup.
  - [ ] Hide terminal session maps and locks from `serviceServer`.
- [ ] Extract `terminalWebSocketTicketStore`.
  - [ ] Own ticket creation, lookup, expiration, and deletion.
- [ ] Extract `rateLimiter`.
  - [ ] Encapsulate actor key calculation and request counting.
  - [ ] Add tests for per-actor limits and window rollover.
- [ ] Extract `authFailureTracker`.
  - [ ] Encapsulate failure count, reset, and lockout behavior.
- [ ] Extract `modelCatalogCache`.
  - [ ] Own cache key construction, TTL, refresh, and concurrency behavior.
- [ ] Shrink `serviceServer` to dependencies and component pointers.

**Review findings covered:** 2, 10, 14

**Acceptance checks**

- `serviceServer` no longer directly owns unrelated mutex/map pairs.
- Each extracted component has focused unit tests.
- Handler tests can construct only the dependencies they need.

---

## Phase 5: Routing Cleanup

Replace manual path parsing and duplicate route registration with structured routing.

- [ ] Choose router approach.
  - [ ] Prefer Go 1.22+ `http.ServeMux` method/path patterns if the project Go version supports it.
  - [ ] Otherwise use a small router dependency only if it is already acceptable for the project.
- [ ] Replace root-plus-slash duplicate registrations in `newServiceMux`.
- [ ] Split `handleAuth()` dynamic route parsing into explicit route handlers.
- [ ] Gradually convert other manually parsed route families:
  - [ ] subagents
  - [ ] pairing/devices
  - [ ] files/artifacts
  - [ ] terminal
  - [ ] configure/settings
- [ ] Add route tests for trailing slash behavior and path parameters.

**Review findings covered:** 6, 19

**Acceptance checks**

- Route declarations show method and path shape in one place.
- No handler needs to strip its own route prefix for common path params.
- Existing app/API clients keep working or documented redirects/compatibility handlers are retained.

---

## Phase 6: Terminal Streaming Robustness

Improve terminal event delivery after the terminal code is isolated.

- [ ] Replace silent subscriber drops with an explicit policy.
  - [ ] Option A: per-subscriber ring buffer.
  - [ ] Option B: close lagging subscribers and require reconnect/replay.
  - [ ] Option C: reuse the existing job SSE streaming pattern if it fits terminal output.
- [ ] Make dropped/disconnected subscriber behavior observable in logs or metrics.
- [ ] Replace WebSocket helper closures with a small writer type or direct helper functions if profiling shows useful reduction.
- [ ] Add tests for slow subscribers, reconnect history, close behavior, and event ordering.

**Review findings covered:** 11, 12

**Acceptance checks**

- Slow clients do not silently miss terminal output without a visible recovery path.
- WebSocket connection lifecycle remains clean under rapid connect/disconnect tests.

---

## Phase 7: Turn Job Error Handling Consolidation

- [ ] Extract shared turn job completion/error classification.
  - [ ] Cover context cancellation.
  - [ ] Cover approval-required errors.
  - [ ] Cover fallback text behavior.
  - [ ] Cover public job error mapping.
- [ ] Use the helper from both `runTurnJob` and `runApprovedResumeJob`.
- [ ] Add regression tests proving both paths classify errors consistently.

**Review finding covered:** 17

**Acceptance checks**

- Adding a new turn error type only requires touching one classifier.
- Regular turns and approval-resumed turns produce matching public behavior.

---

## Phase 8: Configure And Skills API Type Cleanup

- [ ] Make configure field values typed at the API boundary.
  - [ ] Toggles return `bool`.
  - [ ] Text/select/secret fields return stable documented types.
  - [ ] Update frontend consumers if they currently expect `"on"` / `"off"` strings.
- [ ] Reduce `serviceSkillItemFromMeta` coupling.
  - [ ] Pass only the needed skills sub-config or API key state.
  - [ ] Add focused tests for permission state and API key configured flags.

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

- `service.go` is reduced to construction, route setup, and shared service orchestration.
- `config.go` no longer contains all config types, defaults, env overrides, load, save, normalize, and validate logic in one file.
- `serviceServer` no longer acts as a terminal manager, rate limiter, auth tracker, cache, and DI container at the same time.
- Config defaults and env overrides have direct tests.
- Routing behavior is declared structurally instead of being hidden in string parsing inside handlers.
- No request behavior depends on substring matching against `err.Error()`.
- Terminal streaming has an explicit slow-consumer policy.
- `go test ./...` passes, aside from any documented pre-existing failures.
