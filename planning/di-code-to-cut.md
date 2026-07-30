# di-code-to-cut.md — Dead Code Audit After Runner-First Migration

**Generated:** 2026-06-07
**Scope:** `or3-intern/internal/{approval,config,doctor,providers,tools,triggers}`
**Context:** Native agent harness (`internal/agent/`) deleted. Runners (`internal/agentcli/`) handle all AI reasoning + tool execution. Doctor/config/tools/providers/approval/triggers packages still contain leftovers that fight for their life.

This document started as a plan. The checklist below tracks the cleanup applied from it.

---

## 0. Headline Numbers

| Package | Files Audited | Full Delete | Partial Cut | Clean | Est. Lines Cut |
|---|---:|---:|---:|---:|---:|
| `internal/approval` | 19 | 0 | 6 | 13 | ~110 |
| `internal/config` | 25 | 0 | 3 | 22 | ~70 (+ 15 fields annotated dead) |
| `internal/doctor` | 24 | 0 | 9 | 15 | ~120 findings + 1 fix branch |
| `internal/providers` | 5 | **2** | 2 | 1 | ~434 |
| `internal/tools` | 7 | **1** | 4 | 2 | ~218 (~64% of package) |
| `internal/triggers` | 7 | 0 | 2 | 5 | ~30 |
| **TOTAL** | **87** | **3** | **26** | **58** | **~982 + 15 dead config fields** |

**The big wins:**
- `internal/providers` — two full files are 100% dead (schema sanitizer + profile). They existed only to feed tool schemas into the deleted agent's LLM calls.
- `internal/tools/metadata.go` — full delete, zero references.
- `internal/tools/tools.go` — 100 of 123 lines are dead. The `Tool` interface that drove the model-callable registry has no consumer.
- Doctor package — 10+ findings that gate on dead `run_skill` / `exec` / `spawn_subagent` tool names.

**No package collapses entirely.** Every package is still load-bearing for the runner-first architecture — it just has crud.

---

## 1. `internal/providers` — The Biggest Win

This package was used by the old agent to (a) make chat/turn LLM calls, (b) sanitize tool schemas for the LLM, and (c) select per-provider tool/stream/retry policies. (a) is dead (runners handle their own chat). (b) and (c) are entirely dead.

### Full Deletes

| File | Lines | Why dead |
|---|---:|---|
| `schema_sanitizer.go` | 151 | Sanitized tool defs before sending to LLM. External runners manage their own tool schemas. **Zero external callers.** |
| `schema_sanitizer_test.go` | 63 | Tests for the above. |
| `profile.go` | 109 | `ProviderProfile`, `ToolSchemaPolicy`, `StreamPolicy`, `ProviderRetryPolicy`, `OpenAICompatibleProfile`, `OpenRouterCompatibleProfile`, `LocalCompatibleProfile`, `SelectProviderProfile`. All zero external callers. |

### Partial Cuts — `openai.go` (~55 lines)

Dead symbols to delete (lines 103-156):
- `SupportsExplicitPromptCache()` — zero callers, only own test
- `BuildCacheAwareSystemContent()` — zero callers, only own test
- `BuildCacheAwareTieredContent()` — zero callers, only own test
- `cacheableTextBlock()` — helper for above

These were Anthropic prompt-cache helpers for the old agent's chat. Runners handle caching.

After `profile.go` deletion, also delete:
- `Client.ProviderName` field (line 20) — only consumed by dead `Client.ProviderProfile()` method

### Partial Cuts — `openai_test.go` (~56 lines)

Delete these tests:
- `TestClient_SupportsExplicitPromptCache` (lines 37-44)
- `TestClientProviderProfile_UsesProviderName` (lines 46-51)
- `TestClientProviderProfile_UsesAPIBaseFallback` (lines 53-58)
- `TestBuildCacheAwareSystemContent` (lines 60-92)

### Collateral Cleanup Outside the Package

- `cmd/or3-intern/runtime_build_config_test.go:73` — remove `client.ProviderProfile()` assertion
- `cmd/or3-intern/main.go` — remove `ProviderName` assignment on the `Client` struct after field removal

### What Stays (Alive)

