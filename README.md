# or3-intern (v1)

`or3-intern` is a Go rewrite of nanobot with SQLite persistence, hybrid long-term memory retrieval, external channel integrations, autonomous triggers, and a hardened tool runtime.

The README now stays focused on orientation and quick start. Detailed guides and references live under [`docs/`](docs/README.md).

## Quick start

1. Run guided setup:
   ```bash
   go run ./cmd/or3-intern init
   ```
2. Start an interactive local session:
   ```bash
   go run ./cmd/or3-intern chat
   ```
3. Or run enabled external channels and automation:
   ```bash
   go run ./cmd/or3-intern serve
   ```

The `init` command can store provider settings in `~/.or3-intern/config.json`, so you only need environment variables when you prefer them.

## Core features

- Shared agent runtime for CLI, service mode, channels, and autonomous jobs
- SQLite-backed history with hybrid memory retrieval and document indexing
- External channels for Telegram, Slack, Discord, Email, and a local WhatsApp bridge
- ClawHub/OpenClaw-compatible skills with trust and quarantine controls
- Webhook, file-watch, heartbeat, and cron-based automation
- Phase-based hardening, audit, secret store, profile, and network controls
- Set `runtimeProfile` to `local-dev`, `hosted-service`, `hosted-no-exec`, or `hosted-remote-sandbox-only` to select the intended execution posture — see [docs/security-and-hardening.md](docs/security-and-hardening.md).
- Optional MCP tool integrations over stdio, SSE, and streamable HTTP

## Commands

- `or3-intern init` guided first-run setup
- `or3-intern chat` interactive CLI
- `or3-intern serve` run enabled external channels and automation
- `or3-intern service` run the internal authenticated HTTP API for OR3 Net
- `or3-intern agent -m "hello"` run a one-shot turn
- `or3-intern doctor [--strict]` print hardening warnings for the current config
- `or3-intern secrets <set|delete|list>` manage encrypted secret refs stored in SQLite
- `or3-intern audit [verify]` inspect or verify the append-only audit chain
- `or3-intern skills ...` list, inspect, search, install, update, check, and remove skills
- `or3-intern scope <link|list|resolve>` link multiple session keys to a shared history scope
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`
- `or3-intern version`

See [CLI Reference](docs/cli-reference.md) for command details.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md)
- [Configuration reference](docs/configuration-reference.md)
- [CLI reference](docs/cli-reference.md)
- [Agent runtime](docs/agent-runtime.md)
- [Memory and context](docs/memory-and-context.md)
- [Channel integrations](docs/channels.md)
- [Skills](docs/skills.md)
- [Triggers and automation](docs/triggers-and-automation.md)
- [Security and hardening](docs/security-and-hardening.md)
- [MCP tool integrations](docs/mcp-tool-integrations.md)
- [Internal service API reference](docs/api-reference.md)

## Operational notes

- Uses SQLite with WAL plus bounded connection pools for predictable low-RAM operation.
- History is fetched with bounded queries instead of full scans.
- Hybrid retrieval combines pinned context, vector similarity, and FTS keyword search.
- External channels are disabled by default until configured.
- `or3-intern doctor` is the fastest way to audit an installation before exposing channels, triggers, or the service API.

## Config alignment

- Native runtime settings come from process env plus `~/.or3-intern/config.json`; treat env as the authoritative override layer for service-mode deployments.
- Cross-repo deployment tooling should reserve the `OR3_INTERN_*` prefix, then translate those values into repo-native config before launching `or3-intern`.
- The shared deployment-key mapping for `or3-net` ⇄ `or3-intern` service credentials is documented in [../or3-net/planning/platform-standardization/config-alignment.md](../or3-net/planning/platform-standardization/config-alignment.md).
- Frozen service fixture coverage is enforced via [.github/workflows/contracts.yml](.github/workflows/contracts.yml).

## Dependencies

This repo uses external Go modules (including SQLite drivers, `sqlite-vec`, and the cron parser). If you are building in an offline environment, vendor the modules ahead of time.
