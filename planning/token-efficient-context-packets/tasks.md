# Tasks: Token-Efficient Context Packets

## 1. Phase 1: Cache-Stable Prefix and Low-Risk Quick Wins

- [x] Map current prompt inputs and protected sections in [internal/agent/prompt.go](internal/agent/prompt.go) and document which are stable vs volatile. Requirements: 1, 2, 18, 19.
- [x] Add tests that capture current inclusion of `SOUL.md`, `IDENTITY.md`, `AGENTS.md`, `TOOLS.md`, pinned memory, memory digest, retrieved memory, workspace context, and skills inventory. Requirements: 2, 18, 21.
- [x] Split `composeSystemPrompt` in [internal/agent/prompt.go](internal/agent/prompt.go) into `renderStablePrefix` and `renderVolatileSuffix` without changing the rendered output bytes for the default config (golden test). Requirements: 1, 19, 21.
- [x] Move heartbeat clock and structured trigger context out of the stable prefix region so the prefix becomes byte-stable across turns. Requirements: 19.
- [x] Sort tool schemas by tool name in the prefix and serialize them through a deterministic JSON encoder. Requirements: 19.
- [x] Add `TestStablePrefixIsByteStableAcrossTurns` and `TestStablePrefixExcludesHeartbeatAndTriggerMetadata` regression tests. Requirements: 19.
- [x] Wire Anthropic-style `cache_control` breakpoint placement (or equivalent provider-specific marker) at the boundary in [internal/providers](internal/providers); for providers without an explicit marker (OpenAI/OpenRouter automatic prompt caching), keep the prefix bytes stable so the provider hash matches. Requirements: 19.
- [x] Add artifact preview regression tests around existing `MaxToolBytes` behavior in [internal/agent](internal/agent) and [internal/artifacts](internal/artifacts). Requirements: 12, 17.
- [x] Add diagnostic-only token estimation helpers using deterministic approximate counts before changing prompt rendering. Requirements: 13, 17.
- [x] Document that token efficiency must not remove soul, identity, pinned memory, skills, safety rules, or tool policy, and must not introduce a new top-level package. Requirements: 2, 18, 20.

## 2. Phase 2: In-Place Context Packet Builder

- [x] Add [internal/agent/prompt_budget.go](internal/agent/prompt_budget.go) (same package as `Builder`) with `ContextPacket`, `ContextSection`, `ContextSnippet`, `ContextRef`, `BudgetReport`, `SectionUsage`, `PruneEvent`. Requirements: 1, 13, 20.
- [x] Implement deterministic token estimation and section budget accounting in the same file. Requirements: 3, 4, 13, 17.
- [x] Implement pressure states: normal, warning, high pressure, and emergency compaction. Requirements: 13.
- [ ] Implement protected-section rules so system core, soul/identity/behavior, tool policy, pinned memory, and task-card minimums cannot be dropped. Requirements: 2, 13, 15.
- [ ] Refactor `Builder.BuildWithOptions` to call an internal `buildPacket` then `renderProviderMessages`, returning the existing `PromptParts` shape with an added optional `Budget BudgetReport` field that defaults to zero for ignoring callers. Requirements: 1, 18, 20, 21.
- [ ] Add unit tests for section caps, protected sections, output reserve, pressure transitions, and pruning reasons. Requirements: 1, 2, 13, 17.
- [ ] Add regression tests proving current prompt behavior remains available when no `Context` config block is set. Requirements: 2, 18, 21.

## 3. Phase 3: Configurable Modes and Budgets