`Client` struct, `New()`, `Embed()`, `Chat()`, `ChatMessage`/`ChatCompletionRequest`/`Response`, `ToolDef`/`ToolFunc`/`ToolCall`, `ProviderError`, `IsTransientError()`, `EmbeddingFingerprint()`, `Fallback`, `EmbeddingRequest`/`Response`. These are used by embeddings (`memory`, `memorysvc`), memory consolidation, service models, controlplane, migrate_openclaw, setup_cmd.

---

## 2. `internal/tools` — 64% Reduction Possible

This package shrank from a full model-callable tool registry to a sliver. The sliver has more sliver to give.

### Full Delete

| File | Lines | Why dead |
|---|---:|---|
| `metadata.go` | 12 | `ToolMetadata` struct + `ToolGroupService` constant. **Zero references anywhere.** |

### Partial Cuts

**`tools.go` (123 lines → ~20 lines):**
Delete everything except the `CapabilityLevel` type alias:
- `Tool` interface (lines 23-29) — old model-callable tool interface, zero references
- `CapabilityReporter`, `CapabilityForParamsReporter`, `CapabilityForContextParamsReporter` interfaces (lines 31-41) — zero references
- `ToolCapability()` / `ToolCapabilityForContext()` (lines 43-67) — zero references
- `Base` struct + `SchemaFor` method (lines 69-80) — zero references
- `stringParam()` / `floatParam()` / `boolParam()` (lines 82-123) — unexported, unused

Better long-term: have the 6 callers import `internal/capability` directly and **delete `tools.go` entirely**.

**`names.go` (37 lines → ~10 lines):**
Delete these 7 unused constants (lines 7-14):
- `ToolNameReadFile`, `ToolNameSearchFile`, `ToolNameWriteFile`, `ToolNameEditFile`, `ToolNameListDir`, `ToolNameWebFetch`, `ToolNameWebSearch`, `ToolNameSpawnSubagent`

Keep: `ToolNameExec`, `IsWriteToolName`, `IsToolNotAvailableThisTurn` (used by `cmd/.../service_agents.go`).

**`result.go` (14 lines → ~10 lines):**
Delete unused aliases:
- `EncodeToolResult` (line 11) — zero references
- `EncodeToolFailure` (line 13) — zero references

Long-term: 14 files could import `internal/actionresult` directly and `result.go` could be deleted.

### What Stays (Alive)

- `brave.go` — `BraveSearchConfigured`, `AppendBraveSearchHostIfMissing` (used by `security_setup.go`)
- `child_env.go` — `BuildChildEnv`, `EffectiveChildEnvAllowlist` (used by `agentcli/env.go` and `mcp/manager.go`)
- `paths.go` — `CanonicalizePath`, `CanonicalizeRoot` (used by `agentcli/cwd.go`)

### Package Post-Cut

The package would shrink to **4 files** of genuine process/path helpers for the runner system. The "tool" in the package name becomes a misnomer — consider renaming to `internal/hostenv` or `internal/runnerhelpers` in a future pass.

---

## 3. `internal/doctor` — 10+ Dead Findings

Doctor is the diagnostic spine for the runner-first architecture and stays. The dead code is at the **finding level** — diagnostic checks that gate on `run_skill` / `run_skill_script` / `exec` / `spawn_subagent` tool names that no runner consumes.

### Findings to Delete

**`engine_channels.go`** (3 findings):
- `channels.open_access_skills_without_profile` (lines 92-99)
- `channels.open_access_skill_exec` (lines 126-133)
- `channels.open_access_exec_shell` (lines 118-125) — borderline; `EnableExecShell` is still enforced by terminal route, but not in channel reachability reasoning. **Ask the human** before deleting this one.

**`engine_exec.go`** (1 helper):
- `publicIngressCanReachSkillExec` (lines 46-57) — only caller is `engine_skills.go:53`

**`engine_hardening.go`** (2 findings):
- `quotas.disabled` (lines 22-31)
- `quotas.unset` (lines 32-43) — nothing reads `Hardening.Quotas.*` at runtime any more; only this file and the configure TUI editor

**`engine_skills.go`** (2 findings):
- `skills.public_ingress_reachable` (lines 53-60)
- `skills.webhook_reachable` (lines 61-70)

**`engine_webhook.go`** (4 findings):
- `webhook.skills_without_profile` (lines 74-81)
- `webhook.profile_subagents` (lines 95-102)
- `webhook.exec_shell_exposure` (lines 119-126)
- `webhook.skill_exec_exposure` (lines 127-134)
- **Reconsider:** `webhook.profile_writable_paths` (lines 103-110) — `AllowedTools`/`WritablePaths` no longer constrain runner turns

