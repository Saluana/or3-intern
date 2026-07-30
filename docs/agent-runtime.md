# Runner-First Execution

External runners execute model reasoning, tool use, and workspace edits. OR3 handles orchestration, memory, channels, approvals, artifacts, secure connections, cron, and persistence.

## Turn Flow

1. Ingress arrives from CLI, service, channel, cron, heartbeat, webhook, or file-watch.
2. `RunnerTurnOrchestrator` resolves runner, session, and trigger metadata.
3. OR3 builds bounded trusted context and delimited user task text.
4. `ChatManager.StartTurn` persists messages and enqueues a runner run.
5. Runner events stream to runner event tables and mirror into messages.
6. Memory consolidation reads persisted messages.

## Prompt Shape

Runner prompts use explicit sections:

```text
<trusted_or3_system_instructions>...</trusted_or3_system_instructions>
<or3_context>...</or3_context>
<user_task>...</user_task>
```

## Cron Payloads

Scheduled runner work uses `runner_run`.
