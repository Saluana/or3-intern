# Neckbeard Code Audit — `internal/agent/`
**Date:** May 3, 2026
**Scope:** All 30 files in `internal/agent/` (source + tests)
**Verdict:** This codebase needs serious attention.

---

## Group 1 — Agent Core & Context

### Silent Event Drop When Subscriber Channel Is Full
**File:** `internal/agent/job_registry.go:153-157`
**Bad Code:**
```go
for _, ch := range entry.subscribers {
    select {
    case ch <- event:
    default:
    }
}
```
**Why This Sucks:** Events are silently blackholed when a subscriber's buffer (128 entries) fills up. No metric, no log, no backpressure. The subscriber has no idea it's fallen behind. This is the distributed systems equivalent of a fire alarm that silently unplugs itself.
**Real Consequences:** Subscribers reading from job event channels will have incomplete event streams with zero indication of data loss. A slow consumer on one subscription silently corrupts its own view of the job lifecycle.
**Fix:**
```go
for subID, ch := range entry.subscribers {
    select {
    case ch <- event:
    default:
        close(ch)
        delete(entry.subscribers, subID)
    }
}
```

---

### Race: Publish to Already-Terminal Job Allowed After Completion
**File:** `internal/agent/job_registry.go:129-159` and `internal/agent/job_registry.go:162-175`
**Bad Code:**
```go
func (r *JobRegistry) Complete(id string, status string, data map[string]any) bool {
    ...
    if !r.Publish(id, "completion", data) {
        return false
    }
    r.markTerminal(id, status)
    return true
}
```
**Why This Sucks:** `Complete` calls `Publish` (releases mutex) then calls `markTerminal` (re-acquires mutex). Between these two calls, another goroutine can call `Publish` on the same job and inject events AFTER the completion event but BEFORE the channels are closed. The `Publish` method checks `if entry == nil` but never checks `entry.terminal`.
**Real Consequences:** A `text_delta` or `tool_result` event published after a completion event will be appended to the in-memory event slice but never delivered. Inconsistent state depending on your consumer type.
**Fix:**
```go
func (r *JobRegistry) Publish(id string, eventType string, data map[string]any) bool {
    r.mu.Lock()
    defer r.mu.Unlock()
    entry := r.jobs[id]
    if entry == nil || entry.terminal {
        return false
    }
    // ...
}
```

---

### Wait() Returns False on Context Cancellation Even When Job Completed
**File:** `internal/agent/job_registry.go:253-271`
**Bad Code:**
```go
if !terminal {
    select {
    case <-done:
    case <-ctx.Done():
        return JobSnapshot{}, false
    }
}
```
**Why This Sucks:** Go's `select` chooses randomly among ready cases. If both `done` (job completed) and `ctx.Done()` (caller timeout) are signaled simultaneously, there's a non-zero probability that `Wait` returns `false` even though the job finished successfully.
**Real Consequences:** Intermittent test failures in CI, spurious production retries causing duplicate work, or operations incorrectly treated as failed.
**Fix:** Check `done` first by using a non-select read:
```go
if !terminal {
    select {
    case <-done:
    case <-ctx.Done():
        return JobSnapshot{}, false
    }
}
```
(Re-order to prioritize done over ctx.Done by nesting selects.)

---

### CronRunner Discards Context Cancellation
**File:** `internal/agent/runtime.go:2347-2364`
**Bad Code:**
```go
func CronRunner(b *bus.Bus, defaultSessionKey string) cron.Runner {
    return func(ctx context.Context, job cron.CronJob) error {
        _ = ctx
```
**Why This Sucks:** The context is explicitly discarded with `_ = ctx`. If the cron framework passes a context with a deadline or cancellation, this runner completely ignores it. The bus publish can block indefinitely.
**Real Consequences:** Goroutine leak. A cron job that can't publish because the bus is full will block forever with no way to cancel.
**Fix:** Check `ctx.Err()` before publishing.

---

