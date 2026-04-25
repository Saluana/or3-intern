# or3-intern (v1)

`or3-intern` is a Go rewrite of nanobot with SQLite persistence, hybrid long-term memory retrieval, external channel integrations, autonomous triggers, and a hardened tool runtime.

The README now stays focused on orientation and quick start. Detailed guides and references live under [`docs/`](docs/README.md).

## Quick start

If you want to use the bare `or3-intern` command from your shell, install it once:

```bash
./scripts/install-cli.sh
```

Then verify the binary is available:

```bash
or3-intern version
```

1. Run guided setup:
   ```bash
   or3-intern setup
   ```
2. Start an interactive local session:
   ```bash
   or3-intern chat
   ```
   Inside chat, use `/new` when you want to archive the current conversation into memory and start with a clean live session.
3. Or run enabled external channels and automation:
   ```bash
   or3-intern serve
   ```
4. Check safety and access posture any time:
   ```bash
   or3-intern status
   ```

The `setup` command is the recommended first-run flow. It asks for a provider, workspace folder, scenario, and safety mode, then translates those choices into the existing runtime profile, approvals, audit, service, and hardening settings.

Use `settings` when you want to revisit your setup later. It opens a task-based home for AI Provider, Workspace Folder, Connected Devices, Safety Level, Channels, Tools, Memory, and Advanced:

```bash
or3-intern settings
```

The advanced `configure` command still exists and supports re-running specific sections later with `--section`. The lighter `init` command still exists for the original first-run provider/storage flow.

On an interactive terminal, `configure` and `init` launch the Bubble Tea setup UI with arrow-key navigation, `enter` to open/select, `space` to toggle booleans, `s` to save, and `q` to back out or quit. If stdin or stdout is not a terminal, both commands automatically stay in the plain-text prompt flow so scripts and redirected input remain stable. In plain-text mode, existing secrets stay hidden: leave the field blank to keep the current value, enter a new value to replace it, or type `clear` to remove it. The provider section now also exposes an optional embedding-dimensions override for models/providers that support configurable vector sizes.

Use `go run ./cmd/or3-intern ...` for ad hoc local runs, or install the binary first if you want every command in the reference to work exactly as `or3-intern ...`.

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

Root help shows the full command catalog by default:

- `or3-intern setup` guided first-run setup using scenario and safety choices
- `or3-intern chat` interactive CLI
- `or3-intern status [--advanced]` plain-language safety and access summary
- `or3-intern settings [--section ...] [--export path|-]` review setup, jump to focused task sections, or export config
- `or3-intern connect-device [list|disconnect <device-id>|role <device-id>]` pair a phone or other device
- `or3-intern configure [--section ...]` interactive setup and reconfiguration wizard
- `or3-intern init` guided first-run setup
- `or3-intern config-path` print the resolved config.json path
- `or3-intern serve` run enabled external channels and automation
- `or3-intern service` run the internal authenticated HTTP API for OR3 Net
- `or3-intern agent -m "hello"` run a one-shot turn
- `or3-intern doctor [--strict|--json|--fix]` diagnose readiness issues, explain risk, and repair safe local problems
- `or3-intern embeddings <status|rebuild>` inspect or rebuild stored memory/doc embeddings after provider or embedding-model changes
- `or3-intern capabilities [--channel name|--trigger name|--json]` inspect the effective runtime posture, ingress policy, approvals, and access profiles
- `or3-intern secrets <set|delete|list>` manage encrypted secret refs stored in SQLite
- `or3-intern audit [verify]` inspect or verify the append-only audit chain
- `or3-intern skills ...` list, inspect, search, install, update, check, and remove skills
- `or3-intern approvals <list|show|approve|deny|allowlist>` inspect and resolve approval requests
- `or3-intern devices <list|requests|approve|deny|rotate|revoke>` inspect paired devices and legacy pairing request helpers
- `or3-intern pairing <list|request|approve|deny|exchange>` manage first-class pairing workflows, including channel-bound identities
- `or3-intern scope <link|list|resolve>` link multiple session keys to a shared history scope
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`
- `or3-intern version`
- `or3-intern help [command]` show root or command-specific help

See [CLI Reference](docs/cli-reference.md) for command details.

The setup docs stay text-first in-repo. Screenshots or terminal recordings can be added later, but the written walkthroughs are the source of truth for rollout behavior and keybindings.

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
- After changing `provider.apiBase` or `provider.embedModel`, run `or3-intern embeddings status` and then `or3-intern embeddings rebuild memory` (or `all`) so stored vectors are regenerated in the new embedding space.
- External channels are disabled by default until configured.
- `or3-intern doctor` is the main readiness command before exposing channels, triggers, or the service API.

## Config alignment

- Native runtime settings come from process env plus `~/.or3-intern/config.json`; treat env as the authoritative override layer for service-mode deployments.
- Cross-repo deployment tooling should reserve the `OR3_INTERN_*` prefix, then translate those values into repo-native config before launching `or3-intern`.
- The shared deployment-key mapping for `or3-net` ⇄ `or3-intern` service credentials is documented in [../or3-net/planning/platform-standardization/config-alignment.md](../or3-net/planning/platform-standardization/config-alignment.md).
- Frozen service fixture coverage is enforced via [.github/workflows/contracts.yml](.github/workflows/contracts.yml).

## Dependencies

This repo uses external Go modules (including SQLite drivers, `sqlite-vec`, and the cron parser). If you are building in an offline environment, vendor the modules ahead of time.
