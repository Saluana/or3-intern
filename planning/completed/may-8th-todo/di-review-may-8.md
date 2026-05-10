# Code Review Report - May 8, 2025

Comprehensive neckbeard review of four core `or3-intern` subsystems:
`agent`, `tools`, `cron` + `cronrunner`, and `bus`.

---

## 1. `internal/bus` — 20 Issues

### Issue 1.1: "It's Not a Bus, It's a Hat on a Channel"

**File:** `internal/bus/bus.go:40-63`

```go
type Bus struct {
	ch chan Event
}
func New(buffer int) *Bus { ... }
func (b *Bus) Publish(ev Event) bool { ... }
func (b *Bus) Channel() <-chan Event { return b.ch }
```

**Why it's bad:** Wraps `make(chan Event)` in a struct. No subscribers, no routing, no topic filtering, no handler registration. The `Handler` type (line 37) is defined but never used.

**Consequences:** Developers assume multicast subscriptions exist, waste time building dispatcher logic on top.

**Fix:** Implement a real pub/sub registry with subscriber lists and fan-out, or delete this file and use a channel directly.

---

### Issue 1.2: Unused Handler Type — Interface Pollution

**File:** `internal/bus/bus.go:36-38`

```go
type Handler func(ctx context.Context, ev Event) error
```

**Why it's bad:** Defined and completely ignored. Nothing consumes it, nothing returns it. Forces a `context` import for dead code.

**Consequences:** Loses trust in package coherence. Prevents `go vet` from flagging the unused import.

**Fix:** Delete it.

---

### Issue 1.3: Context Import for No Reason

**File:** `internal/bus/bus.go:4-6`

```go
import (
	"context"
)
```

**Why it's bad:** Only usage is in the unused `Handler` type signature. Dead import.

**Fix:** Remove the import.

---

### Issue 1.4: Silent Data Loss in Publish

**File:** `internal/bus/bus.go:52-60`

```go
func (b *Bus) Publish(ev Event) bool {
	select {
	case b.ch <- ev:
		return true
	default:
		return false
	}
}
```

**Why it's bad:** When buffer is full, events are silently dropped. No log, no metrics, no backpressure signal.

**Consequences:** Production outage — critical event vanishes, logs show nothing, debugging at 3 AM.

**Fix:** Add dropped-event counter (expvar/prometheus), configurable `OnDrop` callback, or log at `Warn` level.

---

### Issue 1.5: No Close Method — Goroutine Leak

**File:** `internal/bus/bus.go:40-63`

**Why it's bad:** No `Close()` method. Any goroutine blocked on `<-b.Channel()` leaks forever.

**Consequences:** Goroutine leaks in long-running services, eventual memory exhaustion.

**Fix:** Add `Close() error` with `sync.Once` protection.

---

### Issue 1.6: Channel() Hands Out Shared Receive End

**File:** `internal/bus/bus.go:62-63`

```go
func (b *Bus) Channel() <-chan Event { return b.ch }
```

**Why it's bad:** Two goroutines ranging over this channel steal events from each other non-deterministically. A bus should broadcast, not randomly load-balance.

**Consequences:** Event handlers in different packages randomly miss messages.

**Fix:** Implement `Subscribe() (sub <-chan Event, unsubscribe func())` with per-subscriber fan-out.

---

### Issue 1.7: Magic Number 128

**File:** `internal/bus/bus.go:44-50`

```go
if buffer <= 0 {
	buffer = 128
}
```

**Why it's bad:** Arbitrary default, no constant, no env override. Silently "corrects" negative values instead of panicking/erroring.

**Fix:** Define `const defaultBufferSize = 128`. Panic on negative input.

---

### Issue 1.8: No Upper Bound Check

**File:** `internal/bus/bus.go:44-50`

**Why it's bad:** `New(1 << 30)` allocates gigabytes of RAM. No max buffer check.

**Consequences:** OOM from malformed config value.

**Fix:** Add `const maxBuffer = 1_000_000` ceiling with panic.

---

### Issue 1.9: EventType Is a String with Zero Validation

**File:** `internal/bus/bus.go:9,11-24`

```go
type EventType string
```

**Why it's bad:** Any random string compiles. No `IsValid()` method. Typos propagate silently.

**Fix:** Add `Valid() bool` with switch over known constants.

---

### Issue 1.10: Meta Map Allocates Even When Empty

**File:** `internal/bus/bus.go:27-34`

```go
Meta map[string]any
```

**Why it's bad:** Every `Event` that carries metadata forces a map allocation. `any` has zero type safety.

**Fix:** Use `Meta *map[string]any` so nil means "no metadata, no allocation."

---

### Issue 1.11: No Timestamp on Events

**File:** `internal/bus/bus.go:27-34`

**Why it's bad:** No way to reconstruct ordering, measure latency, or debug timing.

**Fix:** Add `Time time.Time` populated in `Publish` via `time.Now().UTC()`.

---

### Issue 1.12: Test Context Dead Code

**File:** `internal/bus/bus_test.go:93-94`

```go
_ = context.Background()
```

**Why it's bad:** Dead assignment to shut up compiler instead of removing import.

**Fix:** Delete line and `context` import.

---

### Issue 1.13: Test Ignores Publish Return Values

**File:** `internal/bus/bus_test.go:102-116`

```go
b.Publish(ev) // return value discarded
```

**Why it's bad:** If publish fails, test hangs 100ms instead of failing fast.

**Fix:** Check `if !b.Publish(ev) { t.Fatalf(...) }`.

---

### Issue 1.14: Tests Only Zero, Not Negative

**File:** `internal/bus/bus_test.go:9-21`

```go
b := New(0) // only tests zero, not -1, -42
```

**Fix:** Add sub-tests for `0`, `-1`, `-100`.

---

### Issue 1.15: Six Buses for Six Event Types

**File:** `internal/bus/bus_test.go:80-90`

**Why it's bad:** Allocates 6 buses when 1 with buffer 6 would suffice.

**Fix:** Use one bus, publish all types, drain all types.

---

### Issue 1.16: TestChannel_IsReadOnly Doesn't Test Read-Only

**File:** `internal/bus/bus_test.go:72-78`

**Why it's bad:** Name promises read-only verification; test only checks non-nil.

**Fix:** Delete this test. Return type `<-chan Event` is enforced by compiler.

---

### Issue 1.17: TestPublish_Success Blocks Forever on Failure

**File:** `internal/bus/bus_test.go:42-70`

**Why it's bad:** `<-b.Channel()` with no timeout — hangs on bug.

**Fix:** Wrap in `select` with `time.After`.

---

### Issue 1.18: TestPublish_Overflow Doesn't Verify Buffer State

**File:** `internal/bus/bus_test.go:119-128`

**Why it's bad:** Doesn't assert first two publishes succeeded before testing overflow.

**Fix:** Assert `ok` on first two publishes, then drain to confirm buffer content.

---

### Issue 1.19: No Concurrent Test Coverage

**File:** `internal/bus/bus_test.go`

**Why it's bad:** Zero coverage for concurrent publishers or multiple consumers.

**Fix:** Add `TestPublish_Concurrent` with `sync.WaitGroup`.

---

### Issue 1.20: Package Comment Is a Lie

**File:** `internal/bus/bus.go:1-2`

```go
// Package bus provides a small in-memory event bus for cross-service signaling.
```

