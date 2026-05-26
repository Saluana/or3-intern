# Channel And Approval Review

## ~~Chat approval routing does not exist~~

Status: Fixed. Non-CLI channel messages are intercepted before normal runtime handling for shared `/approve` and `/deny` commands. Request IDs are resolved only while pending, and approval commands are accepted only from the stored originating channel/session/sender before the approved resume path starts. The current low-friction UX is text command based; the stored requester context keeps native callbacks as a small adapter layer instead of a separate approval model.

`or3-intern/docs/api-reference.md:704-709`

Why this is bad: The docs explicitly list "Chat-channel approval routing" as a future phase, and the Telegram, Slack, and Discord adapters only publish user text as normal `user_message` events. There is no shared approval router before `eventBus.Publish`, no `/approve` or `/deny` parsing, and no platform-native buttons or callbacks wired into the broker.

Consequence: A user in Telegram, Slack, Discord, or WhatsApp cannot resolve the approval in the same channel. If they reply "approve", that text goes back through the agent as a normal prompt instead of resolving the pending approval. That is exactly the broken UX the channel layer is supposed to prevent.

Fix: Add a channel approval router that runs before normal message publication. It should recognize approval IDs, reply-to approval prompts, and platform-native interactions, validate the channel identity against the approval owner/operator policy, call `ApproveRequest` or `DenyApproval`, and then start or attach to the approved resume job. Keep the parser shared, with small per-platform adapters for buttons, callbacks, and slash commands.

## ~~Approval-required prompts can silently vanish~~

Status: Fixed. Approval-required turns now build a deterministic fallback prompt from the approval request and request ID. Model narration can still improve the wording, but an empty or failed narration no longer prevents the approval prompt from being persisted and delivered.

`or3-intern/internal/agent/runtime_execution.go:268-320`

Why this is bad: When a tool returns `ApprovalRequiredError`, channel runtime asks the model to narrate the approval requirement. If that extra model call fails or returns no choices, `narrateApprovalRequired` returns an empty string. `Runtime.turn` only persists and delivers the approval message when `finalText` is non-empty.

Consequence: The original turn can hit an approval gate, return an error, and send nothing to Telegram/Slack/Discord. The channel worker then only logs the failure. From the user's perspective, the bot just stops responding.

Fix: Approval prompts must be deterministic, not model-dependent. Build a fallback message directly from `ApprovalRequiredError` and the stored approval request, including the request ID and channel-specific action instructions. The model can improve wording, but failure to narrate must still deliver the fallback.

## ~~Non-CLI channel errors are log-only~~

Status: Fixed. Non-CLI runtime failures now deliver a short channel-safe failure message with a public error code. Approval-required errors are handled by the deterministic approval prompt path so users do not receive duplicate or internal error text.

`or3-intern/cmd/or3-intern/main.go:1151-1158`

Why this is bad: Channel workers deliver CLI errors to the CLI, but for every non-CLI channel they only call `log.Printf`. This includes approval errors with empty narration, timeouts, provider failures, and tool failures that do not produce a final assistant message.

Consequence: Any error path outside the happy path becomes a dead-air failure in Telegram, Slack, Discord, WhatsApp, and email. The operator has to inspect logs or the web app to learn what happened.

Fix: Add a channel-safe error delivery path. For `ApprovalRequiredError`, send the deterministic approval prompt. For public runtime errors, send a short failure message with a retry or web-app pointer. Use public error codes and avoid leaking tool internals.

## ~~Web approval resume cannot deliver back to channels~~

Status: Fixed. Approval resume delivery is now backend-owned. The resume job reads the original requester context from the approval record and delivers final completion or safe failure text back through the channel manager when the approval originated from an external channel.

`or3-intern/cmd/or3-intern/main.go:576-579`

Why this is bad: Service mode creates a job registry and then explicitly sets `rt.Deliver = nil` and `rt.Streamer = nil`. The approval endpoint starts a resume job, but the resume path only publishes job lifecycle events. It does not call the channel manager, and the runtime has no deliverer anyway.

Consequence: Approving in the web app can continue the blocked work and update the web app job stream, but it cannot send the final answer back to the originating Telegram/Slack/Discord conversation. The user approves in the browser and the channel stays silent.

Fix: Make approval resume delivery a backend responsibility. Persist the origin channel and delivery metadata with the approval request, inject a channel deliverer into the resume path, and deliver the final completion to the original channel after the job completes. The web app should observe the same job, not become the delivery mechanism.

## ~~Resume jobs run as `service`, not the original channel~~

Status: Fixed. Approved tool replays reconstruct the original channel envelope from stored requester context, including channel, session key, sender, reply target, and safe thread/reply metadata.

