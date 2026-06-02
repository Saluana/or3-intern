# Requirements: Runner-First Agent Removal

## Overview

Replace the buggy built-in `or3-intern` agent turn loop with a runner-first architecture where external agent CLIs, especially OpenCode, are the default execution engine. Preserve the valuable orchestration pieces already in the repo: tools, channels, memory, artifacts, approvals, Clawhub support, triggers, webhooks, cron, heartbeats, app/service APIs, and runner history.

Assumptions:

- OpenCode becomes the default chat and autonomous runner when installed and authenticated.
- Codex, Claude Code, and Gemini remain supported as alternate runners.
- The internal agent package is not deleted in one unsafe pass; reusable primitives are carved out first, then the provider/tool-loop runtime is removed or quarantined.
- Existing SQLite data remains readable, including `messages`, memory notes, runner chat tables, agent CLI run tables, approvals, audit, and channel sessions.

## Requirements

1. **Runner-first default execution**
   - The system must route new chat, service, channel, heartbeat, cron, webhook, and file-watch turns through runner-backed execution instead of `agent.Runtime.Handle` provider/tool-loop execution.
   - Acceptance criteria:
     - `or3-intern chat` starts a runner-backed session using OpenCode by default when available.
     - `serve` channel messages enqueue runner chat turns instead of invoking the built-in agent loop.
     - `service` turn APIs either call runner chat or clearly expose runner run endpoints as the supported execution path.
     - Tests prove a bus event can be converted into a bounded runner turn request without calling the built-in provider/tool loop.

2. **OpenCode as the default runner**
   - The default runner selection must prefer `opencode`, with explicit fallback behavior when OpenCode is missing or unauthenticated.
   - Acceptance criteria:
     - New default config enables runner delegation and selects `opencode` unless overridden.
     - If OpenCode is unavailable, startup/readiness surfaces an actionable finding instead of silently falling back to the removed OR3 agent.
     - Existing users can choose another supported runner through config or OR3 App runner selection.

3. **Remove OR3 as a selectable agent runner**
   - `or3-intern` must stop advertising the built-in OR3 agent as a selectable chat/admin runner once runner-first execution is enabled.
   - Acceptance criteria:
     - `/internal/v1/chat-runners` no longer marks `or3-intern` as always available.
     - Doctor/admin brain code no longer rewrites API-key provider sessions back to `RunnerOR3` as a hidden fallback.
     - Existing session metadata with `runner_id='or3-intern'` remains readable and is migrated or treated as legacy.

4. **Keep orchestration features**
   - Channels, triggers, cron, heartbeat, webhooks, file-watch, approvals, audit, memory, artifacts, Clawhub, service APIs, and app-facing session history must continue to work after removing the built-in turn loop.
   - Acceptance criteria:
     - `serve` still starts enabled channels, webhook server, file watcher, heartbeat service, cron service, and approval handling.
     - Cron payloads for `agent_cli_run` keep working.
     - Legacy cron payloads for `agent_turn` and `system_event` are translated into runner turns or rejected with migration guidance.
     - Channel approval commands still resolve pending approvals without invoking a model turn.

5. **Preserve useful prompt/context/memory behavior**
   - Runner prompts must include the repo’s retained context sources in a bounded way: session history, static bootstrap files, memory retrieval, doc retrieval where configured, heartbeat instructions for autonomous turns, and attachments metadata.
   - Acceptance criteria:
     - Runner chat prompt construction has deterministic byte/message limits.
     - Memory retrieval and consolidation continue to use existing SQLite-backed memory tables.
     - HEARTBEAT/autonomous context is included only for heartbeat, cron, webhook, and file-watch turns.
     - No prompt path requires the old provider-driven tool loop.

6. **Retain safe-by-default boundaries**
   - Runner execution must preserve or improve existing safety guarantees for workspace access, command execution, network access, secrets, output size, and approvals.
   - Acceptance criteria:
     - Runner runs validate `cwd` against `workspaceDir` or configured restrictions.
     - Runner modes/isolation continue to enforce `review`, `safe_edit`, and explicit sandbox/host-write policy.
     - OpenCode external-directory permissions are limited to OR3-owned paths, not a global bypass.
     - Child process environment allowlists do not leak secrets by default.
     - Runner output/events remain chunked and capped; oversized output is spilled to artifacts when appropriate.

7. **Keep SQLite compatibility**
   - Existing SQLite databases must open cleanly after the migration, with additive migrations or compatibility shims where needed.
   - Acceptance criteria:
     - No migration drops `messages`, `agent_cli_runs`, `agent_cli_events`, `runner_chat_sessions`, `runner_chat_turns`, or memory tables.
     - Legacy `runner_id='or3-intern'` metadata is migrated to a configured default runner where safe, or left readable with a legacy label.
     - Tests cover opening a database with legacy OR3 session metadata.

8. **Shrink the codebase without breaking retained APIs**
   - Remove or quarantine code that only supports the built-in model/tool-loop agent, while keeping shared types or moving them into smaller packages.
   - Acceptance criteria:
     - Build no longer depends on `agent.Runtime.Handle` for primary chat/channel/service execution.
     - Types still needed by runners, service streaming, approvals, or attachments are moved to neutral packages or kept in a reduced compatibility package.
     - Deleted code has corresponding test updates rather than broad test disablement.

9. **Operator migration and documentation**
   - Operators must have a clear migration path from OR3-agent-first installs to runner-first installs.
   - Acceptance criteria:
     - Docs explain installing/authenticating OpenCode and selecting alternate runners.
     - `doctor` or `health` reports runner readiness and legacy OR3-agent deprecation status.
     - Config reference documents new defaults and any deprecated fields.

## Non-functional constraints

- Keep the design simple and bounded; avoid adding a new daemon, web framework, queue service, or multi-process database model.
- Preserve single-process SQLite behavior and deterministic migrations.
- Prefer small changes in existing packages before adding new packages.
- Bound runner concurrency, queue depth, timeout, event chunk size, prompt replay size, and persisted output.
- Do not silently grant more filesystem, command, network, or secret access to external runners than the current safety model allows.
- Keep backward compatibility for config loading, session keys, channel identities, approval records, audit records, stored memory, and history.
- Fail closed when runner readiness or permissions are ambiguous.