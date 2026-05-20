# Grandma UX Simplification Tasks

## 1. Consolidate setup into one normal path

- [ ] 1.1 Audit current `setup`, `init`, `configure`, app host setup, and README quick-start paths; mark which code path is canonical and which are compatibility/advanced. Requirements: 1
- [ ] 1.2 Update `or3-intern setup` to be the only recommended first-run command in normal CLI help and docs. Requirements: 1
- [ ] 1.3 Make `init` delegate to the setup engine or print compatibility copy that points users to `setup`, while preserving existing non-interactive/script behavior. Requirements: 1, 8
- [ ] 1.4 Move `configure` language to advanced/manual configuration in root help, CLI reference, and setup docs without removing `configure --section`. Requirements: 1, 8
- [ ] 1.5 Add regression tests for `setup`, `init`, and `configure --section` compatibility. Requirements: 1, 8, 10

## 2. Add success moments and milestone state

- [ ] 2.1 Define milestone names for setup complete, pairing complete, and first successful chat complete. Requirements: 2
- [ ] 2.2 Add setup completion output with “You did it”, a plain-language summary, and no more than four next-step choices. Requirements: 2
- [ ] 2.3 Add pairing completion output in CLI/app flows that names the connected device and access level. Requirements: 2, 4, 5
- [ ] 2.4 Add first-chat-complete one-time success prompt for CLI chat and app chat where practical. Requirements: 2, 3
- [ ] 2.5 Persist milestone dismissal/completion using existing config, SQLite, or app local storage patterns; do not store secrets or pairing internals. Requirements: 2, 10
- [ ] 2.6 Add tests that milestone messages do not repeat unnecessarily and do not include raw IDs, tokens, CIDRs, MCP, embeddings, runtime profile, or other advanced terms. Requirements: 2, 10

## 3. Add app welcome/onboarding card

- [ ] 3.1 Identify the existing app connection/session state used by chat and settings pairing screens. Requirements: 3
- [ ] 3.2 Add a “Welcome! Let’s get you set up.” card to the unconnected app landing state. Requirements: 3
- [ ] 3.3 Route Electron-capable users from the welcome card into the existing `ElectronHostSetupWizard`. Requirements: 3
- [ ] 3.4 Route web/mobile users from the welcome card into the simplified pairing flow. Requirements: 3, 4
- [ ] 3.5 Add a secondary “What can OR3 do?” explainer action that does not require setup to read. Requirements: 3
- [ ] 3.6 Add app tests for unconnected, Electron host-capable, web/mobile, and connected states. Requirements: 3, 10

## 4. Simplify app pairing to discovery-or-code

- [ ] 4.1 Inventory existing app pairing modes and label manual/legacy methods that should move behind Advanced. Requirements: 4, 8
- [ ] 4.2 Add safe local host discovery where the platform supports it, bounded by a short timeout. Requirements: 4, 10
- [ ] 4.3 If exactly one host is discovered, show a “Connect to this computer” card with visible host name and access label. Requirements: 4
- [ ] 4.4 If discovery finds zero or multiple hosts, fall back to one simple code entry flow. Requirements: 4
- [ ] 4.5 Move request-ID/token/manual-origin/CIDR-oriented pairing details behind Advanced. Requirements: 4, 8, 10
- [ ] 4.6 Add app tests for single-host discovery, no-host fallback, multi-host fallback, unsupported-platform fallback, and redaction of raw pairing internals. Requirements: 4, 10

## 5. Implement `pair --auto` in `or3-intern`

- [ ] 5.1 Add `pair` command routing with `--auto` as the normal documented path, preserving existing `connect-device` and lower-level pairing commands. Requirements: 5, 8
- [ ] 5.2 Reuse the structured health/doctor engine to check service readiness, auth posture, pairing config, listen address, local reachability, and required directories/keys. Requirements: 5, 7, 10
- [ ] 5.3 Add conservative auto-fix/offer-fix behavior for deterministic safe issues such as missing local directories, missing generated secrets/keys, disabled local service prerequisites, or inconsistent loopback/private defaults. Requirements: 5, 10
- [ ] 5.4 Add blocker output for issues that require manual networking, Tailscale/firewall/DNS decisions, public exposure, or unsafe trust broadening. Requirements: 5, 10
- [ ] 5.5 Generate or reuse the existing local pairing flow after prerequisites pass; default output should show only simple code/QR instruction, role, expiration, and app step. Requirements: 5
- [ ] 5.6 Add `--json`, `--name`, `--role`, `--no-fix`, and advanced/manual options only where needed for scripts/operators. Requirements: 5, 8
- [ ] 5.7 Add tests covering ready config, safe-fix config, blocked config, output redaction, compatibility with `connect-device`, and non-interactive behavior. Requirements: 5, 8, 10

## 6. Rename normal health workflow from `doctor` to `health`