`or3-intern/internal/app/service_app.go:272-282`

Why this is bad: Approved tool replays resume by calling `runtime.Handle` with `Channel: "service"` and metadata that only says `approved_tool_replay`. The original channel, target chat/channel ID, thread timestamp, Telegram reply ID, and Discord message reference are not restored.

Consequence: Even if `rt.Deliver` were enabled, this resume turn would try to deliver as the `service` channel, not back to Telegram, Slack, or Discord. It also loses thread/reply context, which makes Slack and Discord conversations drift out of place.

Fix: Resume approved requests with the original channel envelope. Store and replay `channel`, `replyTarget`, and `replyMeta` from the blocked turn. If a resume is triggered by the web app, it should still run with the channel context captured when the approval was created.

## ~~Approval records do not store delivery metadata~~

Status: Fixed. Approval records now store `requester_context_json` with normalized channel, session, sender, reply target, safe reply metadata, and source message information. Existing databases receive an additive migration, and the API exposes the context for app routing.

`or3-intern/internal/db/db.go:536-552`

Why this is bad: `approval_requests` stores `requester_session_id`, but not the channel, reply target, reply metadata, message ID, thread timestamp, or originating user. The channel adapters create this metadata on inbound messages, then the approval system throws it away when creating the approval request.

Consequence: The system cannot reliably resume into the original Telegram message, Slack thread, Discord reply, or isolated group-channel peer. Parsing `telegram:<chat-id>` from `requester_session_id` is not enough for threaded channels or reply semantics.

Fix: Add an approval context column, for example `requester_context_json`, with `channel`, `session_key`, `from`, `reply_target`, `reply_meta`, and `source_message_id`. Populate it from runtime turn context when `requireApproval` creates the request, expose it through the API, and use it for both channel approval prompts and resume delivery.

## ~~The approval slide-over creates fake web conversations for external channels~~

Status: Fixed. The app now treats Telegram, Slack, Discord, WhatsApp, and email approvals as external-channel surfaces. Approving from the slide-over observes the backend resume job and shows a confirmation instead of activating or creating a local web chat session.

`or3-app/app/components/approvals/ApprovalsPanel.vue:402-429`

Why this is bad: The approval panel resolves any non-doctor approval to chat, calls `activateSessionByKey(targetSessionKey, "Approval follow-up")`, creates an approval message if needed, and then calls `send(buildApprovedResumePayload(...))`. For a `telegram:*`, `slack:*`, or `discord:*` requester session, `activateSessionByKey` creates a local web chat session if one does not exist.

Consequence: Approving an external channel request in the slide-over starts or focuses a new web-app conversation instead of continuing the conversation where the request originated. That matches the reported "approval from slide-over starts a new conversation" failure.

Fix: Treat external channel session keys as external surfaces, not web chat sessions. `resolveApprovalResumeTarget` should classify `telegram:`, `slack:`, `discord:`, `whatsapp:`, and `email:` separately. The panel should approve and observe the backend resume job, while the backend sends the final result to the source channel.

## ~~`resolveApprovalResumeTarget` collapses every non-doctor session into web chat~~

Status: Fixed. The resolver now has an explicit `external_channel` surface and can infer it from either requester context or external channel session keys.

`or3-app/app/utils/or3/approval-resume-target.ts:31-43`

Why this is bad: The target resolver has only two surfaces: `doctor_health` and `chat`. That means all external channels are treated as if they belong in the local web chat route.

Consequence: Every new channel integration will inherit the same broken behavior unless the UI code adds ad hoc exceptions elsewhere. The surface model is too small for the actual product.

Fix: Add explicit surfaces for external channels, or a generic `external_channel` surface with a parsed `channel` field. UI code should render an approval status and avoid creating a chat session for external surfaces.

## ~~Pending approval hydration only works for the active web session~~

Status: Fixed. Chat hydration now stays scoped to real local chat sessions and skips external channel session keys. External approvals remain visible through the approvals surface without polluting local chat state.

`or3-app/app/composables/assistant-stream/useApprovalHydration.ts:43-68`

Why this is bad: The web app only hydrates pending approvals whose `requester_session_id` matches the active local chat session. External channel sessions do not become visible in the message stream unless the app first creates or activates a fake session for them.

Consequence: The app cannot cleanly represent "Telegram needs approval" without polluting local chat state. It also makes pending approval visibility depend on whichever web chat happens to be active.

Fix: Keep approval discovery separate from chat hydration. The approvals panel should list pending approvals by host and source surface. Chat hydration should only attach approvals to real local chat sessions.

