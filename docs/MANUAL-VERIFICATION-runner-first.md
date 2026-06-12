# Manual verification: runner-first migration

Run on a machine with OpenCode (or another configured runner) installed.

## Service / CLI

1. `or3-intern doctor` — reports OpenCode install/auth and default runner readiness.
2. `or3-intern chat` — sends a message; turn is handled by the default external runner (not built-in tool loop).
3. `curl …/internal/v1/chat-runners` — lists selectable runners; `or3-intern` is not selectable.
4. Enqueue a cron `runner_run` job — appears in agent CLI run history.
5. Trigger heartbeat with `HEARTBEAT.md` present — autonomous turn uses runner orchestrator.

## OR3 App

1. Chat — runner picker shows external runners; no fake OR3 default.
2. Agents — task handoff requires a selectable runner.
3. Scheduled tasks — new tasks default to external runner when available.
4. Settings — Runners section visible; legacy built-in task toggles hidden when runners enabled.
