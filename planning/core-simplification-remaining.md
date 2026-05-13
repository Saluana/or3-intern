# OR3 Intern Core Simplification Remaining Work

Status key:
- `[ ]` not started
- `[~]` in progress
- `[x]` done
- `[!]` blocked or needs decision

Compatibility rules for every task:
- Keep `/internal/v1` snake_case request/response fields stable.
- Do not remove camelCase aliases in this cleanup pass.
- Do not rename app-facing response keys such as `session_key`, `job_id`, `approval_id`, `timeout_seconds`, or `tool_policy`.
- Run `go test ./...` in `or3-intern` and `bun run typecheck && bun test --runInBand` in `or3-app` after contract-touching changes.
- Update `or3-app` only additively unless a task explicitly creates `/internal/v2`.

## Phase 1: Route And Error Contract Hardening

- [x] Add route contract fixtures from current `or3-app` usage.
  - Capture happy-path and common error payload shapes for: turns, subagents, agent runs, runner chat, jobs stream, cron, approvals, memory/scope, files, terminal, configure, bootstrap, health/readiness.
  - Store fixtures under `cmd/or3-intern/testdata/service_contract/`.
  - Tests should assert required keys and tolerated optional keys, not byte-for-byte JSON.
  - Added `app-usage-routes.json` plus registry coverage to keep representative current `or3-app` route paths registered; existing frozen fixtures continue to cover turn/subagent/job/health/embedding/audit response shapes.

- [x] Replace manual service route dispatch with a route table.
  - Add a small internal router type in `cmd/or3-intern/service_routes.go`.
  - Keep the existing exact paths and subtree behavior from `newServiceMux`.
  - Map method/path to handler functions, then leave each handler body unchanged first.
  - Only after tests pass, move path parsing helpers out of `handleConfigure`, `handleAuth`, and `handleApprovals`.
  - Expected tests: existing `service_*` tests plus new unknown-route tests that return `{error, code, request_id}`.
  - Added `service_routes.go`; `newServiceMux` now registers routes from the table and `/internal/v1/*` misses return structured JSON while preserving handler internals.

- [x] Finish universal service error contract.
  - Prefer `writeServiceError(w, r, ...)` or a new `writeServiceJSONError(w, r, ...)` for new code.
  - Keep `writeServiceJSON` compatibility normalization for old call sites.
  - Add a table test that hits representative endpoints and verifies every non-2xx JSON response has `error`, `code`, and `request_id`.
  - Do not change SSE error event formats until the app stream code has matching fixture coverage.
  - Added cross-route middleware coverage in `cmd/or3-intern/service_contract_test.go` for turns, subagents, jobs, cron, approvals, configure, files, terminal, and bootstrap error responses, with fixed `X-Request-Id` propagation assertions.

- [x] Add structured service error enum documentation.
  - Document lowercase app-facing codes in `docs/api-reference.md`.
  - Keep existing uppercase auth challenge codes as challenge codes: `SESSION_REQUIRED`, `PASSKEY_REQUIRED`, `STEP_UP_REQUIRED`, etc.
  - Documented generic lowercase service error codes and uppercase auth challenge compatibility in `docs/api-reference.md`.

## Phase 2: Auth Middleware Decomposition

- [x] Split auth middleware into composable steps without changing behavior.
  - Suggested files:
    - `service_auth_routes.go`: route sensitivity and bypass classification.
    - `service_auth_rate_limit.go`: failure tracking and retry-after logic.
    - `service_auth_credentials.go`: shared-secret, paired-device, auth-session validation.
    - `service_auth_policy.go`: enforcement mode and step-up challenge decisions.
    - `service_auth_context.go`: context/audit identity injection.
  - Keep `serviceAuthMiddlewareWithBrokerAndLimiter` as the public composition point until tests are migrated.
  - Added composable helper files for route classification, rate-limit rejection, credential authentication, policy challenges, and auth context injection while keeping `serviceAuthMiddlewareWithBrokerAndLimiter` as the public composition point.

- [x] Add auth method selection tests.
  - Cover default order: shared-secret, paired-device, auth-session.
  - Cover `X-Or3-Auth-Method: shared-secret|paired-device|session`.
  - Cover unsupported method returning a structured 401/400-style error without leaking token details.
  - Cover a token valid for paired-device but invalid for shared-secret when the explicit paired-device header is present.
  - Added middleware tests for default/explicit shared-secret, paired-device, auth-session selection, unsupported method handling, and paired-token rejection when `shared-secret` is explicitly requested.

- [x] Make auth failure codes more specific.
  - Use stable codes such as `missing_token`, `invalid_token`, `token_replay`, `auth_rate_limited`, `session_required`, `step_up_required`.
  - Preserve existing uppercase auth challenge codes for app challenge handling until `or3-app` explicitly supports new lowercase challenge codes.
  - Added `missing_token`, `invalid_token`, `token_replay`, and `auth_rate_limited` service codes; kept uppercase challenge codes and added additive app passthrough support for the lowercase auth codes.

## Phase 3: `main()` Wiring Extraction

- [x] Extract config and readiness startup builder.
  - New file: `cmd/or3-intern/runtime_build_config.go`.
  - Inputs: CLI flags, command name, unsafe-dev state.
  - Output: loaded config, validation/readiness metadata, warnings.
  - Tests: env override precedence, `.env` precedence, unsafe-dev bypass behavior.

- [x] Extract storage builder.
  - New file: `cmd/or3-intern/runtime_build_storage.go`.
  - Owns DB open, migrations, artifact paths, cron store path preparation.
  - Tests: temp DB open, missing parent directory repair, failure surfaces sanitized user copy.

