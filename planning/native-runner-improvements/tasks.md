# Native Runner Improvements Tasks

## 1. Runtime Metadata and API Contracts

- [x] (Req 1, 6) Extend `internal/agentcli/runners.go` with model option descriptors, runtime next-action text, native health detail, and provider refs.
- [x] (Req 1) Update `internal/controlplane` builders and `/internal/v1/chat-runners` / `/internal/v1/agent-runners` responses.
- [x] (Req 1, 6) Update `or3-app/app/types/or3-api.ts` and `useChatRunners.ts` to consume the enriched metadata.

## 2. OpenCode Native Runtime

- [x] (Req 3) Refactor `OpenCodeNativeRuntime` startup to capture stdout/stderr, parse ready URL, and report bounded diagnostics.
- [x] (Req 3) Add managed process-group cleanup, idle TTL shutdown, and explicit external-server ownership safeguards.
- [x] (Req 6) Replace or wrap loose model discovery with provider/model/agent/variant inventory normalization.
- [x] (Req 3, 7) Add tests for startup failure diagnostics, external server non-termination, idle shutdown, and model option parsing.

## 3. Codex Native Runtime

- [x] (Req 2) Introduce a Codex session runtime that owns app-server process, thread ref, active turn ref, and request refs.
- [x] (Req 2) Implement Codex `Abort` by interrupting the active turn/session.
- [x] (Req 2, 6) Use app-server probes for account/auth, model list, reasoning effort, fast mode, and skills when available.
- [x] (Req 2) Preserve CLI fallback for auto mode before destructive actions begin.
- [x] (Req 2, 6) Add fixture tests for app-server session start/resume/send/abort/model discovery.

## 4. Approval Continuation

- [x] (Req 4) Persist native approval request refs in runner chat event payloads and turn metadata.
- [x] (Req 4) Add runtime hooks to respond to Codex and OpenCode native approval requests after OR3 approval.
- [x] (Req 4) Keep current approval-token retry flow as fallback when the runtime has exited.
- [x] (Req 4) Update `or3-app` approval hydration/actions only if new response fields are required.
- [x] (Req 4) Add tests for live continuation, dead-runtime fallback, reject/cancel, and approve-for-session.

## 5. Event Normalization and Observability

- [x] (Req 5) Expand `chat_adapters.go` normalization for session state, request opened/resolved, token usage, config warnings, model reroutes, diffs, plans, and tool progress.
- [x] (Req 5) Ensure unknown native events become bounded diagnostics, not assistant text.
- [x] (Req 7) Add optional redacted rotating native/canonical event logs.
- [x] (Req 5, 7) Add fixture tests for Codex/OpenCode event mapping and redaction.

## 6. App Integration

- [x] (Req 1, 6) Update composer runner/model picker to show runtime readiness, fallback warnings, model options, and defaults from backend metadata.
- [x] (Req 4, 5) Update activity/approval UI only where new canonical events require display mapping.
- [x] (Req 1, 4, 6) Add focused app tests for runner metadata normalization and approval-required hydration.

## 7. Rollout

- [x] (Req 1-7) Default native runtime mode remains `auto`; CLI fallback remains available.
- [x] (Req 1-7) Add release notes/manual verification covering OpenCode server startup, Codex app-server chat, approval continuation, abort, and CLI fallback.
- [x] (Req 1-7) Run Go tests for `internal/agentcli`, `internal/controlplane`, and relevant `cmd/or3-intern` service tests; run focused `or3-app` tests.

## Out of Scope

- [ ] Do not port T3's TypeScript Effect provider registry.
- [ ] Do not add a hosted runner service or remote control plane.
- [ ] Do not remove existing CLI command adapters.