**Why it's bad:** Single-channel queue in one process. Not "cross-service." Not a "bus."

**Fix:** Change to `// Package bus provides a single-process, single-channel event queue.`

---

## 2. `internal/agent` — 26 Issues

### Issue 2.1: Global Mutable Cache with sync.Mutex

**File:** `internal/agent/prompt.go:157-160, 875-920`

```go
var promptEmbedCache = struct {
	mu      sync.Mutex
	entries map[embedCacheKey]embedCacheEntry
}{entries: map[embedCacheKey]embedCacheEntry{}}
```

**Why it's bad:** Package-level mutable cache with mutex. Tests interfere with each other, prevents parallel execution, leaks memory. No TTL eviction — expired entries rot until count hits 128. `time.Now()` baked in makes it untestable.

**Consequences:** CI flakes. Heisenbugs that only happen on Tuesdays.

**Fix:** Make cache a field on `Builder`. Use `clock.Clock` for time. Add background janitor.

---

### Issue 2.2: truncateText Splits Multi-Byte Runes

**File:** `internal/agent/prompt.go:719-724`

```go
func truncateText(s string, max int) string {
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n...[truncated]"
	}
	return s
}
```

**Why it's bad:** `s[:max]` on bytes, not runes. Multi-byte UTF-8 characters get sliced in half.

**Consequences:** Corrupt UTF-8 in LLM prompts. Model speaks in tongues.

**Fix:** Convert to `[]rune` before slicing.

---

### Issue 2.3: oneLine Also Butchers UTF-8

**File:** `internal/agent/prompt.go:866-872`

```go
if max > 0 && len(s) > max {
	s = s[:max] + "..."
}
```

**Why it's bad:** Same byte-vs-rune slicing. `strings.Fields` allocates entire slice just to collapse whitespace.

**Fix:** Use `bytes.Buffer` or `strings.Builder`. Slice runes.

---

### Issue 2.4: Token Estimation is (len(s) + 3) / 4

**File:** `internal/agent/prompt_budget.go:379-385`

```go
func estimateTextTokens(s string) int {
	return (len(s) + 3) / 4
}
```

**Why it's bad:** Not how tokenization works. ~4 chars/token works for English, wildly wrong for code, Chinese, emoji. Budget decisions based on arithmetic a middle-schooler could do.

**Consequences:** Overestimate tokens, throw away important context. Or underestimate, API returns 400.

**Fix:** Use `tiktoken-go` or similar model-aware tokenizer.

---

### Issue 2.5: strings.Title is Deprecated (Removed in Go 1.21+)

**File:** `internal/agent/semantic_compression.go:81`

```go
out.WriteString(fmt.Sprintf("- %s Msg:%d %s\n", strings.Title(role), ...))
```

**Why it's bad:** Deprecated since Go 1.18. Won't compile on Go 1.21+.

**Consequences:** Build breaks on Go toolchain upgrade.

**Fix:** Use `cases.Title(language.English).String(role)` from `golang.org/x/text`.

---

### Issue 2.6: SQL Injection via extraWhere

**File:** `internal/agent/runtime_status.go:330-368`

```go
query += ` AND ` + extraWhere
```

**Why it's bad:** `extraWhere` concatenated raw to SQL. Callers pass hardcoded strings today, but the interface accepts arbitrary strings.

**Consequences:** Someone passes `extraWhere = "1=1; DROP TABLE memory_notes;"` and your production DB disappears.

**Fix:** Use proper query builder or validate against whitelist.

---

### Issue 2.7: Dead Code — sql.ErrNoRows After COUNT(*)

**File:** `internal/agent/runtime_status.go:365-367`

```go
if err == sql.ErrNoRows {
	return 0, nil
}
```

**Why it's bad:** `COUNT(*)` never returns `sql.ErrNoRows`. Unreachable dead code.

**Fix:** Delete it.

---

### Issue 2.8: Nil Receiver Checks on Value Receivers — Anti-Pattern

**File:** `internal/agent/runtime.go` (multiple: 102-104, 109-127, 468-470, 609-639, 668-670)

```go
func (r *Runtime) ApplyLiveModelConfig(cfg RuntimeModelConfig) {
	if r == nil { return }
	// ...
}
```

**Why it's bad:** In Go, a nil receiver should panic. These nil checks hide real bugs. Silent no-ops.

**Consequences:** Silent failures, empty configs, hours of debugging.

**Fix:** Remove all nil receiver checks. Fix the callers.

---

### Issue 2.9: executeConversation Mutates Input Slice

**File:** `internal/agent/runtime_execution.go:167, 227`

```go
messages = append(messages, providers.ChatMessage{...})
```

**Why it's bad:** `messages` is a slice header. Appending mutates underlying array if capacity allows. Callers reusing the slice see corrupted history.

**Consequences:** Subagent retries find tool results from previous attempt already injected.

**Fix:** Copy at top: `messages = append([]providers.ChatMessage(nil), messages...)`.

---

### Issue 2.10: narrateApprovalRequired Nil Check Too Late

**File:** `internal/agent/runtime_execution.go:253-278`

```go
func (r *Runtime) narrateApprovalRequired(ctx context.Context, messages []providers.ChatMessage) (string, bool) {
	modelCfg := r.CurrentModelConfig() // dereferences r BEFORE nil check
	if r == nil || modelCfg.Provider == nil { return "", false }
```

**Why it's bad:** `r.CurrentModelConfig()` dereferences `r` before the nil check. Performative security.

**Fix:** Check `r == nil` first.

---

### Issue 2.11: SHA-1 for Tool Call ID Generation in 2025

**File:** `internal/agent/tool_calls.go:135-140`

```go
sum := sha1.Sum([]byte(fmt.Sprintf("%d:%s:%s", index, name, canonicalJSON(args))))
```

**Why it's bad:** SHA-1 is cryptographically broken. Signals you stopped reading specs in 2010.

**Fix:** Replace with `fnv32a` or `xxhash`.

---

### Issue 2.12: cloneEventData is Shallow Copy Landmine

**File:** `internal/agent/job_registry.go:431-440`

```go
func cloneEventData(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in { out[key] = value }
	return out
}
```

**Why it's bad:** Copies map, not values. `[]string` or nested maps shared between copies.

**Consequences:** Mutations in one event leak to others. Data corruption.

**Fix:** Use `json.Marshal` + `json.Unmarshal` for deep copy.

---

### Issue 2.13: Publish Silently Drops Events on Slow Subscribers

**File:** `internal/agent/job_registry.go:154-159`

```go
for _, ch := range entry.subscribers {
	select {
	case ch <- event:
	default: // event DROPPED silently
	}
}
```

**Why it's bad:** `default` case drops events on full channel. No log, no retry, no error.

**Consequences:** UI stops updating, users think job is stuck, support tickets ensue.

**Fix:** Log dropped events. Use buffered channel large enough for bursts. Or block with timeout.

---

### Issue 2.14: redactDiagnostic — Broken Regex-Wannabe

**File:** `internal/agent/diagnostics.go:37-55`

```go
if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") {
	words[i] = "[redacted]"
	redactNext = true
}
```

**Why it's bad:** `"secret"` matches "secretly", "secretariat". Redacts innocent words. Splits on `Fields` and rejoins, destroying original whitespace.

**Fix:** Use regex word boundaries (`\bsecret\b`). Redact value, not next token. Preserve whitespace.