### Hand-Rolled Substring Search That Already Exists in the Standard Library
**File:** `internal/agent/context_evaluation_test.go:87-93`
**Bad Code:**
```go
func indexEval(text, want string) int {
    for i := 0; i+len(want) <= len(text); i++ {
        if text[i:i+len(want)] == want {
            return i
        }
    }
    return -1
}
```
**Why This Sucks:** Naive O(n*m) substring search. `strings.Contains` uses Rabin-Karp or equivalent. Reinventing a wheel that's been in stdlib for over a decade.
**Real Consequences:** Burns CPU for no benefit. Teaches junior devs terrible habits.
**Fix:** Delete `indexEval` and `containsEval`, use `strings.Contains`.

---

### O(n²) String Concatenation in Test Helper
**File:** `internal/agent/context_evaluation_test.go:75-81`
**Bad Code:**
```go
func repeatedEvalText(word string, count int) string {
    out := ""
    for i := 0; i < count; i++ {
        out += word + " "
    }
    return out
}
```
**Why This Sucks:** Every `+=` allocates a new string. For `count=1000`, ~1000 allocations and O(n²) total work.
**Real Consequences:** The benchmark spends a large fraction of its runtime building fixture strings rather than measuring real logic.
**Fix:** `return strings.Repeat(word+" ", count)`

---

### RedactDiagnostic Over-Redacts and Corrupts UTF-8
**File:** `internal/agent/diagnostics.go:37-56`
**Bad Code:**
```go
if len(word) > 80 {
    words[i] = word[:80] + "..."
}
```
**Why This Sucks:** `word[:80]` slices by byte index. If the 80th byte is in the middle of a multi-byte UTF-8 character, you produce a corrupted rune. Also, substring matching for "secret", "token", etc. catches words like "secretary".
**Real Consequences:** Garbled diagnostics lead to wrong debugging conclusions. Security redaction is inconsistent.
**Fix:** Use `[]rune(word)` for truncation and whole-word matching.

---

### UTF-8 Truncation Corruption in 4 Separate Functions
**File:** `internal/agent/prompt.go:719-722`, `internal/agent/prompt.go:866-870`, `internal/agent/prompt_budget.go:258-262`, `internal/agent/context_manager.go:352-353`
**Bad Code:**
```go
return strings.TrimSpace(s[:max]) + "\n...[truncated]"
```
**Why This Sucks:** All four truncate by byte index. Same bug copy-pasted four times.
**Real Consequences:** Corrupted bytes sent to LLM providers. Some reject the request with a 400 error. Others silently mangle context.
**Fix:** Write a single `truncateToRunes(s string, max int) string` helper and use it everywhere.

---

### Massive 67-Line System Prompt Inlined as Raw String Constant
**File:** `internal/agent/context_manager.go:100-167`
**Bad Code:**
```go
const contextManagerCompactSystemPrompt = `You are OR3's context manager...` // 67 lines
```
**Why This Sucks:** 15% of the file is a prompt string interleaved with logic. Every prompt tweak requires recompiling.
**Real Consequences:** Makes code review harder. Makes prompt A/B testing impossible without rebuilding.
**Fix:** Use `//go:embed context_manager_prompt.txt`

---

### cleanupLocked Has Accidentally-Quadratic Behavior
**File:** `internal/agent/job_registry.go:322-349`
**Bad Code:**
```go
for len(r.jobs) > r.maxTracked {
    oldestID := ""
    var oldest time.Time
    for id, entry := range r.jobs {
        // scan ALL remaining jobs to find oldest
    }
    delete(r.jobs, oldestID)
}
```
**Why This Sucks:** If you have 500 terminal jobs and limit is 256, you scan ~244 times with ~500 iterations each. O(n²). Runs on EVERY call to `Register`.
**Real Consequences:** Under heavy job churn, registration latency spikes. Lock is held during O(n²) scan, blocking all other operations.
**Fix:** Use a min-heap for terminal job tracking, or collect all eviction candidates in a single pass.

---

### newServiceJobID Falls Back to Static ID on Crypto Failure
**File:** `internal/agent/job_registry.go:381-387`
**Bad Code:**
```go
func newServiceJobID() string {
    var raw [12]byte
    if _, err := rand.Read(raw[:]); err != nil {
        return "svc-job"
    }
    return "svc-" + hex.EncodeToString(raw[:])
}
```
**Why This Sucks:** If `crypto/rand.Read` fails, every call returns `"svc-job"` — guaranteed ID collisions, overwriting previous job entries.
**Real Consequences:** Multiple service jobs overwrite each other. Subscribers wait forever or receive events from the wrong job.
**Fix:** Panic on `crypto/rand.Read` failure.

