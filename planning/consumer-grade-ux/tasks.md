# 1. Build shared user-intent foundations

- [ ] [Req 1, Req 2, Req 3] Add `internal/safetymode` with `SafetyMode`, scenario choices, `Apply`, `Infer`, and `Drift` helpers over `config.Config`.
- [ ] [Req 2] Define deterministic config patches for Relaxed, Balanced, and Locked Down, covering runtime profile, workspace restriction, approvals, audit, service exposure, sandbox/exec, network policy, and quotas.
- [ ] [Req 2] Add table-driven tests proving each safety mode produces expected config fields and that custom/drifted configs are detected without losing advanced overrides.
- [ ] [Req 3, Req 6, Req 9] Add `internal/uxcopy` with centralized labels, hints, warning explanations, problem titles, and recovery actions for current config fields and doctor finding IDs.
- [ ] [Req 9] Add a structured user-error translator that maps common raw errors and approval decision reasons to title, explanation, fix, suggested command, and advanced detail.

# 2. Add simple command mode without removing advanced commands

- [ ] [Req 8] Update root help rendering in `cmd/or3-intern/help.go` so default help shows only simple commands and `--advanced` shows the current full catalog.
- [ ] [Req 8] Add command aliases/routes for `setup`, `settings`, `status`, and `connect-device` while preserving `configure`, `doctor`, `capabilities`, `approvals`, `devices`, `pairing`, `audit`, `secrets`, `embeddings`, and `scope`.
- [ ] [Req 8] Add tests for simple help, advanced help, command-specific help, and backwards-compatible advanced command invocation.
- [ ] [Req 8] Update README and CLI docs so beginners start with `setup`, `chat`, `status`, and `settings`, while operators can still find the advanced reference.

# 3. Replace first-run configure with scenario setup

- [ ] [Req 1, Req 10] Implement `setup` as the recommended first-run flow using the existing configure defaults plus scenario and safety-mode patches.
- [ ] [Req 1] Ask the scenario question: this computer, phone too, private server, public hosted service, advanced/manual.
- [ ] [Req 1, Req 2] Ask for Safety Mode with Balanced recommended unless the chosen scenario requires Locked Down.
- [ ] [Req 1, Req 7] Show a review screen summarizing folder access, command behavior, internet/service exposure, connected-device readiness, memory, and safety log state.
- [ ] [Req 1, Req 6] Run a post-save doctor evaluation and present any findings through friendly problem copy instead of raw IDs.
- [ ] [Req 1] Keep non-interactive setup deterministic for tests and scripts, either through flags or stable prompt fallback.
- [ ] [Req 1] Add tests for each scenario-to-config result and first-run completion path.

# 4. Create friendly status and Fix Problems screen

- [ ] [Req 6, Req 7] Add `internal/uxstate` status view models that aggregate config, `doctor.Report`, pending approval count, paired devices, provider readiness, and access posture.
- [ ] [Req 6] Implement `status` command with a concise normal-user summary and a deeper `--problems` or interactive problems view.
- [ ] [Req 6] Render doctor findings as problem, why it matters, recommended action, and keep-as-is/advanced-details where appropriate.
- [ ] [Req 6] Refactor doctor fix application as needed so known fixes can be applied per finding from the simple UI.
- [ ] [Req 9] Use the new user-error translator for startup/config/provider failures surfaced by `status`, `setup`, and `chat` startup checks.
- [ ] [Req 6] Add tests proving internal IDs are hidden by default and visible in advanced/debug output.

# 5. Humanize approvals

- [ ] [Req 4] Add an approval prompt presenter that decodes approval subjects into what/why/risk/choices/advanced-details.
- [ ] [Req 4] Add a risk classifier for exec subjects, skill execution, secret access, message send, and file transfer.
- [ ] [Req 4] Update approval command output so default `list`/`show` uses human wording and hides IDs/matcher JSON unless advanced details are requested.
- [ ] [Req 4] Add friendly choice labels for allow once, always allow this kind of action in this folder, and deny while preserving current broker methods.
- [ ] [Req 4, Req 9] Translate `approval broker unavailable` and `approval required` failures into guided next actions.
- [ ] [Req 4] Add tests for representative approval subjects such as `npm install`, unknown shell command, skill execution, message send, and secret access.

# 6. Replace pairing workflow with Connect a Device

