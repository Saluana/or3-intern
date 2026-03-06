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