---

### Issue 2.15: PublicErrorCode Matches Errors by Substring

**File:** `internal/agent/error_codes.go:43-53`

```go
case strings.Contains(msg, "denied") || strings.Contains(msg, "policy"):
	return PublicErrorPolicy
```

**Why it's bad:** "permission denied" becomes `PublicErrorPolicy`. "not available" becomes `PublicErrorPolicy`. Error classification by grepping. Astrology.

**Fix:** Use `errors.Is` and custom error types with sentinel values.

---

### Issue 2.16: Double JSON Marshal/Unmarshal in attachmentsFromPayload

**File:** `internal/agent/prompt.go:438-440`

```go
b, _ := json.Marshal(raw)
var atts []artifacts.Attachment
json.Unmarshal(b, &atts)
```

**Why it's bad:** Serialize just to deserialize. Pure waste. Marshal error ignored.

**Fix:** Use type switch/type assertion on `raw`. Store attachments as typed structs.

---

### Issue 2.17: Double JSON Marshal/Unmarshal for tool_calls

**File:** `internal/agent/prompt.go:340-345`

```go
b, _ := json.Marshal(raw)
var tcs []providers.ToolCall
json.Unmarshal(b, &tcs)
```

**Why it's bad:** Same anti-pattern as 2.16. Unnecessary allocations on every prompt build.

**Fix:** Store tool calls as typed structs in DB or use `mapstructure`.

---

### Issue 2.18: turn() is a 75-Line God Function

**File:** `internal/agent/runtime.go:178-254`

**Why it's bad:** Handles commands, skills, structured autonomy, task cards, prompt building, execution, delivery, consolidation, pruning. No single responsibility.

**Fix:** Extract: `handleCommand`, `handleSkill`, `handleStructuredAutonomy`, `executeTurn`, `postTurnCleanup`.

---

### Issue 2.19: prompt.go and prompt_budget.go Violate DRY

**File:** `internal/agent/prompt.go:591-657`, `internal/agent/prompt_budget.go:131-198`

**Why it's bad:** Both build identical context sections (SOUL.md, Identity, AGENTS.md, TOOLS.md, Pinned Memory) with nearly identical logic. Will inevitably drift out of sync.

**Fix:** One source of truth for section definitions. Render from the budget packet.

---

### Issue 2.20: selectedToolGroups Uses Substring Matching for Intent

**File:** `internal/agent/runtime_access.go:104-138`

```go
if strings.Contains(lower, "write") || strings.Contains(lower, "edit") { ... }
```

**Why it's bad:** "I want to read a **write**-up" triggers write tools. "What **command**ments should I follow?" triggers exec tools.

**Consequences:** False positives granting dangerous tool access. False negatives denying safe access.

**Fix:** Use word-boundary regexes or actual lightweight classifier. Better: let the model request tools explicitly.

---

### Issue 2.21: Hardcoded Tool Names Scattered Across 5 Files

**Files:** `runtime_access.go`, `runtime_execution.go`, `runtime_quota.go`

```go
if tool.Name() == "send_message" && ...
if tool.Name() == "exec" || tool.Name() == "run_skill" { ...
case "exec", "run_skill", "run_skill_script":
case "write_file", "edit_file":
```

**Why it's bad:** Stringly-typed archaeology. Rename a tool and the compiler can't help.

**Consequences:** Refactor renames `exec` to `shell_exec`. Quotas break. Approvals bypass. Policy enforcement breaks.

**Fix:** Define constants for tool names or attach metadata and query that.

---

### Issue 2.22: validateStructuredValue — No Recursion Depth Limit

**File:** `internal/agent/structured_autonomy.go:96-165`

**Why it's bad:** Recursively validates JSON schema. Malicious 10,000-level nested objects overflow the stack. DoS vector.

**Fix:** Add `maxDepth` parameter, decrement on recursion, return error at zero.

---

### Issue 2.23: boundedContext Escapes Parent Cancellation

**File:** `internal/agent/subagents.go:497-507`

```go
base = context.WithoutCancel(base)
```

**Why it's bad:** Subagent finalization outlives user request cancellation. Lifecycle boundaries are wrong.

**Consequences:** User cancels request but subagent keeps writing to DB. Resource leaks.

**Fix:** Use `context.WithTimeout(base, timeout)` directly. Use `sync.WaitGroup` for graceful shutdown.

---

### Issue 2.24: Test Races on Non-Atomic callCount

**File:** `internal/agent/runtime_test.go` (multiple: 244, 299, 345, 403, 467, 530)

```go
callCount := 0
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	callCount++ // DATA RACE
}))
```

**Why it's bad:** Plain `int` mutated by HTTP handler goroutine, read by test goroutine. `go test -race` should scream.

**Fix:** Use `atomic.Int32` or `atomic.Int64`.

---

### Issue 2.25: repeatedEvalText — O(n^2) String Concat in Benchmark

**File:** `internal/agent/context_evaluation_test.go:75-81`

```go
func repeatedEvalText(word string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += word + " " // O(n^2)
	}
	return out
}
```

**Why it's bad:** String concatenation in benchmark helper skews benchmark results.

**Fix:** Use `strings.Repeat(word+" ", count)`.

---

### Issue 2.26: containsEval and indexEval Reimplement Standard Library

**File:** `internal/agent/context_evaluation_test.go:83-94`

```go
func indexEval(text, want string) int {
	for i := 0; i+len(want) <= len(text); i++ { ... }
}
```

**Why it's bad:** Reimplements `strings.Contains` and `strings.Index` for no reason.

**Fix:** Use `strings.Contains(text, want)`.

---

## 3. `internal/tools` — 50 Issues

### Issue 3.1: Nil Tool Returns "Safe" Capability — Security Lie

**File:** `internal/tools/tools.go:45-71`

```go
func ToolCapabilityForContext(ctx context.Context, t Tool, params map[string]any) CapabilityLevel {
	if t == nil { return CapabilitySafe }
```

**Why it's bad:** Programming error silently downgraded to "safe," bypassing approval brokers and guards.

**Fix:** Return `CapabilityPrivileged` (fail closed) or panic.

---

### Issue 3.2: Registry — No Mutex, Data Races

**File:** `internal/tools/registry.go:11-13, 43-52`

```go
type Registry struct {
	tools map[string]Tool
}
func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) Tool { return r.tools[name] }
```

**Why it's bad:** No `sync.RWMutex`. Concurrent Register/Get/Names causes map corruption.

**Fix:** Add mutex and use in every method.

---

### Issue 3.3: inferToolMetadata — Giant Hardcoded Switch

**File:** `internal/tools/registry.go:110-136`

**Why it's bad:** Hardcodes names of tools from other files. Add a new tool, must update this. Calls `ToolCapability(nil, nil)` (bogus safe level).

**Fix:** Use `MetadataReporter` interface. Default to conservative group instead of guessing.

---

### Issue 3.4: fmt.Sprint + "<nil>" String Checks Everywhere

**File:** `internal/tools/exec.go:77-92, 124-143, 174, 600+`

```go
program := strings.TrimSpace(fmt.Sprint(params["program"]))
if program == "<nil>" { program = "" }
```

**Why it's bad:** Serializes potentially-nil interface values, then string-compares to `"<nil>"`. Not how type assertions work. Allocates garbage strings.

