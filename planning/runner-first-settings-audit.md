# Runner-First Settings Audit

Status: 2026-06-07

This audit tracks old `or3-intern` settings that still appear in `or3-app` now that
normal chat and background agent work are delegated to external runners by default.

## Scrapped From Simple Settings When Runners Are Enabled

These fields should not be shown in the friendly settings UI while
`agentCLI.enabled=true`. Keep accepting them in backend config until a migration
removes or archives old config keys.

| Simple UI control | Backend field(s) | Reason |
| --- | --- | --- |
| Chat provider/model/fallbacks | `routing.chat*`, `provider.model` | External runners choose their own chat model. |
| Agents provider/model/fallbacks | `routing.agents*` | External runner selection replaces the built-in agent model role. |
| Summarization provider/model/fallbacks | `routing.summarization*` | Memory consolidation still exists, but this is now advanced maintenance config, not a primary app setting. |
| Context manager provider/model/fallbacks | `routing.context*`, `contextManager.*` | The context-manager client is no longer called for runner-first chat. |
| Image understanding | `provider.enableVision` | Runner attachment handling is not gated by this toggle. |
| Conversation detail | `context.maxInputTokens` | Runner chat context is assembled by runner-specific context/bootstrap paths. |
| Skill scripts / skill approval | `skills.enableExec`, `security.approvals.skillMode` | Model-callable skill execution was removed from the built-in tool loop. |
| Require plan before writes | `context.taskCard.enforcePlan` | Applies to the legacy model-callable tool loop, not external runners. |
| Command timeout | `tools.execTimeoutSeconds` | No active runner-first execution path consumes this timeout. |
| Shell command strings duplicate | `hardening.enableExecShell` in Tools | Keep the Safety toggle; avoid showing the same backend field twice. |

## Keep Exposed

| Setting | Backend field(s) | Reason |
| --- | --- | --- |
| Runners | `agentCLI.*` | Primary execution surface. |
| Embeddings provider/model/dimensions | `routing.embeddings*`, `provider.embed*` | Memory and document search still need embeddings. |
| Memory cleanup window | `context.historyMaxMessages` | Still used by consolidation and memory recall windows; label must not imply runner prompt size. |
| Memories recalled/search strength | `context.memoryRetrieveLimit`, `memory.vectorSearchK`, `memory.ftsSearchK`, `memory.vectorScanLimit` | Runner bootstrap/context retrieval still uses memory search. |
| Background memory cleanup | `memory.consolidationEnabled`, consolidation limits | Consolidator still summarizes runner chat history. |
| Workspace/search settings | `workspace.*`, `tools.restrictToWorkspace`, `docindex.*` | Still used by file access, indexing, and runner context. |
| Service tool power | `service.maxCapability` | Still gates service/client requested capabilities. |
| Command programs/PATH/exec approvals | `hardening.execAllowedPrograms`, `tools.pathAppend`, `security.approvals.execMode` | Still relevant for service terminal/admin command flows. |
| Terminal command access | `hardening.enableExecShell` in Safety | Still controls shell-style service terminal access. |

## Backend Deletion Candidates

These need a follow-up backend migration because they are persisted config/API
surface, have tests, or are still part of compatibility metadata:

- `ContextManagerConfig` and `ModelRoleContextManager`
- `Provider.EnableVision` / provider profile `enableVision`
- `Context.MaxInputTokens`, `OutputReserveTokens`, and `SafetyMarginTokens`
  after all legacy prompt-packing code is removed
- `Tools.ExecTimeoutSeconds`
- skill execution approval domain and `Skills.EnableExec`, if skill inventory no
  longer needs to distinguish executable skills
- hardening quota fields already documented as legacy
