# Runner-first execution

`or3-intern` is a runner-first host: external agent CLIs (OpenCode by default)
execute turns while OR3 handles orchestration, memory, channels, approvals,
artifacts, and persistence.

## What runs where

| Layer | Responsibility |
| --- | --- |
| **Runners** (OpenCode, Codex, Claude Code, Gemini) | Model reasoning, tools, workspace edits inside runner isolation |
| **OR3** (`RunnerTurnOrchestrator`, `agentcli.ChatManager`) | Session keys, prompts, queueing, history, delivery, memory/doc context |
| **Built-in `agent.Runtime`** | Compatibility only: doctor internal admin brain, tests, legacy tooling |

## Turn flow

1. Ingress (CLI, service, channel, cron bus event, heartbeat, webhook, file-watch)
2. `RunnerTurnOrchestrator` resolves runner, session, and trigger metadata
3. Bounded prompt built with trusted bootstrap blocks, memory/doc snippets, and delimited user task text
4. `ChatManager.StartTurn` persists messages and enqueues `agent_cli_runs`
5. Runner events stream to `agent_cli_events` / `runner_chat_events` and mirror into `messages`
6. Memory consolidation continues from `messages` (including `transport=runner_chat`)

## Default runner

New configs enable `agentCLI.enabled=true` and `agentCLI.defaultRunner=opencode`.

- Override with `OR3_AGENT_CLI_DEFAULT_RUNNER`
- `or3-intern health` / `doctor` report install/auth readiness for the default runner
- Legacy `or3-intern` runner IDs in session metadata remain readable but are not selectable; new turns migrate to the configured default when agent CLI is enabled

## Trusted prompt shape

Runner prompts use explicit sections:

```text
<trusted_or3_system_instructions>...</trusted_or3_system_instructions>
<or3_context>...</or3_context>
<user_task>...</user_task>
```

Bootstrap files (`SOUL.md`, `AGENTS.md`, `IDENTITY.md`, `MEMORY.md`, `TOOLS.md`) feed trusted instructions. `HEARTBEAT.md` is included only for autonomous triggers (heartbeat, cron, webhook, file-watch).

## Context caching

OR3 caches safe fragments (bootstrap file content by mtime/size, runner detection TTL). Approval tokens, secrets, and raw credentials are never cached. Provider-native prompt caching is not used for runner turns.

## Cron payloads

| Kind | Behavior |
| --- | --- |
| `agent_cli_run` | Direct runner background job via `agentcli.Manager` |
| Legacy scheduled chat payloads (`agent_turn` / `system_event`) | Published to the bus for runner chat when `agentCLI.enabled`; rejected with migration guidance when disabled |

## Related documentation

- [Memory and context](memory-and-context.md)
- [Configuration reference](configuration-reference.md)
- [CLI reference](cli-reference.md)
- [Migration: runner-first](migration-runner-first.md)

## Related code

- `internal/app/turn_orchestrator.go`
- `internal/agentcli/chat_manager.go`
- `cmd/or3-intern/main.go`
- `internal/agent/QUARANTINE.md` (built-in runtime inventory)
