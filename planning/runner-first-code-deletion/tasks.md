# Tasks: Runner-First Code Deletion

## 1. Establish deletion guardrails

- [x] Add a temporary migration checklist in this file or PR description that treats `internal/agent` and `internal/tools` production imports as blockers once extraction starts. (Requirements: 1, 3, 5, 10)
- [x] Add focused grep/script checks for forbidden production symbols after deletion: `agent.Runtime`, `agent.SubagentManager`, `tools.Registry`, `tools.Tool`, `ReplayToolCall`, `/internal/v1/turns`, and `/internal/v1/subagents`. (Requirements: 1, 4, 5, 7, 10)
- [x] Run a baseline `go test` subset for runner chat, agent CLI, app service, cronrunner, config, approvals, artifacts, and service auth before deletion. (Requirements: 2, 10)
- [x] Run OR3 App focused tests for chat runners, Assistant send routing, Agents, Scheduled Tasks, Activity, and settings before deletion. (Requirements: 7, 8, 10)

Temporary migration checklist:

- Treat new production imports of `internal/agent` as blockers unless they are in the active extraction slice.
- Treat new production imports of `internal/tools` as blockers unless they are retained safety/service primitives being moved in the same slice.
- Run `scripts/check-runner-first-forbidden.sh` after deletion slices; it is expected to fail until the listed legacy symbols are removed from production Go code.
- Keep focused tests green after each extraction/deletion phase before moving to the next phase.

## 2. Extract shared turn and job primitives

- [x] Move `internal/agent/chat_attachment.go` into a smaller package such as `internal/turns`, renaming `ChatAttachment` to `turns.Attachment` only if the diff stays manageable. (Requirements: 2, 3)
- [x] Update `internal/app`, `internal/agentcli`, `cmd/or3-intern/service_request.go`, `service_runner_chat.go`, and tests to use the moved attachment package. (Requirements: 2, 3)
- [x] Move `internal/agent/job_registry.go` into a package such as `internal/jobs`, preserving bounded retention/event behavior. (Requirements: 2, 3)
- [x] Update service job registry usage in `cmd/or3-intern`, `internal/agentcli.ChatManager`, `internal/controlplane`, and service job persistence code to use `internal/jobs`. (Requirements: 2, 3)
- [ ] Move `internal/agent/noop_streamer.go` and `internal/agent/service_runtime_context.go` observer/streaming interfaces into a package such as `internal/turns` or `internal/streaming`. (Requirements: 2, 3)
- [x] Move `internal/agent/error_codes.go` into a package such as `internal/serviceerrors`, replacing `agent.PublicErrorCode` imports. (Requirements: 2, 3)
- [x] Add or move tests for attachments, job registry retention/SSE behavior, and error code mapping. (Requirements: 3, 10)

## 3. Extract or rewrite runner bootstrap/context code

- [ ] Move runner-relevant bootstrap constants from `internal/agent/prompt.go` into `internal/app` or a new `internal/runnercontext` package. (Requirements: 2, 3)
- [ ] Delete or rewrite `DefaultToolNotes` so bootstrap text no longer documents OR3 model-callable tools. (Requirements: 5, 9)
- [ ] Update `cmd/or3-intern/runtime_build_storage.go` and `internal/app/bootstrap_load.go` to use runner-context defaults instead of importing `internal/agent`. (Requirements: 2, 3)
- [ ] Ensure `RunnerContextBuilder` remains the only bounded prompt/context assembly path for runner turns. (Requirements: 2, 3)
- [ ] Delete old `agent.Builder` prompt assembly tests once runner context tests cover bootstrap + memory + docs. (Requirements: 1, 3, 9)

## 4. Remove direct built-in turn execution

- [ ] Change `internal/app.ServiceApp` to drop `runtime *agent.Runtime`, `subagentManager *agent.SubagentManager`, `ReplayToolCall`, and legacy fallback behavior. (Requirements: 1, 4, 7)
- [ ] Make `ServiceApp.RunTurn` runner-only: it must call `RunnerTurnOrchestrator.StartTurn` or return a setup/config error. (Requirements: 1, 2, 7)
- [ ] Remove `doctorTurnUsesBuiltInRuntime` after Doctor admin brain is converted away from the built-in runtime. (Requirements: 1, 5, 7)
- [ ] Update `cmd/or3-intern/service_agents.go` so `handleTurns` is removed or returns `410 Gone` with guidance to use runner-chat endpoints. (Requirements: 1, 7)
- [ ] Remove `replay_tool_call`, `allowed_tools`, `restrict_tools`, and direct-turn `tool_policy` parsing from `cmd/or3-intern/service_request.go`. (Requirements: 5, 7)
- [ ] Update service tests to assert legacy turn creation is gone and runner-chat remains functional. (Requirements: 7, 10)

