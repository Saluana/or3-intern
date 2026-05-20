# Grandma UX Simplification Requirements

## Overview

This plan focuses the OR3 simplification effort on one clear outcome: a non-technical person should be able to launch OR3, get connected, complete setup, send a first message, and understand where settings live without needing to learn CLI internals, network terminology, multiple setup paths, or advanced agent/runtime concepts.

Scope assumptions:

- This is a cross-product plan touching `or3-intern` and `or3-app`, but planning artifacts live in `or3-intern/planning` because `or3-intern` owns setup, pairing, service readiness, config, and the backend API used by the app.
- The first implementation should simplify default surfaces, not remove advanced power permanently. Existing scriptable commands, config keys, SQLite data, and internal APIs should remain backward-compatible until a later deprecation window.
- The broad jargon cleanup is intentionally deferred. This plan only renames terms that are required for the selected flows, such as `doctor` to `health` and user-facing pairing/setup labels.
- Existing plans under `planning/consumer-ux-facade/` and `planning/doctor-overhaul/` remain useful background. This plan narrows the rollout to setup, pairing, welcome/onboarding, success moments, settings disclosure, and the `doctor` → `health` transition.

## Requirements

### Requirement 1: Make setup one obvious path

**User story:** As a first-time user, I want one setup command and one setup experience, so that I do not need to choose between `setup`, `init`, `configure`, app setup, or raw settings before OR3 works.

#### Acceptance Criteria

1. WHEN a user follows README quick start THEN the only recommended first-run command SHALL be `or3-intern setup`.
2. WHEN `or3-intern init` is invoked THEN it SHALL either delegate to the same setup engine or clearly state that `setup` is the preferred path, while preserving non-breaking behavior for existing users.
3. WHEN `or3-intern configure` is shown in help/docs THEN it SHALL be labeled as advanced/manual configuration, not as a normal first-run path.
4. WHEN setup completes THEN it SHALL write the same config shape used today and SHALL not rename, delete, or rewrite unrelated existing config keys.
5. WHEN setup runs in a non-interactive terminal THEN it SHALL preserve existing plain-text/script-friendly behavior and fail with actionable copy if required answers are unavailable.

### Requirement 2: Add closure and celebration after key milestones

**User story:** As a normal user, I want OR3 to tell me “you did it” after setup, pairing, and my first successful chat, so that I know the task is complete and what to try next.

#### Acceptance Criteria

1. WHEN setup succeeds THEN CLI output SHALL include a short success state, a plain-language summary, and no more than four next-step choices.
2. WHEN pairing succeeds from the CLI or app THEN the user SHALL see a success confirmation that names the connected device and access level.
3. WHEN the first app chat or first CLI chat response completes successfully after setup THEN the user SHALL see a one-time “first chat complete” confirmation or next-step prompt.
4. WHEN a milestone success message is shown THEN it SHALL avoid advanced terms such as raw IDs, tokens, request IDs, CIDRs, service listener, embeddings, MCP, or runtime profile.
5. WHEN the user has already completed a milestone THEN the success message SHALL not repeat on every launch; state SHALL be stored locally using existing config/SQLite/app storage patterns.

### Requirement 3: Show a “Welcome! Let’s get you set up.” card in the app

**User story:** As an app user opening OR3 before pairing or setup, I want the app to guide me through the next action instead of showing an empty or broken chat screen.

#### Acceptance Criteria

1. WHEN `or3-app` starts and no usable host/pairing/session state exists THEN the main screen SHALL show a welcome setup card instead of a dead-end chat state.
2. WHEN the welcome card is shown THEN it SHALL offer a primary action to connect this app to a computer and a secondary action to learn what OR3 can do.
3. WHEN host setup is available in Electron THEN the welcome card SHALL route to the existing host setup wizard instead of duplicating that flow.
4. WHEN the app is web/mobile and no host is connected THEN the welcome card SHALL route to the simplified pairing flow.
5. WHEN pairing or setup completes THEN the welcome card SHALL disappear and the user SHALL land in a usable chat or next-step checklist.

