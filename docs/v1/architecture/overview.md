# Architecture Overview

OR3 Intern has a modular architecture. The main parts work together like this:

## Parts of the System

1. **CLI layer** — all user commands (chat, service, configure, doctor, and more)
2. **Runtime bootstrap** — builds the app runtime from config, storage, security, and integrations
3. **Agent runtime** — the AI agent loop: prompt, context, tools, subagents
4. **Service API** — HTTP API for app integration (OR3 App and OR3 Net)
5. **Storage layer** — SQLite persistence with vector search and FTS
6. **Tools system** — built-in tool implementations (files, web, exec, memory)
7. **Channels** — external messaging integrations (Telegram, Slack, etc.)
8. **Automation** — cron jobs, webhooks, filewatch triggers
9. **Safety system** — approvals, device pairing, auth, audit logging

## How They Connect

Everything shares the same core agent runtime. The runtime is built once at startup. Then it is used by the CLI, the service API, channels, and automation. They all call the same runtime to process turns.

This design keeps things simple. There is one agent engine. It does not matter if you talk to it from the terminal, an app, or a chat channel. The experience is the same.
