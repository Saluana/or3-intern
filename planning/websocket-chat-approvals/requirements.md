# Requirements

## Overview

OR3 should add WebSocket transport for chat turns, job following, runner chat streams, and approval notifications while preserving the current REST and SSE APIs as compatibility and fallback paths. The goal is to make long-running agent work feel reliable and responsive: active jobs must keep a live connection healthy, approvals should appear immediately without polling, and the app should recover cleanly after tab sleep, network changes, or service restarts.

The implementation should reuse the existing terminal WebSocket patterns where possible: short-lived ticket issuance over authenticated REST, route-specific WebSocket subprotocols, origin checks, ping/pong liveness, JSON message frames, and domain-specific handlers rather than a large generic real-time multiplexer.

## Requirements

### 1. WebSocket Transport Compatibility

**User Story:** As a user, I want OR3 to keep the existing SSE and REST endpoints while adding WebSocket alternatives, so that current clients continue working and the app can opt into a more reliable live transport.

#### Acceptance Criteria

1. WHEN WebSocket routes are added THEN existing `/internal/v1/turns`, `/internal/v1/jobs/{id}/stream`, `/internal/v1/runner-chat/.../stream`, `/internal/v1/chat-sessions/.../messages/stream`, and approval REST routes SHALL remain available with unchanged response contracts.
2. WHEN the app detects that a WebSocket route is unsupported or fails before opening THEN it SHALL fall back to the current SSE or polling path without losing the submitted turn or approval state.
3. WHEN WebSocket and SSE payloads represent the same job event THEN they SHALL use the same `JobEvent` shape where practical so the existing frontend event applier can be reused.
4. WHEN the server emits new WebSocket events THEN event names, sequence fields, job IDs, trace IDs, request IDs, and approval IDs SHALL match existing REST/SSE naming conventions.

### 2. Direct Chat Turn WebSocket

**User Story:** As a user sending an OR3 chat message, I want the app to start and stream the turn over WebSocket, so that long-running work does not appear to randomly stop when HTTP/SSE is quiet.

#### Acceptance Criteria

1. WHEN the frontend starts an OR3 direct turn over WebSocket THEN the server SHALL register a service job, return the job ID over the socket, and stream queued, started, tool, text, completion, approval, and error events over the same connection.
2. WHEN the model or a tool is quiet for more than 45 seconds but the job is still running THEN the WebSocket connection SHALL stay alive using WebSocket ping/pong control frames and SHALL NOT produce user-visible assistant text.
3. WHEN the browser disconnects during a running turn THEN the backend job SHALL continue unless the user explicitly aborts it.
4. WHEN the app reconnects with a known job ID and last seen sequence THEN the server SHALL replay missed events before streaming live events.
5. WHEN the user taps Stop during a WebSocket-backed turn THEN the client SHALL send an abort message or call the existing abort endpoint and the server SHALL abort the job consistently with current REST behavior.

### 3. Job Follow WebSocket

**User Story:** As a user returning to a running or completed job, I want the app to follow that job over WebSocket, so that reconnect and snapshot recovery are fast and consistent.

#### Acceptance Criteria

1. WHEN the client opens `/internal/v1/jobs/{id}/ws` THEN the server SHALL replay the current job snapshot events in order before subscribing to live events.
2. WHEN the client provides `after_sequence` or `last_sequence` THEN the server SHALL omit already-seen events and send only newer events.
3. WHEN the job is already terminal THEN the server SHALL send replayed events and a terminal event before closing normally.
4. IF the job ID cannot be found in live or persisted stores THEN the route SHALL fail with a clear `job not found` close reason or pre-upgrade HTTP response.
5. WHEN a job terminal event contains a generic message but a prior runtime error exists THEN the frontend SHALL preserve and display the more detailed runtime error.

### 4. Runner Chat Turn WebSocket

**User Story:** As a user using an external runner, I want runner turn events to stream over WebSocket, so that runner output is lower latency than the current polling SSE stream.

#### Acceptance Criteria

1. WHEN the client opens `/internal/v1/runner-chat/sessions/{sessionId}/turns/{turnId}/ws` THEN the server SHALL replay persisted runner events after `after_seq`, then stream new events until the turn is terminal.
2. WHEN no new runner events are available THEN the route SHALL remain alive with ping/pong control frames rather than sending noisy UI events.
3. WHEN the runner turn reaches a terminal status THEN the server SHALL send a final `done` frame containing status, final text, error message, and assistant message ID.
4. WHEN a runner turn cannot be found or does not belong to the requested session THEN the route SHALL return the same not-found semantics as the existing REST/SSE routes.

### 5. Approval Notification WebSocket

**User Story:** As a user, I want approvals to appear and resolve immediately, so that OR3 does not depend on polling while an agent is waiting for my decision.

