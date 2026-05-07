# Steer vs Follow-Up Input - Tasks

## 1. Backend request contract

- [ ] (R5, R8) Add `serviceJobInputRequest` and strict JSON decoding in `cmd/or3-intern/service_request.go` with `mode`, `message`, `client_message_id`, and `meta`.
- [ ] (R5) Define accepted modes as `interrupt_next_step` and `after_current`; reject unknown modes, empty messages, trailing JSON, unknown fields, and oversized bodies.
- [ ] (R5, R8) Add service tests for decode success/failure cases in `cmd/or3-intern/service_test.go`.
- [ ] (R5) Add TypeScript API types in `or3-app/app/types/or3-api.ts` for `TurnJobInputRequest`, `TurnJobInputResponse`, and `TurnRunningInputMode`.

## 2. Service route and active turn tracking

- [ ] (R2, R3, R5) Extend `cmd/or3-intern/service.go` so `POST /internal/v1/jobs/{job_id}/input` routes beside `stream` and `abort`.
- [ ] (R4) Add a small `serviceTurnInputCoordinator` in `cmd/or3-intern/service_agents.go` or a new service file to track active turn jobs and deferred follow-up queues.
- [ ] (R4) Register active turn state before `runTurnJob` calls `app.RunTurn`, and unregister it after terminal completion/error handling.
- [ ] (R3, R4) Ensure deferred follow-up jobs publish `queued` immediately but do not publish `started` until they actually begin running.
- [ ] (R3, R4) Drain deferred follow-ups FIFO after the target job reaches any terminal state.
- [ ] (R3, R4) Treat `approval_required` as terminal for deferred follow-up dispatch, matching how `JobRegistry.Complete(..., "approval_required", ...)` closes the live job.
- [ ] (R5, R8) Add route tests for unknown job, terminal job, non-turn job, invalid method, and successful accepted responses.

## 3. Runtime steer support

- [ ] (R2, R4) Add `TurnSteerer`, `TurnSteerInput`, and context helpers in `internal/agent`.
- [ ] (R2) Pass the active job's steerer through `internal/app/service_app.go` when running a service turn.
- [ ] (R2) In `internal/agent/runtime_execution.go`, check for pending steer before each provider call after the current atomic step completes.
- [ ] (R2, R7) Persist applied steer messages through `DB.AppendMessage` with payload metadata containing `input_mode`, `target_job_id`, and `client_message_id`.
- [ ] (R2, R8) Append applied steer messages to the in-memory prompt as user messages without accepting raw provider/tool data from the client.
- [ ] (R2, R7) Emit observer/job events for `steer_queued`, `steer_applied`, and `steer_deferred`.
- [ ] (R2) If an active turn finishes with unapplied steer input, convert it to a deferred follow-up instead of dropping it.

## 4. Follow-up execution path

- [ ] (R3, R8) Clone the active turn's session key, tool policy, profile, capability ceiling, audit metadata, actor, and role into each deferred follow-up request.
- [ ] (R3) Start each follow-up with the existing `runTurnJob` path so normal history, prompt building, streaming, approvals, and tool safety are reused.
- [ ] (R3, R7) Include `deferred_after_job_id` and optional `client_message_id` in queued/started lifecycle event payloads.
- [ ] (R3, R4) Cap per-active-job follow-up queue length and return `429` when exceeded.
- [ ] (R3, R4) Add service tests proving two follow-ups for one active job run in FIFO order.

## 5. Auth, limits, and safeguards

- [ ] (R8) Verify `POST /internal/v1/jobs/{job_id}/input` passes through existing service auth, browser boundary, body limit, mutation rate limit, and audit paths.
- [ ] (R8) Update `cmd/or3-intern/service_auth.go` only if the generic route rules do not already enforce the desired mutation posture.
- [ ] (R4, R7) Bound pending steer state per active job and document whether the latest unapplied steer replaces the previous one.
- [ ] (R2, R8) Confirm steer input cannot apply while an approval-required turn is waiting for explicit approval replay.
- [ ] (R7, R8) Keep job input events concise and free of tokens, approval tokens, raw tool output, and full internal request context.

