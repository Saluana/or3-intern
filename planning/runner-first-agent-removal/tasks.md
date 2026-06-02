# Tasks: Runner-First Agent Removal

## 1. Establish runner-first defaults

- [ ] Add `AgentCLIConfig.DefaultRunner` and `OR3_AGENT_CLI_DEFAULT_RUNNER` support in `internal/config/types.go`, `internal/config/defaults.go`, `internal/config/load.go`, `internal/config/env.go`, and `internal/config/validate.go`. (Requirements: 1, 2, 6, 9)
- [ ] Change new default config so `agentCLI.enabled` is true and `defaultRunner` is `opencode`, while preserving safe mode/isolation defaults. (Requirements: 2, 6)
- [ ] Add config tests covering default runner, env override, disabled/unknown runner validation, and existing config upgrade behavior. (Requirements: 2, 7)
- [ ] Update `doctor`/`health` readiness checks to report OpenCode install/auth status and default-runner issues. (Requirements: 2, 9)

## 2. Remove OR3 from runner selection

- [ ] Update `internal/agentcli/registry.go` and `internal/agentcli/runners.go` so `RunnerOR3` is legacy-only or removed from `AllRunners()` chat-selectable output. (Requirements: 3, 8)
- [ ] Update `cmd/or3-intern/service_chat_sessions.go` so `/internal/v1/chat-runners` no longer reports OR3 as always available. (Requirements: 3)
- [ ] Update `cmd/or3-intern/service_doctor_admin_brain.go` and `service_doctor_session.go` to stop rewriting runner sessions to `RunnerOR3`. (Requirements: 3, 4)
- [ ] Update runner selection tests in `cmd/or3-intern/service_runner_chat_selection_test.go` for OpenCode/default-runner behavior and legacy OR3 handling. (Requirements: 2, 3)

## 3. Add runner turn orchestration

- [ ] Introduce a small runner-backed turn orchestration interface in `internal/app` or a minimal new package, using `agentcli.ChatManager` and `agentcli.Manager` under the hood. (Requirements: 1, 4, 8)
- [ ] Implement event-to-runner-turn mapping for session key, channel/from metadata, message, trigger kind, actor/role, capability, approval token, attachments, cwd, mode, isolation, and selected runner. (Requirements: 1, 4, 6)
- [ ] Add bounded prompt/context construction for runner turns using retained history, bootstrap files, memory/doc retrieval, heartbeat/autonomous instructions, and attachments metadata. (Requirements: 5, 6)
- [ ] Add unit tests for prompt bounds, autonomous context inclusion rules, and event-to-runner-turn request construction. (Requirements: 1, 5, 6)

## 4. Replace built-in runtime entrypoints

- [ ] Refactor `cmd/or3-intern/main.go` workers so `chat`, `serve`, and `service` channel workers call the runner turn orchestrator instead of `agent.Runtime.Handle`. (Requirements: 1, 4)
- [ ] Change the `agent -m` command to either enqueue a runner turn/run or become a compatibility alias with deprecation text. (Requirements: 1, 9)
- [ ] Update `internal/app/service_app.go` so service turn execution starts runner chat turns rather than invoking the provider/tool-loop runtime. (Requirements: 1, 8)
- [ ] Preserve direct runner run APIs in `cmd/or3-intern/service_agents.go` and ensure SSE/job event responses still work from `agent_cli_events`. (Requirements: 1, 4)
- [ ] Add tests proving CLI/service/channel paths enqueue runner work without calling the old runtime. (Requirements: 1, 4)

## 5. Preserve automation and channels

- [ ] Keep channel startup, approval command handling, typing indicators, and delivery error reporting in `cmd/or3-intern/main.go` and `channel_approvals.go`. (Requirements: 4, 6)
- [ ] Update `internal/cronrunner` so `PayloadAgentTurn` and `PayloadSystemEvent` are translated to runner turns or fail with migration guidance; keep `PayloadAgentCLIRun` unchanged. (Requirements: 4, 9)
- [ ] Verify webhook and file-watch triggers continue publishing bounded events, and ensure workers route them to runner turns with trigger metadata. (Requirements: 4, 5, 6)
- [ ] Verify heartbeat service still reads `HEARTBEAT.md` per autonomous turn and routes work through the runner orchestrator. (Requirements: 4, 5)
- [ ] Add regression tests for channel approval commands, cron runner payloads, webhook/file-watch event routing, and heartbeat context. (Requirements: 4, 5, 6)