- [x] Add `ContextConfig`, `ContextSectionBudgets`, `ContextRetrievalConfig`, `ContextPressureConfig`, `ContextToolConfig`, `ContextArtifactConfig`, and `ContextTaskCardConfig` to [internal/config/config.go](internal/config/config.go). Requirements: 3, 4, 20.
- [x] Default new configs to `quality` mode for first release so existing users are not silently downgraded; expose `poor`, `balanced`, `quality`, and `custom` as opt-in. Requirements: 3, 4, 21.
- [ ] When no `Context` block is present, the existing `HistoryMax`, `MemoryRetrieve`, `VectorK`, `FTSK`, `BootstrapMaxChars`, `BootstrapTotalMaxChars`, and `MaxToolBytes` remain authoritative and the packet renders with parity to current behavior. Requirements: 3, 18, 21.
- [x] Add validation/clamping for negative budgets, too-small protected-section budgets, invalid modes, and unsafe output reserves. Requirements: 2, 3, 13.
- [x] Add config tests for mode defaults, custom overrides, legacy config loading (no `Context` block), and invalid settings. Requirements: 3, 4, 21.
- [ ] Update CLI settings/configuration docs after implementation fields are stable. Requirements: 3, 18.

## 4. Phase 4: Active Task Card System

- [x] Add an additive SQLite migration for `task_state` in [internal/db/db.go](internal/db/db.go). Requirements: 5, 16, 21.
- [x] Add DB store methods to upsert, fetch, complete, and list active task state by session key in [internal/db/store.go](internal/db/store.go). Requirements: 5, 16.
- [x] Add [internal/agent/task_card.go](internal/agent/task_card.go) (same package as `Builder`) with typed Go structs for current goal, plan, constraints, decisions, open questions, message refs, memory refs, artifact refs, active files, and status. Requirements: 5, 20.
- [x] Implement deterministic task-card rendering with a configurable token budget and source refs. Requirements: 5, 10, 13.
- [x] Update [internal/agent/runtime.go](internal/agent/runtime.go) to update task state after assistant turns. Requirements: 5, 18.
- [x] Update tool-loop handling to add relevant artifact IDs, active files, and tool-run status to the task card. Requirements: 5, 12.
- [ ] Ensure task state is session/channel isolated and respects resolved scope behavior. Requirements: 16.
- [ ] Add tests for task-card persistence, merge behavior, history-pruning survival, bounded refs, and session isolation. Requirements: 5, 16, 17.

## 5. Phase 5: Memory Schema and Lifecycle Extensions

- [x] Add memory kind constants for `decision`, `warning`, `artifact_summary`, and `file_summary` in `internal/db/store.go`. Requirements: 6.
- [x] Add additive migration columns for `summary`, `source_artifact_id`, `confidence`, `updated_at`, `expires_at`, and `supersedes_id` if absent. Requirements: 6.
- [ ] Update memory insert/update methods to preserve existing rows while accepting new metadata fields. Requirements: 6, 18.
- [x] Add lifecycle helpers to mark memories active, stale, superseded, expired, or used without deleting them. Requirements: 6, 9, 15.
- [ ] Extend consolidation output handling to optionally create decisions and warnings when clearly supported by conversation content. Requirements: 6, 14.
- [ ] Add tests proving migrations are backward compatible and existing `memory_notes` retrieval still works. Requirements: 6, 18.

## 6. Phase 6: Artifact Summaries via Existing Tables (Spans Deferred)

- [x] Update large tool output handling in [internal/agent](internal/agent) and [internal/artifacts](internal/artifacts) to: store full output as an artifact, write a bounded preview to history, and write a paired `memory_notes` row of `kind = 'artifact_summary'` with `source_artifact_id` and a bounded summary. Requirements: 12, 17, 20.
- [x] Confirm existing `memory_fts` and `memory_vec` indexes pick up the new `artifact_summary` rows without schema changes. Requirements: 9, 12, 20.
- [ ] Add safe fetch-on-demand support for artifact details with size/session checks if current tools do not already cover it. Requirements: 7, 12, 16.
- [x] Add tests for artifact summary storage as memory notes, large-output history pruning, and retrieval of artifact summaries via the existing memory pipeline. Requirements: 8, 12, 17, 20.
- [ ] **Deferred**: do not introduce a `message_spans` table or a separate `artifact_summaries` table in this iteration. Re-evaluate after Phase 7 measurements; only add later if existing tables prove insufficient. Requirements: 8, 17, 20.