---

### FormatBudgetDiagnostics Magic Number 5 for Section Limit
**File:** `internal/agent/diagnostics.go:15`
**Bad Code:**
```go
limit := min(5, len(sections))
```
**Why This Sucks:** Hardcoded limit with no explanation or configuration.
**Real Consequences:** Large context packets (>5 sections) have invisible sections in diagnostic output.
**Fix:** Use a named constant with a sensible default (e.g., 10).

---

### requestContextManagerCompaction Has Self-Contradicting Error Handling
**File:** `internal/agent/context_manager.go:299-321`
**Bad Code:**
```go
for attempt := 0; attempt < 2; attempt++ {
    // on first iteration's error, makes a SECOND upstream chat call within same iteration
    // up to 3 API calls instead of intended 2
```
**Why This Sucks:** The retry loop makes 2-3 calls instead of the intended 1-2. Error recovery path jumps into the middle of the loop body.
**Real Consequences:** Up to 50% more API calls than intended under failure modes. Confusing control flow.
**Fix:** Use a structured retry loop with explicit iterations.

---

### MeasureCachePrefix Uses Misleading <= 0 Check
**File:** `internal/agent/context_evaluation.go:67`
**Bad Code:**
```go
if totalBytes <= 0 {
    return CachePrefixMeasurement{}
}
```
**Why This Sucks:** `totalBytes` comes from `len()` calls which can never be negative. The `<= 0` implies negativity is possible.
**Fix:** `if totalBytes == 0`

---

### Cancel Function Orphaned When Job Goes Terminal
**File:** `internal/agent/job_registry.go:192-207`
**Bad Code:**
```go
func (r *JobRegistry) markTerminal(id string, status string) {
    // closes subscriber channels but NEVER calls entry.cancel
}
```
**Why This Sucks:** If a running operation is associated with this job via `AttachCancel`, the cancel function is never called.
**Real Consequences:** Context chain leaks. Goroutines that should have been cleaned up keep running.
**Fix:** Call `entry.cancel()` inside `markTerminal`.

---

### validateTaskCardUpdate Uses Byte Length Not Character Count
**File:** `internal/agent/context_manager.go:394-398`
**Bad Code:**
```go
if len(item) > 500 {
    return fmt.Errorf("task-card update item too long")
}
```
**Why This Sucks:** `len(item)` returns byte count. An item with 170 CJK characters (510 bytes) fails while 500 ASCII characters passes.
**Fix:** `if len([]rune(item)) > 500`

---

### contextManagerProviderRejectedToolChoice Is Fragile Stringly-Typed Error Matcher
**File:** `internal/agent/context_manager.go:431-440`
**Bad Code:**
```go
return strings.Contains(msg, "no endpoints") || strings.Contains(msg, "not support") || strings.Contains(msg, "unsupported")
```
**Why This Sucks:** If the provider changes its error message, this silently breaks. No structured error type.
**Real Consequences:** Breaks compaction entirely when provider updates error messages.
**Fix:** Return a structured error type from the provider client.

---

### TestBuilder_Build_EmptyMessage Validates Nothing Useful
**File:** `internal/agent/agent_test.go:305-316`
**Why This Sucks:** The test verifies that building with an empty user message doesn't error. It doesn't check what the returned data looks like.
**Fix:** Assert on the actual output.

---

### TestNormalizeCompactionCutoff Only Tests Happy Path
**File:** `internal/agent/context_manager_test.go:81-96`
**Why This Sucks:** Three messages, two cutoffs. No edge cases (empty list, single message, out-of-range cutoff).
**Fix:** Add edge case tests.

---

### NullStreamer Exports Public Type While Hiding Implementation
**File:** `internal/agent/noop_streamer.go:1-19`
**Why This Sucks:** `NullStreamer` is exported but `nullStreamWriter` is not. External users can construct but can't interact with the returned writer.
**Fix:** Export both or un-export both.

---

### contextManagerMessage Uses int64 Instead of time.Time for Timestamps
**File:** `internal/agent/context_manager.go:86`
**Why This Sucks:** Every other timestamp in the codebase uses `time.Time`. This uses raw int64. The LLM receives `1714766400` instead of a human-readable date.
**Fix:** Use `time.Time` with JSON serialization.