- [ ] [Req 5] Implement `connect-device` as the simple pairing entry point.
- [ ] [Req 5] Add prerequisite checks for service enablement, service secret strength, approval key availability, pairing mode, and device store readiness.
- [ ] [Req 5] Offer safe automatic repairs for missing service secret/key and protected pairing defaults using existing config/doctor helpers where possible.
- [ ] [Req 5] Show pairing code, expiration, plain setup steps, and role choices: Chat only, Chat and workspace files, Admin device.
- [ ] [Req 5] Add a friendly connected-device list with display name, role label, last used, status, change access, and disconnect actions.
- [ ] [Req 5] Keep `devices` and `pairing` as advanced commands and document them as operator tools.
- [ ] [Req 5] Add tests for role mapping, prerequisite repair prompts, code rendering, and revoke/change-access actions.

# 7. Build task-based Settings UI

- [ ] [Req 3, Req 10] Add a `settings` TUI home with AI Provider, Workspace Folder, Connected Devices, Safety Level, Channels, Tools, Memory, and Advanced.
- [ ] [Req 3] Reuse `internal/uxcopy` labels and hints for every field shown in simple settings.
- [ ] [Req 3] Implement Safety Level settings as the primary security control and move raw runtime profile/approval/audit/hardening knobs under Advanced.
- [ ] [Req 3] Implement Workspace Folder settings with folder path, only-this-folder toggle, and clear warnings for broader access.
- [ ] [Req 3, Req 10] Implement progressive panels for Channels, Tools, Memory, and Advanced so disabled power features do not dominate the default view.
- [ ] [Req 3] Add Export Advanced Config action that writes or prints current JSON without making JSON the editing surface.
- [ ] [Req 3] Add TUI/view tests for settings home, safety mode edit, workspace edit, and advanced escape hatch.

# 8. Build What Can OR3 Access dashboard

- [ ] [Req 7] Add an access dashboard view model sourced from config, doctor findings, capabilities posture, approvals, devices, channels, and memory settings.
- [ ] [Req 7] Render sections for Files, Commands, Internet, Connected Apps, Connected Devices, Memory, and Activity Log.
- [ ] [Req 7] Add plain status labels and color/risk states for bounded, asks first, allowed, blocked, protected, unprotected, off, and needs attention.
- [ ] [Req 7] Add change actions that route into the relevant settings panel or fix-problems action.
- [ ] [Req 7] Ensure a normal user can answer "Can OR3 see my whole computer?" from the first dashboard screen.
- [ ] [Req 7] Add tests for local-only, phone-paired, private-server, and hosted-service dashboard states.

# 9. Enforce guided recovery everywhere

- [ ] [Req 9] Replace raw error rendering in simple-mode commands with structured user-error output.
- [ ] [Req 9] Cover common errors: approval broker unavailable, audit logger unavailable, unknown tool in tool policy, runtime unavailable, service auth missing, workspace missing, sandbox missing, provider key missing.
- [ ] [Req 9] Add advanced details expansion that preserves the raw error, finding ID, or validation text.
- [ ] [Req 9] Add golden-output tests for translated error messages and suggested next actions.
- [ ] [Req 9] Audit startup and command paths to ensure new user-facing flows never dead-end without a clear next action.

# 10. Polish progressive disclosure and documentation

- [ ] [Req 10] Update beginner docs around the mental model: Folder, Safety Level, Connected Devices, Allowed Actions, Activity Log.
- [ ] [Req 10] Move exhaustive command/config material into clearly marked Advanced/operator references.
- [ ] [Req 10] Add manual QA walkthrough for first run under two minutes, safety-mode change, approval decision, connect-device, fix-problems, and access-dashboard checks.
- [ ] [Req 10] Add copy review checklist banning internal terms from simple-mode surfaces unless advanced details are expanded.
- [ ] [Req 10] Add release checklist for backwards compatibility: existing configure, doctor, capabilities, approvals, devices, pairing, service, audit, secrets, embeddings, and scope commands still work.
- [ ] [Req 10] Run focused tests after each milestone and the existing `Build Go workspace` task before merge.

# Suggested milestone order

1. Shared user-intent foundations.
2. Simple help and command aliases.
3. Scenario setup and safety modes.
4. Friendly status and Fix Problems.
5. Human approval prompts.
6. Connect a Device.
7. Task-based Settings.
8. Access dashboard.
9. Guided recovery everywhere.
10. Documentation, QA, and polish.

# Definition of done

- New users can run setup, choose a folder and safety mode, and start chat in under two minutes.
- Default help no longer looks like an operator/admin surface.
- Safety state is explained consistently across setup, settings, status, doctor, capabilities-backed dashboard, and approvals.
- Every security warning shown in simple mode includes a plain-language reason and a next action.
- Advanced users retain the full current command set and can inspect raw config, finding IDs, and approval details when requested.
