# Cron Tool

Name: `cron` | Capability: `safe` | Group: `cron`

Manages scheduled jobs. Jobs can trigger agent turns or external agent CLI runs.

## Actions

- **add** - create a new scheduled job
- **list** - list all jobs
- **remove** - delete a job
- **run** - run a job immediately
- **status** - scheduler status (job count, next wake time)

Source: `internal/tools/cron.go:33-102`

## Parameters

- `action` (required) - one of: add, list, remove, run, status
- `job` - job object for add (schedule + payload)
- `id` - job ID for remove or run
- `force` - run even if disabled

## Job defaults

When adding a job, if fields are missing:
- `enabled` defaults to `true`
- `payload.kind` defaults to `agent_turn`
- `schedule.kind` defaults to `every` with 24-hour interval

Source: `internal/tools/cron.go:85-95`

## Payload kinds

- `agent_turn` - wakes the OR3 agent with a message
- `system_event` - compatibility alias for agent_turn
- `agent_cli_run` - enqueues an external agent CLI run (requires `agent_run.runner_id` and `agent_run.task`)

Source: `internal/tools/cron.go:88-91` (defaults)

## Integration

The CronTool wraps `cron.Service` which loads/saves jobs from a JSON file or SQLite database, arms them with a cron scheduler, and dispatches them through the cron runner.

Source: `internal/cron/cron.go` (Service), `internal/cronrunner/dispatcher.go` (Dispatcher)