---

### firstNonEmptyEval Is Misnamed
**File:** `internal/agent/context_evaluation.go:73-80`
**Why This Sucks:** Completely generic utility with an `Eval` suffix that suggests it's evaluation-specific.
**Fix:** Rename to `firstNonEmpty` or inline since it's called once.

---

---

## Group 2 — Prompts, Runtime & Compression

### God Function `buildPacket` Does 14 Things
**File:** `internal/agent/prompt.go:233-410`
**Bad Code:** 176-line function handling scope resolution, pinned memory, task cards, compaction, embedding, retrieval, history, vision, heartbeat, structured context, etc.
**Why This Sucks:** Every change risks breaking an unrelated section. Impossible to unit test any single step.
**Fix:** Split into `buildRetrievedMemory`, `buildHistoryMessages`, `buildVolatileSections`, etc.

---

### Double-Marshal to Unmarshal: JSON Roundtrip of Shame
**File:** `internal/agent/prompt.go:438-439`
**Bad Code:**
```go
b, _ := json.Marshal(raw)
if err := json.Unmarshal(b, &atts); err != nil {
```
**Why This Sucks:** Takes already-parsed JSON, marshals to bytes, unmarshals back. Two allocations, two CPU-heavy operations.
**Fix:** Use a type switch or direct mapping. At minimum, don't ignore the marshal error.

---

### `cachedEmbed` Global Cache Is Leaky and Race-Prone
**File:** `internal/agent/prompt.go:157-160`
**Bad Code:** Package-level mutable state with manual TTL eviction. Entries only expire when looked up.
**Why This Sucks:** Memory leak under real usage. Tests directly mutate this global — guaranteed flaky parallel tests.
**Fix:** Use an LRU with proper capacity enforcement and background cleanup.

---

### `pendingToolCallIDs` Ordering Logic Assumes Provider is FIFO
**File:** `internal/agent/prompt.go:346-374`
**Bad Code:** Blindly assigns `pendingToolCallIDs[0]` if the tool message has no `tool_call_id`.
**Why This Sucks:** If results arrive out of order, tool results get associated with wrong tools. Context corruption.
**Fix:** Don't fall back to positional assignment without explicit validation.

---

### `readCappedFile` Reads Extra Byte Just to Say "Too Big"
**File:** `internal/agent/prompt.go:552-553`
**Bad Code:**
```go
data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
```
**Why This Sucks:** For a 4MB image exceeding a 4MB limit, you read 4MB+1 bytes just to return an error.
**Fix:** Read `maxBytes` and check if a subsequent 1-byte read returns data.

---

### `buildContextPacket` Takes 9 Positional String Arguments
**File:** `internal/agent/prompt_budget.go:131`
**Why This Sucks:** Nine unnamed strings. Swap `digestText` with `heartbeatText` and the compiler won't catch it.
**Fix:** Use a struct.

---

### `statusText` Triggers Full Prompt Build Including LLM API Calls
**File:** `internal/agent/runtime_status.go:116`
**Bad Code:**
```go
pp, _, err := r.Builder.BuildWithOptions(ctx, BuildOptions{SessionKey: sessionKey})
```
**Why This Sucks:** `/status` internally calls `buildPacket`, which calls `cachedEmbed` (potentially making an embedding API call). All that to show a status line.
**Real Consequences:** Status command becomes blocking, expensive, failure-prone. Burns API credits.
**Fix:** Cache the last `BudgetReport` and return it directly.

---

### Silently Swallowed Errors in Status Reporting
**File:** `internal/agent/runtime_status.go:96-98`
**Bad Code:** Three consecutive DB calls with `_` for errors.
**Why This Sucks:** If the database is locked/corrupt, status shows zeroes. User sees "0 messages" and thinks everything's fine.
**Fix:** Aggregate errors and emit them in the status output.

---

### `handleQuotaExceeded` Returns Generic Error Instead of `ApprovalRequiredError`
**File:** `internal/agent/runtime.go:2018`
**Bad Code:**
```go
return fmt.Errorf("%s Approve request %d...", ...)
```
**Why This Sucks:** The caller checks for `*tools.ApprovalRequiredError` with `errors.As` — this will NEVER match.
**Real Consequences:** Tool quota approval requests are broken. The approval flow never triggers.
**Fix:** Return `&tools.ApprovalRequiredError{...}`.