- [ ] 6.1 Add `or3-intern health` as a wrapper over the existing doctor/health engine. Requirements: 7
- [ ] 6.2 Make `or3-intern health` default to the check/readiness report; support `health --check` as explicit equivalent. Requirements: 7
- [ ] 6.3 Keep `or3-intern doctor` as a compatibility alias and add gentle copy pointing normal users to `health`. Requirements: 7, 8
- [ ] 6.4 Reduce short help to the core forms: `health`, `health --fix`, and `health --json`; move area/severity/fixable/probe filters to advanced help. Requirements: 7, 8
- [ ] 6.5 Ensure health output groups overall status, blockers, available fixes, and next steps. Requirements: 7
- [ ] 6.6 Add tests proving `health` and `doctor` use the same report engine and that `--check` is equivalent to default. Requirements: 7, 10

## 7. Collapse default settings to max five sections plus Advanced

- [ ] 7.1 Define the final default section list: AI Service, Workspace, Safety, Connected Apps/Devices, and Appearance/App Preferences where supported. Requirements: 6
- [ ] 7.2 Update CLI settings builders so normal mode renders no more than five sections plus Advanced. Requirements: 6
- [ ] 7.3 Update app settings home/simple settings to render no more than five visible sections plus Advanced. Requirements: 6
- [ ] 7.4 Move raw model routing, MCP/custom tool config, embeddings/doc index details, runtime profile, logs/observability, env-style config, and developer diagnostics behind Advanced. Requirements: 6, 8
- [ ] 7.5 Preserve `settings --section` and `configure --section` for existing section keys. Requirements: 6, 8
- [ ] 7.6 Add CLI/app tests that normal settings renders at most five sections and advanced paths still expose existing controls. Requirements: 6, 8, 10

## 8. Add confirmation gates for destructive actions

- [ ] 8.1 Inventory destructive app actions: disconnect/unpair, revoke device/passkey, delete scheduled task, remove integration/add-on, reset PIN/session, and similar actions. Requirements: 9
- [ ] 8.2 Add reusable app confirmation component or pattern that displays item name, consequence, and undo availability. Requirements: 9
- [ ] 8.3 Apply confirmation gates to destructive app actions without changing backend authorization checks. Requirements: 9, 10
- [ ] 8.4 Inventory destructive CLI actions and add interactive confirmations where stdin/stdout are terminals. Requirements: 9
- [ ] 8.5 Preserve scriptability with explicit `--force`, existing non-interactive behavior, or compatibility paths where needed. Requirements: 8, 9
- [ ] 8.6 Add tests for app confirmation rendering and CLI confirmation/force behavior. Requirements: 9, 10

## 9. Update copy, docs, and command discovery

- [ ] 9.1 Update README quick start to show the normal path: `setup`, `chat`, `pair --auto`, `health`, and `settings`. Requirements: 1, 5, 7
- [ ] 9.2 Update CLI reference to group commands into normal, setup/connection, and advanced/operator sections. Requirements: 1, 7, 8
- [ ] 9.3 Update app integration docs to prefer auto-discovery or simple code and move manual networking details to troubleshooting/advanced. Requirements: 4, 5
- [ ] 9.4 Add success-moment screenshots/text examples to setup and pairing docs where docs support them. Requirements: 2
- [ ] 9.5 Keep advanced docs for configure, raw pairing, old commands, environment variables, and hosted/service deployment. Requirements: 8

## 10. Validate safety and compatibility

- [ ] 10.1 Add no-secret/no-raw-pairing-output regression checks for setup, health, pair auto, app welcome state, and app pairing state. Requirements: 2, 4, 5, 10
- [ ] 10.2 Run focused Go tests for changed CLI commands, health engine wrappers, config compatibility, and pairing helpers. Requirements: 1, 5, 7, 8, 10
- [ ] 10.3 Run app unit/component tests for welcome card, settings section cap, pairing fallback, first-chat success, and confirmations. Requirements: 3, 4, 6, 9
- [ ] 10.4 Run the Go workspace build after backend implementation. Requirements: 10
- [ ] 10.5 Run the app build/test commands after frontend implementation using the repo’s Bun scripts. Requirements: 3, 4, 6, 9, 10
- [ ] 10.6 Smoke-test the full normal path manually: setup → health → pair auto → app welcome → pair → first chat → settings → destructive confirmation. Requirements: 1, 2, 3, 4, 5, 6, 7, 9, 10

## Out of scope

- [ ] Do not do the broad 43-term jargon pass in this implementation.
- [ ] Do not delete `doctor`, `init`, `configure`, `connect-device`, old pairing commands, existing config keys, SQLite data, or internal service routes in the first pass.
- [ ] Do not implement a new hosted discovery service, daemon, or external control plane.
- [ ] Do not weaken auth, approvals, file restrictions, terminal restrictions, service binding rules, or secret redaction to make pairing easier.
- [ ] Do not migrate all app screens to a new facade API unless that work is separately scheduled from `planning/consumer-ux-facade/`.
