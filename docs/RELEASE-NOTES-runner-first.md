# Release notes: runner-first execution (phases 1–10)

## Breaking / behavioral changes

- **New turns require an external runner** when `agentCLI.enabled=true` (default for new configs).
- The built-in OR3 agent loop (`agent.Runtime.Handle`) is no longer used for `chat`, `serve`, channel workers, or normal service turns.
- `or3-intern agent -m` enqueues runner chat work instead of calling the built-in model loop.
- `/internal/v1/chat-runners` no longer lists `or3-intern` as an always-available runner.
- Legacy cron `agent_turn` payloads fail when `agentCLI.enabled=false`.

## What to do

1. Install [OpenCode](https://opencode.ai) (or another supported runner) and authenticate (`opencode auth login`).
2. Run `or3-intern health` and resolve runner findings.
3. Set `agentCLI.defaultRunner` if you prefer Codex, Claude Code, or Gemini.
4. Recreate scheduled jobs as `agent_cli_run` where possible.

## Unchanged

- SQLite history, memory notes, approvals, audit, and channel data are preserved.
- `agent_cli_run` direct jobs and SSE event streams behave as before.
- Doctor internal admin brain may still use the built-in runtime when configured.

See [migration-runner-first.md](migration-runner-first.md) for details.
