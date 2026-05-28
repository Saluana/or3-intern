# Tasks

## 1. Establish Shared WebSocket Infrastructure

- [ ] Add a shared service WebSocket ticket store in `cmd/or3-intern/service_websocket.go`. Requirements: 6.1, 6.2, 6.3
- [ ] Implement `POST /internal/v1/ws-tickets` with route, scope, expiry, role, and one-time ticket semantics. Requirements: 6.1, 6.3, 10.1
- [ ] Add route-specific subprotocol constants for turn, job, runner turn, and approvals WebSockets. Requirements: 6.2
- [ ] Add a shared WebSocket upgrade helper that validates subprotocol, consumes tickets, enforces origin, sets read limits, and configures write deadlines. Requirements: 6.2, 6.4, 9.1, 9.2
- [ ] Add service auth route-risk classification for each ticket scope and WebSocket route. Requirements: 6.5
- [ ] Add backend tests for ticket expiry, one-time consumption, wrong-route rejection, wrong-scope rejection, missing protocol rejection, and origin rejection. Requirements: 6.1, 6.2, 6.3, 6.4

## 2. Add Job Follow WebSocket

- [ ] Implement `GET /internal/v1/jobs/{id}/ws` beside the existing job SSE stream. Requirements: 1.1, 3.1
- [ ] Reuse `JobRegistry.SubscribeJob` to replay live snapshot events and stream new job events. Requirements: 3.1, 3.3
- [ ] Support `after_sequence` or `last_sequence` to omit already-seen events during reconnect. Requirements: 3.2, 7.1, 7.5
- [ ] Add persisted snapshot fallback for service jobs, subagent jobs, and agent CLI runs when the live registry no longer has the job. Requirements: 3.4, 7.2
- [ ] Send terminal completion/error frames with public error details when available before normal close. Requirements: 3.3, 8.3, 8.4
- [ ] Add tests for live replay, terminal replay, missing job, persisted job replay, sequence resume, and slow-client disconnect. Requirements: 3.1, 3.2, 3.3, 3.4, 9.3

## 3. Add Approval Realtime Events

- [ ] Create an in-memory approval event bus with bounded non-blocking subscriber fanout. Requirements: 5.2, 9.4
- [ ] Wire approval request creation in `internal/approval` to publish created or updated events. Requirements: 5.2
- [ ] Wire approve, deny, cancel, expire, exchange, and allowlist changes to publish updated count and state events. Requirements: 5.3, 5.4
- [ ] Implement `GET /internal/v1/approvals/ws` with initial pending snapshot and live bus subscription. Requirements: 5.1, 5.2, 5.3
- [ ] Keep approval action endpoints REST-authoritative and use WebSocket only for notification. Requirements: 5.4
- [ ] Add tests for snapshot, created, approved, denied, canceled, expired, allowlist updated, reconnect snapshot repair, and slow subscriber behavior. Requirements: 5.1, 5.2, 5.3, 5.5, 9.4

## 4. Add Direct Turn WebSocket

- [ ] Implement `GET /internal/v1/turns/ws` with `turn.start`, `turn.abort`, and optional `turn.follow` client frames. Requirements: 2.1, 2.5
- [ ] Reuse `decodeServiceTurnRequest`, `validateServiceToolCapabilities`, `serviceLifecyclePayload`, `runTurnJob`, and `JobRegistry` rather than creating a second agent execution path. Requirements: 1.3, 2.1
- [ ] Ensure socket close does not cancel the detached job unless the client explicitly sends abort. Requirements: 2.3
- [ ] Stream job events over WebSocket using the same event shape consumed by the frontend event applier. Requirements: 1.3, 2.1
- [ ] Add ping/pong liveness and write deadlines without generating assistant-visible heartbeat messages. Requirements: 2.2, 9.1
- [ ] Add reconnect support by returning job ID early and allowing follow through `/internal/v1/jobs/{id}/ws`. Requirements: 2.4, 7.1, 7.2
- [ ] Add tests for successful turn, provider text deltas, tool call/result events, approval required, runtime error, generic terminal error with prior runtime error, explicit abort, and disconnect-with-job-continuing. Requirements: 2.1, 2.3, 2.5, 8.4

## 5. Add Runner Turn WebSocket

- [ ] Implement `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/ws` beside the existing SSE route. Requirements: 4.1
- [ ] Reuse persisted `runner_chat_events` replay and `after_seq` semantics for v1. Requirements: 4.1, 7.5
- [ ] Send ping/pong while DB polling finds no new events. Requirements: 4.2
- [ ] Send a final `done` frame with status, final text, error message, and assistant message ID. Requirements: 4.3
- [ ] Preserve existing not-found and session mismatch semantics. Requirements: 4.4
- [ ] Add tests for replay, after-seq resume, terminal done, not found, session mismatch, and fallback to SSE. Requirements: 4.1, 4.3, 4.4, 1.2

