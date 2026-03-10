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
- `or3-intern serve` run enabled external channels (Telegram / Slack / Discord / WhatsApp bridge / Email)
- `or3-intern agent -m "hello"` one-shot
- `or3-intern doctor [--strict]` print hardening warnings for the current config
- `or3-intern secrets <set|delete|list>` manage encrypted secret refs stored in SQLite
- `or3-intern audit [verify]` verify the append-only audit chain
- `or3-intern skills ...` list, inspect, search, install, update, check, and remove ClawHub/OpenClaw-compatible skills
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`

## Notes

- Uses SQLite with WAL + single-connection for deterministic low-RAM operation.
- History is always fetched with `LIMIT` and never full-scanned.
- Hybrid memory retrieval: pinned + vector (cosine) + FTS keyword search.
- External channels are disabled by default; configure them in `config.json` or via env vars before using `serve`.
- Supported non-CLI channels: Telegram, Slack, Discord, Email, and a local WhatsApp bridge.

## Hardening Defaults

Phase 1 hardening is now wired into the default runtime profile:

- file tools stay workspace-rooted by default
- external channels stay closed unless explicitly allowlisted or opened
- child processes use a scrubbed environment allowlist instead of inheriting the full parent env
- `exec` prefers `program` + `args`; legacy shell commands are disabled by default and require explicit `hardening.enableExecShell` opt-in
- tool calls are checked against capability tiers and bounded per-session quotas
- external channel session keys can isolate peers so unrelated senders do not share the same conversation state

Example hardening block:

```json
{
  "hardening": {
    "guardedTools": false,
    "privilegedTools": false,
    "enableExecShell": false,
    "execAllowedPrograms": ["cat", "echo", "find", "git", "grep", "head", "ls", "pwd", "sed", "tail"],
    "childEnvAllowlist": ["PATH", "HOME", "TMPDIR", "TMP", "TEMP"],
    "isolateChannelPeers": true,
    "quotas": {
      "enabled": true,
      "maxToolCalls": 16,
      "maxExecCalls": 2,
      "maxWebCalls": 4,
      "maxSubagentCalls": 2
    }
  }
}
```

  Phase 2 adds four narrow hardening layers on top of that baseline:

  - skills can declare permission metadata plus a `tools` allowlist, and those bounds are enforced during explicit skill execution while script-capable skills still default to a quarantined state until explicitly approved in `skills.policy.approved`
  - heartbeat, webhook, and file-watch turns attach a bounded `structured_event` payload in event metadata and surface it in the autonomous system prompt
  - privileged shell exec and `run_skill_script` can optionally route through a Bubblewrap wrapper via `hardening.sandbox`
  - `or3-intern doctor` audits the current config for common unsafe settings and supports `--strict` for CI-style failures

  Example Phase 2 additions:

  ```json
  {
    "skills": {
      "enableExec": true,
      "policy": {
        "quarantineByDefault": true,
        "approved": ["runner", "deploy-skill"]
      }
    },
    "hardening": {
      "sandbox": {
        "enabled": true,
        "bubblewrapPath": "bwrap",
        "allowNetwork": false,
        "writablePaths": []
      }
    }
  }
  ```

  Notes:

  - `skills list` now prints both eligibility and permission state
  - `skills info` shows declared permissions and approval/quarantine notes
  - `skills check` reports `[quarantined]` for script-capable skills that have not been approved yet
  - Bubblewrap support is optional and Linux-first; when enabled but unavailable, privileged execution is denied instead of silently falling back

  Phase 3 adds four heavier controls on top of that baseline:

  - config can reference encrypted secrets stored in SQLite via `secret:<name>` while plaintext fallback remains available during migration
  - sensitive actions append HMAC-chained audit records and can be verified with `or3-intern audit verify`
  - named access profiles can cap capability tiers, allowed tools, writable paths, outbound hosts, and subagent use per channel/trigger
  - outbound HTTP, provider, and MCP traffic can be limited with `security.network` trusted-host policy

  Example Phase 3 additions:

  ```json
  {
    "security": {
      "secretStore": {
        "enabled": true,
        "required": false,
        "keyFile": "/Users/me/.or3-intern/master.key"
      },
      "audit": {
        "enabled": true,
        "strict": true,
        "keyFile": "/Users/me/.or3-intern/audit.key",
        "verifyOnStart": true
      },
      "profiles": {
        "enabled": true,
        "default": "interactive",
        "channels": {"telegram": "interactive"},
        "triggers": {"webhook": "autonomous"},
        "profiles": {
          "interactive": {
            "maxCapability": "guarded",
            "allowedTools": ["read_file", "list_dir", "web_fetch"],
            "allowedHosts": ["api.openai.com", "api.search.brave.com"],
            "writablePaths": ["/Users/me/workspace"],
            "allowSubagents": false
          },
          "autonomous": {
            "maxCapability": "safe",
            "allowedTools": ["read_file", "list_dir"],
            "allowedHosts": [],
            "writablePaths": [],
            "allowSubagents": false
          }
        }
      },
      "network": {
        "enabled": true,
        "defaultDeny": true,
        "allowedHosts": ["api.openai.com", "api.search.brave.com", "*.slack.com"],
        "allowLoopback": false,
        "allowPrivate": false
      }
    },
    "provider": {
      "apiKey": "secret:provider.openai"
    },
    "triggers": {
      "webhook": {
        "secret": "secret:webhook.github"
      }
    }
  }
  ```

  Phase 3 rollout notes:

  - create the secret store key by running `or3-intern secrets set <name> <value>` after enabling `security.secretStore`
  - update config values to `secret:<name>` one secret at a time; plaintext config still loads unless `secretStore.required=true`
  - enable `security.audit.verifyOnStart=true` only after the initial audit key and audit chain are in place
  - keep `security.network.defaultDeny=false` during discovery, then switch to `true` once all required outbound hosts are listed
  - start with permissive profiles and narrow them per channel/trigger after verifying tool usage

## Dependencies

This repo uses external Go modules (SQLite driver + cron parser). If you're building in an offline environment, you must vendor modules ahead of time.

## MCP Tool Integrations

MCP support is optional and disabled by default. Configure servers under `tools.mcpServers`; enabled servers connect during startup, their tools are registered before workers begin handling turns, and per-server connection failures are logged and skipped instead of aborting the whole process.

Remote tools are exposed to the model as normal function tools with stable local names like `mcp_<server>_<tool>`.

```json
{
  "tools": {
    "mcpServers": {
      "filesystem": {
        "enabled": true,
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/root"],
        "env": {
          "NODE_ENV": "production"
        },
        "connectTimeoutSeconds": 10,
        "toolTimeoutSeconds": 30
      },
      "localDocs": {
        "enabled": false,
        "transport": "streamableHttp",
        "url": "http://127.0.0.1:8080/mcp",
        "headers": {
          "Authorization": "Bearer <token>"
        },
        "allowInsecureHttp": true,
        "connectTimeoutSeconds": 10,
        "toolTimeoutSeconds": 30
      }
    }
  }
}
```

Supported transports:

- `stdio`
- `sse`
- `streamableHttp`

Safety notes:

- Prefer `stdio` for local trusted servers.
- HTTP transports are explicit. Plain `http://` endpoints are rejected unless `allowInsecureHttp=true`, and even then only for loopback/localhost addresses.
- Stdio MCP servers inherit only the configured child environment allowlist plus any explicitly configured `env` entries.
- MCP tool calls use the existing tool loop, per-call timeout, error handling, and artifact spill path.
- When `security.network` is enabled, MCP HTTP transports must also satisfy the global trusted-host policy.
- v1 intentionally does not include live reconnect loops, hot-add/hot-remove of MCP tools, SQLite persistence for tool catalogs, or a separate MCP gateway service.

