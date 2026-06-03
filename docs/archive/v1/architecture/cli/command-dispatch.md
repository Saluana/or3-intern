# CLI Command Dispatch

Command dispatch is how the CLI routes from the first argument to the correct handler.

## Argument parsing

`parseRootCLIArgs` splits the arguments into config path, command args, and flags for help, advanced help, and unsafe dev mode.

Source: `cmd/or3-intern/command_args.go` (argument parsing utilities)

## Default command

If no command is given, the CLI defaults to `chat`:

```go
cmd := "chat"
if len(args) > 0 {
    cmd = args[0]
}
```

Source: `cmd/or3-intern/main.go:219-222`

## Pre-config commands

These commands don't need a config file to exist:

| Command | Handler | Description |
|---------|---------|-------------|
| `config-path` | inline | Print default config path |
| `version` | inline | Print "or3-intern v1" |
| `configure` | `runConfigure` | Interactive config TUI |
| `init` | `runInit` | Create default config file |
| `setup` | `runSetup` | First-run setup wizard (may chain to chat) |
| `settings` | `runSettings` | Read/write config settings |

Source: `cmd/or3-intern/main.go:231-269`

## First-run setup

If no config file exists and the command is `chat`, the setup wizard runs automatically via `maybeRunFirstRunSetup`. After setup, if the user chose to start chatting, the command is changed to `chat`.

Source: `cmd/or3-intern/main.go:832-867`

## Health, doctor, and status

The `health`, `doctor`, and `status` commands load config in repair mode (lenient loading) and run without building the full runtime. `health` is the normal readiness wrapper around the doctor engine. `doctor` exposes stricter advanced diagnostics and filters. `status` optionally opens the database for additional checks.

Source: `cmd/or3-intern/main.go:278-324`

## Pre-runtime commands

These commands need config, security, and database but not the full agent runtime:

| Command | Handler | Needs |
|---------|---------|-------|
| `capabilities` | `runCapabilitiesCommand` | config, broker |
| `embeddings` | `runEmbeddingsCommand` | config, database, provider |
| `scope` | `runScopeCommand` | config, database |
| `secrets` | `runSecretsCommand` | secret manager, audit |
| `audit` | `runAuditCommand` | audit via control plane |

Source: `cmd/or3-intern/pre_runtime_commands.go:34-47`, `cmd/or3-intern/main.go:367-389`

## Full runtime commands

These commands build the complete runtime (providers, tools, channels, MCP, cron, subagents):

| Command | Handler | Runtime components |
|---------|---------|-------------------|
| `chat` | `cli.Channel.Run` | Workers, spinner, CLI deliverer |
| `serve` | inline in main | Workers, all channels, webhook, filewatch, heartbeat |
| `service` | `runServiceCommandWithBrokerOptionsCronMCP` | Workers, HTTP API, subagents, agent CLI, MCP |
| `agent` | inline in main | One-shot `rt.Handle` |
| `skills` | `runSkillsCommandWithDeps` | Skills inventory, hub client, audit |
| `approvals` | `runApprovalsCommand` | Approval broker |
| `devices` | `runDevicesCommand` | Approval broker |
| `pairing` | `runPairingCommand` | Approval broker |
| `connect-device` | `runConnectDeviceCommand` | Approval broker, database |
| `pair` | `runPairCommand` | Approval broker, database |
| `migrate-jsonl` | `migrateJSONL` | Database |
| `migrate-openclaw` | `runMigrateOpenClawCommand` | Database, provider |

Source: `cmd/or3-intern/main.go:632-758`

## Runtime build order

The runtime is built in this order:

1. Storage (directories for DB, artifacts, cron)
2. Database (SQLite via `db.Open`)
3. Security (secret store key, audit logger)
4. Approval broker (signing key, broker config)
5. Provider clients (main, embedding, consolidation)
6. Event bus (256-buffer channel)
7. Channel manager (Telegram, Slack, Discord, WhatsApp, Email)
8. MCP manager (MCP server connections)
9. Skills inventory
10. Tool registry (all registered tools)
11. Agent runtime (builder, model config, hardening)
12. Agent CLI manager (external runner detection)
13. Cron service (scheduled jobs)
14. Subagent manager (background tasks)
15. Heartbeat service (serve mode only)
16. Consolidation scheduler (if consolidation enabled)
17. Workers (event processing goroutines)

Source: `cmd/or3-intern/main.go:342-629`

## Worker pool

Workers consume events from the bus and invoke `rt.Handle`. The default worker count is 4, configurable via `workerCount`. Each event gets a fresh context with 120-second timeout and conversation session tracking.

Source: `cmd/or3-intern/main.go:1086-1114`
