# Tasks: Runner-First Agent Removal

## 1. Establish runner-first defaults

- [x] Add `AgentCLIConfig.DefaultRunner` and `OR3_AGENT_CLI_DEFAULT_RUNNER` support in `internal/config/types.go`, `internal/config/defaults.go`, `internal/config/load.go`, `internal/config/env.go`, and `internal/config/validate.go`. (Requirements: 1, 2, 7, 10)
- [x] Change new default config so `agentCLI.enabled` is true and `defaultRunner` is `opencode`, while preserving safe mode/isolation defaults. (Requirements: 2, 7)
- [x] Add config tests covering default runner, env override, disabled/unknown runner validation, and existing config upgrade behavior. (Requirements: 2, 8)
- [x] Update `doctor`/`health` readiness checks to report OpenCode install/auth status and default-runner issues. (Requirements: 2, 10)

## 2. Remove OR3 from runner selection

- [x] Update `internal/agentcli/registry.go` and `internal/agentcli/runners.go` so `RunnerOR3` is legacy-only or removed from `AllRunners()` chat-selectable output. (Requirements: 3, 9)
- [x] Update `cmd/or3-intern/service_chat_sessions.go` so `/internal/v1/chat-runners` no longer reports OR3 as always available. (Requirements: 3)
- [x] Update `cmd/or3-intern/service_doctor_admin_brain.go` and `service_doctor_session.go` to stop rewriting runner sessions to `RunnerOR3`. (Requirements: 3, 4)
- [x] Update runner selection tests in `cmd/or3-intern/service_runner_chat_selection_test.go` for OpenCode/default-runner behavior and legacy OR3 handling. (Requirements: 2, 3)

## 3. Add runner turn orchestration

- [x] Introduce a small runner-backed turn orchestration interface in `internal/app` or a minimal new package, using `agentcli.ChatManager` and `agentcli.Manager` under the hood. (Requirements: 1, 4, 9)
- [x] Implement event-to-runner-turn mapping for session key, channel/from metadata, message, trigger kind, actor/role, capability, approval token, attachments, cwd, mode, isolation, and selected runner. (Requirements: 1, 4, 7)
- [x] Add bounded prompt/context construction for runner turns using retained history, bootstrap files, memory/doc retrieval, heartbeat/autonomous instructions, and attachments metadata. (Requirements: 5, 6)
- [x] Preserve trusted system prompt semantics by explicitly separating OR3-controlled instructions from untrusted user/task content in runner prompts or runner-native message fields. (Requirements: 5, 7)
- [x] Add unit tests for prompt bounds, trusted system prompt ordering, autonomous context inclusion rules, and event-to-runner-turn request construction. (Requirements: 1, 5, 6, 7)

## 4. Replace prompt caching with runner-safe context caching

- [x] Inventory current prompt cache diagnostics/building helpers in `internal/agent` and identify which are provider-specific versus reusable context-cache primitives. (Requirements: 6, 9)
- [x] Implement or retain OR3-owned caches for bootstrap file loads, memory/doc retrieval fragments, replay prompt fragments, and runner detection using content hashes and bounded TTLs where appropriate. (Requirements: 5, 6)
- [x] Add cache invalidation for bootstrap file changes, memory/doc index changes, session history changes, runner/model/mode/isolation changes, prompt builder version changes, and `HEARTBEAT.md` changes. (Requirements: 5, 6)
- [x] Ensure approval tokens, secrets, requester identities, and raw environment values are excluded from reusable cache entries. (Requirements: 6, 7)
- [x] Add tests for cache hits, invalidation, cache failure fallback, and sensitive-value exclusion. (Requirements: 6, 7)

## 5. Replace built-in runtime entrypoints

- [x] Refactor `cmd/or3-intern/main.go` workers so `chat`, `serve`, and `service` channel workers call the runner turn orchestrator instead of `agent.Runtime.Handle`. (Requirements: 1, 4)
- [x] Change the `agent -m` command to either enqueue a runner turn/run or become a compatibility alias with deprecation text. (Requirements: 1, 10)
- [x] Update `internal/app/service_app.go` so service turn execution starts runner chat turns rather than invoking the provider/tool-loop runtime. (Requirements: 1, 9)
- [x] Preserve direct runner run APIs in `cmd/or3-intern/service_agents.go` and ensure SSE/job event responses still work from `agent_cli_events`. (Requirements: 1, 4)
- [x] Add tests proving CLI/service/channel paths enqueue runner work without calling the old runtime. (Requirements: 1, 4)

## 6. Preserve automation and channels

