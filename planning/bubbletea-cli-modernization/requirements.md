# Requirements

## Overview

Port the interactive CLI experience in `or3-intern` to a modern terminal UI built on Bubble Tea while fixing the concrete CLI correctness and security issues already identified in the current command surface.

Scope assumptions:
- Keep `or3-intern` as a Go CLI-first application.
- Preserve existing non-interactive command flags and stdout/stderr behavior for scripting and automation.
- Focus Bubble Tea on interactive setup, inspection, approval, pairing, and admin workflows rather than HTTP service internals or channel runtime loops.
- Use the Bubble Tea ecosystem (`bubbletea`, `bubbles`, `lipgloss`) for model/update/view flow, interactive controls, and styling.

## Requirements

1. **Interactive CLI flows must move from line prompts to Bubble Tea screens**
   - The commands with interactive user flows (`configure`, `init`, and any approval/pairing/admin flows that currently rely on repeated flag-less prompts) must use a Bubble Tea-based interface.
   - Acceptance criteria:
     - `or3-intern configure` launches a Bubble Tea TUI with keyboard navigation, selection, and clear affordances instead of numeric menus and `y/n` prompts.
     - `or3-intern init` reuses the same Bubble Tea setup flow, scoped to the original first-run sections.
     - The TUI supports arrow keys and Enter as the primary navigation path, with visible key hints.

2. **The Bubble Tea UI must support modern controls and styling**
   - The TUI must provide toggles, lists, grouped sections, focus indicators, status badges, and color styling appropriate for modern terminals.
   - Acceptance criteria:
     - Boolean fields render as toggle-like controls instead of requiring text entry.
     - Section pickers render as interactive lists or menus rather than typed numbers.
     - Summary and review screens use consistent colors, borders, spacing, and emphasis via `lipgloss`.
     - The UI adapts to terminal resize events without broken layout.

3. **Existing scripted CLI behavior must remain backward compatible**
   - Current non-interactive flags, positional arguments, and machine-readable command outputs must continue to work.
   - Acceptance criteria:
     - `--help`, `--config`, `--section`, and all existing non-interactive admin command syntaxes continue to function.
     - Commands used in docs and tests for scripting (`config-path`, `doctor --strict`, `capabilities --json`, `secrets`, `approvals`, `devices`, `pairing`, `skills`, `service`) preserve non-TUI behavior unless explicitly entered in interactive mode.
     - Interactive behavior must only activate when the command is intended to be interactive and stdin/stdout are attached to a TTY.

4. **Utility and inspection commands must avoid unnecessary runtime bootstrap**
   - Read-only or utility CLI commands must not require the full runtime setup path when it is not needed.
   - Acceptance criteria:
     - `or3-intern version` succeeds without loading config, opening SQLite, or bootstrapping runtime files.
     - `or3-intern capabilities` runs from config + approval metadata only and does not connect MCP servers, start cron, or initialize full runtime tooling.
     - Tests prove those commands can run without runtime side effects.

5. **Configure and init flows must not disclose secrets on screen**
   - Interactive setup must never print saved secrets, tokens, or passwords as visible prompt defaults.
   - Acceptance criteria:
     - Provider API key, Slack/Discord/Telegram/WhatsApp tokens, email passwords, and service secret are never rendered in cleartext in the TUI.
     - Existing secret values can be preserved without being shown.
     - The UI clearly distinguishes between “keep existing”, “replace”, and “clear” for secret-bearing fields.

6. **Configure and init flows must preserve existing auth values unless the user changes them**
   - Declining to re-enter a secret must not erase an existing configured value.
   - Acceptance criteria:
     - If a config already has a provider API key or secret reference, choosing to keep the current value leaves it intact.
     - Re-saving unrelated sections does not blank secret-bearing config fields.
     - Regression tests cover both plain values and secret-ref-like strings in provider config.

7. **Admin command parsing must become stricter and safer**
   - Fixed-shape CLI commands must reject extra positional garbage rather than silently proceeding.
   - Acceptance criteria:
     - `approvals approve`, `approvals deny`, `devices` subcommands, `pairing exchange`, `secrets set`, `skills install`, and similar fixed-arity commands reject unexpected extra args with usage errors.
     - Tests cover exact-arity behavior on each touched command.

8. **Allowlist creation must reject wildcard-by-accident rules**
   - Approval allowlist creation must not permit empty exec/skill matchers that become match-all rules.
   - Acceptance criteria:
     - `approvals allowlist add --domain exec` without any matcher fields fails with a clear error.
     - Equivalent empty skill matcher submissions also fail.
     - Broker-level validation enforces this even if other call sites are added later.

9. **Secrets mutation must not partially succeed when strict audit rejects the operation**
   - CLI secret writes/deletes must not mutate state before a strict-audit failure is surfaced.
   - Acceptance criteria:
     - In strict audit mode, failed audit append does not leave mutated secret state behind.
     - Tests verify both set and delete behaviors.

10. **Service HTTP request validation must be stricter and safer**
    - The internal service surface that the CLI exposes/manages must reject malformed request bodies and bound request resource usage.
    - Acceptance criteria:
      - JSON decoders for service turn/subagent requests reject unknown fields and trailing JSON.
      - Service handlers enforce request size limits.
      - HTTP server configuration includes appropriate read/idle timeout coverage beyond header timeout.

11. **Pairing security must block unauthenticated trusted auto-approval**
    - The pairing HTTP routes and underlying broker behavior must not allow anonymous trusted-mode token minting.
    - Acceptance criteria:
      - Unauthenticated pairing creation in trusted mode results in pending approval, not auto-approved exchangeable requests.
      - Exchange fails until an authorized approver resolves the request.
      - Existing authenticated operator flows continue to work.

12. **The Bubble Tea implementation must remain low-complexity and repo-aligned**
    - The TUI layer must fit inside the current CLI package structure and avoid introducing a second application architecture.
    - Acceptance criteria:
      - Interactive TUI code lives under the existing CLI package area (or a small CLI-focused internal package) without introducing a web frontend or daemon dependency.
      - Shared config mutation logic is centralized so text-mode and TUI-mode behavior cannot drift.
      - The design uses Bubble Tea’s model/update/view loop and Bubbles components instead of a bespoke terminal framework.

## Non-functional constraints

- **Deterministic behavior:** Interactive updates must produce predictable config mutations and preserve stable ordering of summaries, section lists, and saved values.
- **Low memory usage:** The TUI must use lightweight Bubble Tea/Bubbles models and avoid loading large history or runtime state just to render setup/admin screens.
- **Bounded loops/output/history:** The TUI must render bounded content per screen, paginate long lists where needed, and avoid dumping large secrets/config blobs to the terminal.
- **SQLite safety and migration compatibility:** No schema changes should be introduced unless required for one of the reviewed bugs; if any are needed, they must preserve existing data and single-process behavior.
- **Secure handling of files, network access, and secrets:** No secret values should be echoed in the UI, read-only commands should avoid outbound network work, and service hardening must reject malformed or oversized requests safely.
