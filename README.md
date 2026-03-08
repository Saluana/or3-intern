# or3-intern (v1)

Go rewrite of nanobot with SQLite persistence + hybrid long-term memory retrieval.

## Quick start

1) Run guided setup:
```bash
go run ./cmd/or3-intern init
```

2) Start interactive chat:
```bash
go run ./cmd/or3-intern chat
```

3) Or run enabled external channels:
```bash
go run ./cmd/or3-intern serve
```

The `init` command can store your provider settings in `~/.or3-intern/config.json`, so you do not need to manually manage env vars unless you want to.

## Commands

- `or3-intern init` guided first-run setup
- `or3-intern chat` interactive CLI
- `or3-intern serve` run enabled external channels (Telegram / Slack / Discord / WhatsApp bridge)
- `or3-intern agent -m "hello"` one-shot
- `or3-intern skills ...` list, inspect, search, install, update, check, and remove ClawHub/OpenClaw-compatible skills
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`

## Notes

- Uses SQLite with WAL + single-connection for deterministic low-RAM operation.
- History is always fetched with `LIMIT` and never full-scanned.
- Hybrid memory retrieval: pinned + vector (cosine) + FTS keyword search.
- External channels are disabled by default; configure them in `config.json` or via env vars before using `serve`.
- Supported non-CLI channels: Telegram, Slack, Discord, and a local WhatsApp bridge.

## Dependencies

This repo uses external Go modules (SQLite driver + cron parser). If you're building in an offline environment, you must vendor modules ahead of time.

## Channel Integrations

`or3-intern` supports these non-CLI channels:

- Telegram
- Slack
- Discord
- WhatsApp via a local bridge

All external channels are disabled by default.

### Running Channels

Use the CLI chat for local terminal interaction:

```bash
go run ./cmd/or3-intern chat
```

Use the channel runner for enabled external integrations:

```bash
go run ./cmd/or3-intern serve
```

`serve` starts the agent workers plus any enabled channels from your config.

### Environment Variables

You can configure channels through `config.json` or environment variables.

Available env vars:

```dotenv
OR3_TELEGRAM_TOKEN=
OR3_SLACK_APP_TOKEN=
OR3_SLACK_BOT_TOKEN=
OR3_DISCORD_TOKEN=
OR3_WHATSAPP_BRIDGE_URL=ws://127.0.0.1:3001/ws
OR3_WHATSAPP_BRIDGE_TOKEN=
```

### Config Shape

The `config.json` channel section looks like this:

```json
{
	"channels": {
		"telegram": {
			"enabled": false,
			"token": "",
			"apiBase": "https://api.telegram.org",
			"pollSeconds": 2,
			"defaultChatId": "",
			"allowedChatIds": []
		},
		"slack": {
			"enabled": false,
			"appToken": "",
			"botToken": "",
			"apiBase": "https://slack.com/api",
			"socketModeUrl": "",
			"defaultChannelId": "",
			"allowedUserIds": [],
			"requireMention": true
		},
		"discord": {
			"enabled": false,
			"token": "",
			"apiBase": "https://discord.com/api/v10",
			"gatewayUrl": "",
			"defaultChannelId": "",
			"allowedUserIds": [],
			"requireMention": true
		},
		"whatsApp": {
			"enabled": false,
			"bridgeUrl": "ws://127.0.0.1:3001/ws",
			"bridgeToken": "",
			"defaultTo": "",
			"allowedFrom": []
		}
	}
}
```

### Telegram

- Set `channels.telegram.enabled=true`
- Set `channels.telegram.token` or `OR3_TELEGRAM_TOKEN`
- Optionally set `defaultChatId` for outbound `send_message` defaults
- Optionally restrict inbound traffic with `allowedChatIds`

Telegram uses polling, so no webhook setup is required.

### Slack

- Set `channels.slack.enabled=true`
- Set `channels.slack.appToken` and `channels.slack.botToken`
- Optionally set `defaultChannelId`
- Optionally restrict inbound traffic with `allowedUserIds`
- `requireMention=true` is recommended for shared channels

Slack uses Socket Mode for inbound events and Web API for outbound messages.

### Discord

- Set `channels.discord.enabled=true`
- Set `channels.discord.token`
- Optionally set `defaultChannelId`
- Optionally restrict inbound traffic with `allowedUserIds`
- `requireMention=true` is recommended for guild channels

Discord uses the Gateway for inbound events and REST for outbound messages.

### WhatsApp Bridge

WhatsApp support expects a compatible local bridge service.

- Set `channels.whatsApp.enabled=true`
- Set `channels.whatsApp.bridgeUrl` or `OR3_WHATSAPP_BRIDGE_URL`
- Optionally set `channels.whatsApp.bridgeToken`
- Optionally set `defaultTo` and `allowedFrom`

The bridge should expose a websocket endpoint compatible with the message format used by `or3-intern`.

### Session Keys

External channels automatically namespace session keys by platform, for example:

- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `whatsapp:<chat-id>`

This keeps chat history and long-term memory isolated by channel/session.

## New Features

### Bootstrap Files

Three markdown files configure the agent's identity and persistent context:

- **IDENTITY.md** – Loaded once at startup; defines who the agent is (name, role, personality traits). Injects into every system prompt.
- **MEMORY.md** – Static knowledge the agent always has access to (facts, preferences, standing instructions). Injects into every system prompt.
- **HEARTBEAT.md** – Autonomous task list injected only during scheduled (cron/webhook/file-watch) turns, not user-initiated chats. Useful for periodic background tasks.

Configure file paths in `config.json`:

```json
{
  "identityFile": "/path/to/IDENTITY.md",
  "memoryFile":   "/path/to/MEMORY.md",
  "heartbeat": { "tasksFile": "/path/to/HEARTBEAT.md" }
}
```

### Document Index

Opt-in file indexing allows the agent to retrieve relevant file excerpts as context for each query.

```json
{
  "docIndex": {
    "enabled": true,
    "roots": ["/path/to/docs", "/path/to/notes"],
    "maxFiles": 200,
    "maxFileBytes": 65536,
    "refreshSeconds": 300,
    "retrieveLimit": 5
  }
}
```

- Files are indexed at startup and re-synced every `refreshSeconds`.
- Retrieval uses full-text search (FTS5) to find relevant excerpts.
- Only non-empty matches are injected into the system prompt.
- Supported file types: `.md`, `.txt`, `.go`, `.py`, `.js`, `.ts`, `.json`, `.yaml`, `.toml`, `.sh`.

### Session Scopes

Link multiple session keys to a shared scope for cross-channel continuity. Sessions in the same scope share conversation history.

```bash
# Link a Telegram session and a Discord session to one scope
or3-intern scope link telegram:12345 my-project
or3-intern scope link discord:67890 my-project