- [x] Keep channel startup, approval command handling, typing indicators, and delivery error reporting in `cmd/or3-intern/main.go` and `channel_approvals.go`. (Requirements: 4, 7)
- [x] Update `internal/cronrunner` so `PayloadAgentTurn` and `PayloadSystemEvent` are translated to runner turns or fail with migration guidance; keep `PayloadAgentCLIRun` unchanged. (Requirements: 4, 10)
- [x] Verify webhook and file-watch triggers continue publishing bounded events, and ensure workers route them to runner turns with trigger metadata. (Requirements: 4, 5, 7)
- [x] Verify heartbeat service still reads `HEARTBEAT.md` per autonomous turn and routes work through the runner orchestrator. (Requirements: 4, 5)
- [x] Add regression tests for channel approval commands, cron runner payloads, webhook/file-watch event routing, and heartbeat context. (Requirements: 4, 5, 7)

## 7. Keep memory, artifacts, tools, and approvals decoupled

- [x] Move or retain only the prompt/context pieces needed by runner prompt building; avoid depending on `agent.Runtime` for memory retrieval. (Requirements: 5, 9)
- [x] Keep memory consolidation running from `messages`, and add tests that runner chat messages are eligible for existing consolidation windows. (Requirements: 5, 8)
- [x] Ensure artifacts still receive spilled/oversized runner output and that event previews obey `AgentCLIConfig` byte limits. (Requirements: 4, 7)
- [x] Keep tool registry and approval broker for OR3-owned tools/admin operations, but remove model tool-loop execution paths that runners already handle externally. (Requirements: 4, 7, 9)
- [x] Preserve Clawhub skill/package operations and tests, decoupling only imports that point at removed agent runtime code. (Requirements: 4, 9)

## 8. SQLite compatibility and migration

- [x] Audit existing schema usage for `runner_id='or3-intern'` in `chat_session_meta`, `runner_chat_sessions`, and tests. (Requirements: 3, 8)
- [x] Implement either a no-schema compatibility shim or an additive migration for legacy runner metadata; do not drop existing rows/tables. (Requirements: 8)
- [x] Add SQLite-backed tests for opening legacy OR3-agent sessions, listing them, and starting/migrating/rejecting a new turn deterministically. (Requirements: 3, 8)
- [x] Keep `agent_cli_runs`, `agent_cli_events`, `runner_chat_sessions`, `runner_chat_turns`, `runner_chat_events`, `messages`, memory, approval, and audit data readable. (Requirements: 4, 8)

## 9. Shrink or quarantine old agent code

- [x] Inventory `internal/agent` symbols still used after runner-first entrypoints are in place: job registry/events, attachments, observer/streaming interfaces, trusted system prompt/bootstrap helpers, prompt cache diagnostics, public error codes, subagent manager, task-card helpers. (Requirements: 6, 9)
- [x] Move retained neutral types to smaller packages only when it reduces imports without broad churn. (Requirements: 9)
- [x] Delete or quarantine provider-driven turn loop files such as runtime execution, tool-call loop, provider prompt execution, and old subagent model loop once no primary path imports them. (Requirements: 1, 9)
- [x] Update tests by moving retained coverage to new packages and deleting tests that only assert removed built-in agent behavior. (Requirements: 9)
- [x] Run focused package tests after each deletion pass to avoid large-bang breakage. (Requirements: 9)

## 10. Documentation and rollout

- [x] Rewrite `docs/agent-runtime.md` around runner-first execution, OpenCode default behavior, retained orchestration features, trusted system prompt handling, and context caching. (Requirements: 1, 2, 5, 6, 10)
- [x] Update `docs/cli-reference.md`, `docs/configuration-reference.md`, `docs/channels.md`, `docs/memory-and-context.md`, and `README.md` for OpenCode setup and runner selection. (Requirements: 2, 4, 5, 10)
- [x] Document migration behavior for existing OR3-agent sessions and configs. (Requirements: 3, 8, 10)
- [x] Add release notes warning that the built-in OR3 agent loop is removed/deprecated and external runners are now required for active turns. (Requirements: 2, 9, 10)

## 11. Update OR3 App runner UX and API assumptions

