# 1. Add metadata columns and DB helpers

- [x] [Req 1, Req 6] Update `internal/db/db.go` to add additive `memory_notes` columns for `kind`, `status`, `importance`, `use_count`, and `last_used_at`, following the current column-by-column migration style.
- [x] [Req 1, Req 6] Add the requested lightweight indexes for `session_key`, `kind`, and `status` in `internal/db/db.go`, plus any small supporting composite index needed for cleanup queries, without changing existing vector or FTS tables.
- [x] [Req 1] Add legacy backfill logic in `internal/db/db.go` so existing consolidation-tag rows become summary-kind notes while other rows receive safe defaults.
- [x] [Req 1, Req 4, Req 5] Extend note/query structs in `internal/db/store.go` to expose the new metadata and add focused helpers for usage updates and stale-summary cleanup.
- [x] [Req 1, Req 6] Add SQLite migration and reopen regressions in `internal/db/db_test.go` for old-database upgrade behavior.

# 2. Keep consolidation bounded, but make it structured

- [x] [Req 2] Update `internal/memory/consolidate.go` so the consolidation prompt asks for compact JSON with `summary`, `facts`, `preferences`, `goals`, and `procedures`.
- [x] [Req 2, Req 6] normalize parsed output from `summary`, `facts`, `preferences`, `goals`, and `procedures` into a bounded set of note writes with validated `kind`, `status`, and `importance` values instead of trusting raw model output.
- [x] [Req 2] Extend the DB write path in `internal/db/store.go` so consolidation can atomically write multiple typed notes, update the canonical pinned entry, and advance `last_consolidated_msg_id`.
- [x] [Req 2] keep `memory_pinned` tiny by only upserting a very small set of ultra-stable identity, preference, or long-running project facts rather than dumping all durable notes into pinned storage.
- [x] [Req 2, Req 5] After successful consolidation, call the bounded stale-summary cleanup helper for the same resolved memory scope.
- [x] [Req 2] Add regression coverage in `internal/memory/consolidate_test.go` for structured output, malformed-output fallback, and pinned-memory minimality.

# 3. Improve retrieval scoring without changing retrieval architecture

- [x] [Req 3, Req 6] Extend vector and FTS candidate queries in `internal/db/store.go` to return note metadata alongside text and timestamps.
- [x] [Req 3] Update `internal/memory/retrieve.go` so `Retrieved` carries metadata and hybrid scoring gets small bounded adjustments for `kind`, `status`, `importance`, `use_count`, and age.
- [x] [Req 3] keep diversification and top-K behavior intact while ensuring stale rows are filtered or demoted and active `fact` and `procedure` notes slightly outrank otherwise similar summaries.
- [x] [Req 3] Add focused ranking tests in `internal/memory/retrieve_test.go` covering active-vs-stale ordering, importance boosts, and capped reuse effects.

# 4. Add prompt digest and usage logging

- [x] [Req 3, Req 4] Update `internal/agent/prompt.go` to build a short `Memory Digest` section from top active fact/preference/goal/procedure notes while preserving the current `Pinned Memory`, `Retrieved Memory`, and `Indexed File Context` sections.
- [x] [Req 4] After the retrieved note set is finalized for prompt inclusion, issue a best-effort usage update for the corresponding note IDs.
- [x] [Req 3, Req 4, Req 6] keep digest formatting to roughly 8-12 lines total and keep retrieved-memory formatting within existing bootstrap truncation limits and one-line formatting conventions.
- [x] [Req 3, Req 4] Add prompt-builder tests in `internal/agent/prompt_test.go` for digest rendering, section ordering, and usage logging only for prompted note IDs.

# 5. Validate cleanup behavior stays lightweight

- [x] [Req 5, Req 6] Implement the stale-memory cleanup rule in `internal/db/store.go` or an adjacent DB helper so it only touches old, never-used `summary` or `episode` rows and caps rows per pass.
- [ ] [Req 5] Add tests proving cleanup leaves facts, preferences, goals, procedures, and pinned memory untouched.
- [ ] [Req 5, Req 6] Add a regression test showing repeated consolidation passes remain bounded and do not trigger unbounded summary deletion scans.

# 6. Keep the change small and repo-aligned

- [ ] [Req 2, Req 3, Req 6] Review `internal/agent/prompt.go`, `internal/memory/consolidate.go`, and any inline agent instructions so wording reflects that pinned memory is for ultra-stable facts and retrieved notes now have a short digest.
- [ ] [Req 6] Confirm no new config or env settings are required; if a constant must be introduced, keep it local to existing packages rather than expanding config.

# 7. Out of scope

- [ ] Do not add a new memory service, graph store, wiki, or claims/evidence layer.
- [ ] Do not add multiple retrieval engines or a separate contradiction-resolution subsystem.
- [ ] Do not add dashboards, manual memory review UIs, or background workers beyond the current scheduler path.
- [ ] Do not redesign session scope, prompt assembly, or pinned memory into a new architecture.
