# Memory Consolidation Deep Dive: `or3-intern` vs `nanobot`

## Scope

This review focuses on the new `or3-intern` memory consolidation path and compares it against the reference implementation captured in `nanobot-repo.md`.

### `or3-intern` files reviewed

- `/home/runner/work/or3-intern/or3-intern/internal/memory/consolidate.go`
- `/home/runner/work/or3-intern/or3-intern/internal/agent/runtime.go`
- `/home/runner/work/or3-intern/or3-intern/internal/db/store.go`
- `/home/runner/work/or3-intern/or3-intern/internal/db/db.go`
- `/home/runner/work/or3-intern/or3-intern/internal/config/config.go`
- `/home/runner/work/or3-intern/or3-intern/cmd/or3-intern/main.go`
- `/home/runner/work/or3-intern/or3-intern/internal/memory/consolidate_test.go`

### `nanobot` references reviewed

- Consolidation scheduling / lifecycle (`nanobot-repo.md:2453-2469`)
- Consolidation implementation (`nanobot-repo.md:2639-2720`)
- Session model behavior (`nanobot-repo.md:11878-11931`)
- Memory model docs (`nanobot-repo.md:12260-12281`)

---

## High-level comparison

### Where `or3-intern` is better

1. **Stronger persistence/retrieval architecture**
   - `or3-intern` stores notes in SQLite with indexed access and FTS (`internal/db/db.go:53`, `internal/db/db.go:80-90`, `internal/db/db.go:152`) and can blend global + session-scoped memory (`internal/db/store.go:114-120`, `internal/db/store.go:153-163`).
   - `nanobot` primarily uses markdown files (`MEMORY.md` + `HISTORY.md`) and grep-centric lookup (`nanobot-repo.md:12260-12267`).
   - Result: better foundation for scale, filtering, and deterministic retrieval.

2. **Session-scoped memory retrieval integration is cleaner**
   - Prompt builder retrieves memory directly from DB-backed retriever (`internal/agent/prompt.go:79-83`) and composes prompt sections in a bounded format (`internal/agent/prompt.go:114-157`).
   - This is structurally easier to evolve than line-oriented history files.

3. **Operationally safer DB defaults**
   - WAL, busy timeout, and deterministic single connection improve reliability in constrained environments (`internal/db/db.go:19-25`).

### Where `or3-intern` is worse than `nanobot`

1. **No canonical long-term memory rewrite path**
   - `or3-intern` consolidation produces a single summary note (`internal/memory/consolidate.go:131-137`).
   - `nanobot` asks for both a timeline entry and a full long-term memory update via tool call (`nanobot-repo.md:2593-2608`, `nanobot-repo.md:2705-2714`).
   - Impact: factual drift can accumulate because there is no explicit “current truth” artifact being reconciled.

2. **Consolidation runs synchronously in the user turn path**
   - In `or3-intern`, consolidation is called inline at end of turn while session mutex remains held (`internal/agent/runtime.go:50-52`, `internal/agent/runtime.go:152-160`).
   - `nanobot` schedules background consolidation tasks with per-session lock/task tracking (`nanobot-repo.md:2453-2469`, `nanobot-repo.md:2156-2158`).
   - Impact: higher user-visible latency and reduced throughput for busy sessions.

3. **Less complete lifecycle semantics**
   - `nanobot` supports explicit archive-all on `/new` and cancellation-safe flow (`nanobot-repo.md:2422-2447`).
   - `or3-intern` currently supports rolling summarization only.

---

## Potential correctness issues

1. **Non-atomic write sequence may create duplicates (high risk)**
   - Consolidation currently does:
     1) insert memory note (`internal/memory/consolidate.go:133-137`) then
     2) update consolidation cursor (`internal/memory/consolidate.go:140-142`, `internal/db/store.go:226-229`)
   - If process crashes between these statements, the same message range may be consolidated again, creating duplicate notes and retrieval noise.
   - Recommendation: make note insert + cursor advance a single DB transaction.

