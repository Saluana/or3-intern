# Tasks: Token-Efficient Context Packets

## 1. Phase 1: Low-Risk Quick Wins

- [ ] Map current prompt inputs and protected sections in `internal/agent/prompt.go`. Requirements: 1, 2, 18.
- [ ] Add tests that capture current inclusion of `SOUL.md`, `IDENTITY.md`, `AGENTS.md`, `TOOLS.md`, pinned memory, memory digest, retrieved memory, workspace context, and skills inventory. Requirements: 2, 18.
- [ ] Add artifact preview regression tests around existing `MaxToolBytes` behavior in `internal/agent` and `internal/artifacts`. Requirements: 12, 17.
- [ ] Reduce default effective recent history for new context modes without changing legacy `HistoryMax` behavior yet. Requirements: 3, 4.
- [ ] Add diagnostic-only token estimation helpers using deterministic approximate counts before changing prompt rendering. Requirements: 13, 17.
- [ ] Document that token efficiency must not remove soul, identity, pinned memory, skills, safety rules, or tool policy. Requirements: 2, 18.

## 2. Phase 2: Context Packet Builder

- [ ] Create `internal/contextpack` with `ContextPacket`, `ContextSection`, `ContextSnippet`, `ContextRef`, `BudgetReport`, `SectionUsage`, and `PruneEvent` types. Requirements: 1, 13.
- [ ] Implement deterministic token estimation and section budget accounting in `internal/contextpack/budget.go`. Requirements: 3, 4, 13, 17.
- [ ] Implement pressure states: normal, warning, high pressure, and emergency compaction. Requirements: 13.
- [ ] Implement protected-section rules so system core, soul/identity/behavior, tool policy, pinned memory, and task-card minimums cannot be dropped. Requirements: 2, 13, 15.
- [ ] Implement packet rendering into provider messages while preserving existing `providers.ChatMessage` history and tool-call fields. Requirements: 1, 18.
- [ ] Update `internal/agent.Builder.BuildWithOptions` to delegate section assembly to `internal/contextpack` behind a compatibility path. Requirements: 1, 18.
- [ ] Add `internal/contextpack` unit tests for section caps, protected sections, output reserve, pressure transitions, and pruning reasons. Requirements: 1, 2, 13, 17.
- [ ] Add `internal/agent` regression tests proving current prompt behavior remains available through the packet renderer. Requirements: 2, 18.

## 3. Phase 3: Configurable Modes and Budgets

- [ ] Add `ContextConfig`, `ContextSectionBudgets`, `ContextRetrievalConfig`, `ContextPressureConfig`, `ContextToolConfig`, `ContextArtifactConfig`, and `ContextTaskCardConfig` to `internal/config/config.go`. Requirements: 3, 4.
- [ ] Add default `balanced` mode for new configs with poor, balanced, quality, and custom mode support. Requirements: 3, 4.
- [ ] Preserve existing `HistoryMax`, `MemoryRetrieve`, `VectorK`, `FTSK`, `BootstrapMaxChars`, and `BootstrapTotalMaxChars` as fallback or compatibility settings. Requirements: 3, 18.
- [ ] Add validation/clamping for negative budgets, too-small protected-section budgets, invalid modes, and unsafe output reserves. Requirements: 2, 3, 13.
- [ ] Extend config tests for mode defaults, custom overrides, legacy config loading, and invalid settings. Requirements: 3, 4.
- [ ] Update CLI settings/configuration docs after implementation fields are stable. Requirements: 3, 18.

## 4. Phase 4: Active Task Card System

- [ ] Add an additive SQLite migration for `task_state` in `internal/db/db.go`. Requirements: 5, 16.
- [ ] Add DB store methods to upsert, fetch, complete, and list active task state by session key. Requirements: 5, 16.
- [ ] Create `internal/contextpack/task_state.go` with typed Go structs for current goal, plan, constraints, decisions, open questions, message refs, memory refs, artifact refs, active files, and status. Requirements: 5.
- [ ] Implement deterministic task-card rendering with a configurable token budget and source refs. Requirements: 5, 10, 13.
- [ ] Update `internal/agent/runtime.go` to update task state after assistant turns. Requirements: 5, 18.
- [ ] Update tool-loop handling to add relevant artifact IDs, active files, and tool-run status to the task card. Requirements: 5, 12.
- [ ] Ensure task state is session/channel isolated and respects resolved scope behavior. Requirements: 16.
- [ ] Add tests for task-card persistence, merge behavior, history-pruning survival, bounded refs, and session isolation. Requirements: 5, 16, 17.

