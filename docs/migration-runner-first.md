# Migration: runner-first execution

## Summary

The built-in OR3 provider/tool-loop agent has been removed from the primary
execution path. Chat, channels, service turns, and automation use external
runners (OpenCode recommended). `/internal/v1/turns` and `/internal/v1/subagents`
are no longer registered. Install and authenticate a
runner before starting new work.

## Config changes

| Before | After |
| --- | --- |
| Implicit built-in agent | `runners.default=opencode` |
| — | `OR3_RUNNERS_DEFAULT` env override |

## Session metadata

Sessions with `runner_id=or3-intern` remain listed. API responses include
`legacy_runner_id` and `runner_selectable=false`. The first new turn migrates
to `runners.default`.

## Cron jobs

- Use `runner_run` for scheduled runner tasks.
- Legacy scheduled chat payloads (`agent_turn`) publish bus events that become runner chat turns.
- Legacy cron `agent_turn` jobs that have not been migrated will still fail; recreate them as `runner_run` to route them through a runner. Runner-only mode is the only supported execution path.

## Commands

| Command | Change |
| --- | --- |
| `or3-intern chat` | Uses runner chat (default OpenCode) |
| `or3-intern agent -m` | Enqueues a runner turn (deprecation notice on stderr) |
| `or3-intern health` | Reports runner readiness |

## Doctor admin brain

Doctor sessions that use the internal API-key admin brain still invoke the
built-in runtime with a restricted tool allowlist. Select an external runner in
doctor settings to use runner chat instead.

## OR3 App

OR3 App sends through runner chat only. Legacy built-in task creation and direct
`/internal/v1/turns` recovery are removed. Choose an external runner before
sending.

## Runner memory bridge

Runners and integrations can call authenticated service endpoints:

- `POST /internal/v1/runner-memory/search`
- `POST /internal/v1/runner-memory/notes`
- `GET` / `POST /internal/v1/runner-memory/pinned`

Writes are audited (`runner_memory.search`, `runner_memory.add_note`,
`runner_memory.set_pinned`). Store durable preferences and facts only—not
scratch work or secrets.
