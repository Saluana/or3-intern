# May 8 Fix Task List

Tasks ordered by priority. Check off as completed.

## P0 — Data Loss / Crashes / Deadlocks

- [ ] **cron: Fix Stop() deadlock** — release mutex before waiting on callbacks (`cron.go:198-211`, issue 4.3)
- [ ] **cron: Fix JSON corruption on save** — check `json.MarshalIndent` error, use atomic write via temp file + rename (`cron.go:153-159`, issues 4.1, 4.2)
- [ ] **tools: Add mutex to Registry** — prevent concurrent map access panics (`registry.go:11-52`, issue 3.2)
- [ ] **tools: Fix nil tool returns "Safe" capability** — fail closed instead of silently bypassing guards (`tools.go:45-71`, issue 3.1)

## P1 — Silent Failure / Security

- [ ] **bus: Log dropped events** — add counter/metric + warn log on publish overflow (`bus.go:52-60`, issue 1.4)
- [ ] **tools: Fix sandbox symlink TOCTOU** — compare `os.SameFile` after open before returning fd (`files.go:102-117, 158-171`, issues 3.14, 3.15)
- [ ] **tools: Fix skill encryption key reuse** — separate signing/encryption keys, use HKDF with distinct contexts (`skill_run.go:596-639`, issue 3.21)
- [ ] **agent + tools: Fix all `io.ReadAll` / `json.Marshal` / `_` error discards** — check every `_` error return (`web.go:176,447`, `cron.go:153`, `html_converter.go:156`, `prompt.go:438`, issues 3.8, 3.12, 4.1, 2.16)
- [ ] **agent: Fix SQL injection surface in `countMemoryRows`** — validate `extraWhere` against whitelist (`runtime_status.go:330-368`, issue 2.6)
- [ ] **agent + tools: Fix silent event drops in Publish/JobRegistry** — log, retry, or buffer (`job_registry.go:154-159`, `bus.go:52-60`, issues 2.13, 1.4)
- [ ] **agent: Fix boundedContext escaping parent cancellation** — remove `context.WithoutCancel`, use proper lifecycle (`subagents.go:497-507`, issue 2.23)
- [ ] **agent: Fix validateStructuredValue no recursion limit** — add `maxDepth`, prevent stack overflow DoS (`structured_autonomy.go:96-165`, issue 2.22)

## P2 — Data Integrity / State Bugs

- [ ] **cron: Fix runJobByID TOCTOU race** — keep job in memory, don't reload file mid-execution (`cron.go:392-454`, issue 4.4)
- [ ] **cron: Fix timezone ignored on cron registration** — pass `cron.WithLocation` (`cron.go:520-532`, issue 4.6)
- [ ] **cron: Fix sub-second intervals silently changed to 60s** — reject `EveryMS < 1000` in validation (`cron.go:504-509`, issue 4.7)
- [ ] **cron: Fix KindAt check-then-act race** — compute `time.Until` once (`cron.go:493-503`, issue 4.8)
- [ ] **cron: Fix DeleteAfterRun inline rebuild instead of delegating to Remove** — reuse `Remove()`, prevent one-shot resurrection (`cron.go:436-445`, issue 4.38)
- [ ] **agent: Fix `executeConversation` mutating caller's slice** — copy at function top (`runtime_execution.go:167,227`, issue 2.9)
- [ ] **agent: Fix `cloneEventData` shallow copy** — deep clone maps/slices (`job_registry.go:431-440`, issue 2.12)
- [ ] **agent: Fix test data races** — use `atomic.Int32` for handler call counters (`runtime_test.go`, issue 2.24)
- [ ] **cron: Fix Add() blindly appending duplicate IDs** — check before append (`cron.go:248-278`, issue 4.19)
- [ ] **tools: Fix skill-run plan permanently stuck "Running"** — add heartbeat or lease expiry (`skill_run.go:339-406`, issue 3.23)

## P3 — Bugged Behavior

