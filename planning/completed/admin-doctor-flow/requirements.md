# Requirements

## Overview

OR3 needs one simple troubleshooting and repair path for local personal use: the user describes the problem in plain language, Doctor/Admin chat investigates using safe diagnostics, health checks, config summaries, skill status, and redacted logs, then proposes and applies approved fixes through OR3’s safety layer. The existing Doctor should become the default safe surface for diagnosing app, service, settings, runner/provider, approval, logs, and installable skill problems; Admin Assistant is the higher-power version of the same chat flow when privileged action is needed.

Scope assumptions:

- This is an additive plan across `or3-intern` and `or3-app`; implementation state and backend contracts should be planned in `or3-intern` first.
- Existing `internal/doctor`, `/internal/v1/configure`, `/internal/v1/actions/restart-service`, secure auth step-up, approvals, settings health, simple settings, service logs, runner-chat/session storage, and skill inventory should be adapted rather than replaced.
- Local personal mode is the first target. Do not build hosted enterprise RBAC, runner marketing, or a second settings product.
- Codex, opencode, Gemini CLI, Claude Code, `or3-intern`, and remote model providers are implementation plumbing. Product copy should expose only whether an `AdminBrainProvider` is available.

## Requirements

### 1. Extend the existing Doctor into the default troubleshooting surface

OR3 should expose Doctor as the normal-person entry point for “something is broken” without asking the user to understand config files, env vars, session keys, runners, skills, service restarts, or tool permissions.

Acceptance criteria:

- The app provides a plain-language Doctor/Admin chat input where users can say things like “my connected app is broken” or “OR3 can’t connect,” and OR3 maps that intent to bounded diagnostics.
- Doctor reuses `internal/doctor.Evaluate` and existing service health/readiness/config/skills/capabilities APIs instead of creating a parallel diagnostics engine.
- Doctor can inspect service health, readiness, pairing, auth/session state, approvals, runner/provider availability, config validity, skill status, tool status, redacted logs, local app connection state, and restart capability.
- Doctor/Admin chat history, diagnostic tool calls, proposed fix cards, accepted actions, and checkpoints are saved so the user can continue the conversation and undo accepted changes when possible.
- Doctor returns display-ready findings with plain-language `what_i_found`, `what_this_means`, `recommended_fix`, `risk_level`, `approval_needed`, `restart_needed`, and optional advanced details.
- Existing CLI `or3-intern doctor` remains available and compatible; app Doctor surfaces may consume richer JSON or a new service endpoint.

### 2. Keep basic Doctor deterministic and usable without AI

Basic Doctor must work when no local runner is installed and no API key/provider is configured.

Acceptance criteria:

- Basic Doctor can run deterministic checks for service reachability, pairing, bootstrap, approvals, config validation, runner availability, provider key presence, restart requirements, skill diagnostics, and known log patterns.
- If the service is down, the app still runs client-side checks: host profile exists, paired token/session token presence, last known bootstrap, service URL reachability, TCP/HTTP failure category, and restart/action availability from cached state.
- If AI-backed mode is unavailable, the UI says “Basic Doctor is available. AI repair is not configured yet…” and offers setup, basic checks, and manual instructions.
- Basic Doctor never depends on runner-chat, external providers, or remote model APIs.
- Deterministic checks have bounded runtime and memory usage and must not run unrestricted shell commands.

### 3. Introduce a generic AdminBrainProvider abstraction

OR3 should distinguish only between AI-backed mode being available or unavailable, not between runner brands or provider personalities.

Acceptance criteria:

- Backend and UI represent admin AI availability as `runner`, `apiKeyProvider`, or `unavailable`.
- The selected runner/provider is treated as plumbing in advanced details only.
- UI copy does not claim that specific runners are best for specific task categories.
- `useChatRunners`, `/internal/v1/chat-runners`, `/internal/v1/agent-runners`, and provider settings can feed this abstraction without duplicating runner discovery.

### 4. Require plan-based settings changes

Doctor and Admin Assistant must not mutate config directly from chat or runner output.

Acceptance criteria:

- Any config-affecting repair is represented as a `SettingsChangePlan` validated by normal Go code before apply.
- Plans include title, summary, creator, risk, restart requirement, approval/step-up requirements, affected areas, changes, validation results, impact, rollback plan, post-apply checks, user-facing explanation, and hidden exact config diff.
- Each plan change records config path, old value, new value, operation, impact, risk reason, and validation status.
- Plan validation uses the same config setters/validation path as `/internal/v1/configure/apply` where practical, not ad hoc mutation.

### 5. Centralize config metadata for UI, Doctor, validation, and docs

Config field descriptions and safety metadata should have one backend-owned source of truth.

Acceptance criteria:

- A machine-readable config metadata registry exists in Go and covers at least the first vertical slice fields.
- Each field defines section, config path, label, plain-English description, default, allowed values, current value, risk level, restart requirement, approval/step-up requirement, dependencies, conflicts, validation rules, rollback behavior, user-intent examples, and docs text/link.
- `/internal/v1/configure/fields` can be extended or accompanied by metadata endpoints so `or3-app` no longer maintains primary risk/descriptions in `app/settings/riskRules.ts` and `app/settings/labels.ts` for covered fields.
- Advanced settings can render exact keys, but simple settings and Doctor cards hide raw keys by default.

### 6. Enforce deterministic risk classification in OR3 code

AI runners can reason and propose, but OR3 itself owns risk and permission decisions.

Acceptance criteria:

- Risk levels are `safe`, `notice`, `warning`, and `danger`.
- Final plan risk is computed by Go policy code from metadata, change content, affected areas, restart need, file scopes, command classes, skill auth changes, and security posture changes.
- AI-provided risk labels are stored only as suggestions/evidence and cannot lower computed risk.
- Risk policy is covered by table-driven tests for representative safe/notice/warning/danger changes.

### 7. Apply risk-based approval behavior

OR3 should ask for approval only when needed, with stronger verification for higher-risk changes.

Acceptance criteria:

- Safe actions apply automatically or show quiet confirmation.
- Notice actions apply automatically when clearly requested and show a changed card with undo when possible.
- Warning actions require explicit consent plus identity verification. If passkey or PIN is configured, the approval card may offer “Yes, and don’t ask again for 5 minutes” for the same action family, actor, risk level, and affected scope.
- Danger actions require admin approval with passkey or PIN. If neither passkey nor PIN is configured, danger changes are blocked and the UI explains that security setup is required before OR3 can apply them.
- Typed confirmation phrases are not part of the default flow for warning/danger changes; use passkey/PIN setup instead for a safer and less confusing UX.
- Existing service auth step-up and approval broker behavior are reused; routes for warning/danger plan apply are sensitive and step-up protected.

### 8. Introduce Admin Assistant as a privileged escalation flow, not a separate product area

Admin Assistant should appear when Doctor finds a fix that needs privileged diagnostics or mutation.

Acceptance criteria:

- Doctor can request Admin Assistant escalation for warning/danger fixes or AI-backed deeper reasoning.
- Admin Assistant tools remain scoped and policy-controlled: plan creation/validation/apply, rollback, restart, scoped file reads, scoped config writes, skill inspection/repair, approved diagnostic commands, and post-fix checks.
- Admin Assistant cannot call unrestricted shell, unrestricted file write, direct secret read, direct config mutation, direct env mutation, or direct restart outside OR3 policy.
- If AdminBrainProvider is unavailable, Admin Assistant UI offers basic Doctor and manual instructions instead of pretending AI repair is available.

### 9. Add safe apply, audit, rollback, restart, and post-check pipeline

Every applied fix should be traceable, reversible when safe, and verified when possible.

Acceptance criteria:

- Apply flow stages changes, validates the staged config, computes final risk, checks approval/step-up, saves atomically where possible, records rollback data, applies live-reloadable fields, requests restart when approved, runs post-apply checks, and reports success/failure.
- Applied fixes write tamper-evident audit events using existing audit infrastructure or a compatible extension.
- Checkpoint/rollback records capture old values, new values, config path, accepted tool card ID, conversation ID, restart needs, rollback safety, and manual rollback instructions when automatic rollback is unsafe.
- Restart-required plans show restart impact before applying and handle temporary app disconnection gracefully.
- Users can undo any visible rollback-capable accepted tool card from the conversation, subject to stale-state validation and current approval requirements.

### 10. Provide strict Doctor and Admin tool boundaries

Doctor should be safe by default; Admin Assistant should be stronger but still policy-gated.

Acceptance criteria:

- Doctor read/safe tools include health, pairing, approvals, runtime profile, config summary, config validation, skill status, tool status, runner status, provider status, redacted logs, safe diagnostics, fix proposal, safe fix application, and admin approval request.
- Doctor does not receive raw dangerous tools: unrestricted shell, unrestricted file write, direct config write, direct restart, direct secret read, or direct env mutation.
- Admin Assistant tools are scoped, audited, and subject to risk policy and route requirements.
- Tool outputs destined for AI providers are redacted and prompt-injection bounded before use.

### 11. Add SkillDiagnosticManifest support

Skills should define deterministic diagnostics and safe repair metadata without embedding unsafe shell scripts.

Acceptance criteria:

- A `SkillDiagnosticManifest` schema can be loaded from skill metadata or adjacent manifest files and exposed through skill inventory/Doctor.
- Manifests can define required binaries, env vars, files, auth checks, safe test commands, expected outputs, known failure patterns, redaction rules, safe/warning/danger fixes, and post-fix checks.
- Commands are declarative and run only through an allowlisted diagnostic runner with timeout/output limits and redaction.
- First implementation slice includes one generic installable skill diagnostic fixture plus placeholders or basic manifests for GitHub, filesystem, terminal, model provider, and runner integration.