- [x] Update `../or3-app/app/composables/useChatRunners.ts` so selectable/default runner logic prefers `opencode`, then other selectable external runners, and never synthesizes a fake `or3-intern` fallback. (Requirements: 2, 3, 11)
- [x] Update `../or3-app/app/composables/useJobs.ts` so runner discovery no longer prepends `builtinInternRunner()`, while old jobs with `runner_id='or3-intern'` still render as legacy OR3-agent activity. (Requirements: 3, 8, 11)
- [x] Update `../or3-app/app/types/or3-api.ts` to distinguish legacy runner IDs from active/selectable runner IDs without breaking older host responses. (Requirements: 3, 8, 11)
- [x] Update `../or3-app/app/pages/agents/index.vue` so task handoff requires a selectable runner and setup guidance replaces “built-in OR3 assistant” fallback behavior. (Requirements: 1, 2, 11)
- [x] Update `../or3-app/app/pages/activity.vue` and `../or3-app/app/pages/scheduled.vue` labels, filters, and scheduled-task creation so new work defaults to `agent_cli_run` with the selected/default runner. (Requirements: 3, 4, 11)
- [x] Update `../or3-app/app/composables/useChatSessions.ts`, doctor/admin chat composables, and chat components so session creation, fork target selection, and model/runner pickers use service-provided runner capabilities. (Requirements: 1, 3, 11)
- [x] Verify `../or3-app/server/api/or3/[...path].ts` proxies new runner/chat/session/SSE endpoints without header or response-shaping changes; patch only if the updated service needs additional pass-through headers. (Requirements: 4, 11)
- [x] Add or update OR3 App unit/component tests for no-runner empty states, OpenCode default selection, legacy OR3 labels, task handoff validation, and scheduled `agent_cli_run` payloads. (Requirements: 2, 3, 11)

## 12. Remove duplicate model-callable OR3 tools

- [x] Split `buildToolRegistryWithOptions` into explicit registries for runner context internals, service/admin APIs, and legacy compatibility instead of one model-callable registry. (Requirements: 4, 7, 12)
- [x] Remove `exec` from runner-first/model-callable paths; keep terminal/service command execution only where authenticated, role-checked, and explicitly requested. (Requirements: 7, 12)
- [x] Remove file mutation/read tools (`read_file`, `search_file`, `write_file`, `edit_file`, `delete_file`, `list_dir`) from model-callable paths; keep OR3 App file APIs, doc indexing, and metadata scanning. (Requirements: 5, 7, 12)
- [x] Remove `web_fetch`, `web_fetch_markdown`, and `web_search` from OR3 model-callable paths and delete SSRF/network policy code that is no longer used elsewhere. (Requirements: 7, 9, 12)
- [x] Remove `read_skill`, `run_skill`, `run_skill_script`, and `builtin_skills/*` execution paths unless a non-agent app/catalog use remains; keep Clawhub/package metadata only if used by OR3 App or runner prompt context. (Requirements: 4, 9, 12)
- [x] Remove `spawn_subagent` and old subagent model-loop entrypoints; migrate background delegation to runner jobs. (Requirements: 1, 9, 12)
- [x] Remove MCP model-callable tool registration from OR3 turns; keep MCP catalog/settings only if needed for app visibility or runner-native setup. (Requirements: 9, 12)
- [x] Remove general model-callable plan tools (`create_plan`, `update_plan`, `complete_plan_task`, `remove_plan`) while preserving task-card/planning metadata if still used by app/admin flows. (Requirements: 9, 12)
- [x] Keep memory capabilities (`memory_set_pinned`, `memory_add_note`, `memory_search`, `memory_recent`, `memory_get_pinned`) as internal runner-context/service APIs with explicit write guards, not broad runner-callable tools. (Requirements: 5, 7, 12)
- [x] Keep artifact read/store helpers, metadata scanners, channel delivery, cron management, approvals, audit, and runner orchestration as service/internal APIs with auth/profile checks. (Requirements: 4, 7, 8, 12)
- [x] Update access profiles, doctor/capabilities output, tool catalogs, docs, and OR3 App settings so removed tools no longer appear as available agent tools. (Requirements: 10, 11, 12)
- [x] Add regression tests that removed tools are absent from runner-first tool definitions, old audit/approval/history rows with removed tool names still render, and retained internal APIs still work. (Requirements: 8, 12)

## 13. Clean remaining legacy agent-era surfaces

