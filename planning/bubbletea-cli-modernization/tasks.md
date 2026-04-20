# Tasks

## 1. Add Bubble Tea foundations
- [ ] (Req: 1, 2, 12) Add Bubble Tea ecosystem dependencies to [go.mod](go.mod): `bubbletea`, `bubbles`, and `lipgloss` with versions compatible with the current Go toolchain.
- [ ] (Req: 1, 2, 12) Create a focused TUI implementation area under `cmd/or3-intern` or a small CLI-only internal package for Bubble Tea models, keymaps, styles, and screen routing.
- [ ] (Req: 2, 12) Define a shared style/theme module using Lip Gloss with adaptive colors, status badges, section cards, and bordered panels.
- [ ] (Req: 2, 12) Define keymaps/help text using Bubbles key/help support so the TUI shows modern navigation hints.

## 2. Split CLI command routing by execution mode
- [ ] (Req: 3, 4) Refactor [cmd/or3-intern/main.go](cmd/or3-intern/main.go) so `help`, `version`, and `config-path` return before config load and runtime bootstrap.
- [ ] (Req: 3, 4) Refactor [cmd/or3-intern/main.go](cmd/or3-intern/main.go) so `doctor` and `capabilities` run from config/security state only, without MCP connect, channel manager build, cron start, or runtime tool bootstrap.
- [ ] (Req: 3, 12) Add a small TTY detection helper and route interactive commands into Bubble Tea only when stdin/stdout are terminals.
- [ ] (Req: 3, 12) Preserve existing text-mode/flag behavior for non-interactive invocations and scripts.

## 3. Extract shared configure logic out of prompt code
- [ ] (Req: 1, 6, 12) Refactor [cmd/or3-intern/configure.go](cmd/or3-intern/configure.go) into pure section draft/application helpers so UI state is separate from config mutation logic.
- [ ] (Req: 1, 6, 12) Keep [cmd/or3-intern/init.go](cmd/or3-intern/init.go) as a thin first-run alias that preselects the provider/storage/workspace/web sections.
- [ ] (Req: 6, 12) Preserve current lenient repair loading behavior, but expose it via a structured warning state consumable by the TUI.
- [ ] (Req: 3) Update [cmd/or3-intern/help.go](cmd/or3-intern/help.go) and docs so interactive vs non-interactive behavior stays discoverable.

## 4. Build the Bubble Tea configure/init experience
- [ ] (Req: 1, 2) Implement a root Bubble Tea model for `configure` with section navigation, resize handling, save/apply state, and global quit/help behavior.
- [ ] (Req: 1, 2) Implement a section picker using Bubbles `list` with status badges and descriptions for provider, storage, workspace, web, channels, and service.
- [ ] (Req: 2) Implement boolean controls as toggle-style widgets instead of `y/n` prompts.
- [ ] (Req: 2) Implement review/apply and success screens with styled summaries and next-step actions.
- [ ] (Req: 2, 12) Use bounded viewports/tables for long summaries or access lists so the TUI remains responsive and low-noise.

## 5. Add per-section modern forms
- [ ] (Req: 1, 2, 6) Implement provider form state for API base, chat model, embed model, and secret-aware API key management.
- [ ] (Req: 1, 2) Implement storage form state for DB path and artifacts directory.
- [ ] (Req: 1, 2) Implement workspace form state for restriction toggle and workspace directory.
- [ ] (Req: 1, 2) Implement web form state for Brave key and proxy configuration.
- [ ] (Req: 1, 2) Implement channels as a nested Bubble Tea submenu so users can configure Telegram/Slack/Discord/WhatsApp/Email individually instead of walking every channel every time.
- [ ] (Req: 1, 2) Implement service form state for enable toggle, listen address, and shared secret handling.

## 6. Fix configure secret handling while migrating to TUI
- [ ] (Req: 5, 6) Replace secret-bearing prompt defaults with secret-aware field state that supports keep/replace/clear without displaying existing values.
- [ ] (Req: 5) Apply this to provider API key, Telegram/Slack/Discord/WhatsApp tokens, email passwords, and service secret in [cmd/or3-intern/configure.go](cmd/or3-intern/configure.go) and shared helpers.
- [ ] (Req: 6) Ensure declining to replace a configured secret preserves the current stored value rather than blanking it.
- [ ] (Req: 5, 6) Add regression tests in [cmd/or3-intern/configure_test.go](cmd/or3-intern/configure_test.go) for hidden secrets and preserved values.

