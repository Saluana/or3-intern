# Documentation

This directory holds the detailed guides and references that were previously packed into the root README.

## Guides

- [Getting started](getting-started.md) — first-run flow, local paths, and the quickest way to get a working install
- [Agent runtime](agent-runtime.md) — how turns move through the shared runtime across CLI, service mode, channels, and automation
- [Memory and context](memory-and-context.md) — history, hybrid retrieval, bootstrap files, document indexing, and session scopes
- [Channel integrations](channels.md) — Telegram, Slack, Discord, Email, and WhatsApp bridge setup and behavior
- [Skills](skills.md) — skill loading, precedence, trust policy, and management commands
- [Triggers and automation](triggers-and-automation.md) — webhook, file-watch, heartbeat, cron, and structured task execution
- [Security and hardening](security-and-hardening.md) — phase 1/2/3 controls, doctor, secrets, audit, profiles, and network policy
- [MCP tool integrations](mcp-tool-integrations.md) — optional MCP server registration and transport safety

## References

- [Configuration reference](configuration-reference.md) — top-level config map and the major nested sections in `config.json`
- [CLI reference](cli-reference.md) — command-by-command summary for the `or3-intern` binary
- [Internal service API reference](api-reference.md) — authenticated HTTP endpoints for `or3-intern service`

## Suggested reading order

1. [Getting started](getting-started.md)
2. [Configuration reference](configuration-reference.md)
3. [Agent runtime](agent-runtime.md)
4. Any feature guides relevant to the way you plan to run the system