**Fix:** Write a real parameter helper: `func stringParam(params map[string]any, key string) (string, bool) { ... }`

---

### Issue 3.5: Capability Logic Duplicated in Two Methods

**File:** `internal/tools/exec.go:77-92`

```go
func (t *ExecTool) CapabilityForParams(params map[string]any) CapabilityLevel {
	// ... fmt.Sprint horror ...
}
func (t *ExecTool) CapabilityForContextParams(ctx context.Context, params map[string]any) CapabilityLevel {
	// ... identical fmt.Sprint horror, plus one extra check ...
}
```

**Why it's bad:** Copy-pasted between two methods. Fix one bug, forget the other.

**Fix:** Have context version call params version, then adjust.

---

### Issue 3.6: Hand-Rolled Shell Parser Can't Handle Escapes

**File:** `internal/tools/exec.go:321-367`

```go
func splitDirectCommand(command string) ([]string, error) {
	// manual loop over runes
}
```

**Why it's bad:** Reimplements subset of POSIX word splitting. Mishandles `\"` inside quotes. Security boundary on regex-tier logic.

**Fix:** Use proper shell lexer or reject non-trivial whitespace-only commands.

---

### Issue 3.7: PreviewString Mangles UTF-8 by Byte Truncation

**File:** `internal/tools/result.go:82-88`

```go
return s[:maxBytes] + "\n...[preview truncated]", true
```

**Why it's bad:** `len(s)` is bytes. `s[:maxBytes]` slices at byte offset. Multi-byte UTF-8 produces invalid output.

**Fix:** Truncate by rune index.

---

### Issue 3.8: io.ReadAll Errors Silently Ignored in Web Tools

**File:** `internal/tools/web.go:176, 447`

```go
body, _ := io.ReadAll(io.LimitReader(resp.Body, readLimit))
```

**Why it's bad:** Connection drop yields partial response treated as success. Data integrity silently violated.

**Fix:** Check the error.

---

### Issue 3.9: PlaywrightRenderer Writes Temp File on Every Render

**File:** `internal/tools/web.go:283-291`

```go
dir, _ := os.MkdirTemp("", "or3-web-render-*")
os.WriteFile(filepath.Join(dir, "render.js"), []byte(playwrightRenderScript), 0o600)
```

**Why it's bad:** Disk I/O for every single render call. SSD wear, read-only filesystem failures.

**Fix:** Use embedded file or write once at startup.

---

### Issue 3.10: WebSearch URL Built with String Concat

**File:** `internal/tools/web.go:423`

```go
endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(q) + "&count=" + fmt.Sprint(count)
```

**Why it's bad:** Addition of new params introduces injection. `url.Values` exists for this.

**Fix:** Use `url.Values` and `vals.Encode()`.

---

### Issue 3.11: WebFetchMarkdown — Copy-Paste of WebFetch

**File:** `internal/tools/web_markdown.go:55-131`

**Why it's bad:** Duplicates URL validation, request context, HTTP client setup, redirect handling, host policy. Will drift out of sync.

**Fix:** Extract common HTTP fetching into shared internal function.

---

### Issue 3.12: decodeHTML Ignores Charset Detection Errors

**File:** `internal/tools/html_converter.go:156-167`

```go
encoding, _, _ := charset.DetermineEncoding(raw, requestedCharset)
```

**Why it's bad:** Error from `charset.DetermineEncoding` discarded. Blinds use of potentially nil/wrong encoding.

**Fix:** Check error. Fall back to UTF-8 explicitly.

---

### Issue 3.13: CleanHTMLForLLM Double-Parses and Double-Walks HTML

**File:** `internal/tools/html_converter.go:209-244, 275-308`

**Why it's bad:** Parse with goquery (builds full DOM), then manually walk `html.Node` tree in second pass. Map lookup per element node. Reimplements what goquery already does.

**Fix:** Use goquery's built-in text extraction in one pass.

---

### Issue 3.14: openSafeRead Has TOCTOU Race

**File:** `internal/tools/files.go:102-117`

```go
f, _ := os.Open(path)
info, _ := f.Stat()
t.validateOpenedPath(path, info)
```

**Why it's bad:** Between `os.Open` and `f.Stat()`, attacker can swap the file. Validate the new path against old fd info.

**Consequences:** Symlink attacks reading files outside allowed root.

**Fix:** Use `f.Stat()` then `os.Stat(path)` and compare `os.SameFile` before returning.

---

### Issue 3.15: validateOpenedPathUnchanged Also Racy

**File:** `internal/tools/files.go:158-171`

**Why it's bad:** Between `f.Stat()` and `os.Stat(resolved)`, file can change again. Not atomic.

**Fix:** Use file descriptors and fstat exclusively. Do not re-resolve paths after opening.

---

### Issue 3.16: grepFile Advertises Regex But Uses Substring

**File:** `internal/tools/files.go:579-622`

```go
if !strings.Contains(text, pattern) { continue }
```

**Why it's bad:** Schema says "Substring or regex pattern" but `strings.Contains` is literal only.

**Fix:** Compile with `regexp.Compile`, fall back to substring if it fails.

---

### Issue 3.17: EditFile Loads Entire Files Into Memory — No Size Limit

**File:** `internal/tools/files.go:401-443`

```go
b, _ := io.ReadAll(in)
s := string(b)
```

**Why it's bad:** 10 GB log file = 10 GB RAM allocation. No size check, no streaming.

**Consequences:** OOM kills. DoS via large files.

**Fix:** Reject files above configurable size limit. Use streaming line-based editor.

---

### Issue 3.18: commandWithSandbox Returns (nil, nil)

**File:** `internal/tools/sandbox.go:19-25`

```go
if !cfg.Enabled {
	return nil, nil
}
```

**Why it's bad:** Forces every caller to check `if command == nil`. Violates Go idiom.

**Fix:** Return sentinel error or check `cfg.Enabled` before calling.

---

### Issue 3.19: appendSandboxBind Silently Ignores Missing Sources

**File:** `internal/tools/sandbox.go:90-96`

```go
if _, err := os.Lstat(src); err != nil {
	return
}
```

**Why it's bad:** Required bind sources missing — silently skipped. Sandbox launches missing critical libraries.

**Fix:** Return error for required binds.

---

### Issue 3.20: canPersistSkillRunApprovalLink Compares *sql.DB Pointers

**File:** `internal/tools/skill_run.go:660-665`

```go
return t.DB.SQL == t.ApprovalBroker.DB.SQL
```

**Why it's bad:** Pointer comparison. Two connections from same pool returns false. Read replicas return false.

**Fix:** Compare `DatabaseID` string or host name.

---

### Issue 3.21: ApprovalBroker.SignKey Reused as Encryption Key

**File:** `internal/tools/skill_run.go:596-639`

**Why it's bad:** Signing key reused as AES-GCM encryption master key. Cryptographic key reuse is a vulnerability.

**Fix:** Dedicated encryption key. Derive both subkeys from master secret using HKDF with distinct contexts.

---

### Issue 3.22: authorizeAndRunSkill — No Timeout on Approval Evaluation

**File:** `internal/tools/skill_run.go:285-337`

**Why it's bad:** `EvaluateSkillExec` called with parent ctx, no dedicated timeout. Slow/remote DB hangs forever.

**Fix:** `context.WithTimeout(ctx, 30*time.Second)` for evaluation.