## 5. Remove old runtime construction and worker fallback

- [ ] Refactor `cmd/or3-intern/main.go` startup to build DB, memory/doc retrievers, runner manager, chat manager, turn orchestrator, cron, channels, approvals, audit, and service without constructing a full `agent.Runtime`. (Requirements: 1, 2)
- [ ] Delete `buildRuntimeTools`, `buildBackgroundTools`, and all calls to `buildToolRegistry` from `main.go`. (Requirements: 1, 5)
- [ ] Update `runWorkers` to require a `RunnerTurnOrchestrator` and remove fallback to `rt.Handle`. (Requirements: 1, 2)
- [ ] Remove `channelWorkerRuntime` and related runtime cloning. (Requirements: 1)
- [ ] Delete `runtimeModelConfigFromConfig` and `agent.RuntimeModelConfig` usage unless consolidation/context-manager still needs a separate model config type. (Requirements: 1, 8)
- [ ] Update `runtime_build_runtime.go` to keep only runner manager/chat manager/orchestrator builders, using moved `jobs.Registry`. (Requirements: 1, 2, 3)
- [ ] Add startup tests that runner-first service fails clearly when no runner orchestrator can be built. (Requirements: 1, 10)

## 6. Remove legacy subagents and skill execution

- [ ] Delete `internal/agent/subagents.go` and its tests. (Requirements: 4, 9)
- [ ] Remove `subagentManager` fields and parameters from `cmd/or3-intern/service.go`, `main.go`, `internal/app.ServiceApp`, and `internal/controlplane`. (Requirements: 4)
- [ ] Remove `POST /internal/v1/subagents` creation path from `service_agents.go`; keep only optional historical listing if OR3 App still renders old rows. (Requirements: 4, 7)
- [ ] Delete `tools/spawn.go` and tests. (Requirements: 4, 5)
- [ ] Delete `tools/skill_exec.go`, `tools/skill_run.go`, execution tests, and service endpoints that resume or run skill plans. (Requirements: 4, 5)
- [ ] Keep skill catalog/read-only metadata only if Clawhub or OR3 App still displays it; move it out of `internal/tools` if retained. (Requirements: 2, 4, 5)
- [ ] Remove config fields and settings for subagent concurrency/queue/timeouts and skill execution. (Requirements: 4, 8)

## 7. Replace or delete generic tool infrastructure

- [ ] Inventory current non-runtime uses of `tools.Registry`, especially Doctor admin tools, MCP manager, service capability checks, and service skill endpoints. (Requirements: 5, 6)
- [ ] Convert Doctor/admin tool operations in `service_doctor_tools.go` into typed service action functions or route them through external runner chat with explicit service APIs. (Requirements: 5, 6)
- [ ] Move capability levels and approval-required error types into narrow packages if still needed by service/admin actions. (Requirements: 6)
- [ ] Move request identity/approval token context helpers out of `internal/tools/context.go` if retained. (Requirements: 6)
- [ ] Move `ToolResult`/preview helpers into an admin/action result package if Doctor still needs a result envelope. (Requirements: 6)
- [ ] Delete `internal/tools/registry.go`, `tools.go` schema helpers, metadata scanners, dynamic availability/behavior helpers, and registry tests once no production code depends on them. (Requirements: 5, 6, 9)

## 8. Build retained runner memory bridge

- [ ] Extract validation and DB logic from `internal/tools/memory.go` into a non-tool memory service package used by OR3 App/service code and runner bridges. (Requirements: 2, 2a, 5)
- [ ] Add bounded typed operations for memory search, add note, get pinned, and set pinned, preserving scope resolution and secret-like content rejection. (Requirements: 2a, 6)
- [ ] Choose and implement the runner-facing bridge shape: local MCP server, JSON stdin/stdout bridge command, or authenticated typed service endpoints. Prefer the smallest path runners can actually call. (Requirements: 2a)
- [ ] Add audit/activity records for runner-created memory writes, including runner id, session key, and runner turn id when available. (Requirements: 2a)
- [ ] Update runner bootstrap/instructions so runners know when to call memory search and when to save durable memories. (Requirements: 2a, 9)
- [ ] Add tests for bounded search results, memory note creation, pinned memory updates, scope isolation, rejected secret-like content, and audit metadata. (Requirements: 2a, 10)