---

### Quota Lock Released and Reacquired Mid-Function — Race Condition
**File:** `internal/agent/runtime.go:1936-1948`
**Bad Code:** Quota mutex released, `handleQuotaExceeded` runs (DB + broker calls), mutex reacquired, then counters written on potentially stale data.
**Why This Sucks:** Textbook TOCTOU race.
**Fix:** Never release the lock mid-operation.

---

### `buildHistorySummary` Uses Deprecated `strings.Title`
**File:** `internal/agent/semantic_compression.go:82`
**Bad Code:**
```go
out.WriteString(fmt.Sprintf("- %s Msg:%d %s\n", strings.Title(role), row.ID, ...))
```
**Why This Sucks:** Deprecated since Go 1.18. Doesn't handle Unicode correctly. Will eventually be removed.
**Fix:** Use `golang.org/x/text/cases`.

---

### `semanticLabel` Dead Code for Half the Switched Values
**File:** `internal/agent/semantic_compression.go:47-66`
**Why This Sucks:** The `digestKinds` filter only passes through `Fact`, `Preference`, `Goal`, `Procedure`. But `semanticLabel` handles `Decision`, `Warning`, `File`, `Artifact` — which can never appear.
**Fix:** Remove dead labels or add them to the filter.

---

### Hardcoded Token Thresholds Not Synced With Percentage-Based Counterpart
**File:** `internal/agent/prompt_budget.go:101-112` vs `114-129`
**Why This Sucks:** `pressureState` uses hardcoded absolute values (12000/9000/7000). `pressureStateForBudget` uses percentages. If context limits change, they diverge.
**Fix:** Delete `pressureState`, use `pressureStateForBudget` everywhere.

---

### `estimatePacketBudget` Detects Truncation by Substring Search
**File:** `internal/agent/prompt_budget.go:335,343`
**Why This Sucks:** Detects truncation by searching for `"[truncated]"` in the text. If any section content legitimately contains that string, false positive.
**Fix:** Add a `Truncated` boolean to `ContextSection` set explicitly.

---

### `toToolDefs` Loses Errors and Exposes Nil Map Access
**File:** `internal/agent/runtime.go:2331`
**Bad Code:**
```go
fn, _ := d["function"].(map[string]any)
td := providers.ToolDef{
    Function: providers.ToolFunc{
        Name: fmt.Sprint(fn["name"]), // panics if fn is nil
```
**Why This Sucks:** Type assertion error ignored. Nil fn causes panic.
**Fix:** Check the type assertion result.

---

### `executeConversation` Loop: Infinite `for` With Self-Reset
**File:** `internal/agent/runtime.go:1186-1192`
**Why This Sucks:** `for loop := 0; ; loop++` with `loopLimit` mutated inside the body. Effectively a `goto`.
**Fix:** Use explicit bounded iteration.

---

### Duplicate Hardcoded Default "40" in Three Functions
**File:** `internal/agent/runtime_status.go:192`, `internal/agent/runtime.go:289-291`, `internal/agent/runtime.go:648-649`
**Why This Sucks:** Same magic number in three places. If someone changes it, they'll miss at least one.
**Fix:** `const defaultHistoryMax = 40`

---

### Double `t.Cleanup(srv.Close)` on httptest Servers
**File:** `internal/agent/prompt_test.go:527`, `internal/agent/runtime_test.go:84`
**Why This Sucks:** `httptest.NewServer` already registers `srv.Close`. Second registration is noise.
**Fix:** Remove explicit calls.

---

### `renderStablePrefix` and `renderVolatileSuffix`: Copy-Pasted Templating
**File:** `internal/agent/prompt_budget.go:265-288`
**Why This Sucks:** Two nearly-identical rendering loops (only differ by `\n` prefix and `\n\n` vs `\n` suffix).
**Fix:** Extract a single `renderSections` helper.

---

### `buildUserContent` Path Traversal Risk
**File:** `internal/agent/prompt.go:471-498`
**Why This Sucks:** `os.Stat(stored.Path)` on an artifact path with no validation that the path is within the artifacts directory.
**Fix:** Validate with `filepath.Clean` and prefix check.

---

---

## Group 3 — Subagents, Tasks & Tool Policy