---

### Issue 3.23: runPreparedSkillRun — Plan Permanently Stuck in "Running"

**File:** `internal/tools/skill_run.go:339-406`

**Why it's bad:** Claim plan, set running, execute command. Crash between claim and completion = plan stuck running forever.

**Fix:** Heartbeat updating `UpdatedAt` during execution, or lease with automatic expiry.

---

### Issue 3.24: readOptionalString Duplicated from stringParam

**File:** `internal/tools/spawn.go:96-105`, `internal/tools/memory.go:246-255`

**Why it's bad:** Identical function, different name, different file. Copy-paste landfill.

**Fix:** Delete one. Use typed parameter parser in `tools.go`.

---

### Issue 3.25: optionalBool Accepts Strings for Boolean Parameter

**File:** `internal/tools/message.go:112-131`

```go
case "yes": return true, nil
```

**Why it's bad:** Schema says `type: boolean`. Parser accepts `"yes"`, `"1"`. Runtime doesn't match contract.

**Fix:** Reject strings. Parse as boolean only.

---

### Issue 3.26: ContextWith* Functions Encourage Nil Contexts

**File:** `internal/tools/context.go:54-62`

```go
if ctx == nil { ctx = context.Background() }
```

**Why it's bad:** Go says "Do not pass a nil Context." Silently replacing with Background trains codebase to be sloppy.

**Fix:** Remove nil check. Let it panic. Or return error.

---

### Issue 3.27: ActiveProfileFromContext Returns Fat Struct by Value

**File:** `internal/tools/context.go:276-282`

**Why it's bad:** Contains maps and slices. Returns by value causing allocation on every call.

**Fix:** Return `*ActiveProfile`.

---

### Issue 3.28: toolFailureAdvice — 400-Line Monolith

**File:** `internal/tools/result.go:237-399`

**Why it's bad:** Giant switch mapping error substrings to advice. Violates Open/Closed. Fragile substring matching.

**Fix:** Each tool implements `AdviceProvider` interface. Or use structured error codes.

---

### Issue 3.29: CronTool Marshal-Then-Unmarshal Dance

**File:** `internal/tools/cron.go:69-86`

```go
b, _ := json.Marshal(raw)
json.Unmarshal(b, &j)
```

**Why it's bad:** `map[string]any` → JSON → struct. Wasteful round-trip.

**Fix:** Use typed decoder or validate map manually.

---

### Issue 3.30: CronTool Stringifies Any Type for Action

**File:** `internal/tools/cron.go:38, 96`

```go
act := strings.TrimSpace(fmt.Sprint(params["action"]))
```

**Why it's bad:** `[]string{"rm","-rf"}` becomes `"[rm -rf]"` then "unknown action." Confusing error.

**Fix:** Type-assert to string immediately.

---

### Issue 3.31: ReadSkill.Execute Ignores Context Cancellation

**File:** `internal/tools/skill.go:32-33`

```go
func (t *ReadSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
```

**Why it's bad:** User cancels request, skill read keeps going. Especially bad if I/O is involved.

**Fix:** Pass `ctx` to `skills.LoadBody` or check `ctx.Err()`.

---

### Issue 3.32: scriptCommand Hardcodes Only .sh and .py

**File:** `internal/tools/skill_exec.go:118-135`

**Why it's bad:** "Generic" skill execution only supports Bash and Python. No Perl, Ruby, Node, compiled binaries.

**Fix:** Support shebang-line parser. Or allow skill manifests to declare interpreter.

---

### Issue 3.33: skillCommandHash Reads Entire File Into Memory

**File:** `internal/tools/skill_exec.go:152-164`

```go
blob, readErr := os.ReadFile(scriptPath)
sum := sha256.Sum256(blob)
```

**Why it's bad:** Loads entire script just to hash. Large scripts cause memory spikes.

**Fix:** Use `sha256.New()` with `io.Copy` from `os.Open`.

---

### Issue 3.34: FilterSuspiciousExternalTools Only Scans MCP Tools

**File:** `internal/tools/metadata_scanner.go:58-96`

```go
if !hasMetadataGroup(meta.Groups, ToolGroupMCP) { continue }
```

**Why it's bad:** Security scanner generic, but only runs on MCP-grouped tools. Built-in tools immune? Security theater.

**Fix:** Scan ALL tools.

---

### Issue 3.35: splitter/main.go Has Hardcoded Line Ranges

**File:** `internal/tools/splitter/main.go:127-156`

```go
case start >= 566 && start <= 1199: return "service_files.go"
```

**Why it's bad:** Most brittle code ever. Only works on one specific file. Hardcoded line numbers.

**Fix:** Delete this file. Use `go/parser` to analyze imports.

---

### Issue 3.36: WriteFile Does Redundant os.Stat Calls

**File:** `internal/tools/files.go:354-375`

**Why it's bad:** `existingFileMode` → `os.Stat`. `openSafeWrite` → `f.Stat` → `validateOpenedWritePath` → more resolution. 3+ syscalls.

**Fix:** Stat once, pass mode down.

---

### Issue 3.37: WebFetch HTTP Client Shallow Copy Dangerous

**File:** `internal/tools/web.go:133-139`

```go
copyClient := *t.HTTP
client = &copyClient
```

**Why it's bad:** Shallow copy. Transport shared (counters, pools, auth tokens). Mutating copied client affects shared state.

**Fix:** Build new client instead of copying.

---

### Issue 3.38: isBlockedFetchAddr Redundantly Checks AWS Metadata IP

**File:** `internal/tools/web.go:367-375`

```go
return addr.String() == "169.254.169.254"
```

**Why it's bad:** `169.254.169.254` is link-local unicast. `IsLinkLocalUnicast()` already true. Dead code.

**Fix:** Delete the last line.

---

### Issue 3.39: PreviewString Corrupts UTF-8 — Used Everywhere

**File:** `internal/tools/result.go:82-88`

**Why it's bad (reiteration):** Used by `readPreview`, `grepFile`, `outlineFile`, `readLineRange`, `web_fetch`, `skill_run`. One bad utility corrupts output across entire package.

**Fix:** Fix once in `result.go`, every consumer fixed.

---

### Issue 3.40: TestSendMessage Hardcodes Internal Constant

**File:** `internal/tools/message_test.go:175-178`

```go
paths, ok := gotMeta["media_paths"].([]string)
```

**Why it's bad:** Production uses typed constant `rootchannels.MetaMediaPaths`, test hardcodes string.

**Fix:** Use `gotMeta[rootchannels.MetaMediaPaths].([]string)` in test.

---

### Issue 3.41: boundedPositiveInt Only Handles float64, Not int

**File:** `internal/tools/memory.go:257-269`

```go
if v, ok := raw.(float64); ok && int(v) > 0 { ... }
```

**Why it's bad:** Native `int` falls through to fallback. Generic Go utility should handle `int`, `int64`, `json.Number`.

**Fix:** Add cases for `int`, `int64`, `json.Number`.

---

### Issue 3.42: validateMediaPaths Does Redundant Path Resolution

**File:** `internal/tools/message.go:149-157`

```go
p, _ := filepath.Abs(strings.TrimSpace(item))
p, _ = CanonicalizePath(p)
info, _ := os.Stat(p)
```

**Why it's bad:** `filepath.Abs` + `CanonicalizePath` (with its own lstat loop) + `os.Stat`. Multiple syscalls.