## Channel Integrations

`or3-intern` supports these non-CLI channels:

- Telegram
- Slack
- Discord
- Email
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
OR3_EMAIL_IMAP_HOST=
OR3_EMAIL_IMAP_PORT=993
OR3_EMAIL_IMAP_USERNAME=
OR3_EMAIL_IMAP_PASSWORD=
OR3_EMAIL_SMTP_HOST=
OR3_EMAIL_SMTP_PORT=587
OR3_EMAIL_SMTP_USERNAME=
OR3_EMAIL_SMTP_PASSWORD=
OR3_EMAIL_FROM_ADDRESS=
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
    },
    "email": {
      "enabled": false,
      "openAccess": false,
      "consentGranted": false,
      "allowedSenders": [],
      "defaultTo": "",
      "autoReplyEnabled": false,
      "pollIntervalSeconds": 30,
      "markSeen": true,
      "maxBodyChars": 4000,
      "subjectPrefix": "Re: ",
      "fromAddress": "",
      "imapMailbox": "INBOX",
      "imapHost": "",
      "imapPort": 993,
      "imapUseSSL": true,
      "imapUsername": "",
      "imapPassword": "",
      "smtpHost": "",
      "smtpPort": 587,
      "smtpUseTLS": true,
      "smtpUseSSL": false,
      "smtpUsername": "",
      "smtpPassword": ""
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
- when `hardening.isolateChannelPeers=true`, inbound sessions are isolated per sender instead of sharing one thread per channel

Slack uses Socket Mode for inbound events and Web API for outbound messages.

### Discord

- Set `channels.discord.enabled=true`
- Set `channels.discord.token`
- Optionally set `defaultChannelId`
- Optionally restrict inbound traffic with `allowedUserIds`
- `requireMention=true` is recommended for guild channels
- when `hardening.isolateChannelPeers=true`, inbound sessions are isolated per sender instead of sharing one thread per channel

Discord uses the Gateway for inbound events and REST for outbound messages.

### WhatsApp Bridge

WhatsApp support expects a compatible local bridge service.

- Set `channels.whatsApp.enabled=true`
- Set `channels.whatsApp.bridgeUrl` or `OR3_WHATSAPP_BRIDGE_URL`
- Optionally set `channels.whatsApp.bridgeToken`
- Optionally set `defaultTo` and `allowedFrom`
- when `hardening.isolateChannelPeers=true`, inbound sessions are isolated per sender even inside shared chats

The bridge should expose a websocket endpoint compatible with the message format used by `or3-intern`.

### Email

- Set `channels.email.enabled=true`
- Set `channels.email.consentGranted=true` only after explicit permission to access the mailbox
- Set either `channels.email.openAccess=true` or a non-empty `allowedSenders` allowlist
- Configure IMAP with `imapHost`, `imapPort`, `imapUsername`, `imapPassword`, and optionally `imapMailbox`
- Configure SMTP with `smtpHost`, `smtpPort`, `smtpUsername`, `smtpPassword`, and optionally `fromAddress`
- `autoReplyEnabled=false` keeps inbound email from being auto-answered by normal turns; explicit `send_message` sends still work when a `to` address is provided
- v1 is text-first: plain text is preferred, HTML falls back to lightweight text conversion, and attachments are intentionally ignored

Email only starts under `serve`. Inbound mail is polled over IMAP, routed into session keys like `email:user@example.com`, and outbound replies reuse the latest stored subject/message-id threading metadata when available.

### Session Keys

External channels automatically namespace session keys by platform, for example:

- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `email:<normalized-address>`
- `whatsapp:<chat-id>`

This keeps chat history and long-term memory isolated by channel/session.

### Structured Trigger Inputs

Autonomous trigger producers now attach a bounded `structured_event` object in `bus.Event.Meta`:

- `heartbeat` includes the tasks path and session key
- `webhook` includes route, request id, remote address, content type, and a bounded body preview
- `filewatch` includes path, size, and mtime

The runtime still keeps the current plain-text trigger message for backward compatibility, but autonomous prompts also receive the structured payload under a dedicated system-prompt section.

## New Features

### Bootstrap Files

Three markdown files configure the agent's identity and persistent context:

- **IDENTITY.md** – Loaded once at startup; defines who the agent is (name, role, personality traits). Injects into every system prompt.
- **MEMORY.md** – Static knowledge the agent always has access to (facts, preferences, standing instructions). Injects into every system prompt.
- **HEARTBEAT.md** – Autonomous task list injected only during heartbeat, cron, webhook, and file-watch turns, not user-initiated chats. It is reloaded on each autonomous turn so edits apply without restart.

Configure file paths in `config.json`:

```json
{
  "identityFile": "/path/to/IDENTITY.md",
  "memoryFile":   "/path/to/MEMORY.md",
  "heartbeat": {
    "enabled": false,
    "intervalMinutes": 30,
    "tasksFile": "/path/to/HEARTBEAT.md",
    "sessionKey": "heartbeat:default"
  }
}
```

`heartbeat.enabled` is off by default and only applies to `or3-intern serve`.

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
- Not every ClawHub skill is portable. Skills that depend on unsupported OpenClaw-only tools, malformed `tools` declarations, Nix/plugin flows, or remote node assumptions are reported as unavailable instead of failing silently.

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

### Heartbeat Service

Heartbeat is a timer-driven autonomous trigger that runs inside `or3-intern serve`.

```json
{
  "heartbeat": {
    "enabled": true,
    "intervalMinutes": 15,
    "tasksFile": "/path/to/HEARTBEAT.md",
    "sessionKey": "heartbeat:default"
  }
}
```

- Heartbeat is disabled by default.
- Heartbeat does not run during `chat` or one-shot `agent` commands.
- The interval is configured in minutes and normalized to a sane minimum.
- Heartbeat uses its own session key so its history and long-term memory stay deterministic across ticks.
- `HEARTBEAT.md` is reread on each autonomous turn, so edits apply without restarting `serve`.
- Empty files, comment-only files, and missing files are skipped instead of triggering a model call.
- Heartbeat turns do not auto-send a normal assistant reply anywhere. If the agent should proactively notify someone, it must call `send_message` explicitly.

Use heartbeat when the agent should periodically review a standing background task list. Use cron when you need a specific schedule or per-job delivery target.

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