- [x] Audit provider/model config and remove active chat/subagent model roles, stream assembler, schema sanitizer, and provider tool-call paths unless still used by embeddings, memory consolidation, diagnostics, or migration commands. (Requirements: 6, 10, 13)
- [x] Keep bootstrap files as compatibility aliases, but update docs/copy so `SOUL.md`, `AGENTS.md`, `IDENTITY.md`, `MEMORY.md`, and `HEARTBEAT.md` are described as runner context inputs rather than built-in agent identity. (Requirements: 5, 10, 13)
- [x] Stop creating `subagent_jobs` and `skill_run_plans`; keep stores and response shaping only for existing records until a later DB archival migration. (Requirements: 8, 9, 13)
- [x] Replace exec/skill/tool-call approval creation paths with runner permission and service/admin action approvals; keep legacy approval subjects readable. (Requirements: 7, 8, 13)
- [x] Remove or compatibility-gate service replay-tool APIs that resume built-in tool calls; route approvals for runner work through runner permission flows. (Requirements: 1, 7, 13)
- [x] Update `../or3-app/app/composables/useLocalCache.ts` and assistant stream execution routing so missing runner metadata does not fall back to `or3-intern`. (Requirements: 11, 13)
- [x] Update OR3 App settings, health, permissions, and copy so provider/model/tool settings reflect runner-first behavior and no longer advertise removed agent-tool controls. (Requirements: 10, 11, 13)
- [x] Rewrite or archive stale `docs/v1` pages covering built-in tools, provider request lifecycle, subagents, tool schema sanitizer, provider profiles for chat, MCP tool registration into OR3 turns, and old roadmap items. (Requirements: 10, 12, 13)
- [x] Decide naming compatibility for `/internal/v1/agent-runs`, `agent_cli_*`, and app “Agent runs” labels: keep as compatibility aliases or introduce new runner-job names with migration docs. (Requirements: 9, 11, 13)

## 14. Simplify settings and config

- [x] Add config metadata statuses for active, deprecated, hidden, and compatibility-only fields; make `or3-intern configure`, `/internal/v1/configure/*`, doctor metadata, and OR3 App settings use that shared status. (Requirements: 10, 11, 14)
- [x] Promote `agentCLI` to the primary runner settings section, add `defaultRunner`, and rename UI/copy from “External CLI Agents” to “Runners” where practical. (Requirements: 1, 2, 10, 14)
- [x] Remove chat-provider setup from first-run/chat settings; split remaining provider fields into embeddings, memory summarization/consolidation, approval moderator, diagnostics, and compatibility-only groups. (Requirements: 5, 6, 10, 14)
- [x] Deprecate or remove active settings for `subagents`, built-in `tools` web/file/exec fields, Brave web search, skill execution, OR3 model-callable MCP registration, dynamic tool exposure, task-card enforce-plan, tool-schema budgets, `maxToolLoops`, and old tool-loop byte limits. (Requirements: 10, 12, 13, 14)
- [x] Re-scope hardening/access-profile/approval fields from `exec`, web, skill, subagent, and tool-call subjects to runner permission and service/admin action subjects; keep legacy values readable. (Requirements: 7, 12, 13, 14)
- [x] Update env var handling so removed vars such as `OR3_MODEL`, `OR3_SUBAGENTS_*`, and built-in tool env vars warn or map to replacements, while new runner-first vars like `OR3_AGENT_CLI_DEFAULT_RUNNER` are documented and tested. (Requirements: 2, 10, 14)
- [x] Update `../or3-app/app/composables/useConfigure.ts`, settings pages, advanced settings search, health cards, and setup copy to hide removed controls and prioritize runner readiness plus retained OR3 control-plane settings. (Requirements: 10, 11, 14)
- [x] Add config migration/regression tests proving legacy config fields load safely, saves do not resurrect removed active fields, metadata statuses are consistent, and OR3 App settings search cannot expose hidden/deprecated fields as actionable controls. (Requirements: 8, 10, 11, 14)

## 15. Final validation

- [x] Run `go test ./internal/agentcli ./internal/db ./internal/app ./internal/cronrunner ./internal/config ./cmd/or3-intern`. (Requirements: 1-14)
- [x] Run broader `go test ./...` after the old runtime deletion pass, fixing only failures caused by this migration. (Requirements: 9)
- [x] Run OR3 App checks from `../or3-app`, including TypeScript, lint, and focused tests for runner/chat/job UI changes. (Requirements: 11)
- [x] Manually verify OpenCode readiness, `or3-intern chat`, service runner APIs, one channel or webhook turn, cron runner enqueue, and heartbeat turn recording. (Requirements: 1, 2, 4, 5)
- [x] Manually verify OR3 App chat, Agents, Activity, Scheduled Tasks, and doctor/admin chat against the updated service. (Requirements: 11)
- [x] Confirm config/doctor output gives clear guidance when OpenCode is not installed or authenticated. (Requirements: 2, 10)

## Out of scope

- [ ] Do not build a new frontend, REST backend, or distributed worker service. (Requirements: 4, 9)
- [ ] Do not remove SQLite persistence or migrate to a separate queue/database. (Requirements: 8)
- [ ] Do not grant external runners global filesystem, shell, network, or secret access as a shortcut. (Requirements: 7)
- [ ] Do not delete memory, history, runner, approval, audit, or channel data during migration. (Requirements: 8)