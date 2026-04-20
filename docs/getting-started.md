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

### 0. Install the CLI if you want the bare `or3-intern` command

```bash
./scripts/install-cli.sh
or3-intern version
```

If you prefer not to install anything yet, every example below also works with `go run ./cmd/or3-intern ...`.

### 1. Initialize the runtime

```bash
or3-intern configure
```

The guided wizard writes provider and runtime settings to `~/.or3-intern/config.json`. Re-run targeted sections later with commands like `or3-intern configure --section provider --section web`.

When stdin and stdout are both terminals, `configure` opens the Bubble Tea setup UI. Use arrow keys to move, `enter` to select sections or fields, `space` to toggle booleans, `s` to save, and `q` to go back or quit.

When input or output is redirected, piped, or otherwise non-interactive, `configure` falls back to the plain-text prompt flow. That keeps scripted installs and remote automation compatible with earlier behavior. Secret prompts stay single-step in plain-text mode: leave them blank to keep the current value, enter a new value to replace it, or type `clear` to remove the secret.

`or3-intern init` follows the same activation rules, but starts with the original first-run sections: provider, storage, workspace, and web.

### 2. Start an interactive local session

```bash
or3-intern chat
```

Use this first when you want to confirm that provider configuration, storage, and prompts are working before enabling any external integrations.

### 3. Run external channels and automation

```bash
or3-intern serve
```

`serve` starts the shared runtime plus any enabled channels, triggers, heartbeat jobs, and other background workers.

### 4. Run internal service mode when integrating with OR3 Net

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

1. Run `configure`
2. Confirm `chat` works with a simple question
3. Review [configuration-reference.md](configuration-reference.md)
4. Run `or3-intern doctor --strict` before exposing channels or service mode
5. Enable one advanced feature at a time: channels, skills, triggers, MCP, or service mode

## Interactive vs scripted setup

- Use `or3-intern configure` from a normal terminal when you want the full-screen Bubble Tea setup flow.
- Use `or3-intern configure --section ...` when you want to revisit only specific areas.
- Use redirected stdin/stdout, CI shells, or wrappers without TTYs when you want the plain-text prompts instead of the TUI.
- Use `go run ./cmd/or3-intern ...` anywhere you do not want to install the binary yet; the same TTY detection rules still apply.

## Where to go next

- Runtime behavior: [agent-runtime.md](agent-runtime.md)
- Context loading and retrieval: [memory-and-context.md](memory-and-context.md)
- External integrations: [channels.md](channels.md)
- Hardening controls: [security-and-hardening.md](security-and-hardening.md)
