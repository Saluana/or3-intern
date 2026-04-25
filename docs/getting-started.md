# Getting started

## What `or3-intern` is

`or3-intern` is a local-first agent runtime with:

- a Go CLI
- SQLite-backed history and memory
- external channel adapters
- optional autonomous triggers
- optional internal HTTP service mode
- a hardened tool execution model

## Quick start

The beginner mental model is:

- Folder — where OR3 is allowed to work
- Safety Level — how careful OR3 should be
- Connected Devices — who can talk to OR3
- Allowed Actions — what OR3 can do without asking
- Activity Log — what OR3 records for later review

### 0. Install the CLI if you want the bare `or3-intern` command

```bash
./scripts/install-cli.sh
or3-intern version
```

If you prefer not to install anything yet, every example below also works with `go run ./cmd/or3-intern ...`.

### 1. Run guided setup

```bash
or3-intern setup
```

`setup` is the recommended first-run flow. It writes provider and runtime settings to `~/.or3-intern/config.json`, asks where you are using OR3, and lets you choose a safety mode before saving.

The setup review summarizes:

- folder access
- command behavior
- internet posture
- connected-device readiness
- activity log status

Re-run `or3-intern settings` later when you want to review or change your setup.

If you want the lower-level section-based editor, use `or3-intern configure` or commands like `or3-intern configure --section provider --section web`.

When stdin and stdout are both terminals, `configure` opens the Bubble Tea setup UI. Use arrow keys to move, `enter` to select sections or fields, `space` to toggle booleans, `s` to save, and `q` to go back or quit.

When input or output is redirected, piped, or otherwise non-interactive, `configure` falls back to the plain-text prompt flow. That keeps scripted installs and remote automation compatible with earlier behavior. Secret prompts stay single-step in plain-text mode: leave them blank to keep the current value, enter a new value to replace it, or type `clear` to remove the secret.

`or3-intern init` follows the same activation rules, but starts with the original first-run sections: provider, storage, workspace, and web.

### 2. Check safety and access

```bash
or3-intern status
```

Use `status` after setup any time you want a quick answer to:

- what OR3 can access
- what safety mode you are effectively using
- whether any problems still need attention
- whether devices and approvals are ready

Use `or3-intern status --advanced` when you also want the internal finding IDs. When a safe automatic repair is available, advanced output shows a focused `or3-intern status --fix <finding-id>` command.

### 3. Start an interactive local session

```bash
or3-intern chat
```

Use this first when you want to confirm that provider configuration, storage, and prompts are working before enabling any external integrations.
Inside chat, `/new` archives the current conversation into long-term memory before clearing the active message history.

### 4. Review or change settings

```bash
or3-intern settings
```

`settings` opens a task-based home with AI Provider, Workspace Folder, Connected Devices, Safety Level, Channels, Tools, Memory, and Advanced. Use `or3-intern settings --section safety` to change safety mode, `or3-intern settings --section workspace` to change the folder boundary, or `or3-intern settings --export config.json` to export the raw config for advanced review.

### 5. Connect another device

```bash
or3-intern connect-device
```

This flow helps you pair a phone or other device using a short code and simple access levels.

Use:

```bash
or3-intern connect-device list
```

to review already connected devices.

### 6. Run external channels and automation

```bash
or3-intern serve
```

`serve` starts the shared runtime plus any enabled channels, triggers, heartbeat jobs, and other background workers.

### 7. Run internal service mode when integrating with OR3 Net

```bash
or3-intern service
```

This exposes the authenticated loopback HTTP API documented in [api-reference.md](api-reference.md).

## Important local paths

By default, runtime data lives under `~/.or3-intern/`.

Common files and directories include:

- `config.json` — primary runtime configuration
- `or3-intern.sqlite` — SQLite database for history, memory, secrets, audit data, and related state
- `artifacts/` — spilled large tool outputs and related artifacts
- `skills/` — managed skill installs
- `IDENTITY.md` — agent identity/persona injected into prompts
- `MEMORY.md` — always-available standing context
- `HEARTBEAT.md` — autonomous task list reloaded during autonomous turns

## Recommended first-run sequence

1. Run `setup`
2. Run `status`
3. Confirm `chat` works with a simple question
4. Review or adjust anything important with `settings`
5. Review [configuration-reference.md](configuration-reference.md) when you need raw config keys
6. Run `or3-intern doctor --strict` before exposing channels or service mode; use `or3-intern doctor --fix` for safe automatic repairs and `or3-intern doctor --fix --interactive` for guided fixes
7. Enable one advanced feature at a time: channels, skills, triggers, MCP, or service mode

## After changing embedding providers or models

If you switch `provider.apiBase` or `provider.embedModel`, check and rebuild stored embeddings so older memory and indexed docs stay semantically compatible with the new provider/model pair.

```bash
or3-intern embeddings status
or3-intern embeddings rebuild all
```

If you only need long-term memory rebuilt, use `or3-intern embeddings rebuild memory`.

## Interactive vs scripted setup

- Use `or3-intern setup` for the plain-language first-run path.
- Use `or3-intern settings` when you want to revisit your saved setup.
- Use `or3-intern configure` from a normal terminal when you want the full-screen Bubble Tea setup flow.
- Use `or3-intern configure --section ...` when you want to revisit only specific areas.
- Use redirected stdin/stdout, CI shells, or wrappers without TTYs when you want the plain-text prompts instead of the TUI.
- Use `go run ./cmd/or3-intern ...` anywhere you do not want to install the binary yet; the same TTY detection rules still apply.

## Where to go next

- Runtime behavior: [agent-runtime.md](agent-runtime.md)
- Context loading and retrieval: [memory-and-context.md](memory-and-context.md)
- External integrations: [channels.md](channels.md)
- Hardening controls: [security-and-hardening.md](security-and-hardening.md)