## 7. Phase 7: Retrieval Improvements and Budgeted Snippet Packing

- [x] Extend [internal/memory/retrieve.go](internal/memory/retrieve.go) in place by adding internal `retrieveCandidates` and `packToBudget` helpers. The existing `Retriever.Retrieve` signature is preserved as a thin wrapper around the two new helpers so all current callers compile unchanged. Requirements: 7, 9, 18, 20, 21.
- [ ] Retrieve candidates from `memory_notes` (vector + FTS), `memory_docs`, and `memory_notes` rows with `kind = 'artifact_summary'`; reuse existing indexes. Requirements: 7, 8, 20.
- [ ] Add task-overlap scoring using active task card goals, constraints, decisions, active files, and latest user query. Requirements: 9.
- [ ] Add novelty and dedupe filtering using embeddings when available and lexical/shingle fallback otherwise. Requirements: 7, 9.
- [ ] Exclude or demote stale, superseded, expired, low-confidence, duplicate, wrong-scope, and unsafe candidates. Requirements: 7, 9, 16.
- [ ] Render retrieved snippets with concise text, source IDs, kind, score, and reason; render large sources as refs. Requirements: 7, 10.
- [ ] Track rejected candidate reasons in `BudgetReport`. Requirements: 13.
- [x] Add tests for relevant inclusion, irrelevant pruning, stale exclusion, novelty dedupe, FTS fallback, vector fallback, refs instead of full history, and a `TestExistingRetrieveAPIStillWorks` compatibility test. Requirements: 7, 8, 9, 13, 21.

## 8. Phase 8: Semantic Compression

- [x] Add prompt renderers that use short stable labels such as `Goal`, `Constraint`, `Decision`, `Warning`, `Procedure`, and `Ref`. Requirements: 10.
- [ ] Store structured state and summaries as compact JSON in SQLite where useful, but render readable natural text in prompts. Requirements: 10.
- [ ] Add summary builders for old history, tool outputs, artifact refs, file summaries, and memory digest lines. Requirements: 10, 12.
- [ ] Ensure compressed summaries keep source message IDs, memory IDs, artifact IDs, and important constraints. Requirements: 7, 10.
- [ ] Add tests that CamelCase/no-whitespace compression is not used by default and that semantic compression preserves refs. Requirements: 10.

## 9. Phase 9: Dynamic Tool Schema Exposure

- [ ] Add tool metadata for groups/capabilities in [internal/tools/registry.go](internal/tools/registry.go), covering core read, memory, write, exec, web, cron, skills, channels, MCP, and service tools. Requirements: 11, 20.
- [ ] Implement a deterministic selector that chooses likely tool groups from user intent, task card, runtime profile, hardening config, and current channel. Requirements: 11, 16.
- [ ] Change provider request assembly in [internal/agent](internal/agent) to pass only selected tool schemas for the turn, sorted by tool name to keep the prefix byte-stable. Requirements: 11, 13, 19.
- [ ] Recompute the exposed tool set per turn but only swap it into the stable prefix when it actually changes; add `TestExposedToolSetChangeRebuildsPrefix` and `TestUnchangedExposedToolsLeavesPrefixIdentical`. Requirements: 19.
- [ ] Preserve `ToolGuardFromContext`, approval broker checks, sandboxing, network policy, workspace restrictions, and quotas as runtime enforcement. Requirements: 11, 15.
- [ ] Add tests for default read tools, write intent, exec intent, web intent, cron intent, hidden schema exclusion, and guard enforcement for exposed tools. Requirements: 11.

## 10. Phase 10: Optional Cheap Context Manager Model