### Requirement 4: Prefer auto-discovery, then one simple pairing code

**User story:** As a user connecting the app to my computer, I want the app to find my computer automatically or show me one simple code flow, so that I do not need to understand request IDs, tokens, origins, CIDRs, listen addresses, or multiple pairing methods.

#### Acceptance Criteria

1. WHEN the app opens pairing THEN it SHALL first attempt safe local discovery of reachable OR3 hosts where platform capabilities allow it.
2. WHEN discovery finds exactly one likely host THEN the app SHALL offer a simple “Connect to this computer” action with the host name and safety/access label.
3. WHEN discovery finds zero or multiple hosts THEN the app SHALL show a simple code entry flow and clear instructions for the matching CLI command.
4. WHEN an advanced/manual pairing method remains available THEN it SHALL be behind an Advanced section and not compete with the default flow.
5. WHEN pairing data is displayed in the default flow THEN it SHALL not show raw request IDs, device IDs, bearer tokens, service secrets, trusted origins, or CIDR values.

### Requirement 5: Add `pair --auto` as the default repair-guided pairing command

**User story:** As a user pairing a phone or browser, I want one CLI command that checks service readiness, fixes safe local config problems, starts or verifies the service, and prints the simplest possible next step.

#### Acceptance Criteria

1. WHEN a user runs `or3-intern pair --auto` THEN the command SHALL evaluate the local service, pairing, auth, listen address, and browser/mobile reachability prerequisites before creating a code.
2. WHEN a prerequisite has a deterministic safe fix THEN `pair --auto` SHALL apply or offer that fix using the same conservative repair model as the health engine.
3. WHEN a prerequisite cannot be fixed automatically THEN `pair --auto` SHALL explain the blocker in plain language and provide one next action.
4. WHEN `connect-device` or older pairing commands still exist THEN help/docs SHALL point normal users to `pair --auto` and label older/manual forms as advanced or compatibility paths.
5. WHEN `pair --auto` succeeds THEN it SHALL print one simple code or QR/invite action, the device access level, expiration time, and what screen to open in the app.

### Requirement 6: Collapse default settings to at most five visible sections plus Advanced

**User story:** As a normal user, I want settings to show only a few understandable categories, so that I can change common things without seeing every internal runtime, model, network, tool, memory, and integration knob.

#### Acceptance Criteria

1. WHEN a user opens default CLI settings THEN no more than five normal sections SHALL be shown before an explicit Advanced entry.
2. WHEN a user opens default app settings THEN no more than five normal sections SHALL be shown before an explicit Advanced entry.
3. The default visible sections SHOULD be: AI Service, Workspace, Safety, Connected Apps/Devices, and Appearance/App Preferences where supported.
4. WHEN advanced settings are opened THEN existing raw config sections, env-oriented controls, MCP/custom tool details, model routing, runtime profile, health internals, logs, and developer diagnostics MAY remain accessible.
5. WHEN old section keys are used through `settings --section` or `configure --section` THEN they SHALL continue to work for backward compatibility.

### Requirement 7: Rename `doctor` to `health` for normal users and make check the default

**User story:** As a user checking whether OR3 is working, I want to run a command named `health` that checks the system by default, instead of learning the `doctor` metaphor and a long list of flags.

#### Acceptance Criteria

1. WHEN a user runs `or3-intern health` THEN it SHALL perform the default human-readable readiness check without requiring a subcommand.
2. WHEN a user runs `or3-intern health --check` THEN behavior SHALL match the default check and be documented as optional/explicit.
3. WHEN a user runs `or3-intern doctor` THEN it SHALL continue to work as a compatibility alias during the transition and point users toward `health` in normal copy.
4. WHEN health help is shown THEN default help SHALL prioritize `health`, `health --fix`, and `health --json`; deeper filters such as area/severity/fixable/probe SHALL be advanced help.
5. WHEN health reports issues THEN output SHALL show overall status, blockers, safe fixes, and next steps using the existing structured doctor/health engine.

