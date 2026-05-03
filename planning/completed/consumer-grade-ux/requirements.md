# Overview

This plan turns `or3-intern` from a powerful developer/admin CLI into a consumer-grade agent experience that keeps the existing security and runtime architecture, but presents it through plain-language choices, visual safety state, and guided recovery.

The product goal is simple: OR3 should feel like a trustworthy desktop app that happens to have a CLI, not like a security appliance. Users choose intent; the system translates that intent into config, runtime profiles, approvals, audit, service auth, pairing, tool policy, network policy, and hardening.

Primary mental model:

- Folder: where OR3 is allowed to work.
- Safety Level: how careful OR3 should be.
- Connected Devices: who can talk to OR3.
- Allowed Actions: what OR3 can do without asking.
- Activity Log: what OR3 did and why.

Scope assumptions:

- Keep advanced commands and configuration available for power users.
- Preserve backwards compatibility for `config.json`, current command names, service APIs, and existing tests.
- Build first in the terminal/Bubble Tea experience; leave a web/desktop shell as a later adapter over the same view models.
- Prefer reusable presenter/view-model packages over hardcoding user-facing copy inside every command.
- Do not weaken security defaults to make setup feel easier.

# North-star UX requirements

## 1. First run must be scenario-based, not section-based

Users must start with real-life setup choices instead of technical config sections.

Acceptance criteria:

- First run asks no more than four core questions: AI provider, workspace folder, safety mode, and whether to start chat.
- The setup wizard asks "Where are you using OR3?" with plain choices: this computer, phone too, private server, public hosted service, advanced/manual.
- Scenario choices map internally to existing runtime profiles and config settings without exposing profile names by default.
- JSON paths and raw config keys are hidden unless the user enters advanced mode or explicitly exports config.
- Non-interactive configure behavior remains stable for scripts.

## 2. Safety must collapse into three understandable modes

Users must control security through `Relaxed`, `Balanced`, and `Locked Down`, with detailed knobs hidden behind advanced mode.

Acceptance criteria:

- `Balanced` is the default and recommended option.
- Each mode has one-sentence user copy and a deterministic config patch.
- The safety-mode presenter can explain what changes: file access, command prompts, service exposure, audit logging, sandboxing, network limits, and connected-device requirements.
- `doctor` validates whether the current config still matches the selected safety intent and reports drift in plain English.
- Advanced config can override mode-derived settings, but the UI marks the result as "custom" instead of pretending it is still one of the three modes.

## 3. Settings must be task-based, not schema-based

The main settings UI must expose user goals instead of config sections.

Acceptance criteria:

- The default settings home shows: AI Provider, Workspace Folder, Connected Devices, Safety Level, Channels, Tools, Memory, and Advanced.
- Each settings panel uses toggles, choices, and plain-language descriptions for common settings.
- Existing configure sections remain available under Advanced.
- Every user-facing setting has a translation from internal key to plain-language label.
- "Export advanced config" is available, but editing JSON is not the primary path.

## 4. Approvals must read like notification prompts

Approval prompts must answer four questions: what OR3 wants to do, why it wants to do it, what could go wrong, and what choices the user has.

Acceptance criteria:

- Approval list/show views hide request IDs, matcher JSON, subject hashes, and allowlist terminology by default.
- Each pending request has a human title, risk level, short reason, possible harm, and three actions: allow once, remember in this folder, deny.
- Advanced details are one keystroke away for technical users.
- The backend approval broker API and SQLite schema remain unchanged unless a missing field is required for better explanations.
- Agent/tool errors caused by pending approvals point users to the human approval prompt, not raw policy terms.

## 5. Pairing must become "Connect a device"

Device setup must feel like pairing a TV app, not configuring service auth.

Acceptance criteria:

- Simple mode exposes `connect-device` as the primary flow.
- The flow shows a short pairing code, clear steps, and role choices: Chat only, Chat and workspace files, Admin device.
- Device list shows display name, role, last used, and actions: change access, disconnect.
- Internal roles such as `viewer`, `operator`, and `admin` are hidden in simple mode.
- Service auth and pairing broker requirements are automatically checked and fixed through guided prompts.

## 6. Doctor must become a Fix Problems screen

Readiness and security findings must be presented as problems with clear fixes.

Acceptance criteria:

- `status` shows a concise health summary for normal users.
- `doctor` remains available in advanced mode.
- Findings render as: problem, why it matters, recommended fix, and optional keep-as-is action.
- Internal IDs such as `privileged-exec.sandbox_disabled` are hidden unless advanced details are expanded.
- Fixable findings can be repaired individually from the UI, not only via broad `--fix`.

## 7. Users must see what OR3 can access in five seconds

The product must make boundaries visible and easy to change.

Acceptance criteria:

- A dashboard answers: can OR3 see my whole computer, can it run commands, can it use the internet, can other apps connect, and what memory is saved?
- The dashboard uses `capabilities`, config, approvals, and doctor data as backend inputs.
- Each access area has a plain status, risk color, and change action.
- Workspace restriction and allowed folder are visually prominent.
- Channels, service, triggers, MCP, and skills are hidden until enabled or opened under Advanced Tools.

## 8. Help must default to simple mode

The default command list must be short enough for non-technical users.

Acceptance criteria:

- Simple help shows only: `chat`, `setup`, `status`, `settings`, `connect-device`, `update`, and `help`.
- Existing command names remain functional.
- Advanced help is available through an explicit flag, env var, config setting, or `advanced` command group.
- The installed binary can eventually expose a friendly alias such as `or3` while keeping `or3-intern` for development/admin use.
- Documentation separates beginner workflows from operator reference material.

## 9. Errors must include guided recovery

Raw runtime/security failures must be translated into human messages with one clear next action.

Acceptance criteria:

- Common internal errors map to title, explanation, fix action, and advanced details.
- Error translation covers approvals, audit logger, tool policy, runtime startup, service auth, workspace access, sandbox availability, and provider setup.
- Commands use translated errors in simple mode and retain raw details in advanced/debug output.
- Translated errors are testable as data, not scattered string replacements.
- Every translated error includes a suggested command or button label.

## 10. Progressive disclosure must be enforced

Power features must appear only after the user asks for them or a use case requires them.

Acceptance criteria:

- First-run setup does not ask about channels, triggers, MCP, audit, profiles, allowlists, service roles, or cron.
- Settings suggests optional next steps after setup: connect phone, enable email, schedule tasks, install skills, enable advanced tools.
- Advanced surfaces remain searchable and documented.
- Simple UI copy never uses internal terms unless an advanced details panel is open.
- UX tests cover both simple and advanced help/settings rendering.

# UX quality bar

Best-in-industry means the following are true before release:

- Time to first successful chat is under two minutes for a new local user.
- A non-technical user can answer "Can OR3 see my whole computer?" within five seconds.
- Approval decisions are understandable without knowing what an allowlist, scope, matcher, or runtime profile is.
- Every warning has a one-click or one-command next step where safe.
- Advanced users can still reach the full power of the existing system without hidden magic.
- The same safety state is described consistently in setup, settings, status, doctor, capabilities, and approval prompts.
