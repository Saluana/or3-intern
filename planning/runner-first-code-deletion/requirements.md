# Requirements: Runner-First Code Deletion

## 1. Overview

The runner-first migration has made external runners responsible for chat, background work, file edits, shell execution, web access, and general agent behavior. This plan removes as much legacy built-in agent and model-callable tool code as possible now that there are no external users to preserve compatibility for.

Scope includes `or3-intern` backend packages, service endpoints, config/settings, docs/tests, and the companion `or3-app` paths that still call legacy turn/subagent/tool replay APIs. The main assumption is that breaking old local API clients, old config knobs, and old in-progress legacy jobs is acceptable as long as preserved OR3 platform systems continue to work for runner-backed turns.

## 2. Requirements

### Requirement 1: Delete the built-in provider/tool-loop runtime

The implementation must remove the old `agent.Runtime` execution path and all code whose only purpose is provider-native chat turns with model-callable OR3 tools.

Acceptance criteria:
- No normal command, service, channel, cron, webhook, heartbeat, or app chat path can call `agent.Runtime.Handle`, `RunBackground`, `executeConversation`, or built-in tool-loop helpers.
- `/internal/v1/turns` no longer starts built-in provider/tool-loop turns.
- `cmd/or3-intern` startup no longer builds a full `agent.Runtime` for normal operation.
- Searches for `agent.Runtime` in production code either return no matches or only transitional test/build shims scheduled for deletion in the same task group.

### Requirement 2: Preserve runner-first platform behavior

The cleanup must keep OR3-owned systems that feed or supervise external runners.

Acceptance criteria:
- Runner chat sessions, runner turns, and agent CLI jobs still persist durable events/messages in SQLite.
- Memory retrieval, pinned memory, runner-initiated memory writes/searches, document retrieval, bootstrap files, heartbeat, cron, webhooks, file-watch triggers, channel delivery, approvals, audit logging, artifacts, service auth, and Clawhub/catalog metadata still work without the old runtime.
- Runner context assembly remains bounded and deterministic.
- Existing SQLite data remains readable for current runner/chat/activity views.

### Requirement 2a: Provide runner-callable OR3 memory capabilities

External runners should be able to intentionally use OR3 memory, not only receive passive retrieved-memory context. Memory is a retained OR3 platform capability, not disposable legacy tool-loop code.

Acceptance criteria:
- Runners can search OR3 memory for the current session/global scope through a safe, bounded interface.
- Runners can create durable memory notes when the user asks them to remember something or when a durable project decision should be saved.
- Runners can read and update pinned memory through an explicit, audited path with appropriate write guards.
- Memory calls preserve session isolation and scope resolution, and never expose secrets beyond existing memory safety rules.
- The implementation does not keep the old generic `tools.Registry` solely to support memory; it exposes memory through a runner-appropriate interface such as a local MCP server, a runner bridge command, or typed service endpoints that runners can call.
- OR3 App can show memory activity/audit records for runner-created memories.

### Requirement 2b: Preserve channels with runner-first command routing

Channels must keep working after the built-in runtime is deleted, and channel users must be able to select runner/provider/model preferences without those commands being forwarded to the runner as normal prompts.

Acceptance criteria:
- Normal CLI, Telegram, Slack, Discord, WhatsApp, email, webhook, cron, and heartbeat turns route only through `RunnerTurnOrchestrator`; no channel path can fall back to `agent.Runtime`.
- Channel slash commands such as `/help`, `/settings`, `/runners`, `/runner`, `/models`, `/model`, and `/reset` are intercepted before runner turn creation.
- Per-channel or per-session runner/model preferences persist durably and are injected into runner turn metadata as `runner_id` and `model`.
- Invalid or deleted runner/model preferences fail closed with setup guidance and do not silently fall back to the old runtime.
- Telegram registers bot commands so Telegram clients suggest command names; model/runner option selection uses explicit command arguments, inline keyboards, reply keyboards, or follow-up prompts rather than relying on unsupported dynamic argument autocomplete.
- Existing approval commands such as `/approve` and `/deny` continue to work and are ordered before generic runner/model commands.

### Requirement 3: Replace `internal/agent` with small platform packages

Reusable non-runtime types currently inside `internal/agent` must move into smaller packages before deleting the old runtime files.

Acceptance criteria:
- Attachment types/functions move out of `internal/agent/chat_attachment.go` into a package such as `internal/turns` or `internal/chatctx`.
- Job registry/event/snapshot types move out of `internal/agent/job_registry.go` into a package such as `internal/jobs`.
- Streaming observer interfaces, null streamer, public error classification, and runner context bootstrap defaults move to packages whose names do not imply a built-in agent runtime.
- Production imports of `internal/agent` are eliminated.
- Tests are moved or removed with their target code.

### Requirement 4: Delete legacy subagent and skill-run execution paths

The implementation must remove old built-in subagent execution, spawn tools, and skill-run plan execution paths now that runners own background work.

