# Tasks

## 1. Backend metadata and policy foundation

- [x] (Req 4, 5, 6) Create `internal/configmeta` with `RiskLevel`, `ConfigFieldMetadata`, relation/validation/rollback types, and a registry interface.
- [x] (Req 5, 14) Build a typed Go metadata registry as the authoritative source for first-slice fields; use current `configureField` definitions only to reuse labels/descriptions/choices where they already exist.
- [x] (Req 5, 12) Add first-slice metadata for generic installable skill env/config fields, provider keys, runner/admin-brain availability, tool exec policy, allowed programs, service restart, and credential/config paths.
- [x] (Req 6, 7) Create `internal/adminflow` with `SettingsChangePlan`, `SettingsPlanChange`, `RiskDecision`, `ApprovalContext`, `ApplyResult`, and `RollbackResult` types.
- [x] (Req 6, 7, 10) Implement Go-only risk classification for safe/notice/warning/danger, including escalation rules for restart, skill auth, tool permissions, file scope, shell/network/service exposure, approval posture, automation changes, and remembered warning approvals.
- [x] (Req 16) Implement redacted value helpers for config/env/log fields, with secret presence indicators instead of raw secret values.
- [x] (Req 6, 16) Add table-driven tests for risk classification and redaction in `internal/adminflow` and `internal/configmeta`.

## 2. Plan validation and apply pipeline

- [x] (Req 4, 9) Implement `PlanValidator` that stages plan changes against an in-memory `config.Config` copy.
- [x] (Req 4, 9, 17) Reuse existing configure field setters from `cmd/or3-intern/service_configure.go` / `configure_tui.go` through an adapter or shared helper; avoid a second config mutation implementation.
- [x] (Req 4, 9) Run config validation and a Doctor post-save mode against staged config before apply.
- [x] (Req 6, 9) Recompute final risk after staging and reject any plan whose computed risk exceeds the caller’s approved authority.
- [x] (Req 9) Implement stale-plan detection by comparing expected old values with current config values before write.
- [x] (Req 9, 17) Save config through existing `config.Save` path and call existing live reload behavior for model-routing-compatible fields.
- [x] (Req 9) Add post-apply check orchestration with bounded timeouts and per-check status.
- [x] (Req 9) Persist post-check-pending state so checks can resume after app refresh or service restart.
- [x] (Req 4, 9) Add tests for validation failures, stale plans, partial apply prevention, live-reload-only changes, restart-required changes, and post-check failure reporting.

## 3. Audit and rollback persistence

- [x] (Req 9, 11, 17) Add additive SQLite migration(s) for `settings_change_plans`, `doctor_checkpoints`, `settings_change_rollbacks`, and bounded `diagnostic_log_events`.
- [x] (Req 9, 11) Add DB methods for creating, reading, marking applied, and listing recent rollback records.
- [x] (Req 9, 11) Reuse `DB.AppendAuditEvent` for `doctor.plan.*`, `doctor.checkpoint.*`, `doctor.log.*`, and `doctor.post_check.*` event types.
- [x] (Req 9, 11, 16) Ensure audit payloads include redacted old/new values, requester, approver, auth method, risk, restart status, post-check result, rollback availability, and timestamps.
- [x] (Req 9, 11) Implement rollback apply for rollback-safe config changes using the same validation/risk/approval path as forward apply, and render accepted conversation cards as undo cards when rollback is available.
- [x] (Req 9, 11) Add DB migration and audit-chain regression tests in `internal/db` and service-level tests.

## 4. Doctor service API

- [x] (Req 1, 2, 10) Add `cmd/or3-intern/service_doctor.go` with handlers for Doctor status/run, Doctor/Admin chat sessions/messages/events, Admin Brain availability, metadata, plan create/read/validate/apply/rollback/post-check, logs, and skill diagnostics.
- [x] (Req 1, 17) Register `/internal/v1/doctor` routes in `service_routes.go`.
- [x] (Req 7, 10, 15) Update `serviceRouteRequirementForRequest` and `service_auth_rollout_test.go` so read-only Doctor routes are low-risk and apply/rollback/approved diagnostics are sensitive session+step-up routes.
- [x] (Req 1, 2) Build Doctor aggregation over existing health, readiness, app bootstrap, approvals, config validation, runner/provider status, skills, redacted logs, and client-side service-down findings supplied by the app.
- [x] (Req 1, 13) Add display-ready finding/card response types while preserving current `internal/doctor.Report` JSON compatibility for CLI use.
- [x] (Req 4, 9) Implement plan creation from deterministic Doctor findings and settings UI changes.
- [x] (Req 7, 9) Wire plan apply to existing passkey/session auth context and approval broker behavior.
- [x] (Req 1, 17) Add service contract tests for route methods, auth behavior, response shapes, validation errors, and mutation/audit results.

## 5. AdminBrainProvider integration

