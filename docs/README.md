# Documentation

This directory holds the detailed guides and references that were previously packed into the root README.

## Guides

- [Getting started](getting-started.md) — first-run flow, simple commands, local paths, and the quickest way to get a working install
- [Using or3-intern service with Tailscale](tailscale-service-guide.md) — practical setup for reaching service mode over a Tailscale tailnet without tripping over origins, CIDRs, or pairing
- [OR3 App connection guide](v1/user-guide/app-integration/or3-app-connection-guide.md) — run the web, Electron, iOS, or Android app and pair it to `or3-intern service`
- [Agent runtime](agent-runtime.md) — how turns move through the shared runtime across CLI, service mode, connected apps, and automation
- [Memory and context](memory-and-context.md) — history, hybrid retrieval, bootstrap files, document indexing, and conversation memory
- [Connected app integrations](channels.md) — Telegram, Slack, Discord, Email, and WhatsApp bridge setup and behavior
- [Skills](skills.md) — skill loading, precedence, trust policy, and management commands
- [Triggers and automation](triggers-and-automation.md) — webhook, file-watch, heartbeat, cron, and structured task execution
- [Security and hardening](security-and-hardening.md) — phase 1/2/3 controls, doctor, secrets, audit, profiles, and network policy
- [MCP tool integrations](mcp-tool-integrations.md) — optional MCP server registration and transport safety
- [Migration notes](migration-notes.md) — compatibility notes for `/internal/v1`, `.env`, compose, integration warnings, and context defaults

## References

- [Configuration reference](configuration-reference.md) — top-level config map and the major nested sections in `config.json`
- [CLI reference](cli-reference.md) — command-by-command summary for the `or3-intern` binary
- [Internal service REST / HTTP API reference](api-reference.md) — authenticated machine-facing endpoints for `or3-intern service`
- [Release checklist](release-checklist.md) — validation and manual smoke checks for service/app contract releases

## Suggested reading order

1. [Getting started](getting-started.md)
2. [CLI reference](cli-reference.md) for the simple vs advanced command surface
3. [Configuration reference](configuration-reference.md) when you need raw config keys
4. [Agent runtime](agent-runtime.md)
5. Any feature guides relevant to the way you plan to run the system
