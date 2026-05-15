# Slack Channel

Source: `internal/channels/slack/slack.go`

The Slack channel connects to Slack using [Socket Mode](https://api.slack.com/apis/socket-mode) — a WebSocket connection that receives events directly without requiring a public HTTP endpoint.

## Configuration

The channel uses `config.SlackChannelConfig`:

| Field | Purpose |
|---|---|
| `AppToken` | Slack app-level token (starts with `xapp-`) |
| `BotToken` | Slack bot token (starts with `xoxb-`) |
| `APIBase` | API endpoint (default `https://slack.com/api`) |
| `DefaultChannelID` | Fallback target channel for delivery |
| `RequireMention` | Only respond when the bot is `@mentioned` |
| `InboundPolicy` | Access control (deny/pairing/allowlist/open) |
| `OpenAccess` | Allow all inbound messages |
| `AllowedUserIDs` | Allowlist of user IDs |
| `IsolatePeers` | Give each user their own session |

Requirements (`slack.go:48`): Both `AppToken` and `BotToken` must be set.

## Connection Lifecycle

### Start (`slack.go:48`)

1. Calls `apps.connections.open` to get a WebSocket URL (authenticated with the App Token).
2. Dials the WebSocket using `gorilla/websocket`.
3. Launches `readLoop()` in a background goroutine.

### Stop (`slack.go:74`)

Cancels the context and closes the WebSocket connection.

## WebSocket Protocol

### Socket Envelope Format (`slack.go:433`)

```go
type socketEnvelope struct {
    EnvelopeID string
    Type       string    // "hello", "events_api", etc.
    Payload    struct {
        Event struct {
            Type        string      // "message"
            Text        string
            User        string
            BotID       string
            Channel     string
            ChannelType string
            ThreadTS    string
            Files       []slackFile
        }
        Authorizations []struct{ UserID string }
    }
}
```

### Acknowledgment

Every envelope with a non-empty `EnvelopeID` is immediately acknowledged by sending `{"envelope_id": "<id>"}` back on the WebSocket. This tells Slack the event was received.

### Event Filtering

The `readLoop()` in `slack.go:109` filters events:

1. Skips `"hello"` type envelopes.
2. Only processes `"events_api"` envelopes with event type `"message"`.
3. Deduplicates using envelope ID or composite key (`channel|user|thread_ts|text`).
4. Skips events where `BotID` is set (bot's own messages) or `User` is empty.
5. Checks inbound access via `shared.AllowInboundIdentity()`.
6. If `RequireMention` is true and bot ID is known, skips messages that don't contain `<@bot_id>` — unless the message has file attachments (shared files bypass the mention requirement).
7. Strips the bot mention from the message text.

### Session Keys

- Default: `slack:<channel_id>`
- With `IsolatePeers`: `slack:<channel_id>:<user_id>`

## Inbound: File Downloads

`captureFiles()` (`slack.go:279`) processes attached files:

1. Normalizes the filename and detects the artifact kind.
2. Checks `MaxMediaBytes` limits.
3. Downloads the file via `downloadPrivateFile()` using the private download URL, authenticated with the Bot Token.
4. Saves to the `artifacts.Store`.

## Outbound: Sending Messages

### Text Messages (`slack.go:90`)

`Deliver()` posts a message via `chat.postMessage`:
- Uses `to` parameter or falls back to `DefaultChannelID`.
- Adds `thread_ts` from meta for threaded replies.
- Sends as JSON with `Authorization: Bearer <BotToken>`.

### Media Uploads (`slack.go:341`)

Media delivery uses Slack's external upload flow:

1. `uploadFile()` calls `files.getUploadURLExternal` with filename and size to get an upload URL and file ID.
2. Uploads the raw file content to the returned URL with `Content-Type: application/octet-stream`.
3. Returns the file ID.
4. `uploadFiles()` collects all file IDs and calls `files.completeUploadExternal` with the channel ID, file IDs, and optional initial comment text.

### Rate Limiting (`slack.go:221`)

When Slack returns HTTP 429, the channel reads the `Retry-After` header (seconds) and formats an error via `channels.FormatRateLimitError("slack", retryAfter, "")`.

## API Communication

- **`postJSON()`** — HTTP POST with JSON body, Bearer token auth. Returns parsed JSON or rate-limit error.
- **`postForm()`** — HTTP POST with URL-encoded form body. Used for `files.getUploadURLExternal`.
- **`downloadPrivateFile()`** — HTTP GET to the private file URL, authenticated with the Bot Token. Reads up to `MaxMediaBytes+1`.
- Default API base: `https://slack.com/api`
- Default HTTP client timeout: 20 seconds.