## 6. Keep memory, artifacts, tools, and approvals decoupled

- [ ] Move or retain only the prompt/context pieces needed by runner prompt building; avoid depending on `agent.Runtime` for memory retrieval. (Requirements: 5, 8)
- [ ] Keep memory consolidation running from `messages`, and add tests that runner chat messages are eligible for existing consolidation windows. (Requirements: 5, 7)
- [ ] Ensure artifacts still receive spilled/oversized runner output and that event previews obey `AgentCLIConfig` byte limits. (Requirements: 4, 6)
- [ ] Keep tool registry and approval broker for OR3-owned tools/admin operations, but remove model tool-loop execution paths that runners already handle externally. (Requirements: 4, 6, 8)
- [ ] Preserve Clawhub skill/package operations and tests, decoupling only imports that point at removed agent runtime code. (Requirements: 4, 8)

## 7. SQLite compatibility and migration

- [ ] Audit existing schema usage for `runner_id='or3-intern'` in `chat_session_meta`, `runner_chat_sessions`, and tests. (Requirements: 3, 7)
- [ ] Implement either a no-schema compatibility shim or an additive migration for legacy runner metadata; do not drop existing rows/tables. (Requirements: 7)
- [ ] Add SQLite-backed tests for opening legacy OR3-agent sessions, listing them, and starting/migrating/rejecting a new turn deterministically. (Requirements: 3, 7)
- [ ] Keep `agent_cli_runs`, `agent_cli_events`, `runner_chat_sessions`, `runner_chat_turns`, `runner_chat_events`, `messages`, memory, approval, and audit data readable. (Requirements: 4, 7)

## 8. Shrink or quarantine old agent code

- [ ] Inventory `internal/agent` symbols still used after runner-first entrypoints are in place: job registry/events, attachments, observer/streaming interfaces, prompt helpers, public error codes, subagent manager, task-card helpers. (Requirements: 8)
- [ ] Move retained neutral types to smaller packages only when it reduces imports without broad churn. (Requirements: 8)
- [ ] Delete or quarantine provider-driven turn loop files such as runtime execution, tool-call loop, provider prompt execution, and old subagent model loop once no primary path imports them. (Requirements: 1, 8)
- [ ] Update tests by moving retained coverage to new packages and deleting tests that only assert removed built-in agent behavior. (Requirements: 8)
- [ ] Run focused package tests after each deletion pass to avoid large-bang breakage. (Requirements: 8)

## 9. Documentation and rollout

- [ ] Rewrite `docs/agent-runtime.md` around runner-first execution, OpenCode default behavior, and retained orchestration features. (Requirements: 1, 2, 9)
- [ ] Update `docs/cli-reference.md`, `docs/configuration-reference.md`, `docs/channels.md`, `docs/memory-and-context.md`, and `README.md` for OpenCode setup and runner selection. (Requirements: 2, 4, 5, 9)
- [ ] Document migration behavior for existing OR3-agent sessions and configs. (Requirements: 3, 7, 9)
- [ ] Add release notes warning that the built-in OR3 agent loop is removed/deprecated and external runners are now required for active turns. (Requirements: 2, 8, 9)

## 10. Final validation

- [ ] Run `go test ./internal/agentcli ./internal/db ./internal/app ./internal/cronrunner ./cmd/or3-intern`. (Requirements: 1-9)
- [ ] Run broader `go test ./...` after the old runtime deletion pass, fixing only failures caused by this migration. (Requirements: 8)
- [ ] Manually verify OpenCode readiness, `or3-intern chat`, service runner APIs, one channel or webhook turn, cron runner enqueue, and heartbeat turn recording. (Requirements: 1, 2, 4, 5)
- [ ] Confirm config/doctor output gives clear guidance when OpenCode is not installed or authenticated. (Requirements: 2, 9)

## Out of scope

- [ ] Do not build a new frontend, REST backend, or distributed worker service. (Requirements: 4, 8)
- [ ] Do not remove SQLite persistence or migrate to a separate queue/database. (Requirements: 7)
- [ ] Do not grant external runners global filesystem, shell, network, or secret access as a shortcut. (Requirements: 6)
- [ ] Do not delete memory, history, runner, approval, audit, or channel data during migration. (Requirements: 7)