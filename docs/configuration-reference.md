# Configuration reference

`or3-intern` loads its primary configuration from `config.json`, usually under `~/.or3-intern/config.json` after `or3-intern init`.

## Top-level sections

| Key | Purpose |
| --- | --- |
| `dbPath`, `artifactsDir`, `workspaceDir`, `allowedDir` | Storage locations and workspace boundaries |
| `defaultSessionKey`, `session` | Session naming and cross-session identity/scope behavior |
| `identityFile`, `memoryFile` | Prompt bootstrap files |
| `provider` | Model API base, model names, keys, temperature, and timeouts |
| `tools` | Local tool behavior, proxying, timeouts, workspace restrictions, and MCP servers |
| `hardening` | Tool capability tiers, program allowlists, child environment controls, quotas, and sandboxing |
| `skills` | Managed skill loading, per-skill config, policy, and registry settings |
| `triggers` | Webhook and file-watch automation |
| `heartbeat` | Timer-driven autonomous turns |
| `cron` | Scheduled job storage and execution |
| `service` | Internal authenticated HTTP API settings |
| `channels` | Telegram, Slack, Discord, WhatsApp bridge, and Email configuration |
| `security` | Secret store, audit, access profiles, and outbound network policy |
| `docIndex` | Opt-in document indexing for prompt-time retrieval |
| `subagents` | Background job queueing and concurrency controls |

## Minimal shape

```json
{
  "provider": {},
  "tools": {},
  "hardening": {},
  "skills": {},
  "triggers": {},
  "heartbeat": {},
  "cron": {},
  "service": {},
  "channels": {},
  "security": {},
  "docIndex": {},
  "subagents": {},
  "session": {}
}
```

## Important sections

### `provider`

Controls the LLM and embedding provider settings:

- `apiBase`
- `apiKey`
- `model`
- `embedModel`
- `temperature`
- `enableVision`
- `timeoutSeconds`

### `tools`

Controls local tool execution and optional MCP registration:

- `braveApiKey`
- `webProxy`
- `execTimeoutSeconds`
- `restrictToWorkspace`
- `pathAppend`
- `mcpServers`

See [mcp-tool-integrations.md](mcp-tool-integrations.md) for the MCP-specific settings.

### `hardening`

Core runtime safety controls:

- `guardedTools`
- `privilegedTools`
- `enableExecShell`
- `execAllowedPrograms`
- `childEnvAllowlist`
- `isolateChannelPeers`
- `sandbox`
- `quotas`

See [security-and-hardening.md](security-and-hardening.md) for rollout guidance.

### `skills`

Skill loading and trust policy:

- `enableExec`
- `maxRunSeconds`
- `managedDir`
- `load`
- `entries`
- `policy`
- `clawHub`

See [skills.md](skills.md).

### `triggers`, `heartbeat`, and `cron`

These sections control autonomous execution:

- `triggers.webhook`
- `triggers.fileWatch`
- `heartbeat`
- `cron`

See [triggers-and-automation.md](triggers-and-automation.md).

### `service`

Internal service mode settings:

- `enabled`
- `listen`
- `secret`

See [api-reference.md](api-reference.md).

### `channels`

Non-CLI integrations:

- `telegram`
- `slack`
- `discord`
- `whatsApp`
- `email`

See [channels.md](channels.md).

### `security`

Phase 3 security controls:

- `secretStore`
- `audit`
- `profiles`
- `network`

See [security-and-hardening.md](security-and-hardening.md).

### `docIndex`

Opt-in file indexing and retrieval:

- `enabled`
- `roots`
- `maxFiles`
- `maxFileBytes`
- `maxChunks`
- `embedMaxBytes`
- `refreshSeconds`
- `retrieveLimit`

See [memory-and-context.md](memory-and-context.md).

## Environment overrides called out in the README

The codebase documents these direct environment overrides for service and channel setup:

- `OR3_SERVICE_ENABLED`
- `OR3_SERVICE_LISTEN`
- `OR3_SERVICE_SECRET`
- `OR3_TELEGRAM_TOKEN`
- `OR3_SLACK_APP_TOKEN`
- `OR3_SLACK_BOT_TOKEN`
- `OR3_DISCORD_TOKEN`
- `OR3_WHATSAPP_BRIDGE_URL`
- `OR3_WHATSAPP_BRIDGE_TOKEN`
- `OR3_EMAIL_IMAP_HOST`
- `OR3_EMAIL_IMAP_PORT`
- `OR3_EMAIL_IMAP_USERNAME`
- `OR3_EMAIL_IMAP_PASSWORD`
- `OR3_EMAIL_SMTP_HOST`
- `OR3_EMAIL_SMTP_PORT`
- `OR3_EMAIL_SMTP_USERNAME`
- `OR3_EMAIL_SMTP_PASSWORD`
- `OR3_EMAIL_FROM_ADDRESS`

## Related code

- `internal/config/config.go`
- `cmd/or3-intern/init.go`
