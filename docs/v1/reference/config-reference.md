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

## `context`

Token budgets, dynamic tool exposure, task card, and context manager.

```json
{
  "context": {
    "mode": "quality",
    "maxInputTokens": 16000,
    "taskCard": {
      "enabled": true,
      "enforcePlan": false,
      "maxRefs": 12,
      "maxPlanItems": 8
    },
    "tools": {
      "dynamicExpose": true
    }
  }
}
```

| Field | Type | Description |
| --- | --- | --- |
| `mode` | string | `poor`, `balanced`, `quality`, or `custom` packet preset |
| `maxInputTokens` | int | Approximate input-token budget for prompt packets |
| `taskCard.enabled` | bool | Track goal, plan, decisions, and refs across turns |
| `taskCard.enforcePlan` | bool | Require `create_plan` before write/exec/web-style tools (default **false**) |
| `taskCard.maxRefs` | int | Max references kept on the active task card |
| `taskCard.maxPlanItems` | int | Max legacy plan lines on the task card |
| `tools.dynamicExpose` | bool | Expose only likely tool schemas each turn |

Configure field keys: `context_task_card_enabled`, `context_task_card_enforce_plan`, `context_dynamic_tools`, and related `context_*` keys in `or3-intern configure --section context`.

## `skills` (trust policy)

When `skills.enableExec` is true, startup validation requires non-empty trust policy. If `trustedOwners` or `trustedRegistries` are empty, load and doctor `--fix` call `EnsureSkillsExecTrustPolicy` to set:

- `trustedOwners`: `["local"]`
- `trustedRegistries`: your configured ClawHub registry URL (default public registry)

Configure fields: `skills_enable_exec`, `skills_trusted_owners`, `skills_trusted_registries`.

## `security.profiles` (service channel)

Legacy configs may still have `security.profiles.channels.service` set to `electron_local_service`. On load, `MigrateLegacyServiceAccessChannel` remaps that profile to a builtin access level from `service.maxCapability`:

| `service.maxCapability` | New service channel level |
| --- | --- |
| `privileged` | `admin` |
| `guarded` | `operator` |
| `safe` | `reader` |

After migration, write tools follow the admin/operator/reader profile instead of the old read-only Electron bootstrap profile.
