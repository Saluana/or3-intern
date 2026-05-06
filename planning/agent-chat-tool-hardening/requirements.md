---
artifact_id: ffe0f2f0-7057-46d6-8d6e-f995ff83f6a1
artifact_type: requirements
---

# Agent Chat Tool Hardening - Requirements

## Overview

Harden `or3-intern` agent turns, provider streaming, tool calling, and the `or3-app` chat experience so conversations fail less often, recover more gracefully, and feel more polished while preserving the existing service API shape.

The plan is backend-first because most failures originate before the UI receives final text: provider-specific streaming quirks, malformed tool arguments, unsupported JSON schema shapes, unavailable tools, over-broad tool exposure, and errors that are not currently translated into useful model-visible or user-visible state. The app work should consume richer normalized events rather than inventing provider-specific behavior in Vue.

Research notes from `pingdotgg/t3code` that should inform this work:

- T3 Code wraps providers behind adapters and projects provider runtime activity into typed orchestration/domain events before the browser sees it.
- Tool lifecycle is modeled separately from assistant text, with explicit pending/running/completed/failed states.
- Assistant text streaming is deduped before emission so provider snapshots and deltas do not double-render.
- The UI derives a timeline from normalized thread state instead of raw provider chunks.
- Browser tests cover chat row rendering, local dispatch, streaming, and responsive layout as behavior, not just implementation details.

## Requirements

1. **Provider request construction is compatibility-aware.**
   - As an operator, I want OR3 to adapt tool schemas and request options to each configured provider, so that switching models does not break tool calling for avoidable format reasons.
   - Acceptance criteria:
     - WHEN a chat request is built THEN OR3 SHALL apply a provider profile before sending the request.
     - WHEN a provider does not support a JSON Schema keyword used by a tool THEN OR3 SHALL remove or rewrite that keyword without mutating the tool's canonical schema.
     - WHEN sanitized schemas differ from canonical tool schemas THEN OR3 SHALL keep enough diagnostics to explain what was changed.
     - IF the configured provider has no explicit profile THEN OR3 SHALL use a conservative OpenAI-compatible profile.

2. **Streaming assembly is resilient and deterministic.**
   - As a user, I want streamed responses to render once and finish cleanly, so that partial provider chunks do not create duplicate text or invisible failures.
   - Acceptance criteria:
     - WHEN provider SSE sends text deltas THEN OR3 SHALL append only the new visible text.
     - WHEN provider SSE sends snapshot-style text instead of deltas THEN OR3 SHALL compute and emit only the missing suffix.
     - WHEN streamed tool-call arguments arrive in fragments THEN OR3 SHALL assemble them by provider index and tool-call ID.
     - IF a stream ends with no data, truncated JSON, or incomplete tool-call JSON before any visible output is emitted THEN OR3 SHALL retry once using the configured fallback strategy.
     - IF visible output has already been emitted THEN OR3 SHALL not replay a fallback response in a way that duplicates user-visible text.

3. **Tool calls are normalized before execution.**
   - As a maintainer, I want one internal tool-call shape regardless of provider quirks, so that runtime execution and UI events are consistent.
   - Acceptance criteria:
     - WHEN a provider returns `tool_calls`, function-call markup, or provider-specific call fragments THEN OR3 SHALL normalize them into a single `NormalizedToolCall` structure.
     - WHEN a tool call has no provider ID THEN OR3 SHALL generate a stable local ID for the turn.
     - WHEN duplicate tool calls are received from stream replay or snapshots THEN OR3 SHALL dedupe them by provider ID, index, name, and normalized arguments.
     - IF a tool name is blank, unknown, or unavailable in the current turn THEN OR3 SHALL not execute it and SHALL give the model a clear tool-visible error or system correction.

4. **Tool arguments are validated and safely coerced.**
   - As a user, I want simple model mistakes in tool arguments to be corrected when safe, so that avoidable errors do not stop the turn.
   - Acceptance criteria:
     - WHEN arguments are valid JSON and match the tool schema THEN OR3 SHALL execute the tool normally.
     - WHEN arguments contain safe scalar mismatches such as numeric strings for numeric fields or a single string for a string array THEN OR3 SHALL coerce them only when the schema unambiguously allows it.
     - WHEN required fields are missing, JSON is malformed, enum values are invalid, or coercion would be ambiguous THEN OR3 SHALL not execute the tool.
     - IF validation fails THEN OR3 SHALL append a model-visible tool result describing the validation error and allow the model to retry within the existing tool-loop limit.
     - IF validation fails repeatedly for the same tool and same field THEN OR3 SHALL stop with a concise user-visible error instead of looping indefinitely.

