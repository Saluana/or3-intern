# Automation Overview

OR3 Intern's automation system lets you schedule recurring work and trigger actions from external events.

## Components

1. **Cron Service** - schedules and persists cron jobs
2. **Cron Runner** - dispatches cron jobs to the event bus or agent CLI
3. **Trigger System** - handles webhook and file-watch events
4. **Webhook Server** - HTTP endpoint for external event triggers
5. **File Watcher** - polls files for changes and triggers events
6. **Structured Tasks** - embeds tool call instructions in trigger payloads
7. **Heartbeat Service** - periodic review reminders from a tasks file

## How they connect

```
External webhook --> Webhook Server --> Event Bus --> Agent
File changes    --> File Watcher   --> Event Bus --> Agent
HEARTBEAT.md    --> Heartbeat      --> Event Bus --> Agent
Cron jobs       --> Cron Service   --> Cron Runner --> Event Bus / Agent CLI
```

All automation ultimately publishes events to the internal event bus (`internal/bus`). The OR3 agent runtime consumes these events and processes them.

Source: `internal/cron/cron.go`, `internal/cronrunner/dispatcher.go`, `internal/triggers/webhook.go`, `internal/triggers/filewatch.go`, `internal/heartbeat/service.go`

## Event types

Events on the bus have these types:
- `EventCron` - from cron job dispatches
- `EventWebhook` - from webhook HTTP requests
- `EventFileChange` - from file watcher polls
- `EventHeartbeat` - from the heartbeat service

Source: `internal/bus` (event type constants, referenced in dispatchers)
