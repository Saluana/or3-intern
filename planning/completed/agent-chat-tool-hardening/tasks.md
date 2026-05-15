---
artifact_id: 794c3bb2-b6a7-4b41-a15a-0a313c7bb785
artifact_type: tasks
---

# Agent Chat Tool Hardening - Tasks

## 1. Baseline and contracts

- [x] Capture current `/internal/v1/turns` SSE payload shapes in focused service contract tests before changing event fields. (Req: 9, 13, 14)
- [x] Add provider fixtures for current OpenAI-compatible streaming behavior: text-only, fragmented tool call, non-SSE JSON fallback, empty SSE, malformed chunk, and HTTP error. (Req: 2, 13)
- [x] Add app reducer/component fixtures that represent the current queued, text_delta, tool_call, tool_result, assistant, completion, failed, aborted, and approval_required states. (Req: 10, 11, 13)
- [x] Document the current event compatibility promise in the planning notes or API docs before adding enriched fields. (Req: 9, 14)

## 2. Provider profiles and schema sanitization

- [x] Add `ProviderProfile`, `ToolSchemaPolicy`, `StreamPolicy`, and `ProviderRetryPolicy` types in `internal/providers`. (Req: 1, 2)
- [x] Implement conservative profile selection for OpenAI-compatible, OpenRouter-compatible, and local-compatible providers, falling back to OpenAI-compatible behavior. (Req: 1, 14)
- [x] Implement deep-clone schema sanitization for provider-facing tool definitions without mutating canonical tool schemas. (Req: 1, 14)
- [x] Strip or rewrite unsupported schema keywords by profile while preserving `type`, `properties`, `required`, `items`, `enum`, and `description`. (Req: 1)
- [x] Bound long tool/function descriptions during request preparation and report sanitization diagnostics. (Req: 1, 8, 12)
- [x] Update `toToolDefs` or the call site in `executeConversation` to apply schema sanitization per request. (Req: 1, 14)
- [x] Add sanitizer snapshot tests for representative tools: file read/write/edit, exec, web fetch/search, skill run, cron, memory, and MCP-style metadata. (Req: 1, 8, 13)

## 3. Stream assembler hardening

- [x] Extract streaming accumulation from `internal/providers/openai.go` into a testable `StreamAssembler`. (Req: 2, 14)
- [x] Add text dedupe logic that supports both delta-style and snapshot-style provider content. (Req: 2)
- [x] Replace direct `mergeStreamToolCalls` use with an accumulator that tracks provider ID, index, generated local ID, name, type, and argument fragments. (Req: 2, 3)
- [x] Track malformed chunks with bounded previews instead of silently discarding all decode failures. (Req: 2, 12)
- [x] Return a retryable stream error when the stream is empty or malformed before visible output. (Req: 2, 12)
- [x] Ensure fallback retry does not run after visible output has been emitted. (Req: 2, 11)
- [x] Add unit tests for suffix overlap, duplicate snapshot chunks, incomplete tool-call JSON, malformed chunks before output, malformed chunks after output, and fallback-to-non-stream behavior. (Req: 2, 13)

## 4. Tool call normalization

- [x] Add `NormalizedToolCall`, `ToolCallSource`, and `ToolCallIssue` types in the agent/runtime layer or a small internal package. (Req: 3)
- [x] Normalize provider-native tool calls into the new shape, preserving provider ID and index. (Req: 3)
- [x] Normalize existing function/tool markup fallback calls into the same shape. (Req: 3)
- [x] Generate stable local tool-call IDs for calls without provider IDs. (Req: 3, 9)
- [x] Dedupe repeated tool calls from stream replay or reconnect snapshots. (Req: 3, 11)
- [x] Replace runtime execution over raw `providers.ToolCall` with execution over `NormalizedToolCall`. (Req: 3, 4, 14)
- [x] Add tests for unknown names, blank names, duplicate provider IDs, duplicate generated calls, and markup/native parity. (Req: 3, 13)

## 5. Tool argument validation and model-visible correction