5. **Tool execution errors are model-visible and user-actionable.**
   - As a user, I want tool failures to be explained and recoverable, so that I can retry, approve, or adjust the request without guessing.
   - Acceptance criteria:
     - WHEN a tool returns an execution error THEN OR3 SHALL preserve a bounded raw result for diagnostics and a concise result for the model.
     - WHEN approval is required THEN OR3 SHALL mark the tool lifecycle item as `attention` and include the approval request ID in the event payload.
     - WHEN a tool is unavailable due to mode, capability ceiling, profile, or dynamic exposure THEN OR3 SHALL identify that policy reason in the model-visible correction.
     - IF a tool failure produces a known public error code THEN the service event SHALL include that code.

6. **Tool exposure is pruned by task, mode, and policy.**
   - As an operator, I want the model to see only relevant tools, so that it is less likely to choose the wrong action and prompts stay smaller.
   - Acceptance criteria:
     - WHEN OR3 builds a turn request THEN it SHALL apply explicit `tool_policy`, active profile, capability ceiling, and mode policy before dynamic intent pruning.
     - WHEN dynamic pruning hides a tool THEN the hidden tool SHALL still be available for explicit replay only if current policy allows it.
     - WHEN no tool is relevant to the task THEN OR3 SHALL send no tools rather than an empty or misleading subset.
     - IF pruning would hide every tool required by an approved retry THEN OR3 SHALL return a policy error instead of silently failing.

7. **Mode-based behavior is simple and predictable.**
   - As a user, I want chat modes to map to understandable tool permissions, so that I can choose between asking, planning, and acting.
   - Acceptance criteria:
     - WHEN the app submits a normal chat turn THEN it SHALL include an explicit mode-derived `tool_policy` instead of always sending `allow_all`.
     - WHEN the mode is `ask` THEN OR3 SHALL expose read/search/memory/web tools but not write, exec, cron, service, or privileged tools.
     - WHEN the mode is `work` THEN OR3 SHALL expose read/write/web/skill tools and guarded exec subject to existing approval and capability checks.
     - WHEN the mode is `admin` THEN OR3 SHALL expose service/cron/privileged tools only when the authenticated role and capability ceiling allow them.
     - IF a saved profile is stricter than the selected mode THEN the stricter profile SHALL win.

8. **MCP and external tool metadata is scanned before exposure.**
   - As an operator, I want prompt-injection-like tool descriptions flagged, so that untrusted MCP tools cannot smuggle instructions into the model context unnoticed.
   - Acceptance criteria:
     - WHEN MCP or external tool metadata is loaded THEN OR3 SHALL scan names, descriptions, and schema descriptions for suspicious instruction patterns.
     - WHEN suspicious metadata is found THEN OR3 SHALL record a diagnostic with tool name, matched pattern class, and action taken.
     - IF the scanner runs in warn mode THEN OR3 SHALL keep the tool available but mark it in diagnostics.
     - IF the scanner runs in block mode THEN OR3 SHALL hide the tool from provider-visible schemas until the operator allowlists it.
     - The scanner SHALL avoid sending raw secrets or full untrusted metadata to external services.

9. **Turn events represent assistant text and tool lifecycle separately.**
   - As an app developer, I want normalized turn events, so that the chat UI can render a stable timeline without reverse-engineering backend internals.
   - Acceptance criteria:
     - WHEN a turn starts THEN OR3 SHALL emit a turn/job ID and monotonic sequence numbers as it does today.
     - WHEN assistant text streams THEN events SHALL identify the assistant message item and carry a text delta.
     - WHEN a tool call starts or changes state THEN events SHALL include `tool_call_id`, name, status, bounded arguments preview, and timestamps where available.
     - WHEN a tool finishes THEN events SHALL include status, bounded result preview, optional artifact ID, and public error code when present.
     - Existing event names such as `text_delta`, `tool_call`, `tool_result`, `assistant`, and `completion` SHALL remain backward compatible during rollout.