## 9. Delete model-callable tool implementations

- [ ] Delete file tools: `files.go`, file tool tests, and model-callable read/write/edit/list/search constants. Keep service file APIs using direct safe filesystem helpers. (Requirements: 5, 6)
- [ ] Delete exec tools: `exec.go`, sandbox/tool exec tests, and approval subjects that only apply to model tool execution. Keep terminal/service command execution as explicit service code if still supported. (Requirements: 5, 6)
- [ ] Delete web tools: `web.go`, `web_markdown.go`, `html_converter.go`, and tests unless a non-agent docs/indexer path uses the converter. (Requirements: 5)
- [ ] Delete `internal/tools/memory.go` as a model-callable tool file only after the runner memory bridge and memory service tests are in place. (Requirements: 2, 2a, 5)
- [ ] Delete artifact read tool: `artifact.go` and tests after artifact access is direct service/internal API only. (Requirements: 2, 5)
- [ ] Delete cron and message tools: `cron.go`, `message.go`, and tests after cron/channel management is direct service/admin API only. (Requirements: 2, 5)
- [ ] Delete skill read/run tools if catalog-only skill display is moved elsewhere. (Requirements: 4, 5)

## 10. Remove old agent runtime files

- [ ] Delete `internal/agent/runtime.go` and runtime helper files after production imports are gone. (Requirements: 1, 9)
- [ ] Delete runtime tests tied to provider tool loop, streaming tool calls, quotas, sessions, context manager runtime pruning, structured autonomy, model overrides, and task-card enforcement. (Requirements: 1, 9)
- [ ] Delete plan tool/gate files if no non-agent service feature uses them. (Requirements: 5, 9)
- [ ] Delete prompt/cache diagnostics that only apply to provider-native prompt construction. (Requirements: 1, 9)
- [ ] Delete `internal/agent/QUARANTINE.md` or move any still-relevant warning into runner-first docs. (Requirements: 9)
- [ ] Confirm `internal/agent` directory is either gone or contains no production code after extractions. (Requirements: 1, 3, 9)

## 11. Simplify service/controlplane API surface

- [ ] Remove `tools.Registry` and `agent.Runtime` dependencies from `internal/controlplane`. (Requirements: 1, 5, 6)
- [ ] Remove service request validation based on allowed tool names/capability ceilings for direct turns. (Requirements: 5, 7)
- [ ] Keep service auth/role/capability checks for retained direct APIs such as files, terminal, approvals, cron, runner memory bridge, artifacts, runner jobs, and configure. (Requirements: 2, 2a, 6)
- [ ] Remove tool catalog/capabilities endpoints that advertised OR3 model-callable tools. (Requirements: 5, 9)
- [ ] Ensure `/internal/v1/jobs/{job_id}` still returns runner job snapshots and any retained historical rows without requiring `agent.JobSnapshot`. (Requirements: 2, 3)
- [ ] Add regression tests for removed endpoints returning removal guidance instead of creating work. (Requirements: 7, 10)

## 12. Update OR3 App for runner-only execution

- [ ] Delete `streamDirectTurn` and direct `/internal/v1/turns` recovery fallback from `app/utils/assistant-stream/execution.ts`. (Requirements: 7)
- [ ] Simplify `app/composables/assistant-stream/useExecutionRouter.ts` so normal send always uses runner chat and returns a setup error when no runner is selected. (Requirements: 7)
- [ ] Update `app/components/assistant/AssistantComposer.vue` and `app/pages/index.vue` so `canSend` requires a selectable runner and displays runner setup guidance otherwise. (Requirements: 7)
- [ ] Remove `replayToolCall` from `app/types/app-state.ts`, `useAssistantStream.ts`, `useDoctorAdminChat.ts`, `useAssistantMessageState.ts`, and event-applier retry payloads unless replaced by runner permission retry semantics. (Requirements: 7)
- [ ] Remove `ToolPolicy`, `allowed_tools`, direct `TurnRequest`, direct `TurnResponse`, and subagent creation types from `app/types/or3-api.ts`. (Requirements: 7, 8)
- [ ] Remove `queueJob` and `/internal/v1/subagents` calls from `app/composables/useJobs.ts`; keep only runner job APIs and optional historical activity reads if retained. (Requirements: 4, 7)
- [ ] Update Activity/Agents/Scheduled pages to label old rows as historical only and never expose actions that recreate legacy jobs. (Requirements: 4, 7)
- [ ] Add OR3 App visibility for runner-created memory writes/searches if activity/audit surfaces already show memory events. (Requirements: 2a, 7)
- [ ] Add/update Vitest coverage for no-runner blocked send, runner-chat-only send, no subagent creation, no replay-tool retry, and settings search cleanup. (Requirements: 7, 8, 10)

