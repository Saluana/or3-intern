# OR3 Codebase Audit: Gap Analysis & UX Roadmap

**Date:** 2026-04-28
**Auditor:** Susan Q
**Codebases:** `or3-intern` (Go backend) + `or3-app` (Nuxt 4 + Capacitor mobile frontend)
**Goal:** Identify every `or3-intern` feature that still needs frontend exposure in `or3-app`, with actionable UX design for non-technical users.

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Current State: What's Already in the App](#2-current-state-whats-already-in-the-app)
3. [The Gap List: Missing Features](#3-the-gap-list-missing-features)
4. [Grandma-Simple UX Principles](#4-grandma-simple-ux-principles)
5. [Implementation Priority](#5-implementation-priority)
6. [Appendix: Verified API Surface](#6-appendix-verified-api-surface)

---

## 1. Executive Summary

`or3-app` is a solid mobile frontend for `or3-intern` with well-built chat, approvals, settings editing, computer status, memory tools, and pairing flows. However, a significant number of `or3-intern`'s richest features are either completely absent from the app or only partially surfaced behind technical labels.

The biggest gaps cluster around **automation management** (cron, triggers, heartbeat), **advanced computer access** (file browser, terminal), **security tools** (secrets, skills trust, device rotation), and **diagnostics** (doctor, audit log, runtime stats). Some of these gaps are frontend-only (the API already exists), while others require backend REST extensions to be added before the app can consume them.

The app was built with good UX instincts already: pairing is wizard-driven, approvals show plain language, settings are grouped by category, and dangerous actions carry danger callouts. The remaining work is about extending those same patterns to the missing features.

---

## 2. Current State: What's Already in the App

The `or3-app` currently exposes these `or3-intern` capabilities:

| Feature | Page/Composable | Status |
|---------|----------------|--------|
| Chat with AI | `/` (home) via `useChatSessions`, `useAssistantStream` | **Complete** |
| Background agents | `/agents` via `useJobs` | **Complete** |
| Pairing / device trust | `usePairing`, `usePasskeys`, Settings > pairing | **Complete** |
| Approvals | `/approvals` via `useApprovals` | **Complete** |
| Computer health/status | `/computer` via `useComputerStatus` | **Complete** |
| Settings editor | `/settings/:section` via `useConfigure` | **Complete** |
| Memory embeddings | `/memory` via `useMemoryTrust` | **Partial** |
| Audit verify | `/memory` (hidden) | **Partial** |
| Scope tools | `/memory` (hidden under Advanced) | **Partial** |
| File browser | `/computer/files` via `useComputerFiles` | **Stub (needs backend)** |
| Terminal | `/computer/terminal` via `useTerminalSession` | **Stub (needs backend)** |
| Quick add shortcuts | `/add` | **Complete** |

Notable UX wins already in place:
- **Pairing flow** uses passkeys with plain-language titles like "Link This Phone"
- **Approvals** show human-readable domain names with Yes/No/Allow-Always buttons
- **Settings** group by category with descriptions like "Edit this category safely from your phone"
- **Danger callouts** appear before risky actions: "This is your real computer. Anything you type runs on your actual computer."
- **Fallback data** when APIs are unavailable: file browser shows demo data with an explanatory message
- **Helpful empty states**: "No editable options were found for this category."

---

## 3. The Gap List: Missing Features

### Category A: API Already Exists (Frontend Only)

These features have backend REST support but no dedicated app UI.

#### 3.1 Skills Manager
**Backend status:** Full API via `/internal/v1/settings` (skills section under configure). CLI command `or3-intern skills` supports list, inspect, install, update, check, remove.
**App status:** Skills only appear as a text settings section. No browse, install, or trust management.
**What a grandma needs:** A way to see what her AI assistant "knows how to do" and turn skills on/off without editing raw config.
**UX recommendation:**
- Add a "Skills" page (or fold into Settings as a discoverable sub-page)
- Show skills as cards with icons, friendly names, descriptions
- Each card has a toggle: "Let my assistant use this"
- Add "Install from ClawHub" button that opens a searchable marketplace
- Show trust status with simple labels: "Trusted" / "Needs Review" / "Quarantined"
- Never show raw file paths. Show "Where it's from" as a URL or store name.

#### 3.2 Device Manager
**Backend status:** API has `GET /internal/v1/devices`, `POST /devices/{id}/revoke`, `POST /devices/{id}/rotate`.
**App status:** Pairing exists but no device list, revocation, or token rotation.
**What a grandma needs:** See what phones/computers are connected to her AI. Remove ones she doesn't recognize.
**UX recommendation:**
- Add "Connected Devices" under Settings (or in Computer page)
- List devices with friendly names (derived from device model) + last seen time
- Show "This Phone" badge on the current device
- Each device gets a "Disconnect" button with confirmation: "This will sign out "Brendon's iPad." Are you sure?"
- Add "Rotate Security Code" button per device with explanation: "Generate a new secret code for this device."

#### 3.3 Secrets Manager
**Backend status:** CLI command `or3-intern secrets` supports set, delete, list.
**App status:** Completely absent.
**What a grandma needs:** A safe place to store passwords and API keys her AI needs to do its job.
**UX recommendation:**
- Add "Passwords & Keys" under Settings
- Simple list view: name + hidden value + service icon
- Add new: "What service is this for?" (dropdown) -> "Paste the key here" (password field)
- Never show full values. Show "••••••••sk_live_...1234" only.
- Allow deleting with confirmation.
- Explain: "These are like saved passwords in your browser. Only your AI assistant can see them."

#### 3.4 Audit Log Browser
**Backend status:** Audit chain is stored in SQLite. CLI has `verify` and `audit` commands.
**App status:** Only a "Verify" button under Memory > Advanced. No browsing.
**What a grandma needs:** Know what her assistant did and when. Be able to check "did my AI send that email?"
**UX recommendation:**
- Add "Activity Log" as a top-level page or under Computer
- Show chronological feed: "Sent email to Bob", "Ran backup to Google Drive", "Denied file deletion"
- Filter by: "My actions" / "AI actions" / "Errors only"
- Tapping an entry shows full details in a bottom sheet
- Simple verification badge: "Verified" with a checkmark icon

---

### Category B: Needs Backend Extension First

These features exist in `or3-intern` but the service API does not expose REST endpoints for them yet.

#### 3.5 File Browser
**Backend status:** `or3-intern` has `tools/fileaccess.go` with file read/write. The CLI has `terminal` command. But the service API has no file REST endpoints.
**App status:** `useComputerFiles` tries `/internal/v1/files/*` and falls back to demo data.
**What a grandma needs:** Browse and open her computer files from her phone.
**Backend needed:** Add the file API extension the design docs already plan:
- `GET /internal/v1/files/roots`
- `GET /internal/v1/files/list?root_id=...&path=...`
- `POST /internal/v1/files/upload`
- `GET /internal/v1/files/search?q=...`
**UX recommendation (already started in app):**
- The app has a good "Pick an area" + "Folder to start in" pattern
- Keep the "Refresh areas" button
- Show files with icons (pdf, image, text)
- Add "Send to chat" action on files: "Talk about this file"
- Add "Download to phone" for images/documents
- Keep the danger callout: "This is your real computer's files."

#### 3.6 Terminal Sessions
**Backend status:** `or3-intern` has `terminal` tool and CLI TUI. No service REST terminal endpoints.
**App status:** `useTerminalSession` tries `/internal/v1/terminal/*` and expects the endpoints the design docs describe.
**What a grandma needs:** Almost nothing. Terminals are inherently technical. This should be behind an "Advanced" wall.
**Backend needed:** Add terminal REST API:
- `POST /internal/v1/terminal/sessions`
- `GET /internal/v1/terminal/sessions/{id}/stream`
- `POST /internal/v1/terminal/sessions/{id}/input`
- `POST /internal/v1/terminal/sessions/{id}/close`
**UX recommendation (already started in app):**
- Move Terminal under Settings > Advanced, not the main Computer page
- Keep the existing danger callout prominently displayed
- Add a "Learn what you can do" expandable help section
- Consider adding quick-safe commands: "Show disk space", "List running programs"
- Never auto-suggest commands. Make the user type everything.

#### 3.7 Cron & Automation (Triggers)
**Backend status:** `or3-intern` has `cron` package, filewatch triggers, heartbeat, webhook triggers. CLI has `or3-intern init` with automation choices. No REST schedule management.
**App status:** Cron appears only as a settings section "automation" if configured.
**What a grandma needs:** Set up simple repeating tasks without editing cron syntax.
**Backend needed:** REST endpoints for schedule CRUD:
- `GET /internal/v1/schedule` - list recurring tasks
- `POST /internal/v1/schedule` - create
- `DELETE /internal/v1/schedule/{id}` - delete
- `POST /internal/v1/schedule/{id}/toggle` - enable/disable
**UX recommendation:**
- Add "Recurring Tasks" under Settings (or as a dedicated page)
- Show tasks as cards: "Check email every morning at 7am" / "Back up every night at 3am"
- Create flow as a wizard:
  1. "What should happen?" (dropdown: Check Email, Run Backup, Scan Files, etc.)
  2. "How often?" (radio: Every day, Every week, Just once)
  3. "What time?" (time picker)
  4. "Give it a name" (text field with smart default)
- Never expose raw cron syntax. Map "Every day at 7am" to `0 7 * * *` internally.
- Show next run time in human terms: "Next: Tomorrow at 7:00 AM"

#### 3.8 Heartbeat Service Controls
**Backend status:** `or3-intern` has heartbeat package with configurable checks.
**App status:** Not exposed.
**What a grandma needs:** Know if her helper is checking on things regularly.
**Backend needed:** REST endpoints for heartbeat status and configuration.
**UX recommendation:**
- Add "Check-Ins" page showing: "Last check: 15 minutes ago. Everything looks good."
- List what gets checked: Email, Calendar, Health
- Toggle each check on/off
- Show history: "7 checks today, 0 issues found"

#### 3.9 MCP Server Manager
**Backend status:** `or3-intern` has full MCP integration over stdio, SSE, streamable HTTP. `mcp` package exists.
**App status:** Not exposed.
**What a grandma needs:** Add new abilities to her AI by installing "plugins."
**Backend needed:** REST endpoints for MCP server listing, adding, removing.
**UX recommendation:**
- Call it "Add-ons" or "Connections" not "MCP Servers"
- List installed add-ons as cards with logos
- "Add connection" opens a simple form: "What service?" (dropdown/search) -> "Paste the link" -> "Test & Save"
- Show connection status: "Connected" / "Can't reach"
- Examples to show: Google Drive, Calendar, Home Assistant

#### 3.10 Doctor / Diagnostics Dashboard
**Backend status:** `or3-intern` has comprehensive `doctor` command with readiness probes.
**App status:** Computer page shows basic health but no diagnostic report.
**Backend needed:** REST endpoint for doctor report.
**UX recommendation:**
- Add "Health Check" button on Computer page
- Show traffic-light results: **Green** (All good), **Yellow** (Worth checking), **Red** (Needs attention)
- Each finding gets a card:
  - "Your computer can connect to the AI"
  - "Backups are running on schedule"
  - "Warning: Your disk is getting full (85%)"
- One-tap "Run all checks" button
- Show last check time

---

### Category C: Deep System Features (Advanced Users Only)

These should live behind an "Advanced" toggle in Settings.

#### 3.11 Runtime Profiles & Security Hardening
**Backend status:** `or3-intern` has ActiveProfile, SkillPolicy, capability levels, hardening phases, network controls.
**App status:** Security settings are exposed as configure fields but lack a unified security dashboard.
**What a grandma needs:** A single "How cautious should my AI be?" slider.
**UX recommendation:**
- Replace scattered security toggles with a single "Trust Level" slider:
  - **Cautious** (ask before everything, no internet, no file changes)
  - **Balanced** (ask before risky things, internet OK for research)
  - **Helpful** (ask less, can write files, can use the web)
  - Each level shows a plain description of what it means.
- Advanced toggle reveals: tool allowlists, network domains, writable paths
- Never use terms like "ActiveProfile", "CapabilityLevel", "WAL mode"

#### 3.12 Context & Session Scope Manager
**Backend status:** Scope linking, consolidate, memory model with docs.
**App status:** Hidden under Memory > Advanced.
**What a grandma needs:** Understand that her AI remembers conversations differently.
**UX recommendation:**
- Keep it under Memory > Advanced
- Show sessions as a list: "Work chat (started yesterday)" / "Personal notes (started 3 days ago)"
- "Link to main memory" button with explanation: "Let your main assistant remember what we talked about in this chat."
- "Forget this chat" with confirmation

#### 3.13 Configuration Import / Export
**Backend status:** `or3-intern` supports `--export` flag.
**App status:** Not exposed.
**UX recommendation:**
- Add under Settings > Advanced: "Save my settings" and "Load my settings"
- Export produces a shareable file
- Import warns: "This will replace your current settings. Continue?"

#### 3.14 Migration Tools
**Backend status:** `or3-intern migrate-jsonl` exists.
**App status:** Not exposed.
**UX recommendation:**
- Keep CLI-only. No mobile UI needed.

---

## 4. Grandma-Simple UX Principles

Based on the patterns already working well in `or3-app`, these are the design rules for every new feature:

### 4.1 Progressive Disclosure
- Show the simplest options first
- Hide advanced controls behind an "Advanced" toggle that stays off by default
- Example: Security settings show a "Trust Level" slider first. Tapping "Advanced" reveals individual tool permissions.

### 4.2 Plain Language Over Technical Terms

| Don't Say | Say Instead |
|-----------|------------|
| MCP Server | Add-on or Connection |
| Cron schedule | Recurring task |
| Skill quarantine | This add-on needs review |
| Capability level | How much freedom my AI has |
| Scope linking | Remember this conversation |
| Subagent | Background helper |
| Token rotation | Refresh security code |
| Embedding rebuild | Refresh my AI's memory |

### 4.3 One Obvious Action Per Screen
- Each page should have one primary action clearly visible
- Secondary actions are smaller/ghost buttons
- Example: Approvals page has big "Yes" and "No" buttons. "Always allow" is smaller.

### 4.4 Safety Through Confirmation
- Always explain *why* something matters before the user decides
- Dangerous actions get a double-check:
  - "This will disconnect John's iPad from your account. He won't be able to use the app anymore. Continue?"
  - "This will let your AI delete files on your computer. Are you sure?"

### 4.5 Status Visibility
- Every page should answer: "Is it working?" and "When did it last work?"
- Use green/yellow/red status indicators
- Show timestamps in friendly terms: "15 minutes ago" not "2026-04-28T08:26:00Z"

### 4.6 Smart Defaults
- The app should work well without configuration
- Only ask the user to change something when it's needed
- Example: Heartbeat checks should default to "on" with sensible intervals. Don't make the user set up a schedule.

### 4.7 Error Messages That Help
- Don't say "503 capability_unavailable"
- Say "The terminal feature isn't enabled on this computer. Turn on guarded shell access in settings."

### 4.8 Contextual Help
- Every complex page gets a "?" button that opens a simple explanation
- Use the existing `DangerCallout` and `SurfaceCard` patterns
- Example: Skills page has a header card explaining "Skills are like apps for your AI assistant."

---

## 5. Implementation Priority

### Phase 1: Complete What's Already Started
These are stubs or partial implementations that need finishing.

1. **File Browser Backend API** (or3-intern) - The app UI exists but has no real backend
2. **Terminal Backend API** (or3-intern) - Same situation
3. **Device Manager** (or3-app) - API already exists, just needs UI
4. **Skills Manager** (or3-app) - API partially exists via settings, needs dedicated UI

### Phase 2: Administrative Tools
Features that help the user manage their system.

5. **Secrets Manager** (or3-app) - API needs exposing or3-intern secrets as REST
6. **Audit Log Browser** (or3-app) - API exists but needs friendly UI
7. **Doctor / Health Check** (both) - Backend needs REST endpoint, frontend needs dashboard

### Phase 3: Automation
Features that make the AI work in the background.

8. **Recurring Tasks** (both) - Backend REST schedule manager needed
9. **Heartbeat Controls** (both) - Backend REST endpoint needed
10. **MCP Add-ons** (both) - Backend REST endpoint needed

### Phase 4: Power User Features
Hidden by default, only for users who need them.

11. **Security Dashboard** (or3-app) - Unify existing configure fields
12. **Context Manager** (or3-app) - Already partially there, just needs polish
13. **Config Import/Export** (or3-app) - Nice-to-have

---

## 6. Appendix: Verified API Surface

### 6.1 or3-intern Service API (Currently Exposed)

| Method | Path | Used By App? | Status |
|--------|------|-------------|--------|
| POST | `/internal/v1/turns` | Yes (chat) | Active |
| POST | `/internal/v1/subagents` | Yes (agents) | Active |
| GET | `/internal/v1/jobs/{id}` | Yes | Active |
| GET | `/internal/v1/jobs/{id}/stream` | Yes | Active |
| POST | `/internal/v1/jobs/{id}/abort` | Yes | Active |
| POST | `/internal/v1/pairing/requests` | Yes | Active |
| GET | `/internal/v1/pairing/requests` | Yes | Active |
| POST | `/internal/v1/pairing/requests/{id}/approve` | Yes | Active |
| POST | `/internal/v1/pairing/requests/{id}/deny` | Yes | Active |
| POST | `/internal/v1/pairing/exchange` | Yes | Active |
| GET | `/internal/v1/devices` | **No** | Exposed but unused |
| POST | `/internal/v1/devices/{id}/revoke` | **No** | Exposed but unused |
| POST | `/internal/v1/devices/{id}/rotate` | **No** | Exposed but unused |
| GET | `/internal/v1/approvals` | Yes | Active |
| GET | `/internal/v1/approvals/{id}` | Yes | Active |
| POST | `/internal/v1/approvals/{id}/approve` | Yes | Active |
| POST | `/internal/v1/approvals/{id}/deny` | Yes | Active |
| POST | `/internal/v1/approvals/{id}/cancel` | Yes | Active |
| POST | `/internal/v1/approvals/expire` | Yes | Active |
| GET | `/internal/v1/approvals/allowlists` | Yes | Active |
| POST | `/internal/v1/approvals/allowlists` | Yes | Active |
| POST | `/internal/v1/approvals/allowlists/{id}/remove` | Yes | Active |
| GET | `/internal/v1/health` | Yes | Active |
| GET | `/internal/v1/readiness` | Yes | Active |

### 6.2 or3-intern CLI Commands (No App Equivalent)

| Command | Purpose | Priority |
|---------|---------|----------|
| `setup` | First-time wizard | Low (CLI only) |
| `doctor` | System diagnostics | **High** |
| `skills` | Browse/manage skills | **High** |
| `secrets` | Credential storage | **High** |
| `capabilities` | Runtime capability report | Medium |
| `audit` | Audit log interaction | **High** |
| `devices` | Device list management | **High** |
| `scope` | Session scope management | Medium |
| `cron` | Schedule management | **High** |
| `migrate-jsonl` | Data migration | Low (CLI only) |

### 6.3 or3-intern Internal Packages (Backend Only)

These packages exist in the backend but have no REST exposure:

| Package | Contains | Needs REST? |
|---------|----------|-------------|
| `agent` | Core runtime | Already exposed via turns/subagents |
| `approval` | Approval broker | Already exposed |
| `auth` | Authentication service | Already exposed |
| `bus` | Event bus | Internal only |
| `channels` | Channel integrations | Needs channel config REST |
| `clawhub` | Skill marketplace | Needs skills REST |
| `config` | Configuration | Already exposed via configure |
| `controlplane` | Job/pairing registry | Already exposed |
| `cron` | Schedule engine | **Needs REST** |
| `db` | Persistence | Internal only |
| `doctor` | Diagnostics | **Needs REST** |
| `heartbeat` | Heartbeat service | **Needs REST** |
| `mcp` | MCP integrations | **Needs REST** |
| `memory` | Memory/embeddings | Partially exposed |
| `providers` | AI providers | Internal only |
| `safetymode` | Safety controls | Exposed via configure |
| `scope` | Session scopes | Partially exposed |
| `security` | Network security | Internal only |
| `skills` | Skill management | **Needs REST** |
| `tools` | Tool registry | Internal only |
| `triggers` | Filewatch, webhooks | **Needs REST** |
| `uxcopy` | UI text | Internal only |

---

## Summary Count

| Category | Count |
|----------|-------|
| Features already complete in app | 8 |
| Features needing only frontend work | 4 (skills, devices, secrets, audit log) |
| Features needing backend REST first | 7 (files, terminal, cron, heartbeat, MCP, doctor, channels) |
| Features that are CLI-only / power user | 3 (migrate, setup advanced, config export) |
| **Total gaps to close** | **14** |

---

*This audit was compiled from direct source code inspection of both repositories, design documents in `/planning/`, and the service API reference. The `or3-app` codebase is well-structured with strong UX patterns already in place. The remaining work is primarily about extending those same composables and page patterns to cover the unexposed surface area of `or3-intern`.*
