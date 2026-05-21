# Runner UX Requirements

## Overview

OR3 should make OpenCode, OpenAI Codex, and Claude Code feel like first-class OR3 runners instead of command wrappers. The implementation should prefer each runner's native server or SDK session interface when available, preserve the existing CLI execution path as a safe fallback, and present the experience in or3-app as one coherent OR3 workflow for non-technical users.

Assumptions:

- OpenCode should use `opencode serve` plus the OpenCode SDK/API before falling back to `opencode run`.
- Codex should use `codex app-server` JSON-RPC before falling back to `codex exec`/`codex exec resume`.
- Claude should use the Claude Agent SDK permission/session path when a supported bridge is available, but keep the current `claude --output-format stream-json` CLI path as the default fallback until the bridge is proven stable.
- The existing OR3 Intern runner remains the default and all external runners remain optional.
- The plan covers both `or3-intern` runtime work and `or3-app` UX work, with implementation artifacts primarily owned by `or3-intern`.

## Requirements

1. As a user, I can see OpenCode, Codex, and Claude Code as native OR3 runner choices with clear readiness, authentication, and fallback status.
   - Acceptance criteria:
     - `GET /internal/v1/chat-runners` exposes each runner's preferred runtime mode, available runtime modes, server/SDK readiness, binary readiness, auth readiness, and user-facing next action.
     - or3-app labels runner states in plain language such as “Ready”, “Sign in”, “Install”, “Server unavailable; using CLI fallback”, or “Disabled”.
     - OR3 Intern remains selectable even when external runner discovery fails.

2. As a user, runner conversations use native server/session APIs before CLI command execution whenever possible.
   - Acceptance criteria:
     - OpenCode chat turns use a managed local `opencode serve` connection and OpenCode session prompt APIs when server mode is healthy.
     - Codex chat turns use a managed `codex app-server --listen stdio://` or loopback websocket JSON-RPC connection when app-server mode is healthy.
     - Claude chat turns use a Claude Agent SDK bridge only when explicitly available and healthy; otherwise they use the current CLI path.
     - Each runner falls back to the existing command adapter with a visible runtime warning when native runtime startup, handshake, or turn execution fails before a destructive action begins.

3. As a user, approvals from external runners appear in OR3's approval inbox with friendly explanations and one-click decisions.
   - Acceptance criteria:
     - OpenCode permission requests map to OR3 approval requests and reply to OpenCode with once/always/reject decisions.
     - Codex app-server approval requests for command execution, file changes, tool calls, and permission changes map to OR3 approval requests and resume the blocked JSON-RPC request after the user decides.
     - Claude SDK `can_use_tool` or hook permission callbacks map to OR3 approval requests when the SDK bridge is active.
     - Approval details hide secrets, summarize command/file intent, and include an advanced raw payload section only for diagnostics.

4. As a user, runner output feels like normal OR3 chat output instead of raw terminal output.
   - Acceptance criteria:
     - Native runtime events are normalized into the existing runner chat event vocabulary: text deltas, reasoning/plan deltas, tool lifecycle, file changes, approval requests, warnings, errors, and completion.
     - or3-app renders runner tool/file activity in the same activity timeline used by OR3 Intern turns.
     - Empty or malformed provider events never result in a blank assistant response; users receive a clear recovery message and retry option.

5. As a user, runner sessions can be resumed, interrupted, and recovered safely.
   - Acceptance criteria:
     - Native session/thread IDs are stored in existing runner chat session metadata and reused for follow-up turns.
     - Active turns can be aborted through the same or3-app stop button.
     - Service restart marks in-flight turns as aborted or resumable according to backend capability, without leaving the app stuck in streaming state.
     - Existing replay-mode transcript fallback still works for runners without healthy native resume.

6. As a non-technical user, setup failures provide actionable guidance instead of implementation detail.
   - Acceptance criteria:
     - Missing binaries, auth failures, server startup failures, unsupported versions, and disabled configs return stable error codes plus friendly remediation messages.
     - or3-app can offer “Install”, “Sign in”, “Retry detection”, and “Use OR3 Intern instead” actions where applicable.
     - Raw command args, JSON-RPC payloads, and stack traces are available only in advanced/debug views.

7. As an engineer, runner implementations share a small provider-neutral contract instead of one-off command special cases.
   - Acceptance criteria:
     - `internal/agentcli` gains a runtime backend interface for detect/start turn/stream/approve/abort behavior.
     - Existing CLI adapters remain intact and become the fallback backend.
     - Native OpenCode, Codex, and optional Claude SDK backends emit the same canonical `RunnerChatEvent` stream as CLI adapters.
     - Backend selection is deterministic and configurable per runner.

8. As an operator, runner server processes are bounded and local-only by default.
   - Acceptance criteria:
  - Native runner servers are started lazily on first use by default, not eagerly during `or3-intern service` boot, `scripts/restart-service.sh restart`, or `scripts/restart-service.sh start`.
  - Before OR3 starts a managed runner server, it checks for an already-running compatible instance and either attaches safely or starts its own isolated instance.
  - OR3 distinguishes between OR3-managed instances and externally managed user instances so it never kills a server it does not own.
     - Managed server processes bind to loopback or stdio only unless explicitly configured otherwise.
     - Startup, idle, and turn timeouts are enforced.
     - At most one managed server process per runner/workspace is started by default.
     - Process logs and event payloads respect existing output chunk, preview, and persisted-output limits.

9. As an operator, service restarts and weird local state do not leave runner UX in a broken or dangerous state.
   - Acceptance criteria:
  - Restarting `or3-intern` reconciles any OR3-managed native runtime state without assuming child runner servers are still healthy.
  - Stale pid files, occupied loopback ports, orphaned child processes, and externally restarted runner servers are detected and surfaced with clear remediation.
  - If native runtime ownership or health is ambiguous, OR3 prefers a safe new isolated runtime instance or explicit CLI fallback over forcefully reusing the unknown instance.
  - Detection and execution paths are idempotent under repeated start/restart/status checks.

10. As an engineer, the work is testable without requiring real OpenCode/Codex/Claude accounts.
   - Acceptance criteria:
     - Unit tests use fake runtime backends and fake JSON-RPC/SSE streams.
     - Approval request/response tests cover OpenCode, Codex, and Claude-like permission payloads.
     - Existing runner CLI adapter tests continue to pass unchanged or with minimal fixture updates.

## Non-functional constraints

- Keep the design small and local-first: no new remote service, hosted control plane, frontend framework, or background daemon beyond managed local runner server processes.
- Preserve SQLite compatibility and existing runner chat/session history; migrations must be additive and safe for existing databases.
- Keep single-process determinism: in-memory backend sessions can be lost on restart, but persisted SQLite rows must reconcile cleanly.
- Respect existing safety boundaries: restricted working directories, bounded command execution, bounded output, approval tokens, allowlists, and audit behavior.
- Do not leak secrets through runner events, approval payloads, logs, or app toasts.
- Use native runtime APIs only when they improve reliability and approval handling; do not replace stable OR3 Intern behavior with provider-specific complexity.
- Keep fallback behavior explicit and visible so users understand when OR3 is using a lower-fidelity runner path.
- Prefer lazy startup and explicit ownership over boot-time helper daemons so service scripts stay predictable for local users.