### Requirement 8: Keep power available behind explicit gates

**Engineering objective:** Simplify default flows without deleting power-user or operator capabilities needed for debugging, scripts, hosted deployment, custom models, MCP, raw config, terminal, or security review.

#### Acceptance Criteria

1. WHEN a user enters Advanced settings or advanced CLI help THEN existing detailed controls SHALL remain reachable unless separately deprecated.
2. WHEN a command or UI action is destructive, privileged, raw, or developer-oriented THEN it SHALL be gated by explicit labels or confirmations in default surfaces.
3. WHEN scripts rely on existing CLI commands or config keys THEN the first implementation SHALL preserve those paths or provide compatibility aliases.
4. WHEN app routes migrate to simpler flows THEN existing internal service routes SHALL remain available until the app migration is complete and covered by tests.
5. WHEN advanced output exposes raw IDs or tokens THEN it SHALL be opt-in and clearly labeled advanced/debug.

### Requirement 9: Add confirmations for destructive actions

**User story:** As a normal user, I want OR3 to ask before disconnecting, deleting, revoking, or removing something important, so that accidental taps or commands do not break my setup.

#### Acceptance Criteria

1. WHEN the app offers disconnect, delete, revoke, remove, reset, or unpair actions THEN it SHALL show a confirmation before applying the action.
2. WHEN the CLI offers equivalent destructive actions in interactive mode THEN it SHALL confirm before applying the action unless a force/non-interactive flag is explicitly used.
3. WHEN a confirmation is shown THEN it SHALL include the visible name of the affected item, the consequence, and whether undo is available.
4. WHEN a destructive action is scriptable/non-interactive THEN existing behavior MAY remain available behind explicit flags for backward compatibility.
5. WHEN confirmation tests run THEN they SHALL cover at least disconnecting a device, revoking a passkey/device, and deleting/removing a scheduled item or integration where applicable.

### Requirement 10: Preserve safety, determinism, and low-resource behavior

**Engineering objective:** The simplified UX must not weaken OR3’s local-first safety model, SQLite persistence, file restrictions, bounded execution, or single-process determinism.

#### Acceptance Criteria

1. WHEN setup, pairing, health, or app onboarding invokes repairs or discovery THEN those operations SHALL be bounded by short timeouts and local-first assumptions.
2. WHEN pairing or discovery uses network access THEN it SHALL avoid broad scanning by default and SHALL not leak secrets into logs, UI text, URLs, or generated codes.
3. WHEN storing milestone or onboarding state THEN it SHALL use existing config/SQLite/app storage patterns without adding a new service or unbounded cache.
4. WHEN any simplified command modifies config THEN it SHALL preserve comments/unknown fields where the current config writer supports that behavior and avoid unrelated rewrites.
5. WHEN tests validate these flows THEN they SHALL include regression checks for backward compatibility, no-secret output, and deterministic behavior.

## Non-functional constraints

- **Backward compatibility:** Existing commands, config keys, SQLite tables, service routes, and app pairing data must not be removed in the first implementation pass. Hide/deprecate before deleting.
- **Single-process SQLite model:** Do not add a new control-plane service, hosted dependency, or long-running discovery daemon. Use bounded checks in the existing process.
- **Low memory usage:** Discovery, health, settings, onboarding state, and success milestone queries must be small and bounded.
- **Secure by default:** Pairing auto-repair must never silently expose a service publicly, weaken auth, widen trusted origins/CIDRs, or print secrets.
- **Progressive disclosure:** Default UI/CLI surfaces show normal-user choices; advanced controls remain reachable but clearly gated.
- **No broad jargon pass yet:** Only rename terms needed by this plan. A full language audit should be a later focused implementation.
- **Cross-product sequencing:** Backend setup/pairing/health changes should land before app onboarding depends on them. App changes must tolerate older backend capability responses.
- **Regression coverage:** Every simplified default path should have tests proving existing advanced/scriptable paths still work.