- [ ] **agent + tools: Fix all UTF-8 truncation** — `truncateText`, `oneLine`, `PreviewString` slice by runes not bytes (`prompt.go:719-724,866-872`, `result.go:82-88`, issues 2.2, 2.3, 3.7, 3.39)
- [ ] **agent: Fix `narrateApprovalRequired` nil check too late** — check `r == nil` before dereference (`runtime_execution.go:253-278`, issue 2.10)
- [ ] **agent: Fix `redactDiagnostic` over-matching** — use `\bsecret\b` regex, redact value not next token (`diagnostics.go:37-55`, issue 2.14)
- [ ] **agent: Fix `PublicErrorCode` substring classification** — use `errors.Is` + sentinel types (`error_codes.go:43-53`, issue 2.15)
- [ ] **tools: Fix `grepFile` advertises regex but uses substring** — compile pattern or update schema (`files.go:579-622`, issue 3.16)
- [ ] **tools: Fix `optionalBool` accepting strings for boolean param** — reject non-bool types (`message.go:112-131`, issue 3.25)
- [ ] **tools: Fix `isBlockedFetchAddr` redundant 169.254 check** — already covered by `IsLinkLocalUnicast()` (`web.go:367-375`, issue 3.38)
- [ ] **tools: Fix `ReadSkill.Execute` ignores context cancellation** — pass ctx or check `ctx.Err()` (`skill.go:32-33`, issue 3.31)
- [ ] **tools: Fix `commandWithSandbox` returns (nil, nil)** — return sentinel error or move nil check to caller (`sandbox.go:19-25`, issue 3.18)
- [ ] **cron: Fix `randUint` crypto fallback to timestamp** — propagate error instead (`cron.go:663-669`, issue 4.11)
- [ ] **cron: Fix `context.Background()` in callbacks** — store service-scoped ctx cancelled on Stop (`cron.go:500,511,523`, issue 4.34)

## P4 — Performance / Allocations

- [ ] **cron: Convert cronParser() to package-level singleton** — stop allocating parser per call (`cron.go:659-661`, issue 4.10)
- [ ] **agent: Fix double JSON marshal/unmarshal in prompt building** — type-assert directly or store typed structs (`prompt.go:340-345,438-440`, issues 2.16, 2.17)
- [ ] **tools: Fix `CronTool` marshal-then-unmarshal dance** — decode directly from map (`cron.go:69-86`, issue 3.29)
- [ ] **tools: Fix `WebFetchMarkdown` copy-paste of `WebFetch`** — extract shared HTTP fetching (`web_markdown.go:55-131`, issue 3.11)
- [ ] **tools: Fix PlaywrightRenderer temp file per render** — write script once at startup (`web.go:283-291`, issue 3.9)
- [ ] **tools: Fix `EditFile` loads entire file into memory** — add size limit before read (`files.go:401-443`, issue 3.17)
- [ ] **tools: Fix `skillCommandHash` reads entire file** — use `sha256.New()` + `io.Copy` (`skill_exec.go:152-164`, issue 3.33)
- [ ] **tools: Fix `WriteFile` redundant `os.Stat` calls** — stat once, pass mode down (`files.go:354-375`, issue 3.36)
- [ ] **cron: Fix `NormalizePayload` pointless meta copy** — remove defensive copy on value receiver (`cron.go:560-566`, issue 4.17)
- [ ] **cron: Fix `Remove()` allocates even when nothing to remove** — check found first (`cron.go:361-383`, issue 4.15)
- [ ] **cron: Fix closures capturing entire `CronJob`** — capture only `job.ID` (`cron.go:499-526`, issue 4.21)
- [ ] **cron: Convert mutex to RWMutex** — allow parallel reads on List/Status (`cron.go:118-125`, issue 4.13)
- [ ] **cron: Limit job count** — add max cap in Add() to prevent unbounded growth (issue 4.32)
- [ ] **tools: Fix `CleanHTMLForLLM` double-walking DOM** — use goquery text extraction in one pass (`html_converter.go:209-308`, issue 3.13)
- [ ] **tools: Fix `htmlTextSeparatorTags` global map in hot loop** — use switch or pre-lowercase keys (`html_converter.go:267-273`, issue 3.49)

## P5 — Dead Code / Cleanup

- [ ] **bus: Remove unused `Handler` type and `context` import** — dead code (`bus.go:36-38`, issues 1.2, 1.3)
- [ ] **agent: Remove unreachable `sql.ErrNoRows` after `COUNT(*)`** — dead branch (`runtime_status.go:365-367`, issue 2.7)
- [ ] **agent: Remove all nil receiver checks on value receivers** — let panic find real bugs (multiple in `runtime.go`, issue 2.8)
- [ ] **agent: Fix `strings.Title` deprecated** — use `cases.Title` from `x/text` (`semantic_compression.go:81`, issue 2.5)
- [ ] **cron: Delete custom `filepathDir`** — use `filepath.Dir` (`cron.go:161-170`, issue 4.12)
- [ ] **tools: Delete `splitter/main.go`** — hardcoded line numbers for single file (issue 3.35)
- [ ] **tools: Delete redundant `readOptionalString`** — use `stringParam` everywhere (`spawn.go:96-105`, issue 3.24)
- [ ] **tools: Remove redundant `NormalizePayload` in dispatcher** — already normalized upstream (`dispatcher.go:29-39`, issue 4.25)
- [ ] **tools: Remove redundant `ValidatePayload` in dispatcher** — already validated upstream (`dispatcher.go:71-73`, issue 4.31)
- [ ] **agent: Replace `containsEval`/`indexEval` reimplementations** — use `strings.Contains` (`context_evaluation_test.go:83-94`, issue 2.26)