2. **Tool-only windows can repeatedly re-scan without progress (medium risk)**
   - Tool messages are excluded from transcript (`internal/memory/consolidate.go:70-73`).
   - If everything in the candidate window is skipped, function returns without moving cursor (`internal/memory/consolidate.go:79-82`).
   - Impact: repeated work each turn on unchanged range.
   - Recommendation: advance cursor to last candidate message even when transcript is empty.

3. **Session lock duration includes external provider calls (medium/high risk under load)**
   - `Provider.Chat` + `Provider.Embed` execute before lock release (`internal/memory/consolidate.go:96-113`, with lock scope from `internal/agent/runtime.go:50-52`).
   - This increases stall time for all events in that session.

---

## Performance bottlenecks and scaling concerns

1. **Inline LLM call on hot path**
   - Biggest bottleneck is synchronous network latency in consolidation (`internal/memory/consolidate.go:96-113`) under lock.

2. **Unbounded consolidation fetch size**
   - `GetMessagesForConsolidation` fetches all eligible messages in one query (`internal/db/store.go:203-208`) and transcript includes all content (`internal/memory/consolidate.go:67-79`).
   - For large backlogs, this can increase token cost, response latency, and risk prompt oversize.

3. **Fetching more columns than consolidation needs**
   - Query fetches `payload_json` and `created_at` (`internal/db/store.go:205-216`) although consolidation only needs `id`, `role`, and `content`.
   - This is a smaller but consistent overhead.

4. **Consolidation trigger shape is conservative but not adaptive**
   - Trigger is fixed count threshold (`WindowSize`, default 10; `internal/memory/consolidate.go:42-45`, config in `internal/config/config.go:138-139`).
   - Lacks adaptive strategy (token budget or max batch) for pathological transcripts.

---

## Test coverage assessment

Current tests validate mostly no-op/cursor mechanics and helper conversion (`internal/memory/consolidate_test.go`), but **do not exercise real success/failure paths** of provider-backed consolidation.

Gaps compared to risk profile:

- No deterministic test for successful summary -> note insert -> cursor advance path.
- No test for crash-safe / atomicity behavior.
- No test for transcript-empty cursor progression.
- No test for embed failure fallback while still persisting note.

---

## Prioritized improvements (to match/exceed nanobot)

### P0 (first)

1. **Make consolidation persistence atomic**
   - Implement a DB transaction combining note insert and `last_consolidated_msg_id` update.

2. **Move consolidation off synchronous turn path**
   - Queue/background per-session consolidation (single-flight per session) so user response is not blocked.

3. **Handle empty-transcript windows by advancing cursor**
   - Prevent repeated no-progress scans.

### P1

4. **Bound consolidation batch size**
   - Add `ConsolidationMaxMessages` or token-budget cap, fetch in chunks.

5. **Add lightweight DB query for consolidation**
   - Select only needed fields (`id`, `role`, `content`) to reduce I/O.

6. **Strengthen tests with provider seam**
   - Introduce interface for chat/embed in consolidator tests to validate the actual execution path.

### P2

7. **Introduce canonical memory update path (nanobot parity+)**
   - Keep current note insertion for retrieval, but also maintain a compact canonical memory artifact (e.g., pinned/global structured memory) updated during consolidation.
   - This can outperform nanobot by combining canonical memory with indexed retrieval rather than replacing one with the other.

---

## Final verdict

- **Overall**: `or3-intern` has a **better long-term architecture** for memory storage and retrieval than nanobot.
- **Current weakness**: consolidation execution semantics are currently **worse operationally** (inline, non-atomic write sequence, non-adaptive batching).
- **Near-term win**: with the P0 fixes above, `or3-intern` can quickly surpass nanobot in both reliability and performance while preserving its stronger DB-native retrieval model.