- [x] Implement a minimal validator for OR3's current JSON Schema subset: object root, properties, required, scalar types, arrays, objects, and enum. (Req: 4, 14)
- [x] Add safe coercion for numeric strings, boolean strings, one-item arrays, and JSON-object strings only when schema intent is unambiguous. (Req: 4)
- [x] Add validation error formatting as a bounded JSON tool result that the model can use to retry. (Req: 4, 5, 12)
- [x] Add `tools.Registry.ExecuteParams` use in runtime after validation so params are decoded only once. (Req: 4, 14)
- [x] Detect repeated validation failures for the same tool/path and stop with a concise public error before exhausting user patience. (Req: 4, 12)
- [x] Preserve existing approval and capability guard behavior after validation succeeds. (Req: 5, 14)
- [x] Add runtime tests where the model fixes invalid arguments after receiving a validation tool result. (Req: 4, 13)
- [x] Add tests for malformed JSON, missing required fields, enum mismatch, safe coercion, unsafe coercion refusal, and repeated invalid retries. (Req: 4, 13)

## 6. Tool policy, modes, and pruning

- [x] Define backend mode defaults for `ask`, `work`, and `admin` using existing tool metadata groups and capability levels. (Req: 6, 7)
- [x] Apply policy order consistently: explicit `tool_policy`, authenticated role/capability ceiling, active profile, mode defaults, then dynamic intent pruning. (Req: 6, 7)
- [x] Return policy reasons for hidden/unavailable tools so runtime can generate useful model-visible corrections. (Req: 5, 6)
- [x] Update `or3-app` chat submission to send a mode-derived `tool_policy` instead of unconditional `allow_all`. (Req: 7, 14)
- [x] Add a simple app-side mode selector or reuse an existing setting if one already exists; default to `work` only where current behavior expects tools. (Req: 7, 10)
- [x] Ensure approved retry/replay requests preserve mode and policy. (Req: 6, 7, 11)
- [x] Add backend tests for mode policies, profile precedence, capability ceilings, deny_all, allow_list, deny_list, dynamic pruning, and replay policy failures. (Req: 6, 7, 13)
- [x] Add app tests for mode policy payloads and retry preservation. (Req: 7, 11, 13)

## 7. MCP and tool metadata scanning

- [x] Add a metadata scanner for tool names, descriptions, and schema descriptions, with pattern classes for instruction override, secret exfiltration, hidden prompt requests, and unrelated behavioral commands. (Req: 8)
- [x] Integrate scanner results into MCP/external tool registration or registry filtering without scanning canonical local tool code on every turn. (Req: 8, 14)
- [x] Add config for scanner mode: `off`, `warn`, and `block`, defaulting to `warn` for untrusted external/MCP tools. (Req: 8)
- [x] Emit diagnostics for suspicious metadata with bounded previews and no secret leakage. (Req: 8, 12)
- [x] In block mode, hide suspicious tools from provider-visible schemas until allowlisted. (Req: 8)
- [x] Add tests for warn/block behavior, allowlisting, bounded previews, and common false-positive-safe descriptions. (Req: 8, 13)

## 8. Runtime events and service API enrichment

- [x] Extend `ConversationObserver` or add optional event methods for `turn_id`, `item_id`, `tool_call_id`, status, previews, public code, artifact ID, and approval ID. (Req: 5, 9)
- [x] Enrich existing `JobObserver` event payloads while keeping old event names and old fields. (Req: 9, 14)
- [x] Add bounded argument and result preview helpers shared by runtime/service event emission. (Req: 5, 9, 12)
- [x] Mark degraded completions and fallback-generated text explicitly in completion payloads. (Req: 11, 12)
- [x] Ensure `GET /internal/v1/jobs/:job_id` snapshots include enriched historical events when available. (Req: 9, 11)
- [x] Add service contract tests for enriched `tool_call`, `tool_result`, `text_delta`, `runtime_error`, `assistant`, and `completion` payloads. (Req: 9, 12, 13, 14)
- [x] Confirm no SQLite migration is needed for v1; if tests expose retention gaps, add a separate bounded `turn_events` migration plan before implementation. (Req: 11, 14)

## 9. Frontend stream reducer and message parts

