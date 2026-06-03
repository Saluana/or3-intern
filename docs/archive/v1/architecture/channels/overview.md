# Channel System Overview

## What Channels Are

Channels are messaging integrations that let users talk to the or3-intern agent through different platforms. Each channel:

- Receives messages from a platform (Slack, Telegram, Discord, etc.)
- Publishes those messages as events on the shared **event bus** (`internal/bus`)
- Accepts response text from the agent runtime and sends it back to the user

A channel is any struct that implements the `Channel` interface defined in `internal/channels/channels.go:74`:

```go
type Channel interface {
    Name() string
    Start(ctx context.Context, eventBus *bus.Bus) error
    Stop(ctx context.Context) error
    Deliver(ctx context.Context, to, text string, meta map[string]any) error
}
```

- **Name()** — returns a lowercase name like `"telegram"` or `"slack"`.
- **Start()** — opens the connection to the platform and begins receiving messages. Publishes incoming messages as `bus.EventUserMessage` events on the event bus.
- **Stop()** — closes the connection and cleans up.
- **Deliver()** — sends a text response (and optionally media) back to a recipient on that platform.

## The Channel Manager

The `Manager` in `internal/channels/channels.go:82` owns all registered channels:

| Method | Purpose |
|---|---|
| `Register(ch Channel)` | Adds a channel under its normalized name |
| `StartAll(ctx, eventBus)` | Starts every registered channel |
| `Start(ctx, name, eventBus)` | Starts one channel by name (idempotent) |
| `StopAll(ctx)` | Stops all channels |
| `Stop(ctx, name)` | Stops one channel by name |
| `Deliver(ctx, channel, to, text)` | Sends text to a recipient on that channel |
| `DeliverWithMeta(ctx, channel, to, text, meta)` | Sends text with platform-specific metadata |

When no channel name is specified, delivery defaults to `"cli"` (the local terminal).

## How Channels Connect to the Agent Runtime

1. **Startup** — In `cmd/or3-intern/main.go`, the CLI channel is always started. External channels (Telegram, Slack, Discord, WhatsApp, Email) are registered only if enabled in configuration.
2. **Inbound** — When a user sends a message on any platform, the channel publishes a `bus.Event` with `Type: bus.EventUserMessage` onto the event bus. The event carries the `SessionKey`, `Channel` name, `From` identity, `Message` text, and optional `Meta` map.
3. **Processing** — The agent runtime reads from the bus, processes the message through the AI provider, and calls `channelManager.DeliverWithMeta()` to send the response back.
4. **Outbound** — The Manager looks up the channel by name and calls its `Deliver()` method with the recipient ID, response text, and metadata (like thread info).

Event channel registration is in `cmd/or3-intern/main.go:1014-1050`:

```go
func buildChannelManager(cfg config.Config, ...) (*rootchannels.Manager, error) {
    mgr := rootchannels.NewManager()
    mgr.Register(cli.Service{Deliverer: cliDeliverer})
    if cfg.Channels.Telegram.Enabled { mgr.Register(&telegram.Channel{...}) }
    if cfg.Channels.Slack.Enabled    { mgr.Register(&slack.Channel{...}) }
    if cfg.Channels.Discord.Enabled   { mgr.Register(&discord.Channel{...}) }
    if cfg.Channels.WhatsApp.Enabled { mgr.Register(&whatsapp.Channel{...}) }
    if cfg.Channels.Email.Enabled    { mgr.Register(&email.Channel{...}) }
    return mgr, nil
}
```

## Inbound Deduplication

Each channel uses an `IngressDeduplicator` (`internal/channels/channels.go:21`) to avoid processing the same message twice. It maintains an in-memory map of seen message IDs with a configurable TTL (default 5 minutes). Channels generate a deduplication key from message IDs, envelope IDs, or composite fields.

The email channel has an additional persistence-based deduplication layer using the database.

## Inbound Access Control

All external channels use the shared `AllowInboundIdentity()` function in `internal/channels/shared/shared.go:28`. It supports four policies:

| Policy | Behavior |
|---|---|
| `deny` | All inbound messages are blocked |
| `pairing` | Only identities paired via the approval broker are allowed |
| `allowlist` | Only identities in the configured allowlist are allowed |
| Default (open) | `OpenAccess=true` allows everyone; otherwise falls back to allowlist |

## Metadata Convention

Events carry a `Meta` map (`map[string]any`) with channel-specific fields. Common keys defined in `internal/channels/channels.go:62-70`:

| Key | Purpose |
|---|---|
| `media_paths` | Local file paths for media attachments |
| `thread_ts` | Slack thread timestamp |
| `reply_to_message_id` | Telegram/Discord reply target message ID |
| `message_reference` | Discord message reference for replies |

Helper functions:
- `CloneMeta()` — shallow-copies a meta map.
- `ReplyMeta()` — extracts only reply-threading fields from meta.
- `MediaPaths()` — extracts and normalizes media file paths from meta.
- `ComposeMessageText()` — joins user text with media marker strings.

## Streaming Support

Channels can optionally implement the `StreamingChannel` interface (`internal/channels/stream.go:17`):

```go
type StreamingChannel interface {
    BeginStream(ctx context.Context, to string, meta map[string]any) (StreamWriter, error)
}
```

A `StreamWriter` (`internal/channels/stream.go:8`) receives incremental text deltas via `WriteDelta()`, then finalizes with `Close()` or cancels with `Abort()`. Currently only the CLI channel supports streaming output.
