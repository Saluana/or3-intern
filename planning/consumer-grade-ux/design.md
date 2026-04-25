# Overview

The design introduces a consumer UX layer over the existing `or3-intern` backend. It does not replace `configure`, `doctor`, `capabilities`, approvals, pairing, devices, runtime profiles, hardening, audit, or service APIs. Instead, it adds presenters and simple-mode commands that translate those primitives into user intent.

Core design principle:

> Keep the backend precise and powerful. Make the frontend plain, visual, reversible, and safe by default.

# Architecture

## Layered model

Add a thin product-facing layer between CLI/TUI commands and internal config/security packages.

Recommended packages:

- `internal/uxcopy`: plain-language labels, descriptions, risk copy, and error translations.
- `internal/uxstate`: aggregate view models for setup, settings, status, access dashboard, approvals, and connected devices.
- `internal/safetymode`: deterministic mappings from scenario and safety choices to config patches.
- `internal/usertask`: command handlers for simple-mode flows that orchestrate existing packages.

The existing packages remain authoritative:

- `internal/config` owns persisted schema and validation.
- `internal/doctor` owns readiness findings and safe fixes.
- `internal/approval` owns approval decisions, tokens, allowlists, and pairing.
- `cmd/or3-intern/configure*.go` owns advanced/manual configuration.
- Existing service endpoints remain the machine-facing API.

## Command surface

Introduce simple command aliases while keeping existing commands intact.

Simple mode commands:

- `setup`: scenario-based first-run setup.
- `settings`: task-based settings UI.
- `status`: friendly system check and access summary.
- `connect-device`: device pairing flow.
- `chat`: existing interactive chat.
- `update`: initially a placeholder/helpful instruction if no updater exists.
- `help`: simple help by default.

Advanced command access:

- `help --advanced` shows the current full command catalog.
- Existing commands such as `doctor`, `capabilities`, `approvals`, `devices`, `pairing`, `audit`, `secrets`, `embeddings`, and `scope` still work.
- `configure --advanced` or existing `configure` can remain the schema-section editor, while `settings` becomes the normal-user entry point.

Compatibility rule:

- Do not remove or rename current commands during this work. Add friendlier routes and adjust help visibility first.

# User-facing flows

## Scenario-based setup

Flow:

1. Welcome: explain OR3 needs a provider, a folder, and a safety level.
2. AI Provider: choose common provider or custom endpoint.
3. Workspace Folder: default to current directory with clear boundary explanation.
4. Where are you using OR3?
   - Just me, on this computer.
   - Me and my phone.
   - A small private server.
   - Public/hosted service.
   - Advanced/manual setup.
5. Safety Mode: Relaxed, Balanced, Locked Down, with recommendation based on scenario.
6. Review: show what OR3 can access and what it will ask about.
7. Save, run post-save doctor check, offer Start chat.

Scenario mapping:

| User scenario | Default safety | Runtime/profile intent | Notable settings |
| --- | --- | --- | --- |
| Just me, on this computer | Balanced | single-user local posture | workspace-only files, audit on, ask for risky exec |
| Me and my phone | Balanced | single-user hardened with pairing | approvals enabled, pairing ask, service protected |
| Small private server | Locked Down | hosted-service intent | service auth, trusted devices, audit strict, network policy |
| Public/hosted service | Locked Down | hosted-no-exec or sandbox-only intent | no direct exec by default, strict approvals, restricted network |
| Advanced/manual setup | Custom | user-selected profile | open existing configure sections |

Implementation seam:

- Keep `initDefaults` as the base.
- Add a scenario/safety patch function that mutates `config.Config` before save.
- Reuse `runConfigureWithTUI` form components where practical, but avoid exposing all sections in the first-run path.

## Safety modes

Add a stable safety-mode abstraction that can be inferred from config and applied to config.

Types:

```go
type SafetyMode string

const (
    SafetyRelaxed SafetyMode = "relaxed"
    SafetyBalanced SafetyMode = "balanced"
    SafetyLockedDown SafetyMode = "locked-down"
    SafetyCustom SafetyMode = "custom"
)
```

Each mode needs:

- display name,
- one-line description,
- detailed consequences,
- config patch,
- drift detector.

Mode intent:

| Safety mode | User meaning | Internal posture |
| --- | --- | --- |
| Relaxed | Good for local testing. Fewer prompts. | local-only, workspace file access, minimal public service, fewer approval gates |
| Balanced | Recommended. Ask before risky actions. | workspace restriction, audit on, approvals for exec/skills/secrets/message send where supported |
| Locked Down | Best for servers/shared devices. Blocks dangerous actions by default. | no direct host exec unless sandboxed, strict audit, network policy, protected service, deny/ask by default |

Drift behavior:

- If current config exactly matches a mode patch, show that mode.
- If it mostly matches but advanced fields differ, show `Custom based on Balanced`.
- If it violates the chosen mode, `doctor`/`status` shows specific repair actions.