- [x] Add `ContextManagerConfig` with provider/model, timeout, token caps, trigger thresholds, and allowed operations to [internal/config/config.go](internal/config/config.go). Requirements: 14, 20.
- [ ] Add [internal/agent/context_manager.go](internal/agent/context_manager.go) (same package, no new top-level package) with JSON schemas/Go structs for task-card updates, retrieval keep/drop decisions, stale memory proposals, history summaries, artifact summaries, and compaction proposals. Requirements: 14, 20.
- [ ] Add strict JSON validation with unknown-field rejection, max lengths, max list sizes, scope checks, and protected-section guards. Requirements: 14, 15, 16.
- [ ] Trigger the manager only for configured events: over-budget packet, task shift, turn interval, large tool output, low-confidence RAG, stale review, or explicit maintenance. Requirements: 14.
- [ ] Apply safe proposals deterministically; treat stale/superseded changes as proposals and never permanently delete memory. Requirements: 15.
- [ ] Fall back to deterministic pruning when the manager fails, times out, returns invalid JSON, or proposes unsafe changes. Requirements: 14, 15, 17.
- [ ] Add tests for JSON validation, invalid JSON fallback, protected-section rejection, memory-delete rejection, safe task-card merge, and stale proposal handling. Requirements: 14, 15.

## 11. Phase 11: Context Pressure Diagnostics and UX

- [x] Emit `BudgetReport` data from the packet builder for every turn. Requirements: 13.
- [ ] Add concise debug logging or optional CLI/status output for estimated tokens, pressure level, largest sections, pruned sections, and retrieval rejects. Requirements: 13.
- [ ] Ensure diagnostics redact or omit secrets and large content. Requirements: 13, 17.
- [ ] Add tests for diagnostic contents and secret redaction. Requirements: 13, 17.
- [ ] Consider adding a `doctor` or `status` subcommand view for context mode, budget settings, and recent pressure summaries after core behavior is stable. Requirements: 3, 13.

## 12. Phase 12: Evaluation and Regression Coverage

- [ ] Build fixtures for coding, planning, debugging, long-running tasks, repeated memories, stale memories, large tool logs, channel sessions, and workspace retrieval. Requirements: 18.
- [ ] Add benchmark tests for packet construction with large message and memory tables. Requirements: 17.
- [ ] Compare poor, balanced, and quality packet sizes on the same fixture and assert protected sections remain present. Requirements: 2, 3, 4.
- [ ] Verify that with a legacy config (no `Context` block) the rendered prompt is byte-equivalent or strictly a superset of today's prompt for the same inputs. Requirements: 18, 21.
- [ ] Add a measurement harness that reports cache-prefix size and cache-hit-eligible percentage of total input bytes per turn for a representative session. Requirements: 19.
- [ ] Verify balanced mode reduces average raw-history/token usage versus the current default while preserving expected decisions and references. Requirements: 4, 18.
- [ ] Run `go test ./...` and `go build ./...` after each phase when implementation begins. Requirements: 18.
- [ ] Document evaluation results and any quality regressions before changing defaults for existing users. Requirements: 4, 18, 21.

## 13. Out of Scope

- [ ] Do not introduce a new top-level package such as `internal/contextpack`; all new code lives inside existing packages. Requirements: 20.
- [ ] Do not add a `message_spans` or `artifact_summaries` SQLite table in this iteration; reuse `messages`, `memory_notes` (with `kind = 'artifact_summary'`), and `artifacts`. Requirements: 8, 12, 20.
- [ ] Do not replace SQLite with a server database or external vector service. Requirements: 17, 18.
- [ ] Do not build a frontend or REST backend for this work. Requirements: 18.
- [ ] Do not remove soul, identity, pinned memory, skills, tool policy, project context, or safety rules to save tokens. Requirements: 2, 18.
- [ ] Do not make the cheap context manager a hard dependency or allow it to make final/security-sensitive decisions. Requirements: 14, 15.
- [ ] Do not permanently delete memories as part of automated compaction; mark stale/superseded/archive through guarded flows. Requirements: 15.
- [ ] Do not change the default context mode for existing users on first release; default to behavior closest to today (`quality`-leaning) and let users opt into `balanced`/`poor`. Requirements: 4, 21.
- [ ] Do not let any per-turn dynamic content (heartbeat clock, trigger metadata, retrieved snippets, task card, recent history) leak into the cache-stable prefix region. Requirements: 19.