- [x] (Req 3, 8) Implement `AdminBrainProvider` detection from existing `/internal/v1/chat-runners`, `/internal/v1/agent-runners`, and provider key/config status.
- [x] (Req 3) Normalize provider states to `runner`, `apiKeyProvider`, or `unavailable` with generic user-facing copy and advanced-only runner/provider IDs.
- [x] (Req 8, 10, 16) Reuse existing runner-chat/session infrastructure for multi-turn Doctor/Admin conversations with a debugging/fixing system prompt and restricted Doctor/Admin tools.
- [x] (Req 8, 10, 16) Define the allowed Admin Brain prompt/evidence envelope and ensure logs/config/skill output are redacted and marked untrusted.
- [x] (Req 8, 10) Ensure Admin Brain can only propose `SettingsChangePlan` data, create safe diagnostic tool calls, or request approved tool cards; it must not receive direct write/restart/shell tools.
- [x] (Req 2, 3) Add tests for no-runner/no-provider fallback, runner installed but auth broken, API key configured, and generic copy with no runner positioning.

## 6. SkillDiagnosticManifest and installable skill diagnostics

- [x] (Req 11) Create `internal/skilldiag` with manifest schema, parser/validator, safe command runner interface, redaction, known failure matcher, and result types.
- [x] (Req 11, 17) Integrate diagnostic manifest discovery with `internal/skills` inventory without breaking existing `SKILL.md` parsing.
- [x] (Req 11) Extend `cmd/or3-intern/service_skills.go` to include diagnostic availability/status in skill responses.
- [x] (Req 11, 12) Add a first generic installable skill diagnostic fixture/implementation for binary presence, auth source, credential/config path, identity/account field, JSON/config validity, permission hints, split capability checks, env override source, stale external references, and restart need.
- [x] (Req 11, 12, 16) Add generic installable skill redaction rules for credential JSON, OAuth/client secrets, access/refresh tokens, API keys, email/account identifiers where appropriate, and local paths when sent to remote AI.
- [x] (Req 12) Implement deterministic installable-skill known-pattern fixes that create warning-level `SettingsChangePlan` proposals for OR3-managed credential/config source correction, identity preservation, stale managed-reference clearing, and restart/post-check.
- [x] (Req 11, 12) Add placeholder/basic manifests or metadata for GitHub, filesystem, terminal, model provider, and runner integration without overbuilding their repairs.
- [x] (Req 11, 12, 16) Add fixture-based tests for first-pass installable skill failure modes and redaction behavior.

## 7. Diagnostic logs and service-down checks

- [ ] (Req 1, 2, 16) Create `internal/diagnosticlog` for structured, bounded, redaction-aware Doctor/Admin log events with correlation IDs.
- [ ] (Req 16, 17) Add SQLite-backed `diagnostic_log_events` retention with age/count/size pruning and query bounds.
- [ ] (Req 1, 16) Extend `service_logs.go` or adjacent helpers so Doctor/Admin can query redacted logs by source, level, time range, correlation ID, and known failure pattern.
- [ ] (Req 2, 13) Add app-side client diagnostics for service unreachable states: host profile, pairing/session state, base URL, bootstrap reachability, timeout/refused/auth error category, and cached restart guidance.
- [ ] (Req 2, 13) Merge client-side service-down findings with service-side Doctor findings when the service returns.
- [ ] (Req 16) Add tests for log redaction, prompt-injection log lines, retention pruning, query bounds, and service-down classification.

## 8. Restart and recovery integration

- [ ] (Req 7, 9) Reuse `/internal/v1/actions/restart-service` for restart-required plans rather than adding a second restart path.
- [ ] (Req 9, 13) Include restart preview, approval state, operation ID, and log path in plan apply responses.
- [ ] (Req 9) Add backend post-restart readiness polling hooks or app instructions so post-checks can resume after reconnect.
- [ ] (Req 9) Reload persisted Doctor/Admin session, pending plan, accepted checkpoint, and rollback IDs after service restart.
- [ ] (Req 9) Handle restart-start failure, restart timeout, app disconnect, service returning with failed readiness, and manual recovery path.
- [ ] (Req 9, 17) Add tests around restart-required plans and existing restart action availability/approval behavior.

## 9. App Doctor/Admin chat and fix cards

