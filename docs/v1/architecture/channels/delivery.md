# Response Delivery

Source: `internal/channels/channels.go`, per-channel `Deliver()` methods, `internal/channels/cli/deliver.go`

This document describes how agent responses are delivered back to users through channels.

## Delivery Flow

1. The agent runtime composes a response text.
2. It calls `channelManager.DeliverWithMeta(ctx, channelName, recipientID, text, meta)`.
3. The Manager looks up the channel by name and calls its `Deliver()` method.
4. The channel sends the text (and any media) to the platform.

## The Manager Delivery Methods

`internal/channels/channels.go:190-204`

```go
func (m *Manager) Deliver(ctx context.Context, channel, to, text string) error
func (m *Manager) DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error
```

- `Deliver()` is a convenience that calls `DeliverWithMeta()` with `nil` meta.
- If `channel` is empty, it defaults to `"cli"`.
- Looks up the channel by normalized (lowercased, trimmed) name.
- Delegates to `ch.Deliver(ctx, to, text, meta)`.

## Metadata Propagation

When the agent replies, it passes metadata through to the channel's `Deliver()`. This metadata enables threaded replies:

### Thread Reply Fields

The `ReplyMeta()` function in `channels.go:230` extracts only reply-related fields from a meta map:

| Meta key | Used by | Delivery behavior |
|---|---|---|
| `thread_ts` | Slack | Added to `chat.postMessage` payload as `thread_ts` |
| `reply_to_message_id` | Telegram, WhatsApp | Added as `reply_to_message_id` in the send payload |
| `message_reference` | Discord | Added as `message_reference: {message_id: "..."}` |

When a user sends a message, the channel captures the message ID (or thread timestamp) in the event meta. When the agent replies to that event, the runtime includes these fields in the delivery meta, enabling the channel to send a properly threaded reply.

### Media Paths

The `media_paths` key in meta contains paths to local files that should be sent as attachments. `MediaPaths()` (`media.go:23`) extracts and normalizes these paths from `[]string` or `[]any` values.

## Per-Channel Delivery Behavior

### Telegram

`internal/channels/telegram/telegram.go:81`

- Target: `to` parameter, or `DefaultChatID`.
- Without media: sends `POST /sendMessage` with `chat_id`, `text`, and optional `reply_to_message_id`.
- With media: sends each file via the appropriate endpoint (sendPhoto/sendVoice/sendAudio/sendDocument), with caption text on the first file only.

### Slack

`internal/channels/slack/slack.go:90`

- Target: `to` parameter, or `DefaultChannelID`.
- Without media: sends `POST /chat.postMessage` with `channel`, `text`, and optional `thread_ts`.
- With media: initiates `files.getUploadURLExternal`, uploads each file, then calls `files.completeUploadExternal` with `channel_id`, `files`, `initial_comment`, and optional `thread_ts`.

### Discord

`internal/channels/discord/discord.go:100`

- Target: `to` parameter, or `DefaultChannelID`.
- Without media: sends `POST /channels/{id}/messages` with `content` and optional `message_reference`.
- With media: sends multipart form with `payload_json` and `files[0..n]` parts.

### WhatsApp

`internal/channels/whatsapp/whatsapp.go:83`

- Target: `to` parameter, or `DefaultTo`.
- Sends a JSON `send` command over the bridge WebSocket: `{"type":"send","to":"...","text":"..."}`.
- With media: base64-encodes files in an `attachments` array.
- Extra meta keys are forwarded in the command payload.

### Email

`internal/channels/email/email.go:135`

- Target: normalized recipient email address.
- Looks up thread state (in-memory + database fallback) for the sender.
- Prefixes the subject with `"Re:"` (configurable via `SubjectPrefix`).
- Builds a full RFC 5322 email with `In-Reply-To` and `References` headers.
- Sends via SMTP.

### CLI

`internal/channels/cli/deliver.go:29`

- Target: ignored (printed to stdout).
- Rejects media attachments (returns an error).
- Full response: stops spinner, prints formatted response with header, separator, and prompt.
- If the BubbleTea TUI is active, sends a `chatAssistantCloseMsg` to the bridge instead.

## Streaming Delivery (CLI Only)

The CLI channel is the only channel that supports streaming output through the `StreamingChannel` interface (`internal/channels/stream.go`).

### BeginStream

`internal/channels/cli/deliver.go:218`

```go
func (d Deliverer) BeginStream(ctx context.Context, to string, meta map[string]any) (channels.StreamWriter, error)
```

Returns a `CLIStreamWriter` that handles incremental output.

### CLIStreamWriter States

`internal/channels/cli/deliver.go:120`

| Method | Behavior |
|---|---|
| `WriteDelta(ctx, text)` | On first call: stops spinner, prints response header. Subsequent calls: prints the delta text (with newline indentation for multi-line content). In TUI mode: emits `chatAssistantDeltaMsg` to the bridge. |
| `Close(ctx, finalText)` | If deltas were streamed: prints trailing spacing and prompt. If nothing was streamed: prints the full `finalText` as a formatted response. In TUI mode: emits `chatAssistantCloseMsg`. |
| `Abort(ctx)` | If deltas were streamed: prints `[aborted]` marker and prompt. If nothing was streamed: leaves spinner alone (for tool-call continuation). In TUI mode: emits `chatAssistantAbortMsg`. |

After close or abort, further writes are silently ignored.

### Tool Call Streaming

When the agent makes a tool call during streaming:

1. The stream is aborted — `Abort()` is called.
2. The tool runs while the spinner continues (or the TUI shows "Using tools").
3. When the tool completes, a new stream begins with `BeginStream()`, and deltas continue flowing.

## Error and Notice Delivery (CLI)

`internal/channels/cli/deliver.go:63-115`

The CLI Deliverer has special methods for errors and background notices:

- **`ShowError(err)`** — stops spinner, prints `"Error: <message>"` formatted response, separator, prompt.
- **`ShowNotice(text)`** — same format but with `"Notice: "` prefix.
- **`ShowErrorForSession(sessionKey, err)`** and **`ShowNoticeForSession(sessionKey, text)`** — variants that route through the BubbleTea bridge when the TUI is active.
- These are called by the agent runtime outside of normal response delivery, e.g. for background consolidation failures or session reset notices.

## Non-CLI Channels and Non-Text Delivery

External channels handle responses only through their `Deliver()` method. If the agent needs to send an error or notice to an external channel user, it calls `Deliver()` with the error/notice text. There is no separate notification path for external channels.