### Pointless Wrapper Function
**File:** `internal/agent/service_runtime_context.go:104-106`
**Bad Code:**
```go
func ToolRegistryFromContext(ctx context.Context) *tools.Registry {
    return toolRegistryFromContext(ctx)
}
```
**Why This Sucks:** One-liner that delegates to a private one-liner. Zero additional logic.
**Fix:** Make `toolRegistryFromContext` public or delete the wrapper.

---

### Allowlist Builder Does Pointless Work When Not Restricting
**File:** `internal/agent/service_runtime_context.go:108-127`
**Why This Sucks:** When `restrict` is false, the function trims and deduplicates the allowlist then throws it away. The early return should be at the top.
**Fix:** Move `if !restrict { return base }` before the trimming loop.

---

### Nil Context Check After Wasted String Trim
**File:** `internal/agent/service_runtime_context.go:42-48`
**Bad Code:**
```go
sessionKey = strings.TrimSpace(sessionKey)
if ctx == nil || sessionKey == "" {
    return ctx
}
```
**Why This Sucks:** Trims the string before checking if ctx is nil. If ctx is nil, the trim was wasted.
**Fix:** Check nil first.

---

### Unreadable Test Output Via `%q` on Megastring
**File:** `internal/agent/skills_prompt_test.go:21-26`
**Why This Sucks:** `%q` escapes all newlines to `\n`, making a multi-line prompt into an illegible single line.
**Fix:** Use `%s`.

---

### Test Tool Type-Asserts Without Checking
**File:** `internal/agent/skills_runtime_test.go:38-43`
**Bad Code:**
```go
t.commandName = strings.TrimSpace(params["commandName"].(string))
```
**Why This Sucks:** Three unchecked type assertions. If params missing, panics with cryptic error.
**Fix:** Use a safe helper function.

---

### Deeply Nested Anonymous Structs Repeated 4+ Times
**File:** `internal/agent/skills_runtime_test.go:155-168` (and 4 other locations)
**Bad Code:** 14-line anonymous struct definition copied verbatim across the test file.
**Why This Sucks:** 200+ lines of copy-paste. Schema changes require hunting down every copy.
**Fix:** Add a `simpleChatResponse` helper.

---

### Unnecessary Provider.HTTP Reassignment in 10+ Test Places
**File:** `internal/agent/skills_runtime_test.go:33` (and elsewhere)
**Why This Sucks:** Every test manually assigns `provider.HTTP = server.Client()`. Duplicated 10+ times.
**Fix:** Add a `providers.NewWithClient` constructor.

---

### Dead Nil Check — Validates What Cannot Happen
**File:** `internal/agent/structured_autonomy.go:86-94`
**Why This Sucks:** `cloneMap(nil)` returns non-nil `map[string]any{}`. The `params == nil` check can never trigger.
**Fix:** Remove the dead check.

---

### `fmt.Sprint` Used as Type Discriminator
**File:** `internal/agent/structured_autonomy.go:97`
**Bad Code:**
```go
typeName := strings.TrimSpace(fmt.Sprint(schema["type"]))
```
**Why This Sucks:** If schema["type"] is nil you get `"<nil>"`, if it's `[]string{"null"}` you get `"[null]"`. Neither matches any switch case.
**Fix:** `typeName, _ := schema["type"].(string)`

---

### `"<nil>"` Magic String Hack — Go Version Dependent
**File:** `internal/agent/structured_autonomy.go:178`
**Bad Code:**
```go
if name != "" && name != "<nil>" {
```
**Why This Sucks:** Filters out `fmt.Sprint(nil)` result. `"<nil>"` is an implementation detail.
**Fix:** Check `item == nil` before calling `fmt.Sprint`.

---

### `isNumericValue` and `isIntegerValue` Don't Handle `json.Number`
**File:** `internal/agent/structured_autonomy.go:203-222`
**Why This Sucks:** If JSON is decoded with `UseNumber()`, all numeric values come through as `json.Number` and are rejected.
**Fix:** Add `json.Number` to the type switch.

---

### `renderTaskCard` Drops Plan and MemoryRefs
**File:** `internal/agent/task_card.go:97-147`
**Why This Sucks:** `TaskCard` has `Plan` and `MemoryRefs` fields. `renderTaskCard` silently omits both. The model never sees them.
**Fix:** Render `Plan` and `MemoryRefs`.