**Fix:** `CanonicalizePath` should suffice, then single `os.Stat`.

---

### Issue 3.43: makeTestBroker Redundant Wrapper

**File:** `internal/tools/exec_test.go:423-451`

```go
func makeTestBroker(t *testing.T, mode config.ApprovalMode) *approval.Broker {
	return makeTestBrokerWithDB(t, mode, nil)
}
```

**Why it's bad:** Thin wrapper passing nil. Nil check inside triggers side effects (DB creation).

**Fix:** One helper that always requires DB. Or makeTestBroker explicitly creates DB and passes it.

---

### Issue 3.44: TestWriteFile_Mkdirs Asserts Exact Mode Ignoring Umask

**File:** `internal/tools/files_test.go:312-320`

```go
if info.Mode().Perm() != 0o700 { t.Fatalf(...) }
if fileInfo.Mode().Perm() != 0o600 { t.Fatalf(...) }
```

**Why it's bad:** Umask of 0022 makes actual modes 0755/0644. Test fails on normal systems.

**Fix:** Assert minimum bits (`mode.Perm()&0o700 == 0o700`) or set umask(0).

---

### Issue 3.45: ListDir Double-Truncates and Has Redundant Break

**File:** `internal/tools/files.go:474-504`

**Why it's bad:** Read `max+1`, slice to `max`, sort, then `if len(out) >= max { break }`. Break is redundant after slice.

**Fix:** Remove redundant break.

---

### Issue 3.46: memoryScopeFromParams Misuses stringParam

**File:** `internal/tools/memory.go:239-244`

**Why it's bad:** `stringParam` trims whitespace implicitly. Hidden behavior in parameter parsing.

**Fix:** Don't trim in generic extractors. Only trim where schema explicitly allows.

---

### Issue 3.47: resolveExecutable Calls os.Getwd in Loop

**File:** `internal/tools/exec.go:423-447`

**Why it's bad:** Called from `allowedProgram` loop. `os.Getwd()` syscall if no cwd. Done per iteration.

**Fix:** Resolve cwd once at top level, pass as required argument.

---

### Issue 3.48: TestExecServiceCommandPassesGuardedRegistryCeiling in exec_test.go

**File:** `internal/tools/exec_test.go:753-773`

**Why it's bad:** Tests `Registry.ExecuteParams` and `ToolGuard` context. Nothing to do with `ExecTool`. Lazy file placement.

**Fix:** Move to `registry_test.go`.

---

### Issue 3.49: htmlTextSeparatorTags Global Map in Hot Loop

**File:** `internal/tools/html_converter.go:267-273`

```go
if _, ok := htmlTextSeparatorTags[strings.ToLower(node.Data)]; ok { ... }
```

**Why it's bad:** Every HTML element: allocate `strings.ToLower` + map lookup. Switch would be faster.

**Fix:** Pre-lowercase map keys and compare directly, or use switch.

---

### Issue 3.50: TestListDir_OK Asserts Exact Sorting — Brittle

**File:** `internal/tools/files_test.go:475-495`

**Why it's bad:** Asserts exact array content. Implementation detail test. Case-insensitive change breaks it.

**Fix:** Assert directories before files and sorted within groups, not exact array.

---

## 4. `internal/cron` + `internal/cronrunner` — 40 Issues

### Issue 4.1: json.MarshalIndent Error Discarded — Data Destruction

**File:** `internal/cron/cron.go:153-159`

```go
b, _ := json.MarshalIndent(st, "", "  ")
return os.WriteFile(s.path, b, 0o644)
```

**Why it's bad:** Marshal error discarded. If fail, writes zero-length bytes, truncating user's cron config. Returns nil.

**Consequences:** Corrupted on-disk state. Next restart loads empty job list. All jobs vanish.

**Fix:** Check error. Write atomically (see 4.2).

---

### Issue 4.2: Writing Directly to Live File — No Atomicity

**File:** `internal/cron/cron.go:153-159`

**Why it's bad:** `os.WriteFile` truncates before write. Crash mid-write = partially-written JSON. No journal, no temp file, no atomic rename.

**Fix:** Write to tmp file, then `os.Rename`.

---

### Issue 4.3: Stop() Deadlock — Holding Mutex While Waiting for Cron

**File:** `internal/cron/cron.go:198-211`

```go
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		ctx := s.c.Stop()
		<-ctx.Done() // waits for callbacks to finish
	}
```

**Why it's bad:** Callbacks try to acquire `s.mu` in `runJobByID`. `Stop` already holds it. Textbook deadlock.

**Consequences:** Graceful shutdown hangs forever. K8s sends SIGKILL after grace period, corrupting state.

**Fix:** Copy data under lock, release, then stop external resources.

---

### Issue 4.4: runJobByID TOCTOU Race

**File:** `internal/cron/cron.go:392-454`

```go
s.mu.Lock()
st, _ := s.load()
s.mu.Unlock()
// ... runner executes with NO lock ...
s.mu.Lock()
st2, loadErr := s.load()
// updates st2 based on stale assumptions
```

**Why it's bad:** Between unlock and re-lock, another goroutine could have removed/updated the job. State updated based on file that changed.

**Fix:** Keep job definition in memory. Do not reload entire file after runner returns.

---

### Issue 4.5: Runner Succeeds but State Load Fails — Silently Pretend

**File:** `internal/cron/cron.go:410-412`

```go
st2, loadErr := s.load()
if loadErr != nil {
	return true, err // err from runner, loadErr DISCARDED
}
```

**Why it's bad:** Runner succeeded, state not persisted. caller thinks everything is fine. loadErr silently discarded.

**Fix:** Return `fmt.Errorf("runner succeeded but state reload failed: %w", loadErr)`.

---

### Issue 4.6: Timezone Field — Decorative JSON Ornament

**File:** `internal/cron/cron.go:520-532`

**Why it's bad:** `CronSchedule.TZ` parsed and validated on input. But `armJobLocked` completely ignores it when registering with `robfig/cron`. Scheduler runs in default location while `nextRunMS` thinks it's `America/New_York`.

**Consequences:** Jobs fire at wrong time. User sets 9 AM EST, runs at 2 PM UTC.

**Fix:** Use `cron.WithLocation(loc)` or prefix spec with `CRON_TZ=<tz>`.

---

### Issue 4.7: Sub-Second Intervals Silently Changed to 60 Seconds

**File:** `internal/cron/cron.go:504-509`

```go
sec := int64(job.Schedule.EveryMS / 1000)
if sec <= 0 { sec = 60 }
```

**Why it's bad:** User requests 500ms. Integer division → 0. "Helpfully" defaulted to 60 seconds. No error.

**Fix:** Reject `EveryMS < 1000` in validation. Or use `time.Duration(EveryMS) * time.Millisecond`.

---

### Issue 4.8: KindAt Check-Then-Act Nanosecond Race

**File:** `internal/cron/cron.go:493-503`

```go
if time.Now().After(at) { return }
s.timers[job.ID] = time.AfterFunc(time.Until(at), func() { ... })
```

**Why it's bad:** Two `time.Now()` calls — clock ticks between them. `time.Until(at)` could be negative.

**Consequences:** One-shot jobs for "right now" silently dropped.

**Fix:** Compute `dur := time.Until(at)` once. Fire immediately if `<= 0`, or return clear error.

---

