# Migration: runner-first execution

## Summary

The built-in OR3 provider/tool-loop agent is deprecated for chat, channels,
service turns, and automation. Install and authenticate an external runner
(OpenCode recommended) before starting new work.

## Config changes

| Before | After |
| --- | --- |
| `agentCLI.enabled=false` (default) | `agentCLI.enabled=true` for new installs |
| Implicit built-in agent | `agentCLI.defaultRunner=opencode` |
| — | `OR3_AGENT_CLI_DEFAULT_RUNNER` env override |

Existing config files with `agentCLI.enabled=false` are preserved on load.

## Session metadata

Sessions with `runner_id=or3-intern` remain listed. API responses include
`legacy_runner_id` and `runner_selectable=false`. The first new turn migrates
to `agentCLI.defaultRunner` when agent CLI is enabled.

## Cron jobs

- Keep using `agent_cli_run` for scheduled runner tasks.
- Legacy `agent_turn` jobs publish bus events that become runner chat turns when agent CLI is enabled.
- When agent CLI is disabled, legacy cron agent turns fail with guidance to enable runners or recreate jobs as `agent_cli_run`.

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

Update OR3 App to a runner-first build (phase 11+) so the UI does not synthesize
a built-in `or3-intern` runner when discovery returns no external runners.