- [x] Extract security builder.
  - New file: `cmd/or3-intern/runtime_build_security.go`.
  - Owns approval broker, auth service, audit service, secret store, access profiles.
  - Tests: disabled approvals, enabled approvals with keys, auth enabled/disabled matrix.

- [x] Extract integration builder.
  - New file: `cmd/or3-intern/runtime_build_integrations.go`.
  - Owns MCP manager, skills, tools registry, channel constructors.
  - Tests: quarantined integrations are reported, disabled integrations do not start workers.

- [x] Extract runtime and scheduler builder.
  - New file: `cmd/or3-intern/runtime_build_runtime.go`.
  - Owns agent runtime, memory, consolidation, heartbeat, cron, subagents, agent CLI.
  - Tests: minimal runtime builds with nil optional services; service runtime does not require channel startup.

- [x] Replace the large `main()` body with high-level orchestration.
  - Keep command behavior unchanged.
  - Add one integration-style test per top-level command path where practical: `chat`, `service`, `serve`, `doctor`, `settings`.

## Phase 4: Configure TUI Split

- [x] Create a shared screen interface.
  - Suggested interface:
    - `Init(model configureModel) tea.Cmd`
    - `Update(msg tea.Msg, model *configureModel) (handled bool, cmd tea.Cmd)`
    - `View(model configureModel) string`
    - `Save(model *configureModel, cfg *config.Config) error`
  - Keep existing `configureModel` as the state owner for the first split.

- [x] Split screens one at a time.
  - Suggested files:
    - `configure_tui_provider.go`
    - `configure_tui_workspace.go`
    - `configure_tui_channels.go`
    - `configure_tui_mcp.go`
    - `configure_tui_context.go`
    - `configure_tui_safety.go`
    - `configure_tui_service.go`
    - `configure_tui_docindex.go`
    - `configure_tui_review.go`
    - `configure_tui_success.go`
  - First move view/update code mechanically, then refactor shared helpers.
  - Do not change field keys, save semantics, or keyboard behavior during the split.

- [x] Add smoke tests for each configure section.
  - Use existing configure tests as the pattern.
  - Assert section routes render without panic and save back to config correctly.

## Phase 5: CLI Command Consolidation

- [x] Make `settings` the canonical config entrypoint in help text.
  - Keep `setup`, `configure`, `init`, and `doctor --fix` as aliases or workflow wrappers.
  - Update command help to explain when each alias exists.
  - Tests: help snapshots or substring checks for canonical guidance.

- [x] Consolidate device command language around pairing.
  - Keep `devices`, `connect-device`, and `pairing` available.
  - `devices` should become list/manage paired devices.
  - `connect-device` should call the pairing flow.
  - Help should not tell users to bounce between commands.

- [x] Add shared CLI/TUI/non-TTY formatters.
  - New package candidate: `internal/uxformat`.
  - Include error title/body/details rendering, loading state abstraction, and color policy.
  - Migrate one CLI command first, then TUI/plaintext chat.

## Phase 6: Request Parsing Cleanup

- [x] Introduce endpoint-specific request structs.
  - Keep canonical snake_case field names.
  - Put alias handling in shared helpers such as `compat.FirstString(canonical, aliases...)`.
  - Start with high-traffic endpoints: turns, subagents, agent CLI runs, runner chat.

- [x] Add duplicate-field conflict warnings.
  - Non-breaking first step: canonical snake_case wins.
  - Add an internal warning/header only when both canonical and alias are supplied with different values.
  - Do not reject conflicts until a future `/internal/v2`.

- [x] Document request body conventions.
  - `/internal/v1`: snake_case canonical, camelCase accepted as compatibility.
  - `/internal/v2` candidate: strict single convention, conflict rejection.

## Phase 7: Channel Helper Migration

- [x] Add shared channel access helper package.
  - Candidate: `internal/channels/shared`.
  - Include allowlist matching, pairing/open-access decision, inbound policy normalization, dedupe key construction, and common HTTP client creation.

- [x] Migrate channels incrementally.
  - Order: Telegram, Slack, Discord, WhatsApp, Email.
  - For each channel:
    - Add behavior-preserving tests before migration.
    - Replace local duplicated access checks with shared helpers.
    - Keep channel-specific message parsing and send behavior local.

- [x] Add shared channel error formatting.
  - Normalize rate-limit, permission, and config-disabled messages.
  - Surface quarantined/disabled state in status/bootstrap consistently.

## Phase 8: or3-app Compatibility Work

- [x] Keep app API calls snake_case.
  - Audit `app/composables`, `app/utils/or3`, and scheduled/agents/terminal flows.
  - Add mocked API tests for payloads that must remain snake_case.

- [x] Prefer structured service codes everywhere.
  - `useOr3Api` should pass known service codes through.
  - Feature-specific composables should map generic service codes to local UX codes only when needed.

- [x] Add bootstrap warning UI coverage.
  - Test `integration_quarantined`, `legacy_context_mode`, and `embedding_fingerprint_mismatch`.
  - Warnings should be visible but not block normal app load unless severity is `error`.

## Phase 9: Documentation And Release Notes

- [x] Update `docs/api-reference.md` after route/error tests land.
- [x] Update `docs/configuration-reference.md` after request parsing cleanup lands.
- [x] Add a migration note for `.env`, compose, quarantined integrations, and legacy context mode.
- [x] Add a release checklist:
  - `go test ./...`
  - `bun run typecheck`
  - `bun run test`
  - `bun run build`
  - manual app smoke against local `or3-intern service`

