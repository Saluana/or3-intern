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

### 1. Initialize the runtime

```bash
go run ./cmd/or3-intern init
```

The guided setup writes provider and runtime settings to `~/.or3-intern/config.json`.

### 2. Start an interactive local session

```bash
go run ./cmd/or3-intern chat
```

Use this first when you want to confirm that provider configuration, storage, and prompts are working before enabling any external integrations.

### 3. Run external channels and automation

```bash
go run ./cmd/or3-intern serve
```

`serve` starts the shared runtime plus any enabled channels, triggers, heartbeat jobs, and other background workers.

### 4. Run internal service mode when integrating with OR3 Net

```bash
go run ./cmd/or3-intern service
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

1. Run `init`
2. Confirm `chat` works with a simple question
3. Review [configuration-reference.md](configuration-reference.md)
4. Run `or3-intern doctor --strict` before exposing channels or service mode
5. Enable one advanced feature at a time: channels, skills, triggers, MCP, or service mode

## Where to go next

- Runtime behavior: [agent-runtime.md](agent-runtime.md)
- Context loading and retrieval: [memory-and-context.md](memory-and-context.md)
- External integrations: [channels.md](channels.md)
- Hardening controls: [security-and-hardening.md](security-and-hardening.md)
