# Overview

This plan upgrades the existing SQLite-backed memory subsystem without changing its overall shape. It keeps `memory_notes`, `memory_pinned`, the current consolidation scheduler, and the prompt-builder flow. It adds lightweight note metadata, structured consolidation output, slightly smarter retrieval ranking, prompt usage tracking, and a bounded stale-summary cleanup rule.

Scope assumptions:

- The feature stays inside the current Go CLI/runtime architecture.
- `memory_pinned` remains a tiny canonical store for ultra-stable items, not a second rolling summary store.
- Hybrid retrieval remains the only retrieval path; the work improves scoring rather than introducing a second engine.
- No new memory service, graph, wiki, contradiction layer, dashboard, or claims/evidence system is introduced.

# Requirements

## 1. Metadata-aware `memory_notes`

The system must extend `memory_notes` so each note can carry lightweight lifecycle and ranking metadata.

Acceptance criteria:

- `memory_notes` gains additive columns for `kind`, `status`, `importance`, `use_count`, and `last_used_at`.
- The schema uses the requested defaults: `kind TEXT NOT NULL DEFAULT 'note'`, `status TEXT NOT NULL DEFAULT 'active'`, `importance REAL NOT NULL DEFAULT 0`, `use_count INTEGER NOT NULL DEFAULT 0`, and `last_used_at INTEGER NOT NULL DEFAULT 0`.
- Existing databases migrate without manual intervention and without breaking current sessions, history, or vector/FTS behavior.
- Existing rows receive safe defaults; legacy consolidation rows are backfilled into a summary-like kind where it can be inferred from current data such as `tags='consolidation'`.
- New note writes continue to work through existing code paths even when callers do not provide explicit metadata.
- The supported lightweight note kinds include at least `summary`, `fact`, `preference`, `goal`, `procedure`, and `episode`.

## 2. Structured consolidation with minimal pinned memory

The consolidator must keep the current rolling behavior, but emit structured memory items that can be stored directly in `memory_notes`.

Acceptance criteria:

- Consolidation continues to run through the current scheduler and bounded pass model.
- The consolidation prompt returns a single structured JSON object with `summary`, `facts`, `preferences`, `goals`, and `procedures` fields rather than only an untyped summary string.
- Consolidation stores the returned `summary` as `kind='summary'` and stores each item from `facts`, `preferences`, `goals`, and `procedures` as its corresponding typed `memory_notes` row.
- Consolidation updates `memory_pinned` only for a very small set of ultra-stable items such as user preferences, identity facts, and long-running project facts; it does not copy rolling summaries into pinned memory.
- If structured output is malformed or partial, consolidation falls back to a safe minimal write path instead of failing the turn loop.

## 3. Lightweight retrieval and prompt shaping improvements

The existing hybrid retrieval path must rank memory notes a little better and expose the result in a slightly clearer prompt shape.

Acceptance criteria:

- Retrieval still uses the current vector + FTS + lexical + recency flow, with small additional scoring influence from note `kind`, `status`, `importance`, `use_count`, and age.
- Retrieval filters or strongly demotes notes whose `status` is not `active` so stale items do not outrank active durable memory by default.
- Retrieval slightly prefers durable operational kinds such as `fact` and `procedure` over rolling summaries, and slightly demotes old summaries.
- Prompt assembly keeps the existing `Pinned Memory`, `Retrieved Memory`, and `Indexed File Context` structure, and inserts a short `Memory Digest` section between pinned memory and retrieved memory.
- The Memory Digest is built from top active `fact`, `preference`, `goal`, and `procedure` notes and stays bounded to roughly 8-12 lines.

## 4. Usage logging for prompted notes

The system must record when retrieved notes were actually injected into the system prompt.

Acceptance criteria:

- When retrieved notes are included in the prompt, their `use_count` is incremented and `last_used_at` is updated.
- Notes are only marked used when they are actually added to the prompt, not merely when they appear in an intermediate candidate set.
- Usage logging is best-effort and bounded; a usage-write failure must not block prompt building or the agent turn.
- Usage logging preserves current scope isolation rules for global and linked session memory.
- Retrieval can use prior prompt usage as a lightweight promotion signal without introducing a separate promotion subsystem.

## 5. Small stale-summary cleanup

The subsystem must prune low-value rolling summaries without introducing a new background service or large cleanup job.

Acceptance criteria:

- A bounded cleanup rule runs inside the existing consolidation flow or another already-existing memory path.
- Cleanup targets old, never-used `summary` or `episode` notes that are clearly stale, such as older low-use rows that have newer replacements in the same scope.
- Cleanup keeps durable note kinds such as `fact`, `preference`, `goal`, and `procedure` longer and does not touch pinned memory.
- Cleanup work per pass is capped so it remains safe for the current single-process SQLite runtime.

## 6. Backward compatibility and operational safety

The upgrade must remain compatible with the repo’s current runtime and storage constraints.

Acceptance criteria:

- Existing config files continue to load without requiring new memory settings.
- Existing session keys, linked scope behavior, and current prompt bootstrap structure remain compatible.
- All schema changes are additive and fit the current `internal/db/db.go` migration style.
- Retrieval, prompt building, and consolidation remain deterministic, bounded, and low-RAM.

# Non-functional constraints

- Prefer small localized changes in `internal/db`, `internal/memory`, and `internal/agent`.
- Keep SQLite writes deterministic and compatible with the current single-process WAL model.
- Keep history windows, retrieval sets, consolidation passes, and cleanup work bounded.
- Do not expand network access, tool permissions, or secret exposure as part of this change.
- Preserve current prompt limits and avoid materially larger prompt assembly costs.
