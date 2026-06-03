# Cron Service

The cron service manages scheduled jobs stored in a JSON file or SQLite database.

## Schedule kinds

Three schedule types are supported:

| Kind | Description | Required field |
|------|-------------|---------------|
| `at` | Run once at an absolute time | `at_ms` (Unix milliseconds) |
| `every` | Run on a fixed interval | `every_ms` (min 1000ms) |
| `cron` | Run on a cron expression | `expr` (cron string with optional seconds) |

Source: `internal/cron/cron.go:24-33` (ScheduleKind), `internal/cron/cron.go:776-801` (ValidateSchedule)

## Payload kinds

Jobs can produce three types of work:

- `agent_turn` - wakes the OR3 agent runtime with a message
- `system_event` - compatibility alias for agent_turn
- `agent_cli_run` - enqueues an external agent CLI run

Source: `internal/cron/cron.go:35-42` (payload constants)

## Service structure

`Service` wraps a `robfig/cron` scheduler with persistent storage:

- `path` - file path for the JSON store or SQLite database
- `runner` - function that executes a job when it fires
- `c` - the robfig/cron scheduler instance
- `entries` - maps job IDs to cron entry IDs
- `timers` - maps job IDs to one-shot timers (for `at` schedules)

Source: `internal/cron/cron.go:128-139`

## Storage backends

The service supports two backends based on file extension:

1. **JSON file** (default) - stores jobs as a JSON array in a file. Uses atomic writes (write to `.tmp`, rename).
2. **SQLite** - stores jobs as rows in a `cron_jobs` table. Detected when path ends with `.db`, `.sqlite`, or `.sqlite3`.

Source: `internal/cron/cron.go:172-302` (load/save with backend dispatch)

## Loading jobs

`Start()` loads persisted jobs, prepares them (computes next run time), arms them in the scheduler, and starts the scheduler.

Source: `internal/cron/cron.go:305-329`

## Arming jobs

`armJobLocked` schedules jobs based on their kind:
- **at**: creates a one-shot `time.AfterFunc` timer
- **every**: adds to the cron scheduler with `@every <duration>` syntax
- **cron**: adds to the cron scheduler with the cron expression (supports timezone via `CRON_TZ=`)

Source: `internal/cron/cron.go:654-720`

## Running jobs

When a job fires, `runJobByID` calls the runner function. After execution, the job's state is updated: last run time, status (ok/error), and any error message. If `DeleteAfterRun` is set, the job is removed.

Source: `internal/cron/cron.go:555-631`

## Concurrency safety

All state mutations are protected by a `sync.RWMutex`. The service is safe for concurrent access.

## Job limits

Maximum 10,000 jobs can be stored. The limit is checked on `Add()`.

Source: `internal/cron/cron.go:397-399`
