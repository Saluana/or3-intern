# Agent package quarantine inventory

Runner-first execution moved primary turns to `internal/app.RunnerTurnOrchestrator`
and `internal/agentcli.ChatManager`. The `agent` package is shrinking to
compatibility and admin-only surfaces.

## Retained (still imported by active paths)

| Area | Symbols | Used by |
| --- | --- | --- |
| Job streaming | `JobRegistry`, job events | Service API, doctor admin |
| Attachments | `ChatAttachment`, decode helpers | Runner chat, service turns |
| Observers | `ConversationObserver`, streaming hooks | Service jobs, CLI deliverer |
| Errors | `PublicErrorCode`, approval errors | Channels, service |
| Admin brain | `Runtime.Handle`, `Builder`, tool budgets | Doctor internal admin only |
| Subagents | `SubagentManager` | Optional background (config-gated) |
| Prompt builder | `Builder`, bootstrap rendering | Consolidation, doctor, migration |
| Task cards | plan/task-card helpers | App metadata, doctor tools |

## Quarantined / deprecated for new work

| Area | Disposition |
| --- | --- |
| `Runtime.Handle` for chat/serve/channel/cron ingress | Replaced by runner orchestrator |
| Provider tool-call loop | Do not add new callers |
| Model-callable tool registry exposure | Shrinking; runners own tools |
| `RunnerOR3` built-in agent | Legacy metadata only |

## Deletion order (future passes)

1. Remove subagent provider loop when config fully deprecated.
2. Split neutral types (`ChatAttachment`, observers) into `internal/chat` if import churn is low.
3. Delete unused provider prompt-cache paths once doctor brain uses runner or static prompts only.
4. Run `go test ./...` after each deletion slice.