## P6 — Refactor / Architecture

- [ ] **agent: Break up `turn()` 75-line god function** — extract command/skill/execution/post-cleanup handlers (`runtime.go:178-254`, issue 2.18)
- [ ] **agent: Unify `prompt.go` and `prompt_budget.go` section definitions** — single source of truth (`prompt.go:591-657`, issue 2.19)
- [ ] **agent: Replace SHA-1 for tool call IDs** — use `fnv32a` or `xxhash` (`tool_calls.go:135-140`, issue 2.11)
- [ ] **agent: Replace hardcoded tool name strings** — define name constants or use metadata queries (multiple files, issue 2.21)
- [ ] **agent: Replace substring intent classifier** — use word-boundary regex or explicit tool requests (`runtime_access.go:104-138`, issue 2.20)
- [ ] **tools: Extract `stringParam`/`floatParam`/`boolParam` helpers** — eliminate `fmt.Sprint` + `"<nil>"` pattern everywhere (issue 3.4)
- [ ] **tools: Replace `toolFailureAdvice` 400-line switch** — per-tool `AdviceProvider` interface (`result.go:237-399`, issue 3.28)
- [ ] **tools: Replace `inferToolMetadata` hardcoded switch** — use `MetadataReporter` interface (`registry.go:110-136`, issue 3.3)
- [ ] **cron: Replace JSON file with SQLite/BoltDB** — eliminate O(n) write amplification (`cron.go`, issue 4.14)
- [ ] **cron: Fix three-return-value API** — use `(CronJob, error)` + sentinel `ErrNotFound` (`cron.go:281,323`, issue 4.16)
- [ ] **cron: Fix `Start()` swallowing save errors** — return error (`cron.go:190-192`, issue 4.20)
- [ ] **cron: Fix nil runner check at construction** — panic in `New()` not at runtime (`cron.go:404-406`, issue 4.23)
- [ ] **cron: Fix nil bus check at construction** — panic in dispatcher `New()` not at runtime (`dispatcher.go:41-44`, issue 4.39)
- [ ] **bus: Add proper `Subscribe`/`Unsubscribe` instead of shared `Channel()`** — fan-out to per-subscriber channels (`bus.go:62-63`, issue 1.6)
- [ ] **bus: Add `Close()` method with `sync.Once`** — prevent goroutine leaks (`bus.go:40-63`, issue 1.5)
- [ ] **bus: Add max buffer check** — reject `> 1_000_000` in `New()` (`bus.go:44-50`, issue 1.8)

## P7 — Tests

- [ ] **cron: Add concurrent mutation test** — goroutines doing Add/Remove/RunNow/Stop simultaneously (issue 4.37)
- [ ] **bus: Add concurrent publish/consume test** — verify no event loss under load (issue 1.19)
- [ ] **cron: Fix or delete `TestCronRunnerPerJobSession`** — tests nothing through the actual API (`cron_test.go:820-863`, issue 4.29)
- [ ] **cron: Fix or delete `TestRunNow_SaveError`** — doesn't trigger save error (`cron_test.go:653-671`, issue 4.30)
- [ ] **cron: Abstract wall-clock in tests** — inject mock clock instead of `time.AfterFunc` (`cron_test.go:566-595`, issue 4.36)
- [ ] **tools: Fix `TestWriteFile_Mkdirs` umask-affected mode assertions** — use bitwise check instead of exact match (`files_test.go:312-320`, issue 3.44)
- [ ] **tools: Move `TestExecServiceCommandPassesGuardedRegistryCeiling`** — out of `exec_test.go` into `registry_test.go` (issue 3.48)
- [ ] **tools: Fix `TestSendMessage` hardcoded constant** — use `rootchannels.MetaMediaPaths` (`message_test.go:175-178`, issue 3.40)
- [ ] **agent: Fix `repeatedEvalText` O(n^2) in benchmark** — use `strings.Repeat` (`context_evaluation_test.go:75-81`, issue 2.25)
- [ ] **bus: Fix tests discarding Publish return values** — assert `ok` on every publish (`bus_test.go:102-116`, issue 1.13)
- [ ] **bus: Fix `TestChannel_IsReadOnly`** — delete or rename honestly (`bus_test.go:72-78`, issue 1.16)

## P8 — Docs / Names

- [ ] **bus: Fix package comment** — says "cross-service signaling", is a single channel queue (`bus.go:1-2`, issue 1.20)
- [ ] **tools: Fix `grepFile` schema description** — clarify substring vs regex (`files.go:579`, issue 3.16)
- [ ] **tools: Document `scriptCommand` only supports `.sh` and `.py`** — or fix (issue 3.32)
