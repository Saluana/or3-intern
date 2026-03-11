# Triggers and automation

## Overview

`or3-intern` supports several autonomous entrypoints:

- webhook events
- file-watch polling
- heartbeat turns
- cron jobs
- optional structured task execution

These all run through the same runtime used by CLI and service turns.

## Webhook trigger

The webhook server receives POST requests and dispatches them as agent events.

Important config keys:

- `triggers.webhook.enabled`
- `triggers.webhook.addr`
- `triggers.webhook.secret`
- `triggers.webhook.maxBodyKB`

The webhook path is fixed at `/webhook`.

## File-watch trigger

The file watcher polls configured paths for new or changed files.

Important config keys:

- `triggers.fileWatch.enabled`
- `triggers.fileWatch.paths`
- `triggers.fileWatch.pollSeconds`
- `triggers.fileWatch.debounceSeconds`

## Heartbeat

Heartbeat is a timer-driven autonomous trigger that runs inside `or3-intern serve`.

Important config keys:

- `heartbeat.enabled`
- `heartbeat.intervalMinutes`
- `heartbeat.tasksFile`
- `heartbeat.sessionKey`

Operational behavior documented in the README:

- disabled by default
- not used during `chat` or one-shot `agent` runs
- rereads `HEARTBEAT.md` on each autonomous turn
- uses its own session key for deterministic background history
- does not auto-send a normal assistant reply anywhere; explicit `send_message` is required

## Cron jobs

Cron is for schedule-specific work or per-job delivery targets.

The README highlights that job payloads can set `session_key` explicitly to override the global default session.

Use heartbeat for a standing background task list. Use cron when you need a specific schedule or delivery target.

## Structured trigger inputs

Autonomous trigger producers can attach a bounded `structured_event` object in event metadata.

The README calls out examples for:

- `heartbeat`
- `webhook`
- `filewatch`

If trigger content contains a valid `structured_tasks` envelope, the runtime validates and executes those tasks directly through the normal tool registry, quotas, and guards instead of routing them through the model.

Supported forms include:

- raw JSON with a `tasks` array
- a top-level `structured_tasks` field in a larger object
- fenced code blocks tagged `or3-tasks`, `structured-tasks`, or `autonomous-tasks`

## Related documentation

- [Agent runtime](agent-runtime.md)
- [Memory and context](memory-and-context.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/triggers/`
- `internal/heartbeat/`
- `internal/cron/`
