# CLI Architecture Overview

The OR3 Intern CLI (`or3-intern`) is a single binary that handles all commands: chat, serve, service, doctor, setup, and more.

## Entry point

The CLI starts in `cmd/or3-intern/main.go:main()`. The flow is:

1. Parse command-line arguments to find config path, command, and flags
2. Handle help requests (any topic or advanced help)
3. Handle pre-config commands (config-path, version, configure, init, setup, settings)
4. Run first-run setup if no config file exists
5. Handle health, doctor, and status commands (load config without full runtime)
6. Load full runtime config
7. Initialize database and storage
8. Build runtime security (secrets, audit)
9. Handle secrets and audit commands
10. Build approval broker
11. Handle pre-runtime commands (capabilities, embeddings, scope)
12. Build provider clients, tool registry, channels, skills, cron, subagents
13. Dispatch to the command-specific handler

Source: `cmd/or3-intern/main.go:194-763`

## Command dispatch tiers

Commands are dispatched in three tiers:

### Tier 1: Before config load
Handled by `commandHandledBeforeConfigLoad`:
- `config-path` - print config path
- `version` - print version
- `configure` - interactive config TUI
- `init` - create default config
- `setup` - first-run setup wizard
- `settings` - read/write individual settings

Source: `cmd/or3-intern/main.go:823-830`

After first-run setup handling, `health`, `doctor`, and `status` load config in repair mode and return before full runtime bootstrap.

### Tier 2: After config + security, before full runtime
Handled by `commandHandledBeforeRuntimeBootstrap`:
- `capabilities` - show capability report
- `embeddings` - manage embedding vectors
- `scope` - manage session scope links

Source: `cmd/or3-intern/main.go:876-883`

Also at this tier:
- `secrets` - manage encrypted secrets
- `audit` - audit log status and verification

### Tier 3: Full runtime
Commands that need the complete runtime (tools, agents, channels):
- `chat` - interactive CLI chat (default command)
- `serve` - run channels server (Telegram, Slack, Discord, WhatsApp, Email)
- `service` - run internal HTTP API service
- `agent` - one-shot agent invocation
- `skills` - manage installed skills
- `approvals` - manage approval requests
- `devices` - manage paired devices
- `pairing` - manage pairing requests
- `connect-device` - initiate device pairing
- `pair` - initiate normal device pairing with readiness checks
- `migrate-jsonl` - import JSONL conversation history
- `migrate-openclaw` - migrate from OpenClaw

## Help system

Help is available via `--help`, `-h`, or specific help topics. Advanced help is available via `--advanced-help` or `--advanced`.

Source: `cmd/or3-intern/main.go:200-217`

## Error handling

CLI errors are handled by exiting with specific codes:
- Exit 1: operational error (config, runtime, command failure)
- Exit 2: usage error (wrong args, missing required flags)

Source: `cmd/or3-intern/main.go` exit calls throughout
