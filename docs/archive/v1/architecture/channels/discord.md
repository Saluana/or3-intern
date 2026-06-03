# Discord Channel

Source: `internal/channels/discord/discord.go`

The Discord channel connects to the [Discord Gateway](https://discord.com/developers/docs/events/gateway) via WebSocket and sends messages through the HTTP API.

## Configuration

The channel uses `config.DiscordChannelConfig`:

| Field | Purpose |
|---|---|
| `Token` | Bot token |
| `GatewayURL` | Gateway WebSocket URL (auto-discovered if empty) |
| `APIBase` | HTTP API base (default `https://discord.com/api/v10`) |
| `DefaultChannelID` | Fallback target for delivery |
| `RequireMention` | Only respond when the bot is mentioned |
| `InboundPolicy` | Access control (deny/pairing/allowlist/open) |
| `OpenAccess` | Allow all inbound messages |
| `AllowedUserIDs` | Allowlist of user IDs |
| `IsolatePeers` | Give each user a separate session |

Requirements (`discord.go:49`): Bot token must be set.

## Connection Lifecycle

### Start (`discord.go:49`)

1. Resolves the gateway URL: uses `GatewayURL` from config if set, otherwise calls `GET /gateway/bot` to discover it.
2. Dials the WebSocket using `gorilla/websocket`.
3. Launches `readLoop()` in a background goroutine.

### Stop (`discord.go:84`)

Cancels the context and closes the WebSocket connection.

## Gateway Protocol

### Frame Format (`discord.go:454`)

```go
type gatewayFrame struct {
    Op int             // opcode: 10 (Hello), 0 (Dispatch), etc.
    T  string          // event type: "READY", "MESSAGE_CREATE"
    D  json.RawMessage // event data
}
```

### Opcode Handling

- **Op 10 (Hello)** — received after connection. Parses `heartbeat_interval`, sends an **Identify** payload (op 2) with the bot token, intents `513` (GUILDS + GUILD_MESSAGES + DIRECT_MESSAGES), and OS info. Starts a heartbeat ticker that sends op 1 (Heartbeat) at the specified interval.
- **Op 0 (Dispatch)** — normal events:
  - `"READY"` — captures the bot's user ID for mention detection.
  - `"MESSAGE_CREATE"` — processes user messages.

### Message Processing

For `MESSAGE_CREATE` events:

1. Skips messages from bots (`msg.Author.Bot == true`).
2. Deduplicates using the message ID (`msg.ID`), or falls back to `channel|author|content` composite key.
3. Checks inbound access via `shared.AllowInboundIdentity()`.
4. If `RequireMention` is true, skips messages that don't mention the bot (checked via the `mentions` array).
5. Strips the bot mention (`<@id>` and `<@!id>`) from the message text.
6. Processes file attachments.
7. Composes the message with media markers.

### Session Keys

- Default: `discord:<channel_id>`
- With `IsolatePeers`: `discord:<channel_id>:<user_id>`

Metadata includes `channel_id`, `message_reference` (for replies), `guild_id`, and `is_private` flag.

## Inbound: Attachment Downloads

`captureAttachments()` (`discord.go:304`) processes message attachments:

1. For each attachment reference, normalizes the filename and detects kind.
2. Checks `MaxMediaBytes` limits.
3. Downloads via `downloadAttachment()` — a simple HTTP GET to the attachment URL.
4. Saves to the `artifacts.Store`.

## Outbound: Sending Messages

### Text Messages (`discord.go:100`)

`Deliver()` posts a message via `POST /channels/{channel_id}/messages`:
- Uses `to` or falls back to `DefaultChannelID`.
- Adds `message_reference` from meta for reply threading (Discord uses `{"message_id": "..."}` structure).

### Media Messages (`discord.go:365`)

`postMultipart()` sends messages with file attachments:

1. Builds a `payload_json` form field containing text content and optional message reference.
2. Attaches each file as `files[0]`, `files[1]`, etc. in the multipart form.
3. Posts to `POST /channels/{channel_id}/messages` with `Authorization: Bot <token>` and `Content-Type: multipart/form-data`.

### Rate Limiting (`discord.go:292`)

When Discord returns HTTP 429, `discordRateLimitError()` parses the JSON response for `retry_after` (float seconds) and `message`, formatting via `channels.FormatRateLimitError("discord", duration, message)`.

## API Communication

- **`getJSON()`** — HTTP GET with `Authorization: Bot <token>`. Handles 429 rate limits.
- **`postJSON()`** — HTTP POST with JSON body, same auth. Reads error response body for debugging on non-429 errors.
- All HTTP uses `shared.DefaultHTTPClient()` (20s timeout) by default.
- Default API base: `https://discord.com/api/v10`