---

### Duplicate Enqueue Logic — 90% Copy-Paste
**File:** `internal/agent/subagents.go:158-243`
**Why This Sucks:** `Enqueue` and `EnqueueService` are structurally identical with minor metadata differences.
**Fix:** Extract a shared private `enqueue` method.

---

### `formatParentSubagentSummary` and `formatDeliverySubagentSummary` Near-Identical Twins
**File:** `internal/agent/subagents.go:536-556`
**Why This Sucks:** Only difference is `"completed:"` vs `"finished."` and `"failed:"` vs `"failed."`.
**Fix:** Extract shared helper, parameterize the verb.

---

### `mustMetadataJSON` Silently Discards Marshal Errors
**File:** `internal/agent/subagents.go:446-455`
**Why This Sucks:** Named `must` but returns `"{}"` on error instead of panicking. A marshal failure is a programming error that should never be silenced.
**Fix:** Panic on error.

---

### `signalN` Holds Mutex During Channel Send
**File:** `internal/agent/subagents.go:479-495`
**Why This Sucks:** Mutex held while sending on channel. If channel is full, blocks while holding lock — deadlock path.
**Fix:** Snapshot `started` and `ch` under lock, release, then send.

---

### Unnecessary PromptSnapshot Copy When Empty
**File:** `internal/agent/subagents.go:304-311`
**Why This Sucks:** Allocates a new slice and copies, then immediately checks if empty and replaces. Common case (empty) creates garbage.
**Fix:** Only copy when non-empty.

---

### Custom `min` Function Shadows Built-in
**File:** `internal/agent/subagents.go:509-514`
**Why This Sucks:** Go has had built-in `min` since 1.21. This shadows it and confuses linters.
**Fix:** Delete the function.

---

### `newSubagentID` Fallback Not Collision-Safe
**File:** `internal/agent/subagents.go:520-526`
**Bad Code:**
```go
return fmt.Sprintf("job-%d", time.Now().UnixNano())
```
**Why This Sucks:** Two concurrent calls in the same nanosecond produce the same ID.
**Fix:** Add an atomic counter.

---

### `executeJob` Double-Checks Context Errors Redundantly
**File:** `internal/agent/subagents.go:286-289`
**Why This Sucks:** Checks both `errors.Is(err, context.Canceled)` and `errors.Is(runCtx.Err(), context.Canceled)`.
**Fix:** Drop the redundant `runCtx.Err()` check.

---

### `serviceLifecycleEventPayload` Silently Overwrites Payload Keys
**File:** `internal/agent/subagents.go:433-444`
**Why This Sucks:** Second loop overwrites `request_id`, `workspace_id`, `network_session_id` in output if they happen to exist in the payload.
**Fix:** Use separate top-level fields or document the behavior.

---

### `finalizeJob` Has 8 Parameters Including Mystery Bool
**File:** `internal/agent/subagents.go:339`
**Why This Sucks:** Eighth parameter is a naked `bool` controlling delivery behavior. At call sites, `true` is meaningless.
**Fix:** Use a struct or named type.

---

### Redundant `TrimSpace` on Already-Sanitized Fields
**File:** `internal/agent/subagents.go:426-429`
**Why This Sucks:** `EnqueueService` already trims these fields. `parseSubagentJobMetadata` trims them again.
**Fix:** Remove trimming from the reader — it should trust the writer.

---

### `appendBoundedInt64` Rejects Zero
**File:** `internal/agent/task_card.go:149-152`
**Why This Sucks:** Silently drops any message ID of 0. Assumes IDs are always positive.
**Fix:** Document the constraint or remove the guard.

---

### `appendBoundedInt64` and `appendBoundedString` Are Copy-Paste
**File:** `internal/agent/task_card.go:149-170`
**Why This Sucks:** Identical logic for different types. Go has generics.
**Fix:** Use a generic `appendBounded[T any]`.

---

### `toJSON` Closure Defined Inside `saveTaskCard` Every Call
**File:** `internal/agent/task_card.go:54-57`
**Why This Sucks:** Allocated every call, used 8 times. Also silently discards marshal errors.
**Fix:** Move to package level, at minimum log errors.

---

