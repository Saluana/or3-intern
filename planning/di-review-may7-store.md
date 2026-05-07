# internal/db/store.go audit

## Summary

This file is long because it is doing too many unrelated jobs in one place, not because any single concern needs 2,000 lines.

It currently mixes:

- session/message history storage
- pinned memory and context compaction state
- long-term memory note writes and vector search
- consolidation writes and stale note cleanup
- subagent job queue persistence
- agent CLI run queue persistence
- logical session scope linking

The package already split other concerns into dedicated files like `approval_store.go`, `auth_store.go`, `skill_run_plan_store.go`, and `task_state.go`. `store.go` is the outlier.

## Review validation update

I re-checked the code and the six high-confidence findings all hold up. This review is less about taste than the other two: several items are real contract bugs where callers can observe a successful-looking side effect after persistence failed, or a failure after the primary row already committed.

The biggest priority adjustment is to fix the agent CLI finalization path before structural extraction. `FinalizeAgentCLIRun` currently accepts zero-row transitions, `Manager.finalizeRun` logs DB errors but still publishes job completion, and `Manager.reconcileInterruptedRun` ignores the DB error entirely before publishing completion. That is the clearest state-divergence bug.

For memory vectors, pick one explicit contract and test it. The code already treats consolidation vector maintenance as best-effort, so the simplest consistent contract is: SQL note writes succeed even if vector indexing fails, vector failure is recorded/logged or left for a reindex path, and callers do not retry the primary note write just because the index update failed.

For pinned memory, the better fix is probably not a blind behavior change. Add or rename APIs so the difference is explicit: an overlay read (`global` plus scope override) and an exact-scope read. Then point prompt construction and consolidation at the intended read model.

## High-confidence findings

### 1. Memory note writes can report failure after the note row already committed

Relevant code:

- `InsertMemoryNoteTyped`
- `UpdateMemoryNoteTyped`
- `upsertMemoryVec`

The note row is written to `memory_notes` first, then the vector row is written separately. If the vector write fails, the API returns an error even though the primary note row is already durable.

That is a bad contract. A caller can reasonably retry after seeing the error and create duplicate notes, or assume the update did not happen when the SQL row already changed.

This is not a style complaint. It is a correctness problem caused by splitting one logical write across `SQL` and `VecSQL` without compensating behavior.

It is also inconsistent with `WriteConsolidation`, which already treats vector maintenance as best-effort after the SQL transaction commits. Right now the file has two different contracts for what happens when the primary note write succeeds but vector indexing fails.

Practical effect: callers cannot tell the difference between `nothing was written` and `the note exists but the vec index is stale`. That is exactly the kind of ambiguity that creates duplicate retries and weird retrieval drift later.

Recommended implementation: keep the primary `memory_notes` write authoritative, make vector upsert best-effort for insert/update, and add an explicit reindex/repair path for stale vector rows. If the team wants vector failure surfaced, return it through a separate diagnostic mechanism, not as the main write error after the row already committed.

### 2. `FinalizeAgentCLIRun` can silently no-op and still look successful to callers

Relevant code:

- `FinalizeAgentCLIRun`
- `FinalizeSubagentJob`

`FinalizeAgentCLIRun` does an `UPDATE ... WHERE id=? AND status=running` and returns only the `ExecContext` error. It does not check `RowsAffected`.

So if the run does not exist anymore, or it has already transitioned out of `running`, the function returns `nil` even though nothing changed.

That is already inconsistent with the neighboring subagent path, which treats the same zero-row transition as an error.

The downstream effect is worse than a harmless no-op. The manager logs on error in one path and ignores the return entirely in another path, but still publishes completion state to the in-memory job registry. That means the UI or event consumers can believe a run is finished while the DB row still says `running` or some earlier state.

Recommended implementation: match `FinalizeSubagentJob` by checking `RowsAffected` and returning `sql.ErrNoRows` for invalid transitions. In the manager, publish job completion only after finalization succeeds. For reconciliation, log and skip publish on failure, or reload the run and publish based on the persisted terminal state if one already exists.

### 3. `GetLastMessagesScoped` swallows scope-resolution failures and silently narrows history

Relevant code:

- `ResolveScopeKey`
- `ListScopeSessions`
- `GetLastMessagesScoped`

If resolving the logical scope fails, or listing linked sessions fails, `GetLastMessagesScoped` falls back to `GetLastMessages` for only the current physical session.

That means real DB problems get converted into a smaller prompt/history window with no surfaced error. The caller gets degraded behavior and no signal that scope history was lost.

This is the kind of bug that hides in production because the function still returns a valid-looking slice. You do not get a crash, you just get worse context assembly and lower-quality model behavior.

Recommended implementation: preserve fallback only for legitimate "no link exists" behavior. Return errors for `ResolveScopeKey` query failures and `ListScopeSessions` query failures. If a scope exists but has no rows, include the current physical session explicitly rather than treating database errors and empty scopes the same way.

### 4. `LinkSession` writes JSON `null` for nil metadata instead of `{}`

Relevant code:

- `LinkSession`
- `mustJSONMap` in `approval_store.go`

`json.Marshal(nilMap)` returns the bytes for `null`, not `nil`. The current `if mb == nil` check never fixes that case, so `metadata_json` stores `"null"` for nil maps.

