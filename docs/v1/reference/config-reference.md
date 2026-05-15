# Config reference

The `config.json` file has several sections. Here's what each one does.

## `provider`

Settings for the AI provider.

```json
{
  "provider": {
    "api_key": "sk-...",
    "model": "gpt-4o",
    "base_url": "https://api.openai.com/v1"
  }
}
```

| Field | Type | Description |
|---|---|---|
| `api_key` | string | Your API key |
| `model` | string | Model name to use |
| `base_url` | string | API base URL (for custom endpoints) |

## `service`

Settings for the HTTP API server.

```json
{
  "service": {
    "port": 9100,
    "host": "0.0.0.0",
    "secret": "your-secret",
    "tls": {
      "enabled": false,
      "cert_file": "",
      "key_file": ""
    }
  }
}
```

| Field | Type | Description |
|---|---|---|
| `port` | int | HTTP port (default 9100) |
| `host` | string | Bind address |
| `secret` | string | Auth secret for API access |

## `channels`

Settings for external messaging channels.

```json
{
  "channels": {
    "telegram": {
      "enabled": false,
      "bot_token": ""
    },
    "slack": {
      "enabled": false,
      "bot_token": "",
      "app_token": ""
    },
    "discord": {
      "enabled": false,
      "bot_token": ""
    },
    "whatsapp": {
      "enabled": false,
      "api_key": ""
    },
    "email": {
      "enabled": false,
      "smtp_host": "",
      "smtp_port": 587,
      "smtp_user": "",
      "smtp_pass": ""
    }
  }
}
```

## `storage`

File paths for data.

```json
{
  "storage": {
    "path": "~/.or3-intern"
  }
}
```

| Field | Type | Description |
|---|---|---|
| `path` | string | Directory for config and data files |

## `safety`

Approval and safety settings.

```json
{
  "safety": {
    "approval_mode": "normal",
    "profiles": []
  }
}
```

| Field | Type | Description |
|---|---|---|
| `approval_mode` | string | relaxed, normal, or strict |
| `profiles` | array | Named safety profiles |

## `tools`

Control which tools the agent can use.

```json
{
  "tools": {
    "allow": ["*"],
    "block": []
  }
}
```

| Field | Type | Description |
|---|---|---|
| `allow` | array | List of allowed tools (* for all) |
| `block` | array | List of blocked tools |

## `memory`

Vector search settings.

```json
{
  "memory": {
    "vector_enabled": true,
    "fts_enabled": true
  }
}
```

## `cron`

Scheduled jobs.

```json
{
  "cron": {
    "jobs": [
      {
        "name": "daily-report",
        "schedule": "0 9 * * *",
        "prompt": "Generate a daily report"
      }
    ]
  }
}
```

## `triggers`

Webhook and file watch triggers.

```json
{
  "triggers": {
    "webhooks": [],
    "filewatch": []
  }
}
```

## `mcp`

MCP server connections.

```json
{
  "mcp": {
    "servers": [
      {
        "name": "my-server",
        "url": "http://localhost:3000"
      }
    ]
  }
}
```