### Issue 4.9: Timezone Errors Silently Ignored

**File:** `internal/cron/cron.go:644-648`

```go
if loc, err := time.LoadLocation(schedule.TZ); err == nil {
	now = now.In(loc)
}
```

**Why it's bad:** Error silently swallowed. Returns bogus timestamp for cron expressions in invalid timezones.

**Fix:** Validation already guarantees it's valid. If handling errors, return or panic, don't silently ignore.

---

### Issue 4.10: cronParser() — Factory That Never Needed to Exist

**File:** `internal/cron/cron.go:659-661`

```go
func cronParser() cron.Parser {
	return cron.NewParser(cron.SecondOptional | cron.Minute | ...)
}
```

**Why it's bad:** New parser allocated every call. Called on every job add, update, enable, status. Pure garbage.

**Fix:** Package-level `var parser = cron.NewParser(...)`. Delete function.

---

### Issue 4.11: crypto/rand Failure Fallback to Clock — Predictable IDs

**File:** `internal/cron/cron.go:663-669`

```go
if _, err := rand.Read(b[:]); err != nil {
	return uint64(time.Now().UnixNano())
}
```

**Why it's bad:** `crypto/rand` failing is catastrophic. Falling back to timestamp makes IDs predictable.

**Consequences:** Brute-force attacks on predictable IDs. ID collisions in tight loops.

**Fix:** Propagate the error.

---

### Issue 4.12: Reinventing filepath.Dir() — Buggy

**File:** `internal/cron/cron.go:161-170`

```go
func filepathDir(p string) string {
	// custom logic with slash detection
}
```

**Why it's bad:** Go has `path/filepath.Dir`. Handles edge cases you forgot. Your version returns `"."` for `/config.json` (wrong).

**Fix:** Delete. Use `filepath.Dir(s.path)`.

---

### Issue 4.13: sync.Mutex Instead of sync.RWMutex

**File:** `internal/cron/cron.go:118-125`

**Why it's bad:** `List()` and `Status()` are read-only but contend with writes under exclusive lock. Every status check blocks every mutation.

**Fix:** Use `sync.RWMutex`. RLock for read paths.

---

### Issue 4.14: JSON File as Database — O(n) Write Amplification

**File:** `internal/cron/cron.go` (every mutation)

**Why it's bad:** Every Add/Update/Enable/Remove rewrites entire JSON file. 1000 jobs, update one = rewrite all 1000. SSD write amplification.

**Fix:** Use SQLite, BoltDB, or any actual database.

---

### Issue 4.15: Remove() Allocates Even When Nothing to Remove

**File:** `internal/cron/cron.go:361-383`

```go
n := make([]CronJob, 0, len(st.Jobs))
for _, j := range st.Jobs { ... }
```

**Why it's bad:** Allocates new slice with full capacity before checking if job exists. Not found = still allocates and saves identical copy.

**Fix:** Check if found first. Only allocate if found.

---

### Issue 4.16: Three-Return-Value Abomination

**File:** `internal/cron/cron.go:281, 323`

```go
func (s *Service) Update(id string, job CronJob) (bool, CronJob, error)
func (s *Service) SetEnabled(id string, enabled bool) (bool, CronJob, error)
```

**Why it's bad:** `(bool, CronJob, error)`. Callers juggle three values. Inconsistent semantics.

**Fix:** Return `(CronJob, error)`. Use sentinel `ErrNotFound`.

---

### Issue 4.17: NormalizePayload's Pointless Meta Copy

**File:** `internal/cron/cron.go:560-566`

```go
if run.Meta != nil {
	meta := make(map[string]any, len(run.Meta))
	for k, v := range run.Meta { meta[k] = v }
	run.Meta = meta
}
```

**Why it's bad:** Defensive copy on struct received by value. Caller's original already unreachable. O(n) work for zero benefit.

**Fix:** Delete the copy.

---

### Issue 4.18: ValidatePayload Normalizes What Was Already Normalized

**File:** `internal/cron/cron.go:573-592`

**Why it's bad:** Every caller already called `NormalizePayload`. `ValidatePayload` receives already-normalized, normalizes again, throws result away.

**Fix:** Split into `ValidatePayloadNormalized` or trust the contract.

---

### Issue 4.19: Add() Blindly Appends Duplicate IDs

**File:** `internal/cron/cron.go:248-278`

**Why it's bad:** No duplicate ID check. Multiple jobs share same ID. Update/Remove only act on first.

**Consequences:** Silent shadow jobs. Can't remove or update through API.

**Fix:** Iterate and return error if duplicate ID found.

---

### Issue 4.20: Start() Fails Silently on Save Errors

**File:** `internal/cron/cron.go:190-192`

```go
if err := s.save(st); err != nil {
	log.Printf("cron save failed: %v", err)
}
```

**Why it's bad:** Logs and swallows. Jobs armed in scheduler but not persisted. Crash before next mutation = all lost.

**Fix:** Return the error. Fail fast.

---

### Issue 4.21: Closures Capture Entire Job Structs

**File:** `internal/cron/cron.go:499-503, 510-514, 522-526`

**Why it's bad:** Closure captures entire `CronJob` by value. Only uses `job.ID`. Unnecessary allocations.

**Fix:** `id := job.ID; ... func() { s.runJobByID(..., id, ...) }`.

---

### Issue 4.22: Status() Does Disk I/O Under Mutex

**File:** `internal/cron/cron.go:214-234`

**Why it's bad:** Monitoring call opens/reads/parses JSON under exclusive mutex. Every concurrent mutation blocks.

**Fix:** Keep state in memory. Use RLock. Don't hit disk.

---

### Issue 4.23: Nil Runner Check at Runtime Instead of Construction

**File:** `internal/cron/cron.go:404-406`

```go
if s.runner == nil {
	return true, fmt.Errorf("cron runner not configured")
}
```

**Why it's bad:** Programming error discovered only on first job execution, not at startup.

**Fix:** Panic in `New` if `runner == nil`.

---

### Issue 4.24: Dispatcher Duplicates Session Key Fallback Logic

**File:** `internal/cronrunner/dispatcher.go:49-52, 75-78`

**Why it's bad:** Same 4 lines of fallback copy-pasted into two methods. Update one, forget the other.

**Fix:** Extract `func (d Dispatcher) sessionKey(payload) string`.

---

### Issue 4.25: Dispatcher Redundantly Normalizes Already-Normalized Payload

**File:** `internal/cronrunner/dispatcher.go:29-39`

```go
payload := cron.NormalizePayload(job.Payload)
```

**Why it's bad:** Cron service already called NormalizePayload before storing. Normalized twice.

**Fix:** Trust the contract. Remove redundant normalization.

---

### Issue 4.26: Value Receivers on Struct Containing Pointers

**File:** `internal/cronrunner/dispatcher.go:29, 41, 67`

```go
func (d Dispatcher) Run(...)
func (d Dispatcher) publishAgentTurn(...)
```

**Why it's bad:** Dispatcher contains 3 pointer fields. Every method call copies entire struct (24 bytes). Meaningless overhead.

**Fix:** `func (d *Dispatcher)`.

---

### Issue 4.27: Allocating Map for Single Metadata Entry

**File:** `internal/cronrunner/dispatcher.go:59`

```go
Meta: map[string]any{"job_id": job.ID},
```

**Why it's bad:** Heap allocation of entire map for one key-value pair.