# List all sessions in a scope
or3-intern scope list my-project

# Resolve the scope for a session
or3-intern scope resolve telegram:12345
```

### ClawHub-Compatible Skills

Skills can include a `skill.json` manifest for rich metadata:

```json
{
  "summary": "Does something useful",
  "entrypoints": [
    {
      "name": "run",
      "command": ["./run.sh", "--mode", "fast"],
      "timeoutSeconds": 30,
      "acceptsStdin": false
    }
  ]
}
```

`or3-intern` now loads ClawHub/OpenClaw-style skill bundles directly from:

- bundled: `builtin_skills/`
- managed: `~/.or3-intern/skills`
- workspace: `<workspace>/skills`

Precedence is `workspace > managed > bundled`. A legacy `<workspace>/workspace_skills` folder is still scanned below the new workspace root for migration safety.

Supported frontmatter keys include:

- `name`
- `description`
- `homepage`
- `user-invocable`
- `disable-model-invocation`
- `command-dispatch`
- `command-tool`
- `command-arg-mode`

Supported metadata namespaces:

- `metadata.openclaw`
- `metadata.clawdbot`
- `metadata.clawdis`

Eligibility checks cover OS, required binaries, any-of binaries, required env vars, required config flags, and explicit per-skill disable flags from config. Ineligible skills remain inspectable through `read_skill` and `or3-intern skills info/check`.

Per-skill config is additive and lightweight:

```json
{
  "skills": {
    "managedDir": "/Users/me/.or3-intern/skills",
    "load": {
      "extraDirs": ["/opt/shared-skills"],
      "watch": false,
      "watchDebounceMs": 250
    },
    "entries": {
      "demo-skill": {
        "enabled": true,
        "apiKey": "secret",
        "env": {
          "DEMO_MODE": "1"
        },
        "config": {
          "browser": {
            "enabled": true
          }
        }
      }
    },
    "clawHub": {
      "siteUrl": "https://clawhub.ai",
      "registryUrl": "https://clawhub.ai",
      "installDir": "skills"
    }
  }
}
```

Skill env injection is scoped to a live run and is not copied into prompts or persisted message history.

Use the native management commands instead of the Node/Bun `clawhub` CLI:

```bash
or3-intern skills list
or3-intern skills list --eligible
or3-intern skills info <name>
or3-intern skills check
or3-intern skills search "calendar"
or3-intern skills install <slug>
or3-intern skills update <name>
or3-intern skills update --all
or3-intern skills remove <name>
```

Explicit user invocation is supported for user-invocable skills:

```text
/my-skill raw arguments here
```

For `command-dispatch: tool`, `or3-intern` forwards the raw argument string directly to the target tool. Otherwise it starts a normal model turn seeded with the selected `SKILL.md`.

Trust model:

- Treat third-party skills as untrusted input.
- Installer hints from skill metadata are informational only; `or3-intern` does not auto-run them.
- Not every ClawHub skill is portable. Skills that depend on unsupported OpenClaw-only tools, custom frontmatter-defined tools, Nix/plugin flows, or remote node assumptions are reported as unavailable instead of failing silently.

### Triggers

**Webhook server** – receives POST requests and dispatches them as agent events:

```json
{
  "triggers": {
    "webhook": {
      "enabled": true,
      "addr": ":8080",
      "secret": "my-secret-token"
    }
  }
}
```

The webhook server listens at `/webhook` (fixed path).

**File watcher** – polls configured paths for new/changed files:

```json
{
  "triggers": {
    "fileWatch": {
      "enabled": true,
      "paths": ["/path/to/watch", "/another/path"],
      "pollSeconds": 10,
      "debounceSeconds": 2
    }
  }
}
```

Both trigger types use `HEARTBEAT.md` instructions when dispatching autonomous turns.

### Streaming

CLI (`chat` command) supports live streamed output. The assistant's response is printed token-by-token as it arrives from the provider. No additional configuration required.

### Cron Jobs with Per-Job Session Keys

Scheduled jobs can target a specific session (and thus its history/memory) independently of the default session:

```json
{
  "payload": {
    "kind": "agent_turn",
    "message": "Daily standup summary",
    "session_key": "slack:standup-channel",
    "channel": "slack",
    "to": "standup-channel"
  }
}
```

When `session_key` is set on a job payload, it overrides the global `defaultSessionKey` for that job.