## Settings UI

Settings home sections:

- AI Provider: provider endpoint, model, key status, embedding model.
- Workspace Folder: folder path, only-this-folder toggle, indexed docs summary.
- Connected Devices: simple devices list and connect/disconnect actions.
- Safety Level: safety mode, approval behavior, audit status.
- Channels: Telegram/Slack/Discord/WhatsApp/Email as optional app connections.
- Tools: command execution, web search, skills, MCP as progressive advanced tools.
- Memory: saved notes, document indexing, clear/export actions.
- Advanced: current configure sections and export config.

Presenter rule:

- Every field shown in simple settings must be generated from a central translation table, not copied ad hoc into each screen.

Example translation table entries:

| Internal concept | User label | User hint |
| --- | --- | --- |
| `tools.restrictToWorkspace` | Only let OR3 access this folder | Prevents OR3 from reading or writing outside your chosen workspace. |
| `hardening.guardedTools` | Ask before risky actions | OR3 pauses before actions that can change files, run code, or contact services. |
| `security.audit.enabled` | Keep a safety log | Saves a tamper-evident record of important actions. |
| `security.approvals.*.mode` | When should OR3 ask? | Controls prompts for commands, skills, secrets, messages, and pairing. |
| `service.enabled` | Allow other devices/apps to connect | Lets phones or companion apps connect when protected by a key. |
| `runtimeProfile` | Safety posture | Advanced implementation detail behind Safety Mode. |

## Human approval prompts

Presenter input:

- `db.ApprovalRequestRecord`
- decoded subject JSON,
- allowlist match metadata if relevant,
- optional agent-provided reason when available.

Presenter output:

```go
type ApprovalPromptView struct {
    Title string
    ActionSummary string
    Why string
    RiskLabel string
    RiskExplanation string
    Choices []ApprovalChoiceView
    AdvancedDetails []KeyValue
}
```

Risk examples:

- `npm install`: Medium, downloads and runs package install scripts.
- `rm`, shell script, privileged tool, unknown binary: High.
- known read-only command in workspace: Low or Medium depending on policy.
- outbound message send: Medium, may share information externally.
- secret access: High, may expose private credentials.

Choice labels:

- Allow once.
- Always allow this kind of action in this folder.
- Deny.

Advanced details:

- Request ID, subject hash, policy mode, matcher JSON, host ID, agent/session IDs.

## Connect a device

Simple flow:

1. Check service and approval prerequisites.
2. If missing, offer automatic setup: enable protected service, generate secret/key, enable pairing approvals.
3. Create pairing request/code.
4. Show code and expiration in a large friendly layout.
5. Let user select device access: Chat only, Chat and workspace files, Admin device.
6. Confirm connected device and show how to disconnect later.

Role mapping:

| User role | Internal role |
| --- | --- |
| Chat only | `viewer` or restricted `operator` based on API capability model |
| Chat and workspace files | `operator` |
| Admin device | `admin` |

Device list view model:

```go
type DeviceView struct {
    Name string
    UserRoleLabel string
    LastUsed string
    Status string
    Actions []string
    AdvancedDetails []KeyValue
}
```

## Status and Fix Problems

`status` should aggregate:

- selected safety mode or custom posture,
- doctor summary,
- access dashboard,
- connected devices count,
- pending approvals count,
- provider readiness,
- service/channel exposure.

Friendly finding view:

```go
type ProblemView struct {
    Title string
    WhyItMatters string
    RecommendedAction string
    ActionKind string
    AdvancedID string
    Severity string
}
```

Mapping source:

- `internal/doctor.Finding.ID` remains the stable key.
- `internal/uxcopy` maps IDs to titles and explanations.
- Unknown IDs fall back to existing summary/detail under advanced wording.

Example mappings:

| Finding ID / raw error | Friendly title | Primary action |
| --- | --- | --- |
| `security.audit_disabled` | Safety log is off | Turn on safety log |
| `approval broker unavailable` | Approvals are not set up yet | Create the local approval key |
| `privileged-exec.sandbox_disabled` | Risky tools are not isolated | Use safer command settings |
| `service.secret_missing` | Connections are not protected yet | Create a connection password |
| `runtime unavailable` | The assistant engine did not start | Check provider and local runtime settings |

## Access dashboard

Dashboard sections:

- Files: only this folder / broader access / missing folder.
- Commands: blocked / asks first / allowed / sandboxed.
- Internet: web search, proxy, private network blocking, MCP/network policy.
- Connected apps: channel status and inbound policy.
- Connected devices: protected/unprotected service, paired devices.
- Memory: saved chat notes, doc indexing, clear/export actions.
- Activity log: on/off/strict/verify status.

Data sources:

- `config.Config`
- `doctor.Report`
- capabilities report output or internal equivalent
- approval broker/device store
- audit config/status