## 13. Simplify config, configure, and settings

- [ ] Remove active config fields for built-in runtime/tool loop: `maxToolLoops`, tool call/session quotas, dynamic tool exposure, tool schema budgets, old chat/subagent model routes, subagents, skill execution, Brave/web tool config, and OR3 model-callable MCP registration. (Requirements: 8)
- [ ] Keep or rename config fields for retained service features: runner settings, runner memory bridge, service auth, memory/indexing, embeddings/consolidation, channels, cron, heartbeat, triggers, artifacts, files/terminal service APIs, approvals, audit, and security profiles. (Requirements: 2, 2a, 8)
- [ ] Update env override handling to ignore or warn on deleted env vars such as `OR3_MODEL`, `OR3_SUBAGENTS_*`, tool-loop/tool-specific vars, and skill execution vars. (Requirements: 8)
- [ ] Update `cmd/or3-intern/configure_tui.go`, `service_configure.go`, `internal/configmeta`, and OR3 App settings to remove deleted controls entirely rather than marking them deprecated. (Requirements: 8)
- [ ] Update setup/init defaults so new configs do not seed deleted runtime/tool fields. (Requirements: 8)
- [ ] Add config load tests proving old JSON with deleted fields does not break startup and does not re-save removed active fields. (Requirements: 8, 10)

## 14. Update docs, help, and naming

- [ ] Rewrite `docs/api-reference.md` to remove `/internal/v1/turns`, `/internal/v1/subagents`, built-in tools, and always-available `or3-intern` runner language. (Requirements: 7, 9)
- [ ] Delete or archive stale `docs/v1` pages about built-in tool reference, provider request lifecycle, subagent store, tool schema sanitizer, and old roadmap items. (Requirements: 9)
- [ ] Update `docs/agent-runtime.md`, `docs/migration-runner-first.md`, and manual verification docs to say the built-in runtime has been removed, not deprecated. (Requirements: 9)
- [ ] Update CLI help/status copy that describes OR3 as a local-first agent runtime with tools; use runner control-plane terminology. (Requirements: 9)
- [ ] Update any AGENTS/SOUL/TOOLS bootstrap defaults and docs to remove built-in tool instructions. (Requirements: 3, 5, 9)
- [ ] Document runner memory bridge setup and usage, including what should and should not be remembered. (Requirements: 2a, 9)

## 15. Final validation

- [ ] Run focused Go tests after extraction: moved attachment package, moved jobs package, runner memory bridge, runner chat, agent CLI manager, app service, cronrunner, config, approvals, artifacts, and service auth. (Requirements: 10)
- [ ] Run focused Go tests after deletion: `cmd/or3-intern`, `internal/app`, `internal/agentcli`, `internal/cronrunner`, `internal/config`, `internal/controlplane`, retained `internal/tools` replacement packages if any. (Requirements: 10)
- [ ] Run `go test ./...` and fix only failures caused by this deletion. (Requirements: 10)
- [ ] Run OR3 App typecheck and test suite after removing direct-turn/subagent/replay-tool UI. (Requirements: 10)
- [ ] Run final forbidden-symbol/import searches and confirm no production use of deleted runtime/tool symbols remains. (Requirements: 1, 3, 5, 7, 10)
- [ ] Manually verify chat, Agents, Scheduled Tasks, Activity, Doctor/settings, one channel or webhook turn, cron runner enqueue, heartbeat runner turn recording, approvals, passive memory retrieval, active runner memory search/write, and artifact display. (Requirements: 2, 2a, 7, 10)

## Out of scope

- Dropping old SQLite tables in a destructive migration; stop creating old rows first and consider table deletion later.
- Building a new plugin/tool framework for runners.
- Preserving compatibility for external clients of old direct-turn or subagent endpoints.
- Recreating runner-native shell/file/web behavior inside OR3.