## ~~Slack Socket Mode can panic on malformed envelopes~~

Status: Fixed. Slack authorization metadata is guarded before indexing, and malformed envelopes are covered by a regression test.

`or3-intern/internal/channels/slack/slack.go:144-145`

Why this is bad: The code indexes `envelope.Payload.Authorizations[0]` without checking the slice length. Slack envelopes can vary, and tests/mocks can absolutely send an events payload with no authorizations.

Consequence: One malformed or unexpected Slack event can panic the read loop and knock out Slack inbound handling until the process restarts or reconnect logic recovers.

Fix: Guard the slice before indexing:

```go
if len(envelope.Payload.Authorizations) > 0 && envelope.Payload.Authorizations[0].UserID != "" && c.botID == "" {
    c.botID = envelope.Payload.Authorizations[0].UserID
}
```

## ~~Channel event publication ignores drops~~

Status: Fixed. The primary worker queue now applies backpressure once active, and channel adapters log when publication cannot reach an active worker. Optional fan-out subscribers may still drop independently without losing the primary turn queue.

`or3-intern/internal/bus/bus.go:96-119`

Why this is bad: `Publish` is non-blocking and can drop events when subscriber buffers are full. The channel adapters call `eventBus.Publish(...)` and ignore the boolean result.

Consequence: Under load, inbound Telegram/Slack/Discord messages can be silently dropped. Approval replies would be especially bad here because the user thinks they approved, but the broker never sees the event.

Fix: Critical inbound channel events need a reliable path. Either use a blocking worker queue for runtime turns, retry or return an explicit channel error when publish fails, or make channel ingress persist events before dispatch.

## ~~Telegram polling is needlessly chatty and adds latency~~

Status: Fixed. Telegram polling now uses non-zero long polling with bounded timeout, keeping the retry delay only for error recovery.

`or3-intern/internal/channels/telegram/telegram.go:148-180`

Why this is bad: Telegram polling uses a local ticker plus `getUpdates` with `timeout=0`. That creates frequent short polls, burns requests, and adds up to `PollSeconds` latency for every inbound message.

Consequence: Approval prompts and approval replies feel slower than necessary, and the bot wastes API calls while idle.

Fix: Use Telegram long polling with a non-zero `timeout`, for example 25 to 50 seconds, and keep the local retry delay only for errors. If webhooks are supported later, prefer webhook delivery for lower latency and fewer requests.

## ~~Channel turns have a hard 120-second synchronous timeout~~

Status: Fixed for external channel UX. Telegram, Slack, Discord, WhatsApp, and email turns no longer use the 120-second cap; they get a longer channel budget, deterministic approval pauses, visible terminal failures, and approved resumes run through backend jobs that deliver back to the source channel. This avoids adding a new user-facing workflow while removing the dead-air failure mode.

`or3-intern/cmd/or3-intern/main.go:1139-1161`

Why this is bad: Every bus event is handled inside a worker goroutine with `agent.WithTimeout(ctx, 120)`. Long-running channel tasks have no durable job, no progress heartbeat beyond typing indicators, and no guaranteed completion delivery after the context expires.

Consequence: Real tasks like email triage can run into timeouts or approval waits, then disappear from the channel with only a log line. This makes channel UX fragile for exactly the kind of tasks users send from mobile.

Fix: Treat channel turns like service jobs: create durable jobs, stream progress where possible, persist enough state to resume after approval, and always deliver terminal state back to the source channel.

## ~~The tests prove the web job, not the channel behavior~~

Status: Fixed. Added regression coverage for stored requester context, deterministic approval prompts, in-channel approve/mismatch handling, web-approved resume delivery preserving Slack thread metadata, Slack malformed envelopes, and app external-channel routing away from web chat.

`or3-intern/cmd/or3-intern/service_test.go:3779-3886`

Why this is bad: The approval resume test verifies that approving creates a resume job and that the job contains an assistant event. It does not verify that a Telegram, Slack, or Discord origin receives the approval prompt, can approve in-channel, or receives the final completion after a web approval.

Consequence: The exact broken production behavior can pass the current test suite. The system can look green while the primary channel UX is unusable.

Fix: Add end-to-end tests for each required path:

- Telegram message triggers an approval prompt with an actionable approval ID.
- Telegram `/approve <id>` resolves the request and sends the final completion back to the same chat.
- Web approval of a Telegram-origin request sends the final completion back to Telegram and does not create a web chat session.
- Slack approvals preserve `thread_ts`.
- Discord approvals preserve `message_reference`.
- The app slide-over does not call `activateSessionByKey` for external channel sessions.