10. **The chat UI feels polished during streaming and tool use.**
    - As a user, I want to understand what the assistant is doing, so that tool calls, approvals, retries, and partial responses feel intentional instead of broken.
    - Acceptance criteria:
      - WHEN a turn is queued, started, using tools, waiting for approval, or finalizing THEN the UI SHALL show an appropriate compact state.
      - WHEN tool arguments or results are long THEN the UI SHALL show a concise preview with an expandable raw view.
      - WHEN assistant text streams around tool calls THEN the UI SHALL keep text and tool lifecycle in stable order.
      - WHEN no visible final text is produced but tools completed successfully THEN the UI SHALL show a useful completion state rather than a generic empty response.
      - Message actions SHALL support copy, retry, approve/deny where relevant, and stop while avoiding duplicate or stale actions.

11. **Stop, retry, and reconnect behavior is reliable.**
    - As a user, I want interrupt and recovery controls to affect the actual backend job, so that the UI state matches what OR3 is doing.
    - Acceptance criteria:
      - WHEN the user presses stop during a running turn THEN the app SHALL request backend job abort when a job ID is known, then stop reading the stream.
      - WHEN the browser disconnects and reconnects while a job is still retained THEN the app SHALL resume from the job snapshot/stream without duplicating prior events.
      - WHEN a failed tool call is retryable THEN the retry payload SHALL preserve the session key, mode policy, approval token if issued, and original arguments.
      - IF a reconnect snapshot is unavailable THEN the UI SHALL mark the message as recoverable or failed with a clear reason.

12. **Errors are classified, bounded, and observable.**
    - As a maintainer, I want failures to have stable categories, so that support and tests can distinguish provider, stream, validation, policy, approval, and tool execution problems.
    - Acceptance criteria:
      - WHEN an error crosses the service API boundary THEN it SHALL include a stable public code and request/job ID where available.
      - WHEN internal diagnostics include provider bodies, tool arguments, or tool output THEN values SHALL be bounded and redacted according to existing service safety practices.
      - WHEN a provider decode or stream assembly error occurs THEN OR3 SHALL log the provider profile, attempt count, and sanitized body/fragment preview.
      - IF a user-facing fallback is generated THEN the event payload SHALL mark it as degraded.

13. **Testing covers provider quirks and chat behavior.**
    - As a maintainer, I want regressions caught before release, so that tool calling remains stable across providers and UI changes.
    - Acceptance criteria:
      - Provider tests SHALL cover text-only streams, fragmented tool calls, snapshot-style deltas, malformed JSON, empty streams, non-SSE JSON fallback, unknown tool calls, and fallback retry behavior.
      - Runtime tests SHALL cover validation failure as tool result, safe coercion, repeated invalid arguments, policy-hidden tools, approval-required events, and tool-loop limits.
      - App tests SHALL cover streaming text, interleaved tool calls, approval attention state, retry payload preservation, stop/abort, reconnect snapshots, and long tool output rendering.
      - Contract tests SHALL pin event payload shapes for old and new event fields.

14. **Changes stay incremental and performant.**
    - As a maintainer, I want hardening without a large rewrite, so that OR3 improves quickly and safely.
    - Acceptance criteria:
      - The first implementation SHALL keep `/internal/v1/turns`, `/internal/v1/jobs/:id`, and existing SSE event names working.
      - Provider profiles, schema sanitization, validation, and event enrichment SHALL be additive around the current runtime loop.
      - Tool schema sanitization and validation SHALL avoid per-token expensive work and SHALL run once per request or once per tool call.
      - Chat rendering SHALL keep virtualization/autoscroll behavior stable for long conversations.

## Non-functional constraints

- **Security:** Never execute invalid, unavailable, blocked, or suspiciously injected tools just to keep a turn moving.
- **Compatibility:** Preserve OpenAI-compatible providers as the default path and avoid requiring provider-specific SDKs for v1.
- **Performance:** Keep request-time schema preparation proportional to visible tool count and keep event payloads bounded.
- **Recoverability:** Prefer model-visible corrections and retryable user states over opaque hard failures.
- **Observability:** Every failed turn should be diagnosable from request/job ID, public error code, and bounded internal logs.
- **Simplicity:** Implement narrow normalizers and validators for the schemas OR3 actually emits before considering a broader framework.
