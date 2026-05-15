# Configuration basics

OR3 Intern uses a JSON config file. By default it lives at `~/.or3-intern/config.json`. You can also use environment variables — check `.env.example` in the repo for the full list.

## Key config sections

| Section | What it sets |
|---|---|
| `provider` | AI provider, API key, model, base URL |
| `service` | HTTP port, host, auth secret, TLS settings |
| `channels` | Bot tokens and settings for Telegram, Slack, Discord, WhatsApp, Email |
| `storage` | File paths for data and config |
| `safety` | Approval mode, safety profiles |
| `tools` | Allow and block lists for tool access |
| `memory` | Vector index and FTS settings |
| `cron` | Scheduled jobs |
| `triggers` | Webhook and file watch triggers |
| `mcp` | MCP server connections |

## Find your config

Run this to see the config file path:

```bash
or3-intern config-path
```

## Environment variables

You can override any config value with an environment variable. For example:

```bash
export OPENAI_API_KEY=sk-...
export OR3_SERVICE_SECRET=my-secret
```

See the [environment variables reference](../reference/environment-variables.md) for the full list.

## Next step

Start a [chat session](running-chat.md) with your agent.