## 5. Phase 5: Memory Schema and Lifecycle Extensions

- [ ] Add memory kind constants for `decision`, `warning`, `artifact_summary`, and `file_summary` in `internal/db/store.go`. Requirements: 6.
- [ ] Add additive migration columns for `summary`, `source_artifact_id`, `confidence`, `updated_at`, `expires_at`, and `supersedes_id` if absent. Requirements: 6.
- [ ] Update memory insert/update methods to preserve existing rows while accepting new metadata fields. Requirements: 6, 18.
- [ ] Add lifecycle helpers to mark memories active, stale, superseded, expired, or used without deleting them. Requirements: 6, 9, 15.
- [ ] Extend consolidation output handling to optionally create decisions and warnings when clearly supported by conversation content. Requirements: 6, 14.
- [ ] Add tests proving migrations are backward compatible and existing `memory_notes` retrieval still works. Requirements: 6, 18.

## 6. Phase 6: Message Span and Artifact Summary Storage

- [ ] Add an additive SQLite migration for `message_spans` and FTS triggers. Requirements: 8.
- [ ] Implement bounded message chunking for long chat/tool messages, including role, message ID, chunk index, token estimate, summary, tags/entities, and embedding metadata. Requirements: 8, 17.
- [ ] Backfill spans lazily for recent messages or during consolidation rather than loading all history at startup. Requirements: 8, 17.
- [ ] Add optional vector indexing for spans after FTS span retrieval is stable. Requirements: 8.
- [ ] Add an additive SQLite migration for `artifact_summaries`. Requirements: 12.
- [ ] Update large tool output handling to store full output as an artifact, write a bounded preview to history, and upsert an artifact summary. Requirements: 12, 17.
- [ ] Add safe fetch-on-demand support for artifact details with size/session checks if current tools do not already cover it. Requirements: 7, 12, 16.
- [ ] Add tests for span chunking, span retrieval refs, artifact summary storage, and large-output history pruning. Requirements: 8, 12, 17.

## 7. Phase 7: Retrieval Improvements and Budgeted Snippet Packing

- [ ] Split `memory.Retriever.Retrieve` into candidate retrieval and final packing, preserving the current API as a compatibility wrapper during migration. Requirements: 7, 9, 18.
- [ ] Retrieve candidates from `memory_notes`, FTS, vector search, `message_spans`, doc index, and artifact summaries where configured. Requirements: 7, 8.
- [ ] Add task-overlap scoring using active task card goals, constraints, decisions, active files, and latest user query. Requirements: 9.
- [ ] Add novelty and dedupe filtering using embeddings when available and lexical/shingle fallback otherwise. Requirements: 7, 9.
- [ ] Exclude or demote stale, superseded, expired, low-confidence, duplicate, wrong-scope, and unsafe candidates. Requirements: 7, 9, 16.
- [ ] Render retrieved snippets with concise text, source IDs, kind, score, and reason; render large sources as refs. Requirements: 7, 10.
- [ ] Track rejected candidate reasons in `BudgetReport`. Requirements: 13.
- [ ] Add tests for relevant inclusion, irrelevant pruning, stale exclusion, novelty dedupe, FTS fallback, vector fallback, and refs instead of full history. Requirements: 7, 8, 9, 13.

## 8. Phase 8: Semantic Compression

- [ ] Add prompt renderers that use short stable labels such as `Goal`, `Constraint`, `Decision`, `Warning`, `Procedure`, and `Ref`. Requirements: 10.
- [ ] Store structured state and summaries as compact JSON in SQLite where useful, but render readable natural text in prompts. Requirements: 10.
- [ ] Add summary builders for old history, tool outputs, artifact refs, file summaries, and memory digest lines. Requirements: 10, 12.
- [ ] Ensure compressed summaries keep source message IDs, memory IDs, artifact IDs, and important constraints. Requirements: 7, 10.
- [ ] Add tests that CamelCase/no-whitespace compression is not used by default and that semantic compression preserves refs. Requirements: 10.

