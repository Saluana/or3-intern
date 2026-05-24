# Config System Overview

The config system lives in `internal/config/`. It manages all persistent settings for OR3 Intern.

## Config File

The config is stored as a JSON file. The default path is:

```
~/.or3-intern/config.json
```

The file is written with permission `0600` (owner read/write only) because it can contain API keys.

## Loading Sequence

When config is loaded, these steps happen in order:

1. **Load .env files** - `LoadDotEnv()` scans for `.env` files in the current working directory and its parent. Variables in `.env` only take effect if the OS environment does not already have them. Set `OR3_LOAD_DOTENV=0` to skip this step.

2. **Read config file** - `readConfigFile()` in `internal/config/load.go:32` reads the JSON from disk. If the file does not exist, it creates one with default values.

3. **Apply env overrides** - `ApplyEnvOverrides()` in `internal/config/env.go:11` applies environment variables on top of the file config. Most env vars win over file settings. `OR3_MODEL` is an exception: it only applies when provider and chat/agents/subagents routing still match factory defaults, so settings saved from or3-app persist across restarts.

4. **Normalize and validate** - `normalizeAndValidateConfig()` in `internal/config/load.go:61` fills in missing values with defaults, normalizes strings (lowercase, trim spaces), and runs all validators.

## Top-Level Config Struct

The `Config` struct is defined in `internal/config/types.go:92`. It has over 40 top-level fields organized into these sections:

| Section | Field | Covers |
|---------|-------|--------|
| Storage | `dbPath`, `artifactsDir`, `workspaceDir`, `allowedDir` | File paths |
| Bootstrap | `soulFile`, `agentsFile`, `toolsFile`, `identityFile`, `memoryFile` | Agent instruction files |
| Provider | `provider`, `providers`, `modelRouting`, `favoriteModels` | AI provider settings |
| Runtime | `defaultSessionKey`, `bootstrapMaxChars`, `historyMax`, etc. | Runtime behavior |
| Consolidation | `consolidationEnabled`, `consolidationModel`, etc. | Memory consolidation |
| Tools | `tools` | Tool config, MCP servers |
| Hardening | `hardening` | Sandboxing, quotas, exec policy |
| Channels | `channels` | Telegram, Slack, Discord, WhatsApp, Email |
| Security | `security` | Secret store, audit, approvals, profiles, network |
| Auth | `auth` | Passkeys, WebAuthn, session policy |
| Service | `service` | HTTP API listener |
| Automation | `cron`, `heartbeat`, `triggers` | Scheduled jobs, webhooks, file watches |
| Context | `context`, `contextManager` | Token budgets, context management |
| Other | `docIndex`, `skills`, `session`, `subagents`, `agentCLI` | Doc indexing, skills, identity links |

## Key Source Files

| File | Purpose |
|------|---------|
| `types.go` | All config struct definitions |
| `defaults.go` | Default values via `Default()` |
| `load.go` | Loading, normalization, validation |
| `save.go` | JSON serialization and file writing |
| `validate.go` | Structural and safety validation |
| `env.go` | Environment variable overrides |
| `dotenv.go` | `.env` file loading |
| `readiness.go` | Startup readiness checks |
| `routing.go` | Provider profile and model routing normalization |
| `integration_lifecycle.go` | Integration quarantine logic |
