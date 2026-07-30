# Runner-First Execution

OR3 uses external runners for chat, channels, service turns, cron, heartbeat, webhook, and file-watch work. Install and authenticate a runner before starting new work.

## Current Configuration

- `runners.default` selects the default runner.
- `OR3_RUNNERS_DEFAULT` overrides the default runner for the process.
- `or3-intern health` reports runner install and auth readiness.

## Cron Jobs

Use `runner_run` for scheduled runner tasks.

## Runner Memory Bridge

Runners and integrations can call authenticated service endpoints:

- `POST /internal/v1/runner-memory/search`
- `POST /internal/v1/runner-memory/notes`
- `GET /internal/v1/runner-memory/pinned`
- `POST /internal/v1/runner-memory/pinned`