**Fix:** If can't change `bus.Event`, pre-allocate shared map. Better: change `bus.Event` to accept `[]MetaEntry`.

---

### Issue 4.28: publishAgentTurn Returns Empty Enqueued IDs

**File:** `internal/cronrunner/dispatcher.go:41-65`

```go
return cron.RunResult{}, nil
```

**Why it's bad:** `EnqueuedJobID` and `EnqueuedRunID` left empty. State records blank enqueued IDs. Semantically meaningless.

**Fix:** Return sentinel like `"-"` for non-enqueueable jobs. Or use nilable fields.

---

### Issue 4.29: Most Useless Test — Tests Nothing

**File:** `internal/cron/cron_test.go:820-863`

**Why it's bad:** Creates separate unstarted Service with manual runner. Calls runner directly. Bypasses entire scheduling/persistence/state management. Tests absolutely nothing.

**Fix:** Delete. Write integration test that calls `Add()` then `RunNow()` and inspects what runner receives.

---

### Issue 4.30: TestRunNow_SaveError — A Test That Forgot to Be a Test

**File:** `internal/cron/cron_test.go:653-671`

**Why it's bad:** Named `SaveError`. Claims to test save error path. Never triggers a save error. Just runs job normally.

**Fix:** Actually trigger save error (read-only file, bad path, inject faulty WriteFile). Or rename honestly.

---

### Issue 4.31: Dispatcher Validates What Was Already Validated

**File:** `internal/cronrunner/dispatcher.go:71-73`

```go
if err := cron.ValidatePayload(payload); err != nil { ... }
```

**Why it's bad:** Payload already validated by cron service before job is stored/armed. Redundant on every execution.

**Fix:** Trust scheduler's contract. Remove.

---

### Issue 4.32: No Upper Bound on Job Count

**File:** `internal/cron/cron.go`

**Why it's bad:** `st.Jobs` unbounded slice. Malicious/buggy client adds millions. Every operation iterates entire slice. Unbounded file size.

**Fix:** Enforce max job count in `Add()`. Return error when reached.

---

### Issue 4.33: randID() — Modulo Bias on Small Charset

**File:** `internal/cron/cron.go:671-678`

```go
b[i] = chars[int(randUint()%uint64(len(chars)))]
```

**Why it's bad:** `2^64` not evenly divisible by 36. Modulo bias toward first characters. 10 chars = ~3.6e15, birthday collisions at scale.

**Fix:** Use `crypto/rand` with base32/hex encoding. Or proper UUID library.

---

### Issue 4.34: context.Background() in Callbacks — No Cancellation

**File:** `internal/cron/cron.go:500, 511, 523`

```go
s.runJobByID(context.Background(), job.ID, false)
```

**Why it's bad:** Hardcoded `context.Background()`. Service stopped while job running = no cancellation signal. Long jobs block shutdown.

**Fix:** Store context in Service created on `Start()`, cancelled on `Stop()`.

---

### Issue 4.35: Dispatcher New() Allocates Closure for No Reason

**File:** `internal/cronrunner/dispatcher.go:24-27`

```go
return d.Run
```

**Why it's bad:** Method value creates heap-allocated closure. Unnecessary.

**Fix:** Return `(*Dispatcher).Run` or make Dispatcher implement interface.

---

### Issue 4.36: TestArmJob_KindAt Uses Flaky Wall-Clock Timing

**File:** `internal/cron/cron_test.go:566-595`

```go
atMS := time.Now().Add(100 * time.Millisecond).UnixMilli()
```

**Why it's bad:** 100ms timer subject to goroutine scheduling delays. Will flake on overloaded CI.

**Fix:** Abstract `time.AfterFunc` behind interface. Inject mock clock.

---

### Issue 4.37: No Tests for Concurrent Mutation

**File:** `internal/cron/cron_test.go`

**Why it's bad:** No test exercising concurrent Add/Remove/RunNow. Deadlock in Stop() would have been caught.

**Fix:** Write test with 10 goroutines each doing Add/Remove/RunNow/Stop/Start.

---

### Issue 4.38: DeleteAfterRun — Rebuilds Slice Inline Instead of Reusing Remove

**File:** `internal/cron/cron.go:436-445`

**Why it's bad:** Inlines same filter-and-rebuild logic that `Remove()` already implements. Two places to update. Save failure = job disarmed in scheduler but still on disk.

**Consequences:** Delete-after-run jobs resurrect on restart. One-time tasks run twice.

**Fix:** Delegate to `Remove()` after runner succeeds.

---

### Issue 4.39: publishAgentTurn Panics on Nil Bus at Runtime, Not Construction

**File:** `internal/cronrunner/dispatcher.go:41-44`

```go
if d.Bus == nil { return ..., fmt.Errorf("event bus unavailable") }
```

**Why it's bad:** Configuration error discovered hours after startup, on first cron tick. Not at `New()`.

**Fix:** Validate dependencies in `New`. Panic or error early.

---

### Issue 4.40: ValidateSchedule Accepts EveryMS=0, armJobLocked Defaults to 60s

**File:** `internal/cron/cron.go:601-604, 504-509`

**Why it's bad:** `ValidateSchedule` accepts 0. `armJobLocked` silently changes to 60s. User submitted 0ms, got 60s. Hidden default.

**Fix:** Reject `EveryMS == 0` in `ValidateSchedule` or make default explicit.

---

## Summary of Critical Issues

| Severity | Count | Top Examples |
|----------|-------|-------------|
| **CRITICAL** (security/data loss) | 12 | Bus silent drops (1.4), cron deadlock on Stop (4.3), cron JSON truncation (4.1), SQL injection (2.6), sandbox symlink TOCTOU (3.14), skill encryption key reuse (3.21), nil tool = safe (3.1) |
| **HIGH** (races/corruption) | 15 | Registry no mutex (3.2), job_registry silent drop (2.13), executeConversation slice mutation (2.9), cron TOCTOU (4.4), shallow copy landmine (2.12), test data races (2.24) |
| **MEDIUM** (performance/quality) | 41 | JSON-as-database (4.14), cronParser alloc per call (4.10), double marshal/unmarshal (2.16/2.17/3.29), broken UTF-8 truncation (2.2/2.3/3.7), DRY violations (2.19/3.24/4.24) |
| **LOW** (maintenance) | 68 | Unused types/imports (1.2/1.3), deprecated functions (2.5), hardcoded strings (2.21), misleading test names (1.16/4.30), redundant validation (4.18/4.25/4.31) |

### Priority Fix Order

1. **Fix the cron deadlock** — Stop() holding mutex while waiting on callbacks (4.3)
2. **Fix cron JSON corruption** — check marshal error + atomic writes (4.1, 4.2)
3. **Fix bus silent data loss** — log dropped events (1.4)
4. **Fix tool sandbox TOCTOU** — symlink attack protection (3.14)
5. **Fix nil tool = safe** — fail closed (3.1)
6. **Fix key reuse** — separate signing and encryption keys (3.21)
7. **Add mutex to tool registry** — prevent map corruption (3.2)
8. **Fix job_registry silent event drops** — log/retry (2.13)
9. **Fix all UTF-8 truncation** — use rune-safe slicing (2.2, 2.3, 3.7)
10. **Fix all ignored errors** — `_` on io.ReadAll, json.Marshal, charset detection
