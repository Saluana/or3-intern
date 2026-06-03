# Telegram Channel

Source: `internal/channels/telegram/telegram.go`

The Telegram channel polls the [Telegram Bot API](https://core.telegram.org/bots/api) for updates and sends responses via HTTP.

## Configuration

The channel uses `config.TelegramChannelConfig` which includes:

| Field | Purpose |
|---|---|
| `Token` | Bot token from @BotFather |
| `APIBase` | API endpoint (default `https://api.telegram.org`) |
| `PollSeconds` | Poll interval in seconds (default 2) |
| `DefaultChatID` | Fallback target when no chat ID provided on delivery |
| `InboundPolicy` | Access control policy (deny/pairing/allowlist/open) |
| `OpenAccess` | Allow all inbound messages |
| `AllowedChatIDs` | Allowlist of chat IDs |
| `IsolatePeers` | If enabled, gives each group participant their own session |

Requirements (`telegram.go:48`): token must be set, event bus must be provided.

## Connection Lifecycle

### Start (`telegram.go:48`)

1. Validates the token and event bus.
2. Creates a child context with cancellation.
3. Launches `poll()` in a background goroutine.

### Poll Loop (`telegram.go:100`)

- Calls `fetchUpdates()` on a configurable ticker interval (default 2 seconds).
- On error, waits one interval before retrying.
- Exits when the context is cancelled.

### Stop (`telegram.go:68`)

Cancels the polling context. The goroutine exits on the next tick or context check.

## Inbound: Receiving Messages

`fetchUpdates()` (`telegram.go:124`) calls the `/getUpdates` endpoint:

1. Uses the `offset` field to skip previously processed updates (incrementing by `update_id + 1`).
2. Decodes the Telegram `Update` structure with `Message`, `Chat`, `From`, and media fields.
3. For each update:
   - Skips chats not in the allowlist or pairing list (via `shared.AllowInboundIdentity()`).
   - Deduplicates using `chat_id:message_id` key (`telegram.go:277`).
   - Determines the session key:
     - Private chats: `telegram:<chat_id>`
     - Group chats with `IsolatePeers`: `telegram:<chat_id>:<user_id>`
     - Group chats without isolation: `telegram:<chat_id>`
   - Extracts text (from `text` or `caption` field).
   - Processes media attachments (photo, voice, audio, document).
   - Composes the final message text with any media failure markers.
   - Publishes a `bus.EventUserMessage` event with metadata including `chat_id`, `chat_type`, `message_id`, `reply_to_message_id`, `username`, and `attachments`.

### Media Processing (`telegram.go:298`)

Telegram supports four inline media types:

| Type | Telegram API field | Artifact kind |
|---|---|---|
| Photo | `msg.Photo` (largest size) | `image` |
| Voice note | `msg.Voice` | `audio` (`.ogg`) |
| Audio file | `msg.Audio` | `audio` |
| Document | `msg.Document` | `file` |

For each media item:
1. Normalizes the filename and detects the artifact kind.
2. Checks `MaxMediaBytes` — if 0, marks as "disabled by config". If the file size exceeds the limit, marks as "too large".
3. Calls `getFile()` to get the file path from Telegram.
4. Calls `downloadFile()` to fetch the file data.
5. Saves to the `artifacts.Store` via `SaveNamed()`.

Failure markers (e.g., `[image: photo.jpg - download failed]`) are appended to the message text using `channels.ComposeMessageText()`.

## Outbound: Sending Messages

### Text Messages (`telegram.go:81`)

`Deliver()` sends a text message via `/sendMessage`:
- Uses `to` parameter or falls back to `DefaultChatID`.
- If media paths are present, calls `deliverMedia()` instead.
- If `reply_to_message_id` is in meta (and non-zero), adds it to the payload for threaded replies.

### Media Messages (`telegram.go:447`)

`deliverMedia()` iterates over `media_paths` and calls `sendMediaFile()`:

1. Determines the API endpoint and form field name based on file type via `telegramSendSpec()`:
   - Images → `/sendPhoto` with field `photo`
   - Audio (`.ogg`/`.opus`) → `/sendVoice` with field `voice`
   - Other audio → `/sendAudio` with field `audio`
   - Everything else → `/sendDocument` with field `document`
2. Builds a multipart form with `chat_id`, optional `reply_to_message_id`, optional `caption` (only on the first file), and the file content.
3. Posts to the Telegram API.

### Rate Limiting (`telegram.go:284`)

When the Telegram API returns HTTP 429, `telegramRateLimitError()` parses the JSON response body for `retry_after` seconds and formats a rate-limit error using `channels.FormatRateLimitError("telegram", duration, description)`.

## API Communication

- **`getJSON()`** — HTTP GET to `{base}/bot{token}/{path}`, decodes the `{ok, description, result}` envelope.
- **`postJSON()`** — HTTP POST with JSON body, same envelope decoding.
- **`downloadFile()`** — HTTP GET to `{base}/file/bot{token}/{filePath}`, reads up to `MaxMediaBytes+1` with a `LimitReader`.
- Default API base: `https://api.telegram.org`
- Default HTTP client timeout via `shared.DefaultHTTPClient()` (20s).