### 12. Make installable skill diagnostics the first deep repair slice

The first vertical slice should cover a generic installable skill with real-world external state while exercising config metadata, plans, risk, approval, restart, audit, rollback, logs, and post-checks. The plan must not hard-code product UX around any one skill; individual skills provide manifests and copy through the diagnostic system.

Acceptance criteria:

- Skill diagnostics check install state, required binaries, auth status, credential/config source, required files, file validity, missing scopes/permissions, stale overrides, env-var precedence, redacted logs, and restart need where applicable.
- A skill recommended fix can produce a `SettingsChangePlan` for OR3-managed skill settings and a manual fallback for external state OR3 cannot safely mutate.
- Skill warning-level fixes require approval/step-up and show exact changes only in advanced expansion.
- Post-fix checks run bounded manifest-defined diagnostics and report whether the original symptom is likely resolved.

### 13. Add simple UI cards in `or3-app`

The app should explain findings and fixes through cards that avoid raw config keys by default.

Acceptance criteria:

- Cards exist for diagnostic result, recommended fix, settings change preview, risk warning, approval required, exact diff, restart required, post-fix check, undo available, and manual fallback.
- Cards use fields: what I found, what this means, recommended fix, what will change, risk level, approval needed, restart needed, can undo, and buttons.
- Buttons include apply, apply with passkey, show exact changes, request admin approval, run checks again, undo, and cancel.
- Accepted tool/fix cards stay in the conversation and become “Undo changes” cards when rollback is available.
- Existing pages such as `/settings/health`, `/computer/attention`, simple settings, advanced settings, and service restart composables are reused or refactored rather than duplicating entire flows.

### 14. Keep Simple Settings and Advanced Settings, but route both through the same plan pipeline

Settings UX should stay human-first while power users retain exact controls.

Acceptance criteria:

- Simple Settings remains the normal settings page and uses metadata-backed descriptions and plan preview/apply for covered fields.
- Advanced Settings remains a full config editor/search view generated from metadata.
- Any advanced field can offer “Ask Admin Assistant to change this.”
- Existing `useSimpleSettings`, `useConfigure`, and settings review overlays are adapted to use plan preview/apply instead of app-only risk rules for covered fields.

### 15. Keep local roles simple for the first pass

Local personal mode should not block implementation on enterprise RBAC.

Acceptance criteria:

- Safe Doctor can run from paired devices allowed by existing service auth.
- Warning and danger changes require recent step-up on the main/authenticated device using existing passkey/session flows.
- Admin role can map to existing `approval.RoleAdmin`/secure-session role where available, but explicit enterprise role management is deferred.
- The system remains compatible with future hosted mode by keeping risk/approval checks server-side.

### 16. Protect secrets and prompt boundaries

Diagnostics and AI-backed repair must not leak secrets or treat logs/config comments as trusted instructions.

Acceptance criteria:

- Logs, config summaries, env values, file contents, command output, service account JSON, OAuth secrets, API keys, tokens, and local paths are redacted or summarized before being sent to remote AI providers.
- OR3 has a stronger durable log/event layer for Doctor/Admin: bounded, structured, redaction-aware, queryable by time/source/correlation ID, and stored in SQLite where useful rather than only streamed live.
- Local runners may receive more local context only through scoped OR3 tools; remote providers receive stricter summaries by default.
- Prompt injection from logs/docs/skill output/config comments is mitigated by structured envelopes that label data as untrusted evidence.
- Redaction tests cover representative secrets and installable skill credential data.

### 17. Preserve compatibility and bounded behavior

The feature must not break existing CLI/service/app flows.

Acceptance criteria:

- Existing config file format remains backward-compatible.
- SQLite changes are additive migrations only.
- Existing `/internal/v1/configure`, `/internal/v1/skills`, `/internal/v1/actions/restart-service`, `/internal/v1/approvals`, `/internal/v1/auth/*`, and CLI doctor/configure flows continue to work.
- Diagnostic commands, log reads, and runner turns have bounded input size, output size, timeouts, and no secret leakage.

## Non-functional constraints

- Deterministic policy: risk classification, route sensitivity, validation, redaction, and approval decisions must live in OR3 code, not in AI prompts.
- Low memory: diagnostics should stream or summarize logs and cap results; durable log retention should be bounded by time/count/size; no unbounded log ingestion or whole-repo scans from Doctor.
- SQLite safety: migrations must be additive, compatible with the current single-process model, and use existing busy-timeout/WAL behavior.
- Safe defaults: auto-fixes must never broaden tool, file, shell, network, service, approval, or automation permissions.
- Restart resilience: app UI must expect temporary service loss and recover by polling bootstrap/readiness after restart.
- User trust: normal cards should explain what will happen in plain language, with exact config diffs hidden behind Advanced.
- Testability: first slice must be testable with fixtures and fake command runners; tests must not rely on live broken external accounts or services.