## 9. Phase 9: Dynamic Tool Schema Exposure

- [ ] Add tool metadata for groups/capabilities in `internal/tools`, covering core read, memory, write, exec, web, cron, skills, channels, MCP, and service tools. Requirements: 11.
- [ ] Implement a selector that chooses likely tool groups from user intent, task card, runtime profile, hardening config, and current channel. Requirements: 11, 16.
- [ ] Change provider request assembly to pass only selected tool schemas for the turn. Requirements: 11, 13.
- [ ] Preserve `ToolGuardFromContext`, approval broker checks, sandboxing, network policy, workspace restrictions, and quotas as runtime enforcement. Requirements: 11, 15.
- [ ] Add tests for default read tools, write intent, exec intent, web intent, cron intent, hidden schema exclusion, and guard enforcement for exposed tools. Requirements: 11.

## 10. Phase 10: Optional Cheap Context Manager Model

- [ ] Add `ContextManagerConfig` with provider/model, timeout, token caps, trigger thresholds, and allowed operations. Requirements: 14.
- [ ] Implement JSON schemas/Go structs for task-card updates, retrieval keep/drop decisions, stale memory proposals, history summaries, artifact summaries, and compaction proposals. Requirements: 14.
- [ ] Add strict JSON validation with unknown-field rejection, max lengths, max list sizes, scope checks, and protected-section guards. Requirements: 14, 15, 16.
- [ ] Trigger the manager only for configured events: over-budget packet, task shift, turn interval, large tool output, low-confidence RAG, stale review, or explicit maintenance. Requirements: 14.
- [ ] Apply safe proposals deterministically; treat stale/superseded changes as proposals and never permanently delete memory. Requirements: 15.
- [ ] Fall back to deterministic pruning when the manager fails, times out, returns invalid JSON, or proposes unsafe changes. Requirements: 14, 15, 17.
- [ ] Add tests for JSON validation, invalid JSON fallback, protected-section rejection, memory-delete rejection, safe task-card merge, and stale proposal handling. Requirements: 14, 15.

## 11. Phase 11: Context Pressure Diagnostics and UX

- [ ] Emit `BudgetReport` data from the packet builder for every turn. Requirements: 13.
- [ ] Add concise debug logging or optional CLI/status output for estimated tokens, pressure level, largest sections, pruned sections, and retrieval rejects. Requirements: 13.
- [ ] Ensure diagnostics redact or omit secrets and large content. Requirements: 13, 17.
- [ ] Add tests for diagnostic contents and secret redaction. Requirements: 13, 17.
- [ ] Consider adding a `doctor` or `status` subcommand view for context mode, budget settings, and recent pressure summaries after core behavior is stable. Requirements: 3, 13.

## 12. Phase 12: Evaluation and Regression Coverage

- [ ] Build fixtures for coding, planning, debugging, long-running tasks, repeated memories, stale memories, large tool logs, channel sessions, and workspace retrieval. Requirements: 18.
- [ ] Add benchmark tests for packet construction with large message and memory tables. Requirements: 17.
- [ ] Compare poor, balanced, and quality packet sizes on the same fixture and assert protected sections remain present. Requirements: 2, 3, 4.
- [ ] Verify balanced mode reduces average raw-history/token usage versus the current default while preserving expected decisions and references. Requirements: 4, 18.
- [ ] Run `go test ./...` and `go build ./...` after each phase when implementation begins. Requirements: 18.
- [ ] Document evaluation results and any quality regressions before changing defaults for existing users. Requirements: 4, 18.

## 13. Out of Scope

- [ ] Do not replace SQLite with a server database or external vector service. Requirements: 17, 18.
- [ ] Do not build a frontend or REST backend for this work. Requirements: 18.
- [ ] Do not remove soul, identity, pinned memory, skills, tool policy, project context, or safety rules to save tokens. Requirements: 2, 18.
- [ ] Do not make the cheap context manager a hard dependency or allow it to make final/security-sensitive decisions. Requirements: 14, 15.
- [ ] Do not permanently delete memories as part of automated compaction; mark stale/superseded/archive through guarded flows. Requirements: 15.
