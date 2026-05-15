# Architecture Overview

OR3 Intern has a modular architecture. The main parts work together like this:

## Parts of the System

1. **CLI layer** — all user commands (chat, service, configure, doctor, and more)
2. **Runtime bootstrap** — builds the app runtime from config, storage, security, and integrations
3. **Agent runtime** — the AI agent loop: prompt, context, tools, subagents
4. **Control plane and ServiceApp** — typed application facade over runtime, jobs, auth, storage, diagnostics, embeddings, and runner state
5. **Service API** — authenticated HTTP API under `/internal/v1/*` for OR3 App and other machine clients
6. **Storage layer** — SQLite persistence with vector search and FTS
7. **Tools system** — built-in tool implementations (files, web, exec, memory, skills, MCP)
8. **Channels** — external messaging integrations (Telegram, Slack, etc.)
9. **Automation** — cron jobs, webhooks, filewatch triggers, and heartbeat tasks
10. **Safety system** — approvals, device pairing, auth, audit logging, sandboxing, network policy, and runtime profiles
11. **UX state/copy layer** — user-facing summaries, settings sections, approval prompt copy, and friendly error translation

## How They Connect

Everything shares the same core agent runtime. The runtime is built once at startup. Then it is used by the CLI, the service API, channels, and automation. The service surface goes through the ServiceApp/control-plane layer so HTTP handlers do not reach directly into every subsystem.

This design keeps things simple. There is one agent engine. It does not matter if you talk to it from the terminal, an app, or a chat channel. The experience is the same.

## Key maps

- [System map](system-map.md) - high-level data flow
- [Control plane and ServiceApp](control-plane.md) - app facade and typed management operations
- [Event bus](event-bus.md) - in-process fan-out for channels and automation
- [Runtime lifecycle](runtime-lifecycle.md) - startup phases and dependency order
- [Service API overview](service-api/overview.md) - `/internal/v1/*` route families
- [UX state and copy](ux-state-copy.md) - friendly status, settings, approval, and error text