## 7. Tighten admin command parsing and behavior
- [ ] (Req: 3, 7) Add a small exact-arity validation helper and apply it across fixed-shape subcommands in [cmd/or3-intern/approvals_cmd.go](cmd/or3-intern/approvals_cmd.go), [cmd/or3-intern/devices_cmd.go](cmd/or3-intern/devices_cmd.go), [cmd/or3-intern/pairing_cmd.go](cmd/or3-intern/pairing_cmd.go), [cmd/or3-intern/secrets_cmd.go](cmd/or3-intern/secrets_cmd.go), and [cmd/or3-intern/skills_cmd.go](cmd/or3-intern/skills_cmd.go).
- [ ] (Req: 7) Add focused regression tests rejecting extra positional args in the affected command test files.
- [ ] (Req: 1, 2, 3) Decide which human-operated admin flows get Bubble Tea entry screens first (recommended: approvals, pairing/devices) while keeping existing flag syntax intact.

## 8. Fix approval/allowlist safety issues
- [ ] (Req: 8) Add broker-side matcher validation in [internal/approval/broker.go](internal/approval/broker.go) so empty exec and skill allowlist matchers are rejected before persistence.
- [ ] (Req: 8) Mirror validation errors in [cmd/or3-intern/approvals_cmd.go](cmd/or3-intern/approvals_cmd.go) for better CLI UX.
- [ ] (Req: 8) Add regression tests for empty matcher rejection in both broker and CLI tests.

## 9. Fix secrets mutation strict-audit behavior
- [ ] (Req: 9) Add a shared helper in the security/secret-management path that performs secret mutation and audit append as one operation, or otherwise prevalidates strict audit before mutation.
- [ ] (Req: 9) Update [cmd/or3-intern/secrets_cmd.go](cmd/or3-intern/secrets_cmd.go) to use the safe helper.
- [ ] (Req: 9) Add tests proving strict-audit failure does not leave set/delete mutations behind.

## 10. Harden service request validation and pairing auth
- [ ] (Req: 10) Tighten [cmd/or3-intern/service_request.go](cmd/or3-intern/service_request.go) to reject unknown fields and trailing JSON.
- [ ] (Req: 10) Add request body size caps in [cmd/or3-intern/service.go](cmd/or3-intern/service.go) for pairing, turns, subagents, devices, and approvals endpoints as appropriate.
- [ ] (Req: 10) Add server-level `ReadTimeout` and `IdleTimeout` in [cmd/or3-intern/service.go](cmd/or3-intern/service.go).
- [ ] (Req: 11) Update [internal/approval/broker.go](internal/approval/broker.go) so unauthenticated trusted-mode pairing requests stay pending instead of auto-approved.
- [ ] (Req: 11) Add pairing/service regression tests covering unauthenticated create/exchange behavior.

## 11. Strengthen CLI and TUI tests
- [ ] (Req: 3, 4) Add a `main` command routing test slice proving `version` and `capabilities` avoid runtime bootstrap side effects.
- [ ] (Req: 1, 2, 12) Add Bubble Tea model tests that simulate key presses for configure/init navigation, toggles, save, back, and quit-confirmation flows.
- [ ] (Req: 2, 12) Add narrow snapshot-style tests for core screen rendering where stable visual structure matters.
- [ ] (Req: 10, 11) Add service hardening tests for unknown fields, trailing JSON, oversized bodies, and trusted pairing.
- [ ] (Req: 7, 8, 9) Extend current command tests for arity validation, empty matcher rejection, and strict-audit secret handling.

## 12. Documentation and rollout
- [ ] (Req: 1, 2, 3) Update [README.md](README.md), [docs/getting-started.md](docs/getting-started.md), and [docs/cli-reference.md](docs/cli-reference.md) to describe the Bubble Tea setup flow, keybindings, and non-interactive fallbacks.
- [ ] (Req: 3) Document when TUI mode activates vs when the CLI remains plain text for scripting.
- [ ] (Req: 2) Add screenshots or terminal recordings later if desired, but keep core docs text-first and repo-local.

## 13. Out of scope
- [ ] Do not replace the HTTP service, channel workers, or agent runtime with Bubble Tea.
- [ ] Do not introduce a web frontend, REST admin backend, or separate persistent UI daemon.
- [ ] Do not change SQLite schema unless a later implementation step proves it is genuinely necessary for a reviewed bug.