## 6. Backend regression tests

- [ ] (R2, R9) Add `internal/agent` tests where a fake provider returns a tool call, a steer arrives during tool execution, and the next provider request contains the steer.
- [ ] (R2, R9) Add a test that an unapplied steer becomes a follow-up when the active turn completes first.
- [ ] (R2, R9) Add a test that applied steer does not reset tool-loop quota accounting.
- [ ] (R3, R9) Add `cmd/or3-intern` service tests for follow-up job creation, stream events, and completion dispatch.
- [ ] (R5, R9) Update service contract fixtures only if the stable `/turns` or `/jobs` snapshot output intentionally changes.

## 7. App composer UX

- [ ] (R1, R6) Update `or3-app/app/components/assistant/AssistantComposer.vue` so the editor remains enabled while `streaming=true`.
- [ ] (R1, R6) Add distinct icon controls with accessible labels/tooltips for `Interrupt next step`, `Add after this finishes`, and `Stop`.
- [ ] (R1, R6) Make Enter while streaming default to the less disruptive follow-up behavior, with explicit button access for interrupt.
- [ ] (R6) Preserve attachment and workspace mention behavior where possible; if a running input cannot support an attachment, block it with a clear client-side validation message.
- [ ] (R1, R6) Keep layout stable on mobile and desktop so the three running controls do not resize the composer or overlap text.

## 8. App stream/input state

- [ ] (R1, R6) Extend `or3-app/app/types/app-state.ts` with minimal fields needed to show queued/applied running input state.
- [ ] (R1, R6) Add `sendRunningInput(mode, payload)` in `or3-app/app/composables/useAssistantStream.ts`, or extend `send` with an explicit running input mode while preserving normal-send behavior.
- [ ] (R2, R6) For `interrupt_next_step`, add a pending user message, call `/jobs/{activeJobId}/input`, and mark it applied when a matching `steer_applied` event is seen.
- [ ] (R3, R6) For `after_current`, add a queued user message and assistant placeholder linked to the returned follow-up `job_id`.
- [ ] (R3, R6) After the active job finishes, follow the returned follow-up job through the existing `followJobId` stream path.
- [ ] (R6, R7) Make failure states retryable without duplicating already displayed user messages.

## 9. App tests

- [ ] (R1, R9) Add or update composer tests proving streaming mode still allows typing and exposes all three running controls.
- [ ] (R2, R9) Add `useAssistantStream` tests for interrupt requests, `steer_queued`, and `steer_applied`.
- [ ] (R3, R9) Add `useAssistantStream` tests for after-current responses and follow-up job streaming.
- [ ] (R6, R9) Add tests for enqueue failure and retry payload behavior.
- [ ] (R7, R9) Add a recovery-oriented test that job snapshot events can restore a pending/applied running input state.

## 10. Documentation and manual verification

- [ ] (R5, R8) Update `or3-intern/docs/api-reference.md` with `POST /internal/v1/jobs/{job_id}/input`, request modes, responses, and error statuses.
- [ ] (R2, R3) Update `or3-intern/docs/agent-runtime.md` with safe-point semantics and the distinction between steer and follow-up.
- [ ] (R1, R6) Add a short note to relevant `or3-app` planning or README docs describing the running composer controls.
- [ ] (R9) Manually validate long-running provider/tool turns, interrupt steering, after-current FIFO, abort-with-pending-input, approval-required turns, and mobile composer layout.

## Out of scope

- [ ] Do not implement bidirectional WebSockets for this feature; SSE plus a small HTTP input route is enough for v1.
- [ ] Do not cancel provider calls or tool executions mid-call for `interrupt_next_step`.
- [ ] Do not allow steering external CLI jobs, subagents, terminal sessions, file jobs, or approval replay jobs in v1.
- [ ] Do not add a durable SQLite pending-input table unless restart persistence becomes a confirmed requirement.
- [ ] Do not let the app submit raw provider messages, tool results, tool-call IDs, or history patches.
