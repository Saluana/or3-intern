# Steer vs Follow-Up Input - Requirements

## Overview

Users need a clean way to send another message while an `or3-intern` turn is already running. The first implementation should support two explicit modes from `or3-app`:

- **Interrupt next step:** treat the new message as steering for the active turn and apply it at the next runtime-safe boundary.
- **Add after this finishes:** queue the message as the next normal turn for the same chat session after the active turn reaches a terminal state.

Scope assumptions:

- The primary user surface is the `or3-app` chat composer talking to `or3-intern` through `/internal/v1/turns` and `/internal/v1/jobs`.
- The backend implementation belongs in `or3-intern`; `or3-app` should consume stable service contracts and keep the UI state clear.
- "Interrupt next step" must not kill a running provider request, shell command, file operation, or tool execution mid-call. It applies only at explicit safe points in the runtime loop.
- Existing channel behavior for Slack, Discord, WhatsApp, Telegram, email, cron, and CLI should remain backward compatible. They can keep serializing messages by session unless they opt into the new input mode later.

## Requirements

1. **Users can submit input while a chat turn is running.**
   - As an app user, I can type while the assistant is streaming or using tools.
   - Acceptance criteria:
     - `or3-app` no longer disables the composer solely because `isStreaming` is true.
     - The composer exposes two running-turn actions: `Interrupt next step` and `Add after this finishes`.
     - The existing stop/cancel control remains available and is visually distinct from both input modes.
     - Empty messages are rejected client-side and server-side as they are today.

2. **Interrupt input is applied to the active turn at a safe boundary.**
   - As a user correcting the assistant, my steer message is incorporated before the next model step when possible.
   - Acceptance criteria:
     - `POST /internal/v1/jobs/{job_id}/input` accepts `{ "mode": "interrupt_next_step", "message": "..." }` for a live `turn` job.
     - The active runtime turn records the steer request and checks for it before starting the next provider call after the current atomic step completes.
     - The current provider call or tool execution is not canceled by steer input.
     - The steer message is persisted to the session history only once, with metadata marking it as `input_mode=interrupt_next_step`.
     - The job stream emits stable lifecycle events such as `steer_queued` and `steer_applied`.
     - If the active turn reaches completion before the steer can be applied, the system converts the input into an after-current follow-up instead of dropping it.

3. **Follow-up input runs as the next normal session turn.**
   - As a user adding extra context, I can queue a message without disturbing current work.
   - Acceptance criteria:
     - `POST /internal/v1/jobs/{job_id}/input` accepts `{ "mode": "after_current", "message": "..." }` for a live `turn` job.
     - The response returns a distinct follow-up `job_id` in `queued` or `deferred` state.
     - The follow-up starts only after the targeted active turn reaches `completed`, `failed`, `aborted`, or `approval_required`.
     - Follow-up execution uses the same `session_key`, tool policy, profile, requester identity, auth context, and service capability ceiling rules as a normal `/internal/v1/turns` request.
     - Follow-up messages appear in session history in the order they actually run.

4. **Session isolation prevents overlapping turns for the same chat.**
   - As a maintainer, I can reason about message order without races.
   - Acceptance criteria:
     - Only one active runtime turn may mutate a session's prompt/history at a time.
     - Multiple after-current inputs for the same active job preserve FIFO order.
     - Concurrent interrupt inputs for the same active job collapse to a bounded latest-first behavior or an explicit small queue with documented limits.
     - Jobs for different sessions continue to run independently.

5. **The service API makes input mode explicit and backward compatible.**
   - As an API client, I can opt into running-turn input without changing existing turn requests.
   - Acceptance criteria:
     - Existing `/internal/v1/turns` requests continue to behave exactly as they do today.
     - The new job input route rejects unknown fields consistently with existing strict service decoders.
     - Invalid modes return `400` with a stable public error.
     - Unknown, terminal, non-`turn`, or unauthorized job IDs return `404` or `409` without leaking internal state.
     - API responses use snake_case fields and include enough data for `or3-app` to attach UI state to current or queued messages.

6. **The UI clearly represents pending steer and queued follow-up messages.**
   - As a user, I can tell whether my message will change current work or run later.
   - Acceptance criteria:
     - Interrupt messages are displayed immediately in the chat thread with a pending/applied state.
     - Follow-up messages are displayed immediately with a queued state and the returned follow-up `job_id`.
     - When a steer is applied, the related user message updates to complete.
     - When a follow-up starts, the queued user message and assistant placeholder transition into the normal streaming flow.
     - Failed enqueue/apply states show retryable errors without duplicating user messages.

7. **Runtime events remain bounded and replayable enough for app recovery.**
   - As a mobile app user, I can briefly background the app without losing the state of a queued correction.
   - Acceptance criteria:
     - Live job events are bounded by the existing `JobRegistry` cap.
     - Input events use concise payloads and do not include secrets or full internal request context.
     - App recovery via `/internal/v1/jobs/{job_id}` can display whether a steer was queued/applied or a follow-up job was created while the live stream was attached.
     - Follow-up jobs created through the job input route can be followed through the existing job stream path.

8. **Safety and auth match normal turn submission.**
   - As an operator, running-turn input is not a side channel around service auth or tool policy.
   - Acceptance criteria:
     - The new route goes through existing service auth middleware, browser boundary checks, request body limits, mutation rate limiting, and audit metadata capture.
     - The backend records the requesting actor/role in the same way as `POST /internal/v1/turns`.
     - The new route never accepts arbitrary tool output, tool-call IDs, raw history patches, or provider messages from the client.
     - Tool approvals, quota approvals, artifact spilling, and hardening policies continue to apply unchanged after a steer or follow-up.

9. **Regression coverage pins the intended behavior.**
   - As a maintainer, I can change runtime internals without silently breaking running-turn input.
   - Acceptance criteria:
     - Go tests cover steer application before the next provider call.
     - Go tests cover steer fallback to follow-up when the active job completes first.
     - Go tests cover FIFO after-current follow-up order.
     - Service tests cover request decoding, route errors, job stream events, and auth-compatible responses.
     - App unit tests cover composer behavior while streaming and `useAssistantStream` handling for both modes.

## Non-functional constraints

- **Deterministic behavior:** session input ordering must not depend on goroutine scheduling beyond documented FIFO queues.
- **Low memory usage:** pending running-turn input is bounded per active job/session and stores only message text plus small metadata.
- **Bounded loops/output/history:** steer input must not reset or bypass `MaxToolLoops`; it should count as additional context inside the existing loop.
- **SQLite safety and migration compatibility:** avoid a schema migration for v1 unless durable pending input is explicitly required; normal applied messages continue to use the existing `messages` table.
- **Secure handling:** do not expose requester tokens, approval tokens, service secrets, raw tool output, or raw provider messages in input events.
- **Backward compatibility:** existing `/internal/v1/turns`, `/internal/v1/jobs/{id}`, `/internal/v1/jobs/{id}/stream`, `/internal/v1/jobs/{id}/abort`, stored messages, session keys, and app recovery flows must continue to work.