This is inconsistent with the rest of the package, which already has a helper that normalizes empty maps to `{}`.

It is not catastrophic, but it is sloppy data-shape handling. Any consumer that expects an object now has to defensively accept both `null` and `{}` for the same column, which is needless schema fuzziness.

Recommended implementation: use the same normalization pattern as `mustJSONMap`, either by reusing that helper or adding a local `jsonMapObject` helper for session metadata.

### 5. Cross-scope vector search is not a real top-k search

Relevant code:

- `SearchMemoryVectors`
- `SearchVecScope`
- `SearchVecScopeFallback`

The function asks each scope for `k` rows, concatenates the results, dedupes by note id, and returns the union.

It does not globally sort the combined result set by distance and it does not clamp the final output back to `k`.

So with both global and session-scoped hits present, the function can return more than `k` rows and in scope order rather than similarity order.

That means this is not really implementing the API shape its name suggests. Callers asking for top-k nearest matches can receive a merged bag of per-scope candidates instead of the best k candidates overall.

Recommended implementation: after dedupe, sort by ascending `Distance` with a stable tie-breaker such as note id, then clamp to `k`. The per-scope search functions already order their local results; the missing piece is the global merge.

### 6. Pinned-memory read semantics are inconsistent inside the same package

Relevant code:

- `GetPinned`
- `GetPinnedValue`
- prompt builder usage
- consolidation usage

`GetPinned` overlays global and scope-specific pinned entries. `GetPinnedValue` only reads the exact scope.

That means prompt construction can see global pinned memory while consolidation reads a narrower view and may miss the same canonical key unless it has been copied into the scope-specific row.

This is an avoidable semantic split in two APIs that look like they should agree.

Practical effect: two adjacent memory pipelines can build on different canonical baselines even when they are supposed to operate on the same logical scope. That invites churn, overwrites, and confusing consolidation behavior.

Recommended implementation: introduce explicit names. For example, keep an exact read as `GetPinnedValueExact`, add overlay behavior as `GetPinnedValue` or `GetPinnedValueOverlay`, and update consolidation/prompt code intentionally. Consolidation likely wants overlay-as-baseline but scoped writes, so it can inherit global canonical memory without mutating the global row.

## Duplication and design drift

The subagent queue and agent CLI queue sections are the loudest duplication problem in the file.

They both implement the same kind of lifecycle store:

- enqueue with queue limits
- list by status and parent session
- claim queued work
- abort queued work
- finalize running work

They are now drifting:

- subagent listing validates the status filter; agent CLI listing accepts any string
- subagent listing sorts by latest activity; agent CLI listing sorts only by `requested_at`
- subagent finalization rejects zero-row transitions; agent CLI finalization accepts them silently

This is exactly the kind of semantic drift you get when two similar state machines live side-by-side in one giant file without shared helpers.

It also means future fixes are likely to land in only one queue path unless someone remembers to patch both sections every time. The file is already showing that pattern.

## What should be split

This should be split by domain, not by arbitrary line count.

Recommended extraction order:

1. `subagent_store.go`
2. `agent_cli_store.go`
3. `memory_store.go`
4. `message_store.go`
5. `session_scope_store.go`

That is the cleanest cut because the queue-backed sections are already the most internally cohesive and the most duplicated.

## Does any of this look deprecated?

Nothing in the file looks obviously dead just from static reading. The problem is not obvious dead code.

The problem is that active responsibilities were piled into one file and similar workflows were copy-shaped into separate blocks instead of being factored.

## Recommended next steps

1. Fix `FinalizeAgentCLIRun` and manager publish-after-finalize behavior first.
2. Fix the memory note/vector write contract and add failure-path tests.
3. Fix scoped-history error propagation, link metadata normalization, vector top-k merge, and pinned-memory API naming/semantics.
4. Extract `subagent_store.go` and `agent_cli_store.go` after the queue lifecycle tests are in place.
5. After extraction, unify queue transition behavior where the two stores should match.

## Task list

- [ ] Make `FinalizeAgentCLIRun` check `RowsAffected` and return `sql.ErrNoRows` on no-op finalization.
- [ ] Stop the agent CLI manager from publishing completion if DB finalization failed.
- [ ] Add a regression test where agent CLI finalization affects zero rows and verify no job completion is published from the manager.
- [ ] Fix the memory note write contract so callers do not get a hard failure after `memory_notes` already committed.
- [ ] Add a regression test that simulates vec-index failure after a successful note insert or update.
- [ ] Make `GetLastMessagesScoped` surface scope-resolution errors instead of silently narrowing to a single session.
- [ ] Normalize `LinkSession` metadata JSON to `{}` for nil or empty maps.
- [ ] Rework `SearchMemoryVectors` to merge, global-sort, and clamp results to true top-k semantics.
- [ ] Split pinned-memory reads into explicit exact-scope and overlay APIs, then point prompt construction and consolidation at the intended one.
- [ ] Extract the subagent queue code into `subagent_store.go`.
- [ ] Extract the agent CLI queue code into `agent_cli_store.go`.
- [ ] Add shared transition tests so subagent and agent CLI lifecycle behavior cannot drift again.