- [ ] (Req 1, 2, 13) Update `or3-app` `useSettingsHealth.ts` to call backend Doctor status/run and fall back to current client-side checks when the endpoint is unavailable.
- [ ] (Req 1, 3, 8, 13) Add `useDoctorAdminChat.ts` or equivalent composable for problem-description input, multi-turn Doctor/Admin messages, Admin Brain status, streamed tool cards, plan lifecycle, apply, checkpoints, rollback, and post-check state.
- [ ] (Req 13) Add card components for diagnostic result, recommended fix, settings change preview, risk warning, approval required, exact diff, restart required, post-fix check, undo, and manual fallback.
- [ ] (Req 13) Integrate cards into `/settings/health` first; link `/computer/attention` to the same Doctor findings instead of maintaining separate guidance logic long-term.
- [ ] (Req 7, 13, 15) Reuse `useAuthSession.retryWithAuth` so warning/danger applies trigger passkey/PIN verification as supported by backend policy.
- [ ] (Req 7, 13, 15) Remove typed-confirmation fallback from the default warning/danger flow; show “set up passkey or PIN” when neither is available.
- [ ] (Req 7, 13, 15) Add warning approval option “Yes, and don’t ask again for 5 minutes” scoped to actor/device/action family/risk/scope.
- [ ] (Req 9, 13) Reuse `useServiceRestart` reconnect behavior and network-error suppression for restart-required fix cards.
- [ ] (Req 2, 3, 13) Add Basic Doctor unavailable-AI backend copy and actions.
- [ ] (Req 13) Add Vitest coverage for card rendering, buttons, risk states, exact diff expansion, approval rejected, restart reconnect, failed fix, and undo available.

## 10. Settings UI integration

- [ ] (Req 5, 14) Extend `useConfigure.ts` with metadata load, plan preview, plan apply, and rollback calls while preserving `applyChanges`.
- [ ] (Req 5, 14) Update `useSimpleSettings.ts` to consume metadata for covered labels/descriptions/risk/restart status and retain current mappings as fallback.
- [ ] (Req 5, 6, 14) Replace covered `app/settings/riskRules.ts` cases with backend plan/risk decisions; keep uncovered local warnings temporarily.
- [ ] (Req 13, 14) Refactor `SettingSaveReview.vue` into or alongside a `SettingsChangePreviewCard` that renders backend plan responses.
- [ ] (Req 14) Add “Ask Admin Assistant to change this” affordance in advanced settings sections where metadata supports intent mapping.
- [ ] (Req 14) Ensure Advanced Settings can show exact config keys/diffs only after user expands advanced details.
- [ ] (Req 14) Add focused app tests for simple setting apply through plan preview and advanced metadata display.

## 11. CLI compatibility and docs

- [ ] (Req 1, 17) Keep `or3-intern doctor` CLI behavior compatible; optionally add `--app-json` or richer JSON only if needed by the service/app.
- [ ] (Req 1, 9) Consider adding a CLI command for plan preview/apply later, but do not block the app vertical slice on it.
- [ ] (Req 5, 17) Update generated or handwritten config docs from metadata after first-slice fields are covered.
- [ ] (Req 1, 13, 17) Update `docs/getting-started.md`, `docs/configuration-reference.md`, `docs/security-and-hardening.md`, and app integration docs with Doctor/Admin repair flow once implemented.
- [ ] (Req 17) Document manual fallback instructions for no-AI, failed restart, failed rollback, and blocked danger changes.

## 12. First vertical slice acceptance test

- [ ] (Req 1, 2) With no AI backend configured, `/settings/health` shows Basic Doctor availability and runs deterministic checks.
- [ ] (Req 1, 2) User can describe a problem in Doctor/Admin chat, receive diagnostic tool cards, follow up, and reload the conversation.
- [ ] (Req 3) With a working runner or configured provider, Doctor shows generic “Admin Brain available” without runner comparison copy.
- [ ] (Req 12) A generic installable skill stale credential/config fixture produces a warning-level recommended fix plan.
- [ ] (Req 4, 6, 7) The skill fix plan cannot be applied without required explicit consent and passkey/PIN verification.
- [ ] (Req 9, 11) Applying the approved plan writes audit and rollback records.
- [ ] (Req 9) Restart-required plan shows restart preview, calls existing restart action, handles reconnect, and runs post-checks.
- [ ] (Req 11, 12, 16) Skill diagnostics and logs redact secrets and known prompt-injection log text before Admin Brain use.
- [ ] (Req 13) The UI shows success/failure and turns accepted rollback-capable cards into undo buttons.
- [ ] (Req 2, 16) If the service is down, the app still shows client-side Doctor findings and recovery actions.

## 13. Out of scope for the first implementation pass

- [ ] Do not build separate UX positioning for Codex, opencode, Gemini CLI, Claude Code, or any other runner.
- [ ] Do not build enterprise RBAC, team policies, hosted admin consoles, or multi-tenant admin approval workflows.
- [ ] Do not migrate every config field to metadata before shipping the first generic installable skill repair slice.
- [ ] Do not give Doctor unrestricted shell, unrestricted file write, raw secret read, direct env mutation, or direct restart tools.
- [ ] Do not let Admin Brain patch arbitrary local code/config outside scoped OR3 plan/apply tools.
- [ ] Do not auto-grant shell, broad filesystem, public service/network exposure, disabled approvals, or unattended powerful automation.
- [ ] Do not rely on live broken external accounts or services for tests; use fixtures and fake diagnostic runners.