## 6. Add Frontend WebSocket Utility

- [ ] Add a small ticketed WebSocket opener to `useOr3Api` or a new `useOr3WebSocket` composable. Requirements: 6.1, 6.2
- [ ] Build WebSocket URLs from `api.buildUrl` and pass route-specific subprotocol plus `or3.ticket.<ticket>`. Requirements: 6.2
- [ ] Support abort signals, pre-open rejection, close-code capture, and structured logging. Requirements: 7.1, 8.1
- [ ] Cache unsupported WebSocket routes per host and fall back without repeated noisy failures. Requirements: 1.2, 10.2
- [ ] Add unit tests for URL construction, protocol construction, ticket failures, unsupported route caching, and close diagnostics. Requirements: 6.1, 6.2, 7.2, 8.1

## 7. Wire Frontend Chat Transport

- [ ] Add `streamDirectTurnWebSocket` with the same `AssistantExecutionResult` shape as current stream execution helpers. Requirements: 2.1, 7.3
- [ ] Convert WebSocket `job.event` frames into the existing `createAssistantEventApplier` input. Requirements: 1.3, 2.1
- [ ] Track last seen sequence and reconnect to `/internal/v1/jobs/{id}/ws` after unexpected close. Requirements: 2.4, 7.1, 7.5
- [ ] Fall back to current SSE direct turn path if WebSocket fails before opening. Requirements: 1.2, 7.2
- [ ] Preserve existing snapshot recovery for post-open failures that cannot reconnect. Requirements: 7.2
- [ ] Update `useExecutionRouter` to prefer WebSocket for `or3-intern` direct turns behind capability detection. Requirements: 10.2
- [ ] Add frontend tests for success, reconnect, duplicate event dedupe, abort, unsupported fallback, and runtime-error display. Requirements: 2.1, 2.4, 2.5, 7.5, 8.4

## 8. Wire Frontend Approval Realtime

- [ ] Add `useApprovalRealtime` or integrate WebSocket lifecycle into `useApprovals` without disrupting REST actions. Requirements: 5.4
- [ ] Apply `approval.snapshot` to local approvals and pending count. Requirements: 5.1
- [ ] Apply created, updated, removed, count, and allowlist events incrementally. Requirements: 5.2, 5.3
- [ ] Fall back to existing polling when WebSocket is unsupported, pre-open fails, or auth is not ready. Requirements: 1.2, 7.2
- [ ] Refresh approval snapshot on app visibility resume and host changes. Requirements: 7.4
- [ ] Add tests for snapshot, live updates, count changes, host switch reset, fallback polling, and action REST authority. Requirements: 5.1, 5.3, 5.4, 7.4

## 9. Wire Frontend Runner Turn Transport

- [ ] Add `streamFollowRunnerTurnWebSocket` that mirrors the existing runner turn stream return shape. Requirements: 4.1
- [ ] Prefer runner turn WebSocket in `streamRunnerChat` and `streamFollowRunnerTurn` when supported. Requirements: 4.1, 10.2
- [ ] Fall back to current SSE route on unsupported or pre-open failure. Requirements: 1.2
- [ ] Add tests for event replay, final done frame, after-seq reconnect, and SSE fallback. Requirements: 4.1, 4.3, 7.1

## 10. Documentation and Contract Updates

- [ ] Document `/internal/v1/ws-tickets`, subprotocols, ticket scope, expiry, and close codes. Requirements: 10.1
- [ ] Document direct turn, job follow, runner turn, and approval WebSocket routes beside their SSE/REST fallback routes. Requirements: 1.1, 10.1
- [ ] Update service contract tests or route inventories so WebSocket routes are intentionally tracked without removing old routes. Requirements: 1.1, 10.3
- [ ] Add troubleshooting notes for WebSocket failures, route unsupported fallback, reconnect, and provider/runtime errors. Requirements: 8.1, 8.2, 8.3

## 11. End-to-End Validation

- [ ] Run a long direct OR3 turn with quiet model/tool periods and verify no user-visible idle timeout appears. Requirements: 2.2, 7.1
- [ ] Disconnect the browser mid-turn, reconnect, and verify job events replay without duplicated assistant text. Requirements: 2.3, 2.4, 7.5
- [ ] Trigger an approval from a chat turn and verify header count and approval panel update without waiting for polling. Requirements: 5.2
- [ ] Approve a request and verify approval resume job streams or reconnects through job WebSocket. Requirements: 5.3, 3.1
- [ ] Validate mobile/background resume by suspending and restoring the app, then checking chat and approval snapshots repair missed events. Requirements: 7.4
- [ ] Run focused backend and frontend suites, then run broader service and app typecheck/test suites before rollout. Requirements: 10.3