Acceptance criteria:
- `agent.SubagentManager` and `tools.SpawnSubagent` are deleted.
- Service routes for creating new subagent jobs are removed or changed to return `410 Gone` with a runner-job message during the deletion pass.
- `tools.RunSkill`, `tools.RunSkillScript`, `skill_run_plans` creation/resume code, and related approval subjects are removed unless a non-agent catalog-only use remains.
- `or3-app` no longer lists, creates, retries, or follows `/internal/v1/subagents` except optional read-only historical labels if retained.

### Requirement 5: Delete legacy model-callable OR3 tool implementations

Legacy OR3 tool-loop tools should be deleted when they are only needed by the old built-in runtime. Retained platform capabilities, especially memory, should be re-exposed through runner-safe interfaces rather than preserved as generic built-in model tools.

Acceptance criteria:
- Delete file/web/exec/skill/spawn/cron/message tool implementations from `internal/tools` when they only exist for the built-in tool loop.
- Replace memory tools as old `tools.Registry` entries with runner-callable memory APIs or a runner-visible local MCP/bridge; do not delete runner memory capability.
- Delete artifact read tools as model-callable tools; keep equivalent artifact behavior through direct service/internal APIs where still needed.
- Delete tool schema exposure, dynamic tool filtering, tool-loop budget/quota code, plan-gate tool enforcement, tool-call replay, and model-callable MCP registration.
- Any retained Doctor/admin operations use typed Go service functions or a dedicated admin action interface, not the old generic model-callable `tools.Registry` surface.

### Requirement 6: Keep or replace only minimal shared safety primitives

If some safety primitives are still useful for service/admin actions, they must be moved out of `internal/tools` before deleting tool implementations.

Acceptance criteria:
- Capability levels, approval-required errors, requester identity, child env helpers, path canonicalization, and bounded result envelopes either move to narrower packages or are deleted if unused.
- Retained service file/terminal/admin APIs continue to enforce path, role, approval, and output bounds without depending on model-callable tools.
- No generic tool registry is needed for normal runner-first operation.

### Requirement 7: Remove legacy service APIs and app direct-turn fallbacks

The OR3 App and service API must stop using old built-in turn, subagent, and replay-tool endpoints.

Acceptance criteria:
- `or3-app` stops calling `/internal/v1/turns` for normal chat and uses runner chat endpoints exclusively.
- Composer send is blocked with setup guidance when no selectable runner exists.
- Replay-tool retry UI and payload fields are removed or converted to runner permission retry flows.
- `useJobs.ts` no longer calls `/internal/v1/subagents` for creation or polling new work.
- Backend tests pin that old creation endpoints do not create legacy work.

### Requirement 8: Simplify config and settings after deletion

Config fields that existed only for old built-in runtime/tool behavior must be removed from active structs, configure UI, metadata, env overrides, and app settings.

Acceptance criteria:
- Remove active config for `tools.enableExec`, `tools.braveAPIKey`, built-in file/web/tool-loop limits, subagent execution, skill execution, dynamic tool exposure, tool schema budgets, plan-gated tools, and old chat/subagent model routes.
- Keep config only for retained platform systems, runner settings, memory/indexing, embeddings/consolidation if still model-backed, service auth, channels, approvals, audit, cron, artifacts, and file/terminal service APIs.
- Loading old config ignores unknown deleted fields safely.
- `or3-app` settings/search no longer surfaces deleted controls.

### Requirement 9: Remove stale docs/tests and update terminology

The repository should stop documenting and testing deleted behavior as if it is supported.

Acceptance criteria:
- Delete or archive tests for removed runtime/tools instead of keeping compatibility tests.
- Update docs to describe OR3 as a runner control plane, not a built-in agent/tool runtime.
- Remove references to always-available `or3-intern` runners, built-in subagents, built-in tools, provider tool calls, and `/internal/v1/turns` as normal execution.
- Help/status output uses runner terminology.

### Requirement 10: Validate aggressively during deletion

Because this is a large removal, validation must proceed in small compile-safe slices.

Acceptance criteria:
- Each slice compiles before the next broad deletion.
- Focused Go tests cover runner chat, agent CLI jobs, cron runner dispatch, channels, memory retrieval, service auth, approvals, artifacts, and config load.
- OR3 App tests cover no-runner empty states, runner-only chat send, Agents, Scheduled Tasks, Activity labels, and settings search.
- Final validation includes broad `go test ./...` and OR3 App typecheck/test commands.

## 3. Non-functional constraints

- Keep runner context construction bounded in size and deterministic.
- Preserve SQLite schema compatibility for retained data; deleting creation paths must not require deleting old rows immediately.
- Avoid broad new abstractions unless they replace large legacy packages with smaller direct service packages.
- Keep file, terminal, channel, approval, and audit safety checks explicit after removing `tools.Registry`.
- Keep memory as an intentional runner-accessible platform skill/API, with bounded reads and explicit write semantics.
- Do not preserve deprecation paths solely for external compatibility; nobody uses the project yet.
- Prefer deletion over compatibility shims once a replacement runner-first path is in place.
