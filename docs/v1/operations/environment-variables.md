# Environment variables

OR3 Intern uses environment variables for configuration. These override values in the config file.

## Provider keys

| Variable | Description |
|---|---|
| `OPENAI_API_KEY` | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `GOOGLE_API_KEY` | Google AI API key |
| `AZURE_API_KEY` | Azure OpenAI API key |

## Model selection

| Variable | Description |
|---|---|
| `OR3_MODEL` | Model name to use |
| `OR3_PROVIDER` | Provider to use (openai, anthropic, etc.) |

## Service

| Variable | Description |
|---|---|
| `OR3_SERVICE_SECRET` | Auth secret for the service API |
| `OR3_SERVICE_PORT` | HTTP port (default 9100) |

## Channel tokens

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram bot token from BotFather |
| `SLACK_BOT_TOKEN` | Slack bot token |
| `SLACK_APP_TOKEN` | Slack app-level token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `WHATSAPP_API_KEY` | WhatsApp API key |
| `EMAIL_SMTP_USER` | SMTP username |
| `EMAIL_SMTP_PASS` | SMTP password |

## Tool configs

| Variable | Description |
|---|---|
| `BRAVE_API_KEY` | Brave Search API key |
| `SERP_API_KEY` | SerpAPI key |

## Storage

| Variable | Description |
|---|---|
| `OR3_STORAGE_PATH` | Path for data files |
| `OR3_CONFIG_PATH` | Path for config file |

See the full list in `.env.example` in the repo.
