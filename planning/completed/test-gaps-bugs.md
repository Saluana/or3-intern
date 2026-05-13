# Test Gaps & Bugs — or3-intern

Audit date: 2026-05-09
Scope: `internal/agent`, `internal/agentcli`, `internal/controlplane`, `internal/cronrunner`
Method: 4 subagent audits comparing production code paths against existing test coverage.

---

## Bugs Found

Real issues, not just missing tests.

### B1 — workerLoop has no panic recovery
- **File:** `internal/agent/subagents.go:245-264`
- **Severity:** High — if `runOnce` or `executeJob` panics, `wg.Done()` never runs and the worker goroutine is lost permanently.
- **Fix:** Add `defer recover()` inside `workerLoop`.

### B2 — JobRegistry.Publish panics on terminal job
- **File:** `internal/agent/job_registry.go:133-165`
- **Severity:** High — `Publish` doesn't check `entry.terminal`. Calling Publish after `Complete` sends on already-closed subscriber channels, causing a panic.
- **Fix:** Add terminal guard in `Publish`; return no-op if terminal.

### B3 — Data race in runtime test handler counters
- **File:** `internal/agent/runtime_test.go`
- **Severity:** Medium — plain `int` used for goroutine call counters; should be `atomic.Int32`.
- **Fix:** Replace with `atomic.Int32`. (Referenced in existing task list #2.24)

---

## Agent Runtime & Execution

### R1 — RunBackground(): zero test coverage
- **File:** `internal/agent/runtime.go:442-481`
- **Severity:** High — main background agent path has no tests. Empty session key, nil tools fallback, error from `executeConversation`, nil Meta nil-safe all untested.
- **Approach:** Write a test exercising all three error-return paths and the happy path via a mock provider.

### R2 — RunBackground() stop/restart race
- **File:** `internal/agent/runtime.go:442-481`
- **Severity:** Medium — acquires `sessionLock` but no test verifies concurrent `RunBackground` calls serialise properly.
- **Approach:** Run two goroutine `RunBackground` calls with same session key and a sleep-injected provider; verify no data race with `-race`.

### R3 — handleTurnPostCleanup: DisableRollingConsolidation branch untested
- **File:** `internal/agent/runtime.go:256-269`
- **Severity:** Medium — when `DisableRollingConsolidation=true`, neither `Trigger` nor `MaybeConsolidate` is called; never verified.
- **Approach:** Set `DisableRollingConsolidation=true` and assert consolidation functions are not invoked.

### R4 — boundTextResult: output exactly at MaxToolBytes boundary
- **File:** `internal/agent/runtime.go:82-131`
- **Severity:** Low — edge case where final text lands exactly at `MaxToolBytes` is untested.
- **Approach:** Test with output exactly at MaxToolBytes to verify artifact-save-or-not behavior.

### R5 — isNewSessionCommand: whitespace / trailing spaces
- **File:** `internal/agent/runtime.go:271-274`
- **Severity:** Low — only `/new` and `/clear` tested. Empty string, whitespace-only, `/clear ` and `/new ` with trailing spaces untested.
- **Approach:** Add table-driven test with all edge inputs.

### R6 — deliveryTarget: nil ev.Meta fallback
- **File:** `internal/agent/runtime.go:742-751`
- **Severity:** Low — when `ev.Meta` is nil or empty, falls back to `ev.From`. Untested.
- **Approach:** Test with nil Meta; assert delivery channel matches `ev.From`.

### R7 — cloneMap(nil) returns empty map
- **File:** `internal/agent/runtime.go:731-740`
- **Severity:** Low — should return `map[string]any{}` not nil; untested.
- **Approach:** Call cloneMap(nil); assert result is empty map, not nil.

### R8 — releaseEvent: done func is nil
- **File:** `internal/agent/runtime.go:784-793`
- **Severity:** Low — when Meta is non-nil but `done` key is missing or nil, it safely returns. Untested.
- **Approach:** Test releaseEvent with Meta having no `done` key, and with `done` as nil func.

### R9 — shouldAutoDeliver: non-bool auto_reply_enabled values
- **File:** `internal/agent/runtime.go:762-782`
- **Severity:** Low — string "false" tested; string "true", int, nil not tested.
- **Approach:** Table-driven test with non-bool truthy/falsy values.

### R10 — Tool loop limit exceeded → approval Allowed=true
- **File:** `internal/agent/runtime_execution.go:305-312`
- **Severity:** High — only the "ask → denied" path tested. The "approved without interaction" path (`decision.Allowed=true`) is untested.
- **Approach:** Test with a pre-approved token that makes the broker return `Allowed: true`.

### R11 — Context deadline exceeded mid-loop
- **File:** `internal/agent/runtime_execution.go:64-90`
- **Severity:** High — if `ctx` is cancelled/deadline-exceeded during `ChatStream` or `Chat`, the error is returned. No test injects a cancelled context mid-loop.
- **Approach:** Test with a context that expires during the first provider call.

### R12 — handleToolLoopLimitExceeded: nil ApprovalBroker in Ask mode
- **File:** `internal/agent/runtime_execution.go:284-318`
- **Severity:** Medium — `ApprovalBroker == nil` branch returns error. Not directly tested with explicit nil broker.
- **Approach:** Test with `MaxToolLoopsExceededAction=Ask` and `ApprovalBroker=nil`.

### R13 — Provider returns nil Content with no tool calls
- **File:** `internal/agent/runtime_execution.go:144-157`
- **Severity:** Medium — the case where `resp.Choices[0].Message.Content == nil` with no tool calls is untested.
- **Approach:** Test nil Content with no tool calls; assert clean terminal behavior.

### R14 — Broken tool_call markup yielding garbage parse results
- **File:** `internal/agent/runtime_execution.go:112-120`
- **Severity:** Medium — `parseToolMarkupCalls` returns empty when no markup found. Untested: markup found but parser returns garbage calls.
- **Approach:** Test with broken `<tool_call>` markup that yields unparseable args.

### R15 — narrateApprovalRequired: nil receiver, empty provider content
- **File:** `internal/agent/runtime_execution.go:254-276`
- **Severity:** Low — nil-guard and empty-content return untested.
- **Approach:** Test with nil receiver (assert safe) and empty `resp.Choices[0].Message.Content`.

### R16 — terminalToolResultText: invalid JSON, nil result.OK
- **File:** `internal/agent/runtime_execution.go:348-352`
- **Severity:** Low — json.Unmarshal error and nil `*bool` deref guard untested.
- **Approach:** Pass invalid JSON; verify returns "". Test with `result.OK` as nil.

### R17 — Session lock ref-counting race
- **File:** `internal/agent/runtime_session.go:19-48`
- **Severity:** Medium — acquire twice, release once (refs 2→1), acquire new caller on same key, then release the old copy. The `current == entry` check guards some but not all paths.
- **Approach:** Acquire twice (refs=2), release once, verify entry still exists; acquire new caller on same key, release old one, verify no double-free.

### R18 — scheduleIdlePrune: zero/negative delay
- **File:** `internal/agent/runtime_session.go:76-78`
- **Severity:** Low — defaults to 5 minutes; zero/negative input untested.
- **Approach:** Test with `IdlePruneSeconds=0`; assert safe default applied.

### R19 — isDirectMessageEvent: all channel types
- **File:** `internal/agent/runtime_session.go:160-182`
- **Severity:** Low — only telegram, whatsapp, slack tested. Discord (is_private, guild_id), Slack (channel_type=im), and email untested.
- **Approach:** Add table-driven test covering all channel type branches.

### R20 — handleNewSession: Builder set but Consolidator nil
- **File:** `internal/agent/runtime_session.go:184-219`
- **Severity:** Low — when Consolidator is nil but Builder exists, skips archival but still calls `ResetSessionHistory`. Untested.
- **Approach:** Test `/new` with Builder set but no Consolidator.

### R21 — releaseSessionLock: nil Runtime guard
- **File:** `internal/agent/runtime_session.go:34-48`
- **Severity:** Low — `if r == nil` guard at line 35 never tested.
- **Approach:** Call `(*Runtime)(nil).releaseSessionLock("key", entry)`; assert safe no-op.

### R22 — ensureSessionScope: already linked to same scope
- **File:** `internal/agent/runtime_session.go:127-141`
- **Severity:** Low — when `scopeKey == ev.SessionKey`, returns early. Untested.
- **Approach:** Link session to its own scope key; assert early return, no DB call.

---

## Tool Access & Policy

### A1 — exposedToolsForTurn: nil registry, nil Runtime
- **File:** `internal/agent/runtime_access.go:42-46`
- **Severity:** Medium — `exposedToolsForTurn` with nil registry returns nil; nil Runtime skips dynamic exposure. Both untested.
- **Approach:** Test both nil cases; assert safe returns.

### A2 — filterToolsForContext: nil registry, nil Runtime but non-nil reg
- **File:** `internal/agent/runtime_access.go:65-68`
- **Severity:** Medium — nil registry returns nil; nil Runtime skips allowlist filtering. Both untested.
- **Approach:** Test with nil reg and with nil Runtime but valid reg.

### A3 — filterToolsForContext: profile with empty AllowedTools
- **File:** `internal/agent/runtime_access.go:91-96`
- **Severity:** Low — when `len(profileAllowed) > 0` is false, all tools pass. Partially tested but not the empty-allowlist case.
- **Approach:** Test with empty AllowedTools map; assert all tools pass.

### A4 — selectedToolGroups: empty intent string
- **File:** `internal/agent/runtime_access.go:145-147`
- **Severity:** Low — `strings.TrimSpace(intent) != ""` with empty intent skips channels group. Untested.
- **Approach:** Test with empty string; assert channels group not added.

### A5 — guardToolExecution: nil tool, empty SignKey, Audit.Record error
- **File:** `internal/agent/runtime_access.go:170-207`
- **Severity:** Medium — three distinct paths: nil tool (returns nil), empty SignKey (returns error), Audit failure (returns audit error).
- **Approach:** Three focused tests covering each path.

### A6 — enforceSkillPolicy: nil tool, WebSearch with empty AllowedHosts
- **File:** `internal/agent/runtime_access.go:233-283`
- **Severity:** Low — nil tool returns nil; empty `AllowedHosts` skips host validation.
- **Approach:** Test nil tool safe return; test WebSearch with no allowed hosts configured.

### A7 — resolveProfile: profiles not enabled, unknown name
- **File:** `internal/agent/runtime_access.go:349-358`
- **Severity:** Low — AccessProfiles not enabled and unknown profile name both return `(tools.ActiveProfile{}, false)`.
- **Approach:** Two tests: disabled profiles, and unknown profile name.

### A8 — validateProfileWritablePath: nil/empty path
- **File:** `internal/agent/runtime_access.go:445-448`
- **Severity:** Low — returns nil immediately for empty path.
- **Approach:** Test with nil and empty string path.

### A9 — profileNameForEvent: non-string profile_name type
- **File:** `internal/agent/runtime_access.go:332-333`
- **Severity:** Low — string `"<nil>"` tested; what about `profile_name` as int or bool?
- **Approach:** Test `fmt.Sprint` coercion with non-string types.

### A10 — deny_all policy mode: zero coverage
- **File:** `internal/agent/tool_policy.go:39-40`
- **Severity:** High — the `deny_all` policy mode has no test coverage at all.
- **Approach:** Call `ResolveServiceToolAllowlist` with `Mode: "deny_all"`; assert `allowed` is empty and `explicit` is true.

### A11 — Hidden tools exclusion
- **File:** `internal/agent/tool_policy.go:78-79`
- **Severity:** Medium — tools with `meta.Hidden` or `ToolGroupHidden` group should be filtered out; untested.
- **Approach:** Register a tool with `Hidden: true`; verify excluded from `ask`/`work`/`admin` modes.

### A12 — capabilityRankForPolicy: Privileged and unknown
- **File:** `internal/agent/tool_policy.go:143-154`
- **Severity:** Low — `CapabilityPrivileged` and unknown capability (default 0) paths untested.
- **Approach:** Direct unit test with each `CapabilityLevel` value.

### A13 — Unsupported policy mode error
- **File:** `internal/agent/tool_policy.go:62`
- **Severity:** Low — bogus mode should return error; untested.
- **Approach:** Pass `Mode: "bogus"`; assert error message.

### A14 — Tool validation with nil tool
- **File:** `internal/agent/tool_validation.go:34,51-53`
- **Severity:** Medium — `ValidateAndCoerce(nil, argsJSON)` should skip schema validation and pass through params; untested.
- **Approach:** Call with nil tool and valid JSON args; assert no errors and params preserved.

### A15 — Boolean coercion from "yes"/"no"
- **File:** `internal/agent/tool_validation.go:179-184`
- **Severity:** Low — only `"true"`/`"false"` coerced; `"yes"` should reject but is untested.
- **Approach:** Feed `"yes"` for a boolean field; assert error returned.

### A16 — Scalar-to-array coercion with object items type
- **File:** `internal/agent/tool_validation.go:115-124`
- **Severity:** Low — when items schema type is `"object"`, a scalar value should NOT be coerced to array; untested.
- **Approach:** Define array schema with `items: {type: "object"}`, pass a string value, assert error.

---

## Context Manager & Prompt

### C1 — requestContextManagerCompaction: zero coverage
- **File:** `internal/agent/context_manager.go:281-321`
- **Severity:** High — entire provider-call path untested: provider=nil, tool_choice retry, parse failure on both attempts.
- **Approach:** Mock provider; verify retry behavior and error paths.

### C2 — parseContextManagerToolResponse: zero coverage
- **File:** `internal/agent/context_manager.go:323-334`
- **Severity:** Medium — no choices, zero tool calls, multiple tool calls where only non-matching ones exist.
- **Approach:** Table-driven test with stubbed `ChatCompletionResponse`.

### C3 — buildUserContent: zero coverage
- **File:** `internal/agent/prompt.go:617-644`
- **Severity:** High — vision disabled, nil Artifacts, empty attachments, all non-image attachments, imageParts==0 fallback all untested.
- **Approach:** Table-driven test with mock artifacts.Store.

### C4 — imagePart + readCappedFile: zero coverage
- **File:** `internal/agent/prompt.go:646-706`
- **Severity:** High — nil budget, remainingImages exhausted, remainingBytes exhausted, Lookup error, readCappedFile error, data > remainingBytes.
- **Approach:** Mock artifacts.Store + temp image file; cover all skip paths.

### C5 — toolCallsFromPayload + attachmentsFromPayload + serialization helpers: zero coverage
- **File:** `internal/agent/prompt.go:423-557`
- **Severity:** High — all 4 type-switch branches, decode helpers, attachment deserialization completely untested.
- **Approach:** Table-driven JSON unmarshal roundtrip for each helper.

### C6 — payloadStringValue / payloadIntValue / payloadInt64Value: zero coverage
- **File:** `internal/agent/prompt.go:559-603`
- **Severity:** Medium — string, json.Number, nil, int→int64, float64→int64 conversion paths untested.
- **Approach:** Table-driven test for each payload extraction helper.

### C7 — cachedEmbed: provider=nil, Embed error, LRU eviction
- **File:** `internal/agent/prompt.go:1045-1078`
- **Severity:** Medium — provider=nil, Embed API error, and LRU eviction at 128 entries all untested.
- **Approach:** Mock provider with controlled errors; pre-fill cache to trigger eviction.

### C8 — currentHeartbeatText: nil builder, LoadTasksFile error, empty file
- **File:** `internal/agent/prompt.go:410-421`
- **Severity:** Low — nil builder returns ""; LoadTasksFile error; file exists but `HasActiveInstructions=false` returns "".
- **Approach:** Temp workspace with various HEARTBEAT.md contents.

### C9 — formatPinned: empty values, sort order
- **File:** `internal/agent/prompt.go:904-906`
- **Severity:** Low — map entry with empty value (skipped); all values empty producing "(none)"; sorting order.
- **Approach:** Provide pinned map with mixed empty/non-empty values.

### C10 — estimateMessagesTokens / messageContentString: zero coverage
- **File:** `internal/agent/prompt_budget.go:323-353`
- **Severity:** High — ToolCalls content (array type), nil content, content with Name set all untested.
- **Approach:** Table-driven test with realistic ChatMessage fixtures.

### C11 — minProtectedTokens: boundary values
- **File:** `internal/agent/prompt_budget.go:186-194`
- **Severity:** Medium — cap=0→1, cap=63→63, cap=64→16, cap=100→25 boundaries untested.
- **Approach:** Table-driven test with boundary values.

### C12 — estimatePacketBudget: usable <= 0
- **File:** `internal/agent/prompt_budget.go:274-276`
- **Severity:** Medium — when `outputReserve + safetyMargin >= maxInput`, usable becomes <= 0.
- **Approach:** Configure maxInput=1000, outputReserve=600, safetyMargin=500 → usable=-100; assert safe behavior.

### C13 — firstNonEmptyEval: zero coverage
- **File:** `internal/agent/context_evaluation.go:73-80`
- **Severity:** Low — all empty, all non-empty, leading empty followed by non-empty.
- **Approach:** Table-driven test using predefined eval strings.

### C14 — renderTaskCard: truncation at exact maxChars, Plan section
- **File:** `internal/agent/task_card.go:97-147`
- **Severity:** Medium — GIVEN exact char boundary truncation; Plan list rendering; Constraints/Decisions/OpenQuestions null vs empty (no output).
- **Approach:** Full-card rendering test with all fields populated; truncation exactly at maxChars.

### C15 — statusOrDefault: zero coverage
- **File:** `internal/agent/task_card.go:89-95`
- **Severity:** Low — empty, whitespace, non-empty status values.
- **Approach:** Table-driven test.

### C16 — buildHistorySummary: zero coverage
- **File:** `internal/agent/semantic_compression.go:71-87`
- **Severity:** Medium — empty rows, rows < maxItems, rows > maxItems (takes last N), maxItems=0 defaults to 6, empty role.
- **Approach:** Table-driven test with db.Message fixtures.

### C17 — shouldTriggerContextManager: turn-interval + OverBudget via 85%
- **File:** `internal/agent/prompt_budget.go:209-221`
- **Severity:** Medium — turn-interval trigger (turnCount%12==0) and OverBudget via `BudgetUsedPercent>=85` untested.
- **Approach:** Table-driven with all trigger-policy combos and boundary turn counts (11, 12, 13).

### C18 — validateTaskCardUpdate / validateSummaryProposals: zero coverage
- **File:** `internal/agent/context_manager.go:390-416`
- **Severity:** Medium — lists exceeding 20 items, items > 500 chars; summaries > 1000 chars, refs > 20.
- **Approach:** Table-driven validation of each field's limit.

### C19 — contextManagerProviderRejectedToolChoice: zero coverage
- **File:** `internal/agent/context_manager.go:431-440`
- **Severity:** Low — nil error, "tool_choice" not in message, each "no endpoints"/"not support"/"unsupported"/"invalid" variant.
- **Approach:** Table-driven error string matching.

### C20 — cleanRefs: zero coverage
- **File:** `internal/agent/semantic_compression.go:123-138`
- **Severity:** Low — all empty, all duplicates, mixed entries.
- **Approach:** Table-driven test.

---

## Subagents & Job Registry

### S1 — EnqueueService: zero coverage
- **File:** `internal/agent/subagents.go:198-243`
- **Severity:** High — entire service-originated subagent path untested: tool restrictions, profile, prompt snapshot, service metadata, approval token, requester identity.
- **Approach:** Add test calling `EnqueueService` with full `ServiceSubagentRequest`; verify stored metadata and lifecycle events.

### S2 — Abort (manager-level): Cancel path, not-found, not-abortable
- **File:** `internal/agent/subagents.go:373-401`
- **Severity:** Medium — only `DB.AbortQueuedSubagentJob` tested directly, not `mgr.Abort()`. Cancel fast-path, not-found, and running-not-abortable untested.
- **Approach:** Test Abort against a running job (Jobs.Cancel path) and a queued job (DB path).

### S3 — deliverCompletion: Runtime fallback
- **File:** `internal/agent/subagents.go:462-474`
- **Severity:** Medium — the `m.Deliver == nil && m.Runtime != nil` fallback path never exercised.
- **Approach:** Construct manager with `Deliver: nil` and `Runtime.Deliver` set; verify delivery occurs via runtime.

### S4 — Concurrent Enqueue / signalN
- **File:** `internal/agent/subagents.go:158,480-496`
- **Severity:** Medium — no test enqueues multiple jobs concurrently or verifies `signalN` notification; notifyCh capacity and non-blocking send logic untested.
- **Approach:** Spawn N goroutines calling Enqueue simultaneously; verify all jobs are processed.

### S5 — JobRegistry Observer methods: zero coverage
- **File:** `internal/agent/job_registry.go:282-364`
- **Severity:** High — `OnTextDelta`, `OnToolCall`, `OnError`, `OnCompletion` have zero coverage. Only `OnToolResult` and `OnToolLifecycle` tested.
- **Approach:** Add table-driven tests for each method covering nil registry, empty text, and error/non-error paths.

### S6 — JobRegistry.Fail: zero coverage
- **File:** `internal/agent/job_registry.go:182-195`
- **Severity:** Medium — the `"error"` event type, `data["message"]` merging, and `markTerminal("failed")` path are all untested.
- **Approach:** Call `Fail` on a live job; assert snapshot shows status `"failed"` and event type `"error"`.

### S7 — JobRegistry.AttachCancel: entry==nil early-return
- **File:** `internal/agent/job_registry.go:105-114`
- **Severity:** Low — the nil entry guard is untested.
- **Approach:** Call `AttachCancel` with both valid and unknown job ID.

### S8 — JobRegistry.Wait: cancelled context
- **File:** `internal/agent/job_registry.go:271-273`
- **Severity:** Low — `ctx.Done()` branch untested; only happy-path Wait (job already terminal) exercised.
- **Approach:** Call `Wait` with already-cancelled context on non-terminal job; assert returns false.

### S9 — JobRegistry: concurrent Register/Publish/Subscribe
- **File:** `internal/agent/job_registry.go` (entire)
- **Severity:** Medium — no `-race` test for concurrent access to the registry.
- **Approach:** Run `go test -race` with test spawning 10 goroutines doing Register/Publish/Subscribe simultaneously.

### S10 — JobRegistry: maxTracked eviction with only non-terminal jobs
- **File:** `internal/agent/job_registry.go:397-413`
- **Severity:** Low — when all tracked jobs are non-terminal and count exceeds maxTracked, nothing is evicted and map grows unbounded.
- **Approach:** Register maxTracked+1 non-terminal jobs; assert non-terminal jobs preserved; verify no panic.

---

## Agent CLI Runner

### M1 — Manager Start/Stop/Enqueue/Abort lifecycle: entirely untested
- **File:** `internal/agentcli/manager.go:59-314`
- **Severity:** High — nil guards, double-start, restart reconciliation, timeout paths, all rejection paths entirely untested.
- **Approach:** Table-driven test with mock DB/Registry covering each lifecycle method and guard clause.

### M2 — executeRun: builder failure, context deadline, context cancelled
- **File:** `internal/agentcli/manager.go:346-486`
- **Severity:** High — buildCommand failure path, context deadline exceeded → timed_out, context canceled → aborted all untested.
- **Approach:** Inject adapter returning error; run with DeadlineExceeded and Cancelled contexts.

### M3 — executable.go: entire file zero test coverage
- **File:** `internal/agentcli/executable.go`
- **Severity:** High — binary resolution, PATH fallback, `executableCandidates`, `isExecutableFile` all completely untested.
- **Approach:** Dedicated test file covering: empty binary, nil env, PATH empty, binary not found, executable bit missing, Windows PATHEXT.

### M4 — ChatManager StartTurn: rejection paths
- **File:** `internal/agentcli/chat_manager.go:123-135`
- **Severity:** Medium — native continuation rejected when caps don't support it, empty user_message → error both untested.
- **Approach:** Request ContinuationNative with Codex runner (no native caps); call with whitespace-only UserMessage.

### M5 — ChatManager: appendMessage failure → turn finalized as failed
- **File:** `internal/agentcli/chat_manager.go:176-183`
- **Severity:** Medium — rollback path when message insertion fails entirely untested.
- **Approach:** Mock DB to fail on message insert; assert turn status=Failed and no orphaned state.

### M6 — ChatManager ReconcileOnStartup: DB error, zero affected rows
- **File:** `internal/agentcli/chat_manager.go:280-292`
- **Severity:** Medium — DB error and zero-rows paths untested.
- **Approach:** Mock DB returning error; assert error propagated. Test with zero affected rows.

### M7 — ChatManager helpers: nil DB guard, nil manager/registry
- **File:** `internal/agentcli/chat_manager.go:586-704`
- **Severity:** Low — `bumpChatSessionMeta` nil DB guard, `chatRunner` nil manager/registry.
- **Approach:** Direct nil-receiver tests for each helper.

### M8 — Process.Run: binary not found, non-ExitError, stderr-only
- **File:** `internal/agentcli/process.go:51-109`
- **Severity:** Medium — binary-not-found (exit code -1), non-ExitError (SIGKILL), stderr-only output with no stdout.
- **Approach:** Use nonexistent binary; send SIGKILL to external process; run binary that only writes to stderr.

### M9 — Process.Run: nil onEvent callback, negative defaults
- **File:** `internal/agentcli/process.go:24-48`
- **Severity:** Low — nil callback should not panic; negative chunkMaxBytes/previewMaxBytes should apply defaults.
- **Approach:** Call Run with onEvent=nil; assert no panic. Call NewProcessManager with -1 values.

### M10 — Process: ringBuffer overflow, scanner buffer overflow
- **File:** `internal/agentcli/stream.go:53-106`
- **Severity:** Medium — writing >64KB output triggers ring buffer wrap; scanner buffer overflow beyond chunkMaxBytes+1.
- **Approach:** Pipe a line longer than scanner buffer; assert error event. Write >64KB and verify preview truncation.

### M11 — result_extract.go: entire file zero coverage
- **File:** `internal/agentcli/result_extract.go`
- **Severity:** High — `finalTextExtractor` scoring logic, all `extract*FinalText` per-runner variants, `looksMachineOriented`, `extractString` type dispatch completely untested.
- **Approach:** Dedicated test file with table-driven tests for each extractor variant and helper.

### M12 — Detect: Gemini dual-failure path
- **File:** `internal/agentcli/detect.go:65-66`
- **Severity:** Low — when both `--version` and `--help` fail, `RunnerStatusError` is returned. Never triggered.
- **Approach:** Write fake binary that fails both commands; assert error status.

### M13 — Detect: empty Binary, WorkDir propagation, firstLine edges
- **File:** `internal/agentcli/detect.go`
- **Severity:** Low — empty Binary string (should fail resolution), opts.WorkDir for version/auth probes, `firstLine` with empty/whitespace input.
- **Approach:** Table-driven test for each edge case.

### M14 — RunnerPermissions: NormalizeRunnerPermissionRequest rejections
- **File:** `internal/agentcli/runner_permissions.go:29-42`
- **Severity:** Low — empty target, target=".", target="/" should all return invalid.
- **Approach:** Table test for each rejection; assert error messages.

### M15 — RunnerPermissions: runnerPermissionFromMeta type branches
- **File:** `internal/agentcli/runner_permissions.go:57-84`
- **Severity:** Low — nil meta, missing key, map[string]any input, JSON-marshaled other type, unmarshal failure.
- **Approach:** Table test with each meta type variant.

### M16 — ChatAdapters: deep nesting, session ID edges, unknown-type suppression
- **File:** `internal/agentcli/chat_adapters.go:50-308`
- **Severity:** Medium — `findSessionRef` deep nesting, `looksSessionID` edge cases, structured unknown-type returns nil, `extractString` type dispatch.
- **Approach:** Table-driven tests for each adapter helper and edge case.

---

## Structured Autonomy

### T1 — Non-autonomous event type (EventUserMessage)
- **File:** `internal/agent/structured_autonomy.go:16`
- **Severity:** Medium — `EventUserMessage` should return `false, nil` and be a no-op; never tested.
- **Approach:** Call `handleStructuredAutonomy` with a user-message event; assert returns false, nil.

### T2 — Nil r.Tools, empty tasks
- **File:** `internal/agent/structured_autonomy.go:16-21`
- **Severity:** Low — nil `r.Tools` and empty `env.Tasks` should both return `false, nil`.
- **Approach:** Set `r.Tools = nil`; call with empty tasks array.

### T3 — Mixed success/failure across multiple tasks
- **File:** `internal/agent/structured_autonomy.go:37-72`
- **Severity:** High — only single-task success and failure tested. N tasks with partial success (e.g., 2/3) completely untested.
- **Approach:** Enqueue 3 tasks where the 2nd fails; assert 2/3 succeeded and failure message is included.

### T4 — validateStructuredValueDepth: all type branches
- **File:** `internal/agent/structured_autonomy.go:100-172`
- **Severity:** High — string, boolean, integer, number, array, and default type validators are completely untested.
- **Approach:** Table-driven test covering each type with valid and invalid inputs.

### T5 — Max depth exceeded (>32 levels)
- **File:** `internal/agent/structured_autonomy.go:101-103`
- **Severity:** Low — deeply nested object exceeding max recursion depth.
- **Approach:** Create a 33-level deeply nested object; assert error returned.

### T6 — additionalProperties: false with unknown field
- **File:** `internal/agent/structured_autonomy.go:117-118`
- **Severity:** Medium — schema with `additionalProperties: false` and an unknown field should error; untested.
- **Approach:** Define schema with `additionalProperties: false` and pass extra field; assert error.

### T7 — sliceItems with reflected slices ([]string, []int)
- **File:** `internal/agent/structured_autonomy.go:199-207`
- **Severity:** Low — only `[]any` branch tested; typed slices like `[]string`, `[]int` go through reflect.
- **Approach:** Pass `[]string{"a","b"}`; assert returns `[]any{"a","b"}, true`.

### T8 — Scope resolution fallback
- **File:** `internal/agent/structured_autonomy.go:26-29`
- **Severity:** Low — when `ResolveScopeKey` fails or returns empty, falls back to `ev.SessionKey`. Untested.
- **Approach:** Mock DB to return error from `ResolveScopeKey`; verify fallback to SessionKey.

---

## Task Checklist

Track completion here by checking off items as tests are written or bugs fixed.

### Bugs

- [x] B1 — Add panic recovery to `workerLoop` (`subagents.go:245-264`)
- [x] B2 — Add terminal guard to `JobRegistry.Publish` (`job_registry.go:133-165`)
- [x] B3 — Replace `int` counters with `atomic.Int32` in `runtime_test.go`

### Agent Runtime & Execution

- [x] R1 — Test `RunBackground()` all paths
- [x] R2 — Test `RunBackground()` concurrent calls with `-race`
- [x] R3 — Test `handleTurnPostCleanup` with `DisableRollingConsolidation=true`
- [x] R4 — Test `boundTextResult` output at exact `MaxToolBytes`
- [x] R5 — Test `isNewSessionCommand` with whitespace/trailing spaces
- [x] R6 — Test `deliveryTarget` with nil `ev.Meta`
- [x] R7 — Test `cloneMap(nil)` returns empty map
- [x] R8 — Test `releaseEvent` with nil `done` func
- [x] R9 — Test `shouldAutoDeliver` with non-bool `auto_reply_enabled`
- [x] R10 — Test tool loop limit exceeded → approval Allowed=true
- [x] R11 — Test context deadline exceeded mid-loop
- [x] R12 — Test `handleToolLoopLimitExceeded` with nil ApprovalBroker in Ask mode
- [x] R13 — Test provider returns nil Content with no tool calls
- [x] R14 — Test broken tool_call markup yielding garbage parse
- [x] R15 — Test `narrateApprovalRequired` nil receiver + empty content
- [x] R16 — Test `terminalToolResultText` invalid JSON + nil result.OK
- [x] R17 — Test session lock ref-counting race
- [x] R18 — Test `scheduleIdlePrune` with zero/negative delay
- [x] R19 — Test `isDirectMessageEvent` all channel types
- [x] R20 — Test `handleNewSession` Builder set but Consolidator nil
- [x] R21 — Test `releaseSessionLock` nil Runtime guard
- [x] R22 — Test `ensureSessionScope` already linked to same scope

### Tool Access & Policy

- [x] A1 — Test `exposedToolsForTurn` nil registry + nil Runtime
- [x] A2 — Test `filterToolsForContext` nil registry + nil Runtime non-nil reg
- [x] A3 — Test `filterToolsForContext` empty AllowedTools
- [x] A4 — Test `selectedToolGroups` empty intent
- [x] A5 — Test `guardToolExecution` nil tool + empty SignKey + Audit error
- [x] A6 — Test `enforceSkillPolicy` nil tool + WebSearch empty AllowedHosts
- [x] A7 — Test `resolveProfile` disabled + unknown name
- [x] A8 — Test `validateProfileWritablePath` nil/empty path
- [x] A9 — Test `profileNameForEvent` non-string type
- [x] A10 — Test deny_all policy mode
- [x] A11 — Test hidden tools exclusion
- [x] A12 — Test `capabilityRankForPolicy` Privileged + unknown
- [x] A13 — Test unsupported policy mode error
- [x] A14 — Test tool validation with nil tool
- [x] A15 — Test boolean coercion from "yes"/"no"
- [x] A16 — Test scalar-to-array coercion with object items type

### Context Manager & Prompt

- [x] C1 — Test `requestContextManagerCompaction`
- [x] C2 — Test `parseContextManagerToolResponse`
- [x] C3 — Test `buildUserContent`
- [x] C4 — Test `imagePart` + `readCappedFile`
- [x] C5 — Test `toolCallsFromPayload` + `attachmentsFromPayload` + serialization
- [x] C6 — Test `payloadStringValue` / `payloadIntValue` / `payloadInt64Value`
- [x] C7 — Test `cachedEmbed` error paths + LRU eviction
- [x] C8 — Test `currentHeartbeatText` nil builder + errors
- [x] C9 — Test `formatPinned` empty values + sort order
- [x] C10 — Test `estimateMessagesTokens` + `messageContentString`
- [x] C11 — Test `minProtectedTokens` boundary values
- [x] C12 — Test `estimatePacketBudget` usable <= 0
- [x] C13 — Test `firstNonEmptyEval`
- [x] C14 — Test `renderTaskCard` truncation + Plan section
- [x] C15 — Test `statusOrDefault`
- [x] C16 — Test `buildHistorySummary`
- [x] C17 — Test `shouldTriggerContextManager` turn-interval + 85% OverBudget
- [x] C18 — Test `validateTaskCardUpdate` + `validateSummaryProposals`
- [x] C19 — Test `contextManagerProviderRejectedToolChoice`
- [x] C20 — Test `cleanRefs`

### Subagents & Job Registry

- [x] S1 — Test `EnqueueService` service-originated path
- [x] S2 — Test `Abort` Cancel path + not-found + not-abortable
- [x] S3 — Test `deliverCompletion` Runtime fallback
- [x] S4 — Test concurrent `Enqueue` / `signalN`
- [x] S5 — Test JobRegistry Observer OnTextDelta/OnToolCall/OnError/OnCompletion
- [x] S6 — Test `JobRegistry.Fail`
- [x] S7 — Test `JobRegistry.AttachCancel` entry==nil
- [x] S8 — Test `JobRegistry.Wait` cancelled context
- [x] S9 — Test JobRegistry concurrent Register/Publish/Subscribe with `-race`
- [x] S10 — Test JobRegistry maxTracked eviction with only non-terminal jobs

### Agent CLI Runner

- [x] M1 — Test Manager Start/Stop/Enqueue/Abort lifecycle
- [x] M2 — Test `executeRun` builder failure + context deadline + cancelled
- [x] M3 — Test `executable.go` (entire file)
- [x] M4 — Test ChatManager `StartTurn` rejection paths
- [x] M5 — Test ChatManager `appendMessage` failure rollback
- [x] M6 — Test ChatManager `ReconcileOnStartup` DB error + zero rows
- [x] M7 — Test ChatManager nil DB guard + nil manager/registry
- [x] M8 — Test `Process.Run` binary not found + non-ExitError + stderr-only
- [x] M9 — Test `Process.Run` nil onEvent + negative defaults
- [x] M10 — Test ringBuffer overflow + scanner buffer overflow
- [x] M11 — Test `result_extract.go` (entire file)
- [x] M12 — Test Detect Gemini dual-failure
- [x] M13 — Test Detect empty Binary + WorkDir + firstLine edges
- [x] M14 — Test `NormalizeRunnerPermissionRequest` rejections
- [x] M15 — Test `runnerPermissionFromMeta` type branches
- [x] M16 — Test ChatAdapters deep nesting + sessionID + unknown-type

### Structured Autonomy

- [x] T1 — Test non-autonomous event type (EventUserMessage)
- [x] T2 — Test nil r.Tools + empty tasks
- [x] T3 — Test mixed success/failure across multiple tasks
- [x] T4 — Test `validateStructuredValueDepth` all type branches
- [x] T5 — Test max depth exceeded (>32 levels)
- [x] T6 — Test `additionalProperties: false` with unknown field
- [x] T7 — Test `sliceItems` with reflected slices
- [x] T8 — Test scope resolution fallback