#### Acceptance Criteria

1. WHEN the app opens `/internal/v1/approvals/ws` THEN the server SHALL send an initial pending approval snapshot and pending count.
2. WHEN a new approval request is created THEN subscribed clients SHALL receive an `approval.created` or `approval.updated` event within one second under normal local-network conditions.
3. WHEN an approval is approved, denied, canceled, expired, exchanged, or allowlisted THEN subscribed clients SHALL receive an event with the updated request state and current pending count.
4. WHEN approval actions are submitted THEN the existing REST approval action endpoints SHALL remain authoritative for state changes and token issuance.
5. WHEN the app reconnects after missing approval events THEN the initial snapshot SHALL repair client state without requiring durable approval event storage.

### 6. WebSocket Authentication and Authorization

**User Story:** As an operator, I want WebSocket routes to enforce the same security model as REST routes, so that realtime connections do not weaken OR3's access controls.

#### Acceptance Criteria

1. WHEN a browser client wants to open a WebSocket route THEN it SHALL first request a short-lived route-scoped ticket through authenticated REST.
2. WHEN a WebSocket upgrade request arrives THEN the server SHALL require the route-specific subprotocol and a valid one-time ticket passed via `Sec-WebSocket-Protocol`.
3. WHEN a ticket is consumed THEN it SHALL be invalid for future connections and SHALL expire if unused.
4. WHEN an origin header is present THEN the WebSocket route SHALL apply the same allowed-browser-origin policy used by the terminal WebSocket.
5. WHEN route risk classification requires session or step-up auth THEN the ticket issuance endpoint SHALL enforce that risk level before issuing a ticket.

### 7. Reconnect and Recovery

**User Story:** As a mobile or desktop user, I want WebSocket-backed chat and approvals to recover after sleep, backgrounding, and network changes, so that long tasks remain usable.

#### Acceptance Criteria

1. WHEN a WebSocket closes unexpectedly while a job is live THEN the frontend SHALL attempt bounded reconnect with backoff and last seen sequence.
2. WHEN reconnect fails repeatedly THEN the frontend SHALL fall back to existing snapshot, SSE, or polling recovery and show a non-fatal reconnecting state.
3. WHEN a WebSocket closes normally after a terminal event THEN the frontend SHALL mark the turn complete, failed, or attention based on the terminal payload.
4. WHEN the app resumes from background THEN approval and chat subscriptions SHALL refresh snapshots before relying on live events.
5. WHEN duplicate events arrive after reconnect THEN frontend dedupe SHALL prevent duplicated text, tool calls, or approval cards.

### 8. Observability and Diagnostics

**User Story:** As a developer diagnosing OR3, I want WebSocket transport failures to produce useful logs and structured error codes, so that random-looking failures can be explained quickly.

#### Acceptance Criteria

1. WHEN a WebSocket opens, reconnects, closes, or fails THEN the frontend SHALL log route, trace ID, job ID, close code, and close reason where available.
2. WHEN the backend rejects a WebSocket ticket, origin, route, or role THEN it SHALL log a bounded diagnostic without leaking tokens or secrets.
3. WHEN a job fails server-side THEN terminal WebSocket frames SHALL include public error code and useful public message when available.
4. WHEN provider stream errors occur THEN WebSocket chat frames SHALL preserve runtime error detail before any generic terminal failure event.

### 9. Performance and Resource Limits

**User Story:** As a user running long tasks, I want WebSocket connections to be efficient and bounded, so that OR3 remains stable during multiple long-running jobs.

#### Acceptance Criteria

1. WHEN multiple app surfaces subscribe to realtime updates THEN each route SHALL enforce read limits, write deadlines, ping intervals, and cleanup on disconnect.
2. WHEN a client sends malformed or oversized WebSocket messages THEN the server SHALL close the connection with a policy or unsupported-data close code.
3. WHEN a client is slow to read frames THEN the server SHALL close that connection without blocking job execution or other subscribers.
4. WHEN approval notifications fan out to subscribers THEN publishing SHALL be non-blocking or bounded so approval creation cannot hang on a slow browser.

### 10. Documentation and Migration

**User Story:** As a maintainer, I want the WebSocket routes documented alongside the existing API, so that future changes keep SSE compatibility and realtime behavior clear.

#### Acceptance Criteria

1. WHEN WebSocket routes are implemented THEN service API docs SHALL list route paths, ticket routes, subprotocols, client frame types, server frame types, close behavior, and fallback routes.
2. WHEN frontend code defaults to WebSocket THEN a feature flag or capability detection path SHALL allow disabling WebSocket without removing SSE support.
3. WHEN tests cover WebSocket transport THEN they SHALL include direct turn, job follow, runner turn follow, approval snapshot/update, auth rejection, reconnect replay, and fallback behavior.