**`engine_test.go`** (update):
- Update `TestDoctorFindingsHaveConsumerRepairFields` and `TestChannelExposureFindings_PerChannel` to not exercise the dead findings

**`fix.go`** (1 branch):
- `case "quotas.unset":` (lines 52-55) — dead after `engine_hardening.go` cut

### Severity Rescoping — `engine_provider.go`

Demote these from `blockOnStartup=true` to plain warn (or `SeverityInfo`):
- `provider.endpoint_missing`
- `provider.api_key_missing`
- `provider.chat_model_missing`

Reason: these gated the *native agent's* OpenAI chat. External runners bring their own auth. They should not block startup. Embedding model check stays as-is (embeddings are still live in `memory` / `memorysvc`).

### Optional Rename

`engine_agentcli.go` + `engine_agentcli_test.go` → `engine_runners.go` + `_test.go`. The function is `agentCLIFindings` and emits the `runners` area — file name is historical. Purely cosmetic; not blocking.

### What Stays (Alive)

Everything in `engine_runtime.go` (hosted profile checks + probe), all of `engine_security.go` / `engine_service.go` / `engine_mcp.go` / `engine_network.go` / `engine_filesystem.go` / `engine_approvals.go` / `engine_config.go`, the live parts of `engine_channels.go` and `engine_profiles.go`, the live `fix.go` cases, all of `render.go` / `report.go`.

---

## 4. `internal/approval` — Clean Package, ~110 Lines of Crud

Approval is the only channel-runner coordination point (runners ask `EvaluateRunnerPermission`). Most of the package is alive. The dead code is concentrated in **unused evaluation types**, **orphaned internal functions**, and **one unused API type**.

### Cuts

| File | Symbol | Lines | Why dead |
|---|---|---:|---|
| `types.go` | `SkillEvaluation` struct | 85-99 | Never instantiated. No `EvaluateSkill` function ever written. |
| `types.go` | `SecretAccessEvaluation` struct | 101-108 | Never instantiated. No `EvaluateSecretAccess` function ever written. |
| `types.go` | `ToolQuotaEvaluation` struct | 119-128 | Never instantiated. No `EvaluateToolQuota` function ever written. |
| `types.go` | `MessageSendEvaluation` struct | 130-139 | Type alive, but only consumer `EvaluateMessageSend` is dead. Delete both. |
| `types.go` | `Broker.Workspace` field | 51 | Set by `security_setup.go` but never read anywhere. |
| `evaluate.go` | `EvaluateMessageSend()` | 48-64 | Never called anywhere. |
| `tokens.go` | `issueTokenForRequest()` | 84-99 | Old standalone token issuance. Replaced by `signTokenFromRecord` callback path. |
| `requests.go` | `createAllowlistFromRequest()` | 182-192 | Old allowlist derivation. Replaced by `allowlistRecordFromRequest` callback. |
| `audit.go` | `AuditExecEvent()` | 77-89 | Never called. |
| `api_types.go` | `PairingListItem` struct | 97-107 | Never used outside this file. |
| `api_types.go` | `ToPairingListItem()` | 109-120 | Never used outside this file. |

### What Stays (Alive)

Everything else. `EvaluateExec` (76 callers — terminal, channel approvals, service routes), `EvaluateRunnerPermission` (used by `agentcli/chat_manager.go`), all pairing/devices/tokens/audit/preview/queries/request_create logic, all live types.

### Key Pattern

The package has 3 evaluation types that were *designed* but *never implemented*. Dead code by design, not by deletion oversight.

---

## 5. `internal/config` — Mostly Alive, ~70 Lines + 15 Dead Fields

Config is the central hub (183+ importers). Mostly healthy. The dead code is **two functions with zero callers** and a long tail of config fields that are persisted/edited but never enforced at runtime.

### Functions to Delete

| File | Function | Lines | Why dead |
|---|---|---:|---|
| `access_profiles.go` | `AccessLevelToDeviceRole()` | 103-114 | Zero external callers. |
| `access_profiles.go` | `ExpandAccessProfile()` | 182-205 | Zero external callers. |

### Tests to Delete

| File | Test | Why dead |
|---|---|---|
| `access_profiles_test.go` | `TestExpandAccessProfileWorkspaceDir` (lines 24-38) | Tests the dead `ExpandAccessProfile`. |