- [x] Introduce a normalized `TurnEvent` adapter in `useAssistantStream.ts` so raw SSE parsing is separate from chat state reduction. (Req: 9, 10, 14)
- [x] Add `ChatMessagePart` state while preserving `content`, `toolCalls`, and `activityLog` for compatibility. (Req: 9, 10)
- [x] Upsert text parts by assistant item ID and tool parts by tool-call ID. (Req: 9, 10)
- [x] Deduplicate events by backend sequence first and event fingerprint second. (Req: 2, 11)
- [x] Render compact tool lifecycle rows with status, bounded argument/result previews, expandable raw details, copy actions, and artifact links when present. (Req: 10)
- [x] Preserve streamed assistant text when an approval attention state arrives instead of replacing it with only approval copy. (Req: 10, 11)
- [x] Improve empty completion handling so successful tool-only turns show a useful summary state. (Req: 10)
- [x] Add app tests for interleaved assistant text/tool calls, long output, approval attention, degraded completion, duplicate snapshot events, and no-visible-text completion. (Req: 10, 11, 13)

## 10. Stop, retry, and reconnect polish

- [x] Update the app stop action to call `/internal/v1/jobs/:job_id/abort` when `activeJobId` is known before aborting the local stream reader. (Req: 11)
- [x] Keep current local abort behavior as a fallback when no job ID has been assigned yet. (Req: 11, 14)
- [x] Preserve retry payload fields: session key, text, transport text, attachments, mode-derived policy, approval token, replay tool call, and continue message ID. (Req: 7, 11)
- [x] Improve reconnect handling so retained job snapshots resume without duplicate text/tool rows and unavailable snapshots become clear recoverable failures. (Req: 11)
- [x] Add UI states for queued, running, using tool, waiting approval, stopped, reconnecting, degraded, and failed. (Req: 10, 11)
- [x] Add tests for stop with job ID, stop before job ID, reconnect snapshot replay, unavailable job recovery, retry after validation failure, and retry after approval. (Req: 11, 13)

## 11. Error taxonomy and diagnostics

- [x] Add public error code constants for provider, stream, validation, policy, approval, tool execution, loop limit, and abort categories. (Req: 12)
- [x] Map runtime/provider/tool errors to public codes at the service boundary without exposing full provider bodies or raw secret-bearing payloads. (Req: 12)
- [x] Log provider profile, attempt count, sanitized body/chunk preview, job ID, and request ID for provider decode/stream failures. (Req: 2, 12)
- [x] Include public codes in `runtime_error`, `tool_result`, failed completion, and HTTP error payloads where applicable. (Req: 5, 12)
- [x] Add tests for error-code mapping and redaction/bounding of diagnostics. (Req: 12, 13)

## 12. Validation and rollout

- [x] Run focused Go tests for `internal/providers`, `internal/agent`, `internal/tools`, `internal/app`, and `cmd/or3-intern`. (Req: 13, 14)
- [x] Run frontend unit/component tests for `useAssistantStream`, chat message rendering, and approval/retry behavior. (Req: 10, 11, 13)
- [x] Run `bun run typecheck` in `or3-app` after app type changes. (Req: 13, 14)
- [ ] Manually test at least one streamed text turn, one tool turn, one invalid-argument recovery, one approval-required turn, one stop/abort, and one reconnect/follow-job flow. (Req: 2, 4, 5, 10, 11)
- [x] Keep enriched event fields behind backward-compatible payloads until the app has shipped reducer support. (Req: 9, 14)
- [x] Update API or developer docs with event fields, tool policy modes, error codes, and provider compatibility notes. (Req: 1, 7, 9, 12)

## Out of scope for v1

- [ ] Do not replace SSE with WebSockets solely for polish; keep the existing transport unless a separate realtime architecture plan requires it. (Req: 14)
- [ ] Do not add provider-specific SDK dependencies unless OpenAI-compatible HTTP cannot express a required behavior. (Req: 1, 14)
- [ ] Do not build a full JSON Schema implementation before the current OR3 tool schema subset is covered. (Req: 4, 14)
- [ ] Do not persist every token delta to SQLite unless reconnect testing proves the current job snapshot and message persistence are insufficient. (Req: 11, 14)
- [ ] Do not block all MCP tools by default; start with warn mode and make block mode explicit. (Req: 8)
