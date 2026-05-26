# Approval Moderator Tasks

## 1. Config and Defaults

- [ ] 1.1 Add moderator config types in `or3-intern/internal/config/types.go`.
  - Add `ApprovalModeratorConfig`, risk/action enums, action map, and preset type.
  - Requirements: 1, 5, 9

- [ ] 1.2 Add default moderator config in `or3-intern/internal/config/defaults.go`.
  - Default to `balanced` actions: low/medium approve, high escalate, extreme deny.
  - Keep behavior inert when approvals are disabled.
  - Requirements: 1, 3, 9

- [ ] 1.3 Normalize and validate moderator fields in `load.go` and `validate.go`.
  - Validate provider/model strings, timeout bounds, max prompt bounds, action map, preset, and failure action.
  - Requirements: 1, 5

- [ ] 1.4 Add config tests in `or3-intern/internal/config/config_test.go`.
  - Cover missing fields, unsupported action/risk/preset, default migration, and invalid failure action.
  - Requirements: 1, 9

## 2. Persistence

- [ ] 2.1 Add an additive SQLite migration for moderator metadata.
  - Add nullable/default columns on `approval_requests`.
  - Requirements: 6, 7

- [ ] 2.2 Extend `db.ApprovalRequestRecord` in `or3-intern/internal/db/approval_store.go`.
  - Include moderator status, risk, action, reason, model, policy hash, reviewed timestamp, and latency.
  - Requirements: 6, 7

- [ ] 2.3 Update approval request scans, inserts, list/show queries, and tests.
  - Preserve compatibility for rows without moderator fields.
  - Requirements: 7, 8

## 3. Moderator Core

- [ ] 3.1 Add moderator risk/action/result types in `or3-intern/internal/approval/types.go`.
  - Include an interface suitable for fake tests and provider-backed implementation.
  - Requirements: 2, 3

- [ ] 3.2 Extract approval request creation from `requireApproval`.
  - Add a helper that creates/reuses a request without forcing pending behavior.
  - Requirements: 3, 8

- [ ] 3.3 Integrate moderator evaluation in `or3-intern/internal/approval/evaluate.go`.
  - Run after token/policy/allowlist checks and before normal pending request creation.
  - Requirements: 2, 3, 8

- [ ] 3.4 Implement action-map enforcement and hard-deny overrides.
  - Prevent user policy or model output from approving built-in extreme classes.
  - Requirements: 2, 3, 4

- [ ] 3.5 Add audit events for moderator review lifecycle.
  - Record requested, approved, escalated, denied, timeout, parse failure, and hard override events.
  - Requirements: 6

## 4. Prompting and Provider Review

- [ ] 4.1 Build redacted subject facts in `or3-intern/internal/approval/preview.go` or a new moderator file.
  - Support exec, skill execution, runner permission, secret access, message send, and tool quota subjects.
  - Requirements: 2, 4, 10

- [ ] 4.2 Add built-in moderator policy text.
  - Include hard-deny, escalate, usually-approve classes, output contract, user-policy handling, and prompt-injection warning.
  - Requirements: 2, 4, 9

- [ ] 4.3 Implement provider-backed moderator.
  - Use `providers.Client.Chat` with no tools, short timeout, bounded prompt, strict JSON parsing, and configurable provider/model.
  - Requirements: 4, 5

- [ ] 4.4 Add moderator response parser tests.
  - Cover malformed JSON, unknown enum values, missing fields, oversized reason, and unsafe approve attempts.
  - Requirements: 3, 4

- [ ] 4.5 Add redaction tests.
  - Cover API-key shaped strings, approval tokens, env-like secrets, and long argument truncation.
  - Requirements: 4, 6

## 5. Broker Setup and Runtime Wiring

- [ ] 5.1 Build the moderator client in `or3-intern/cmd/or3-intern/security_setup.go`.
  - Attach it to `approval.Broker` only when approvals and moderator config are enabled.
  - Requirements: 1, 5, 8

- [ ] 5.2 Route moderator provider/model selection through existing provider profile helpers.
  - Reuse configured providers and model routing where possible.
  - Requirements: 5

- [ ] 5.3 Ensure moderator provider calls do not trigger tool approvals.
  - Use direct provider chat calls with no tools and no execution path.
  - Requirements: 4, 5

- [ ] 5.4 Add runtime capability reporting.
  - Include moderator enabled state, preset, model, failure action, and action map in capabilities/status without leaking policy text.
  - Requirements: 6, 9

## 6. CLI, Service API, and App Surfaces

- [ ] 6.1 Extend `or3-intern/cmd/or3-intern/approvals_cmd.go`.
  - Show moderator risk/action/status/reason for list/show where present.
  - Requirements: 6, 8

- [ ] 6.2 Extend service approval API response types.
  - Add optional moderator fields so existing clients remain compatible.
  - Requirements: 6, 8

- [ ] 6.3 Update OR3 App approval UI metadata handling if needed.
  - Display risk/status for escalated requests and terminal auto-denials.
  - Requirements: 6, 8

- [ ] 6.4 Preserve channel approval routing tests.
  - Verify escalated Telegram/Slack/Discord/WhatsApp requests still prompt and resume in the original channel.
  - Requirements: 8

## 7. Configuration UI and Docs

- [ ] 7.1 Add configure TUI fields for approval moderator settings.
  - Preset, provider, model, timeout, failure action, per-risk actions, and user policy.
  - Requirements: 1, 5, 9

- [ ] 7.2 Add config edit field keys in `or3-intern/internal/configedit`.
  - Ensure app/service configuration can update moderator settings safely.
  - Requirements: 1, 9

- [ ] 7.3 Update approval workflow docs.
  - Explain moderator defaults, presets, hard-deny classes, failure behavior, model usage, and tuning guidance.
  - Requirements: 3, 4, 5, 9

- [ ] 7.4 Update configuration reference.
  - Document all `security.approvals.moderator.*` fields.
  - Requirements: 1, 5, 9

## 8. Tests and Verification

- [ ] 8.1 Add broker unit tests with fake moderator.
  - Auto-approve, escalate, deny, timeout/failure, hard override, no signing key, and allowlist precedence.
  - Requirements: 2, 3, 8

- [ ] 8.2 Add exec regression tests.
  - Low-risk test command auto-approves; user-policy-denied `grep` returns safe alternative; legacy shell escalates.
  - Requirements: 2, 4, 10

- [ ] 8.3 Add quota regression tests.
  - Low-risk quota bump can auto-approve; high session quota increase escalates.
  - Requirements: 2, 3

- [ ] 8.4 Add service/API tests.
  - Approval list/show includes moderator metadata and existing response compatibility holds.
  - Requirements: 6, 8

- [ ] 8.5 Run focused Go tests.
  - `go test ./internal/config ./internal/db ./internal/approval ./internal/tools ./internal/agent ./cmd/or3-intern`
  - Requirements: all

## Out of Scope

- [ ] Replacing sandboxing, access profiles, network policy, or command allowlists.
- [ ] Giving the moderator tool access or recursive approval capability.
- [ ] Auto-approving extreme-risk actions by default.
- [ ] Building a separate approval microservice.
- [ ] Changing Codex's own auto-review implementation.