### Config Fields Dead at Runtime (Annotate, Don't Delete)

These are persisted in `config.json` and surfaced in the configure TUI but **no runtime path enforces them** now that the agent harness is gone. Removing them breaks backward compat with existing user configs. Add `// Deprecated:` comments; don't delete the fields.

| Field | Reason dead at runtime |
|---|---|
| `ContextConfig.Sections.*` (lines 167-178) | Old agent prompt-packet budgets. No runner reads these. |
| `ContextConfig.Pressure.*` (lines 185-189) | Same. |
| `ContextConfig.Retrieval.*` (lines 180-183) | Same. |
| `ContextConfig.Artifacts.*` (lines 195-197) | Same. |
| `ContextConfig.TaskCard.*` (lines 199-204) | Same. |
| `ContextConfig.OutputReserveTokens` (157) | Same. |
| `ContextConfig.SafetyMarginTokens` (158) | Same. |
| `SessionCache` (103) | No runtime consumer. |
| `MaxToolBytes` (105) | No runtime consumer. |
| `HardeningConfig.Quotas.MaxSubagentCalls` (258) | Subagents gone. |
| `HardeningConfig.Quotas.MaxSessionSubagentCalls` (262) | Subagents gone. |
| `HardeningConfig.MetadataScanner` (226) | No runtime consumer. |
| `AccessProfileConfig.AllowSubagents` (719) | Subagents gone. |

The 4 `ContextConfig` fields with `SeverityWarn` only (Mode, MaxInputTokens, Tools.DynamicExpose, ContextManagerConfig.Enabled) are partially alive — read by `uxstate.go` for status display. Keep but be aware.

`HardeningConfig.Sandbox.Enabled` and `.BubblewrapPath` are partially alive (read by `skills_cmd.go` and `setup_cmd.go`). Keep.

### What the Runner System Actually Reads (Sanity Check)

`internal/agentcli/` reads: `AgentCLIConfig` (all fields — runner selection, modes, timeouts, codex home, etc.), `RunnerFirst()` → `AgentCLI.Enabled`, `Tools.RestrictToWorkspace`, `Tools.EnableExec`.

`internal/app/` reads: `AgentCLIConfig`, `BootstrapMaxChars`, `BootstrapTotalMaxChars`, `VectorK`, `FTSK`, `MemoryRetrieve`, `SoulFile`, `AgentsFile`, `ToolsFile`, `IdentityFile`, `MemoryFile`, `ConsolidationEnabled`, `ContextManager.Enabled`.

Everything else in the config struct is platform plumbing (channels, security, auth, approvals, MCP, services, heartbeats, cron) or the dead fields above.

---

## 6. `internal/triggers` — Mostly Healthy, ~30 Lines of Crud

Triggers (webhooks, filewatch, structured tasks) all feed the bus, which `runWorkers` drains into the runner turn orchestrator. No full-file deletes.

### Cuts

| File | Symbol | Lines | Why dead |
|---|---|---:|---|
| `triggers.go` | `TriggerMeta` struct | 9-15 | Zero external references. Never instantiated. |
| `triggers.go` | `StructuredEventJSON()` | 46-55 | Zero external references. Never called. |
| `structured_tasks.go` | `StructuredTasksFromMeta()` | 51-60 | Zero external references. Never called. |

### Unexport (Not Delete)

`structured_tasks.go` line 62: `ParseStructuredTasksJSON` → `parseStructuredTasksJSON` (only used internally by `ParseStructuredTasksText`).

### What Stays (Alive)

`StructuredEvent`, `StructuredEventMap()`, `MetaKeyStructuredEvent`, `MetaKeyStructuredTasks`, `StructuredToolCall`, `StructuredTaskEnvelope`, `StructuredTasksMap()`, `ParseStructuredTasksText()`, all of `filewatch.go`, all of `webhook.go`, all of `*_test.go`.

### Wiring Sanity Check

| Trigger | Instantiated in `main.go` | Handled by orchestrator |
|---|---|---|
| Webhook | line 543 | `turn_orchestrator.go:229` (`EventWebhook`) |
| FileWatch | line 552 | `turn_orchestrator.go:229` (`EventFileChange`) |
| Heartbeat | `heartbeatServiceForCommand()` | `turn_orchestrator.go:229` (`EventHeartbeat`) |