Status colors:

- Green: bounded/protected/on.
- Yellow: usable but asks/needs attention/custom.
- Red: exposed, unprotected, broken, or blocked.
- Gray: off/not configured.

## Error translation layer

Add a structured translator:

```go
type UserError struct {
    Title string
    WhatHappened string
    Fix string
    Command string
    Advanced string
}
```

Translator inputs:

- sentinel errors,
- string reasons from approval decisions,
- doctor finding IDs,
- config validation errors,
- startup mode failures.

Rules:

- Never discard the raw error; attach it as advanced details.
- Simple-mode commands render `UserError`.
- Advanced/debug mode can render both user copy and raw details.
- Tests should snapshot common translations.

# Rollout plan

## Milestone 1: Shared UX copy and safety-mode engine

Deliverables:

- `internal/safetymode` with apply/infer/drift helpers.
- `internal/uxcopy` with labels for settings, doctor findings, approvals, devices, and errors.
- Tests for mode-to-config patches and copy mappings.

Why first:

- It creates a shared product language before UI work spreads strings across commands.

## Milestone 2: Scenario setup and simple help

Deliverables:

- `setup` command for first-run scenario flow.
- Simple root help by default, advanced help behind explicit option.
- README/getting-started updated around `setup`, `chat`, `status`, `settings`.
- Existing `configure` remains as advanced/manual setup.

Why second:

- It immediately improves new-user onboarding without requiring every downstream screen.

## Milestone 3: Safety status and Fix Problems

Deliverables:

- `status` command with friendly system check.
- Problem view model over `doctor.Report`.
- Individual fix actions for known automatic/interactive doctor fixes.
- Plain-language recovery for common startup/config errors.

Why third:

- It builds trust and turns current warnings into guided recovery.

## Milestone 4: Human approvals

Deliverables:

- Approval prompt presenter.
- Friendly `approvals` simple view or integrated prompt screen.
- Optional advanced details panel.
- Risk classifier for exec, skill execution, secret access, message send, and file transfer subjects.

Why fourth:

- Approvals are a daily interaction; making them understandable is a major UX multiplier.

## Milestone 5: Connect Device

Deliverables:

- `connect-device` flow.
- Friendly connected devices list and role editor.
- Automatic prerequisite repair for service secret, pairing key, and approval mode.
- Existing `devices` and `pairing` commands remain advanced tools.

Why fifth:

- It converts service auth and pairing into a normal app interaction.

## Milestone 6: Settings and Access dashboard

Deliverables:

- `settings` task-based TUI.
- Access dashboard powered by config/doctor/capabilities.
- Memory, channel, tools, and advanced panels.
- Export advanced config action.

Why sixth:

- Settings is larger and benefits from all previous presenters and copy systems.

## Milestone 7: Polish, measurement, and docs

Deliverables:

- First-run walkthrough tests.
- Golden-output tests for simple help/status/settings summaries.
- Documentation split: beginner guide, admin reference, security details.
- Optional terminal recording/manual QA script.
- UX metrics checklist for time-to-chat, safety comprehension, approval comprehension, and error recovery.

# Testing strategy

Use focused Go tests at each layer.

Unit tests:

- safety mode patch/inference/drift.
- user-copy mappings for config fields and doctor finding IDs.
- error translation table.
- approval risk classifier.
- device role mapper.

Command tests:

- `help` simple vs advanced output.
- `setup` non-interactive fallback and scenario config results.
- `status` renders friendly findings and hides internal IDs by default.
- `connect-device` prerequisite checks and role mapping.

TUI/view tests:

- Settings home includes only task-based sections.
- Approval prompt contains what/why/risk/choices.
- Access dashboard answers file, command, internet, devices, memory, and log status.
- Advanced details can be expanded without changing default output.

Integration validation:

- Run focused package tests after each milestone.
- Run the existing `Build Go workspace` task before handoff.
- Keep old command tests passing to prove compatibility.

# Risks and mitigations

- Risk: Friendly commands duplicate configure logic.
  - Mitigation: put mappings in `internal/safetymode` and `internal/uxstate`; commands only orchestrate.

- Risk: Safety modes oversimplify real config.
  - Mitigation: support `Custom based on ...` and show advanced drift details.

- Risk: Copy diverges between setup, status, doctor, and settings.
  - Mitigation: centralize labels and finding translations in `internal/uxcopy`.

- Risk: Hiding commands makes power users think features disappeared.
  - Mitigation: advertise `help --advanced` and preserve all current commands.

- Risk: Approval prompts lack enough reason context.
  - Mitigation: start with subject-derived reasons and add optional agent-supplied rationale in a later schema/service extension if needed.

- Risk: Individual doctor fixes require refactoring current broad fix flow.
  - Mitigation: first wrap existing automatic/interactive fixes, then split fix application by finding ID where needed.