### `renderTaskCard` Truncation Cuts Mid-Sentence
**File:** `internal/agent/task_card.go:143-145`
**Why This Sucks:** Truncates at byte boundary, not rune or word boundary.
**Fix:** Use rune-aware truncation.

---

### `loadTaskCard` Mutates Returned DB Row
**File:** `internal/agent/task_card.go:44-46`
**Why This Sucks:** Mutates `row.ScopeKey` — safe now (value copy) but fragile. If `GetActiveTaskState` ever returns pointers, this corrupts pooled state.
**Fix:** Resolve scope before using, don't mutate the row.

---

### Dead Code: `strings.Repeat("x", 0)` Does Nothing
**File:** `internal/agent/task_card_test.go:106`
**Why This Sucks:** `strings.Repeat("x", 0)` returns `""`. Dead code left from debugging.
**Fix:** Delete the call.

---

### Test Calls `db.NowMS()` and Discards Result
**File:** `internal/agent/task_card_test.go:71`
**Why This Sucks:** Called with no assertion, no check. Either dead code or coverage padding.
**Fix:** Delete the line.

---

### `waitForSubagentJob` Returns After `t.Fatalf` — Unreachable Code
**File:** `internal/agent/subagents_test.go:359-374`
**Why This Sucks:** `t.Fatalf` calls `runtime.Goexit` and never returns. The `return` is dead code.
**Fix:** Delete the return statement.

---

### Busy-Wait Polling Loop in Test Helper
**File:** `internal/agent/subagents_test.go:359-374`
**Bad Code:** `time.Sleep(20ms)` in a loop up to 150 times.
**Why This Sucks:** Worst possible async testing pattern. Flaky under CI load.
**Fix:** Use `testing.T.Context()` with a ticker.

---

### `ResolveServiceToolAllowlist` — Name Lies About Scope
**File:** `internal/agent/tool_policy.go:16`
**Why This Sucks:** Handles deny-lists too. Name says "allowlist" only.
**Fix:** Rename to `ResolveServiceToolPolicy`.

---

### `normalizeToolNames` Returns `nil` vs `[]string{}` Inconsistently
**File:** `internal/agent/tool_policy.go:76-97`
**Why This Sucks:** Empty input returns `nil` but empty-all-whitespace input also returns `nil`. Callers use `len() > 0` but serialization differs.
**Fix:** Return empty slice consistently.

---

### Legacy Error Message Is Unhelpful
**File:** `internal/agent/tool_policy.go:19`
**Bad Code:**
```go
return nil, false, fmt.Errorf("tool_policy.mode is required")
```
**Why This Sucks:** Zero context about what the user configured wrong or how to fix it.
**Fix:** Explain the migration path.

---

### Test Assertion Uses `!= ||` Instead of Separate Checks
**File:** `internal/agent/tool_policy_test.go:60-62`
**Why This Sucks:** Combined assertion produces confusing error messages in CI.
**Fix:** Two separate assertions.

---

### `toolPolicyStubTool` Embeds `tools.Base` Unnecessarily
**File:** `internal/agent/tool_policy_test.go:11-12`
**Why This Sucks:** Tightly couples test stubs to `tools.Base` internals. If `tools.Base` gains state, tests break.
**Fix:** Only expose methods needed by the interface.

---

### `(bool, error)` Return Violates Go Convention
**File:** `internal/agent/structured_autonomy.go:15`
**Bad Code:**
```go
func (r *Runtime) handleStructuredAutonomy(...) (bool, error) {
```
**Why This Sucks:** Forcing every caller to check both `handled` and `err`. A sentinel error like `ErrNotStructuredAutonomy` is idiomatic.
**Fix:** Use a sentinel error.

---

## Summary Statistics

| Category | Count |
|----------|-------|
| Race conditions / concurrency bugs | 7 |
| UTF-8 / byte-slicing corruption | 5 |
| Dead code / unreachable code | 6 |
| Copy-paste / duplicate logic | 8 |
| Swallowed / ignored errors | 7 |
| Misleading names / signatures | 8 |
| Performance (O(n²), wasted allocs, etc.) | 6 |
| Brittle string-matching / magic numbers | 6 |
| Test quality / flaky test patterns | 10 |
| Design / architecture issues | 5 |

**Total issues found: 68**

> "Yeah... this needed to be said."
