# Shared Channel Model

Source: `internal/channels/shared/shared.go` and `internal/channels/channels.go`

All external channels share a common set of patterns, interfaces, and utilities.

## The Channel Interface

Every channel implements the `Channel` interface from `internal/channels/channels.go:74`:

```go
type Channel interface {
    Name() string
    Start(ctx context.Context, eventBus *bus.Bus) error
    Stop(ctx context.Context) error
    Deliver(ctx context.Context, to, text string, meta map[string]any) error
}
```

## Connection Lifecycle Pattern

All external channels follow the same Start/Stop pattern:

1. **Start** validates configuration (token, URL, etc.), creates a `context.WithCancel` child context, stores the cancel function, and launches a read loop goroutine.
2. **Stop** calls the stored cancel function, closes the network connection (WebSocket or HTTP polling), and clears state.
3. The read loop goroutine exits when the context is cancelled or the connection closes.

All channels use a mutex-protected state pattern:

```go
type Channel struct {
    mu     sync.Mutex
    conn   *websocket.Conn   // or running bool
    cancel context.CancelFunc
}
```

## Inbound Deduplication

All external channels use `IngressDeduplicator` (`internal/channels/channels.go:21`):

- Lazily created on first use (nil check in a getter method).
- Uses a map with TTL-based eviction (default 5 minutes).
- Each channel generates a dedupe key from platform-specific identifiers:
  - **Telegram**: `chat_id:message_id`
  - **Slack**: `envelope_id`, or `channel|user|thread_ts|text`
  - **Discord**: `message_id`, or `channel_id|author_id|content`
  - **WhatsApp**: `message_id`, or `chat|from|text`
  - **Email**: Uses its own ring-buffer + DB-persisted deduplication (not `IngressDeduplicator`)

## Inbound Access Control

All channels use `shared.AllowInboundIdentity()` (`shared.go:28`) to determine whether an inbound message should be processed.

### Access Policies

| Policy | Config constant | Behavior |
|---|---|---|
| Deny | `InboundPolicyDeny` | All messages rejected |
| Pairing | `InboundPolicyPairing` | Only paired identities allowed via approval broker |
| Allowlist | `InboundPolicyAllowlist` | Only identities in the allowlist |
| Default (fallback) | (none) | If `OpenAccess` is true, all are allowed. If a non-empty allowlist exists, only allowlisted identities. Otherwise, no messages are allowed. |

### The Pairing Broker

The `shared.PairingBroker` interface (`shared.go:13`) is implemented by `approval.Broker`:

```go
type PairingBroker interface {
    IsPairedChannelIdentity(ctx context.Context, channel, identity string) (bool, error)
}
```

When the policy is `pairing`, the channel queries the broker to check if a channel+identity pair has been pre-approved.

### Identity Normalization

The `Normalize` function in `InboundAccessInput` defaults to `NormalizeIdentity()` which calls `strings.TrimSpace`. Email overrides this with `normalizeAddress()` which parses RFC 5322 addresses and lowercases them.

### OpenAccess Overrides Allowlist

By default, if both `OpenAccess` is true and a non-empty allowlist exists, the allowlist takes priority. The email channel sets `OpenAccessOverridesAllowlist` to `true`, which flips this behavior.

## Session Key Format

Every channel creates a session key that uniquely identifies a conversation:

| Channel | Default session key | With IsolatePeers |
|---|---|---|
| Telegram private | `telegram:<chat_id>` | N/A (already private) |
| Telegram group | `telegram:<chat_id>` | `telegram:<chat_id>:<user_id>` |
| Slack | `slack:<channel_id>` | `slack:<channel_id>:<user_id>` |
| Discord | `discord:<channel_id>` | `discord:<channel_id>:<user_id>` |
| WhatsApp | `whatsapp:<chat_id>` | `whatsapp:<chat_id>:<from>` |
| Email | `email:<sender_email>` | N/A (always per-sender) |
| CLI | `default` | (user-settable) |

## Event Publication Format

All channels publish `bus.Event` with these fields:

```go
bus.Event{
    Type:       bus.EventUserMessage,
    SessionKey: sessionKey,
    Channel:    channelName,
    From:       senderIdentity,
    Message:    composedText,
    Meta:       metadataMap,
}
```

## Metadata Convention

Events carry a `Meta` map with channel-specific metadata. Common meta keys defined as constants in `internal/channels/channels.go:62-70`:

| Constant | Key | Used by | Purpose |
|---|---|---|---|
| `MetaMediaPaths` | `media_paths` | All channels | Local file paths for outbound media |
| `MetaThreadTS` | `thread_ts` | Slack | Thread timestamp |
| `MetaReplyToMessageID` | `reply_to_message_id` | Telegram, WhatsApp | Reply target message ID |
| `MetaMessageReference` | `message_reference` | Discord | Reply message reference |

Helper functions on meta:

- **`CloneMeta(meta)`** — shallow copy of a meta map.
- **`ReplyMeta(meta)`** — extracts only thread-related keys (thread_ts, reply_to_message_id, message_reference), skipping empty/zero values.
- **`MediaPaths(meta)`** — extracts and normalizes the `media_paths` value into a `[]string`.
- **`ComposeMessageText(text, markers)`** — joins the message text with media marker strings, separated by newlines.

## Media Handling Pattern

Inbound media across channels follows the same flow:

1. Extract attachment references from the platform-specific message struct.
2. For each attachment: normalize filename, detect artifact kind.
3. Check `MaxMediaBytes` — if 0: "disabled by config"; if exceeded: "too large".
4. Download the file data (via platform API or inline base64).
5. Save to `artifacts.Store.SaveNamed()`.
6. On success: append to attachments list + add a success marker (e.g. `[image: photo.jpg]`).
7. On failure: add a failure marker (e.g. `[image: photo.jpg - download failed]`).
8. Compose the message text with markers using `channels.ComposeMessageText()`.

## HTTP Client Defaults

`shared.DefaultHTTPClient()` (`shared.go:94`) returns `&http.Client{Timeout: 20 * time.Second}`. All channels use this as the fallback when no custom HTTP client is provided.

## Error Formatting

`shared.APIStatusError()` (`shared.go:98`) creates a formatted error from a channel name, HTTP status, and response body. Used by Discord for error responses.