All three trigger types are live. No orphans.

---

## 7. Cross-Cutting Concerns (Outside the 6 Packages)

These showed up in multiple audits as collateral cleanup:

1. **`config.AccessProfileConfig.AllowSubagents`** and **`HardeningConfig.Quotas.MaxSubagentCalls` / `MaxSessionSubagentCalls`** — subagents are gone. Edit by `configure_tui.go`, surfaced via `controlplane.go`/capabilities, reported by doctor — but no code enforces them. Annotate + hide from TUI in a follow-up.

2. **`tools.ToolNameSpawnSubagent`** and **`config/access_profiles.go:79`** `"spawn_subagent"` entry — dead. `runnercontext/bootstrap_test.go:18` actively forbids documenting `spawn_subagent` in runner docs.

3. **`"run_skill"` / `"run_skill_script"`** in AllowedTools — referenced only by `skills_cmd.go` (which deletes them from the advertised tool set) and the doctor checks marked for deletion. The actual `run_skill` execution path runs via skilldiag/runner integrations, not via the agent tool registry.

4. **Package rename candidate:** `internal/tools` is misnamed post-cut. The remaining 4 files are process/path helpers. Consider `internal/hostenv` or `internal/runnerhelpers` in a follow-up pass.

5. **`internal/capability` / `internal/actionresult` indirection** — `tools.go` and `result.go` are re-export shims for these packages. Long-term, have callers import the canonical packages and delete the shims. (6 callers for capability, 14 for actionresult.)

---

## 8. Suggested Execution Order

Cut in this order to minimize risk and keep tests green at every step:

1. [x] **`internal/providers`** (biggest LOC win, lowest risk — two full file deletes + one test file delete)
2. [x] **`internal/tools`** metadata.go + unused names.go constants (zero callers, trivial)
3. [x] **`internal/tools`** tools.go + result.go (callers switched to `internal/capability`; result shim trimmed to live aliases)
4. [x] **`internal/triggers`** (3 functions, all zero-callers, low risk)
5. [x] **`internal/approval`** (dead evaluation/API/token/audit leftovers removed; `MessageSendSubject` kept because preview rendering still consumes it)
6. [x] **`internal/config`** access_profiles.go (2 functions + 1 test)
7. [x] **`internal/doctor`** (stale tool-registry findings removed; provider startup gating demoted)

### Verification Strategy

After each cut:
- `go build ./...` from or3-intern
- `go vet ./...`
- `go test ./internal/<package>/...` for the touched package
- `go test ./...` for the full repo if cut touches shared types
- The `scripts/check-runner-first-forbidden.sh` guard should continue passing (it forbids `agent.Runtime`, `tools.Registry`, etc.)

### Out-of-Scope Follow-Ups (For a Future Pass)

- Add `// Deprecated:` comments to the 15 dead config fields in `config/types.go`
- Hide dead fields from `configure_tui.go` UI sections
- Rename `internal/tools` → `internal/hostenv` (or similar) once the package is cut down
- Rename `engine_agentcli.go` → `engine_runners.go` in `internal/doctor`
- Decide whether `internal/controlplane/controlplane.go` `SubagentManagerEnabled: false` compat field can be deleted

---

## 9. Summary: What Fights For Its Life

- **`internal/providers`** loses its `schema_sanitizer.go` and `profile.go` entirely — they were 100% agent-loop scaffolding. No survivors.
- **`internal/tools`** loses `metadata.go` entirely. `tools.go` loses 100 of 123 lines. The package is mostly a re-export shim post-cut and could itself be deleted once callers migrate to `internal/capability` / `internal/actionresult`.
- **`internal/doctor`** loses ~10 diagnostic findings that gate on dead `run_skill` / `exec` / `spawn_subagent` tool names. The engine itself stays — the runners need diagnostics.
- **`internal/approval`** loses 3 evaluation types that were designed-but-never-implemented, plus a few orphaned refactoring leftovers.
- **`internal/config`** loses 2 unused functions and surfaces 15 fields as "persisted but unenforced." Annotate, don't delete the fields.
- **`internal/triggers`** loses 3 functions nobody calls. Everything else is load-bearing for the trigger → bus → orchestrator flow.

Estimated total reduction: **~982 lines + 15 annotated dead config fields** across **3 full file deletes** and **~26 partial cuts**.
