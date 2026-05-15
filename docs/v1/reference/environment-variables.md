# Environment variables reference

Complete list of environment variables that OR3 Intern recognizes.

## Provider

| Variable | Description |
|---|---|
| `OPENAI_API_KEY` | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `GOOGLE_API_KEY` | Google AI API key |
| `AZURE_API_KEY` | Azure OpenAI API key |
| `AZURE_ENDPOINT` | Azure endpoint URL |
| `OR3_PROVIDER` | Provider name (openai, anthropic, google, azure) |
| `OR3_MODEL` | Model name override |

## Service

| Variable | Description |
|---|---|
| `OR3_SERVICE_SECRET` | Auth secret for HTTP API |
| `OR3_SERVICE_PORT` | HTTP port (default 9100) |
| `OR3_SERVICE_HOST` | Bind address (default 0.0.0.0) |

## Channels

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `SLACK_BOT_TOKEN` | Slack bot token |
| `SLACK_APP_TOKEN` | Slack app-level token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `WHATSAPP_API_KEY` | WhatsApp API key |
| `EMAIL_SMTP_HOST` | SMTP server host |
| `EMAIL_SMTP_PORT` | SMTP server port |
| `EMAIL_SMTP_USER` | SMTP username |
| `EMAIL_SMTP_PASS` | SMTP password |
| `EMAIL_FROM` | From address for outgoing email |
| `EMAIL_TO` | Default recipient address |

## Tools

| Variable | Description |
|---|---|
| `BRAVE_API_KEY` | Brave Search API key |
| `SERP_API_KEY` | SerpAPI key |

## Storage

| Variable | Description |
|---|---|
| `OR3_STORAGE_PATH` | Path to data directory |
| `OR3_CONFIG_PATH` | Path to config file |

## Safety

| Variable | Description |
|---|---|
| `OR3_APPROVAL_MODE` | Approval mode (relaxed, normal, strict) |

## Logging

| Variable | Description |
|---|---|
| `OR3_LOG_LEVEL` | Log level (debug, info, warn, error) |
