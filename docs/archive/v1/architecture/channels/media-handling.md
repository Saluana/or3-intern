# Media Handling

Source: `internal/channels/media.go`, per-channel capture/deliver methods

Images, files, audio, and other media are handled across channels using a shared set of patterns built on the `artifacts.Store`.

## Core Types

From the `artifacts` package (referenced across all channels):

- **`artifacts.Attachment`** — metadata about a saved file: `ArtifactID`, `Filename`, `MIME`, `Kind`, `SizeBytes`.
- **`artifacts.KindImage`**, **`artifacts.KindAudio`**, **`artifacts.KindFile`** — predefined artifact kind constants.
- **`artifacts.Store`** — saves files to disk and records them in the database. Key methods: `SaveNamed(ctx, sessionKey, filename, mimeType, data)`.
- **`artifacts.DetectKind(path, mimeType)`** — determines the artifact kind from the file extension and MIME type.
- **`artifacts.NormalizeFilename(name, mimeType)`** — ensures filenames have appropriate extensions.
- **`artifacts.Marker(att)`** — generates a success marker like `[image: photo.jpg]`.
- **`artifacts.FailureMarker(kind, filename, reason)`** — generates a failure marker like `[image: photo.jpg - download failed]`.

## Inbound Media Flow

All channels follow the same pattern for receiving media:

1. **Extract** — parse platform-specific attachment fields from the inbound message (Telegram `msg.Photo/Voice/Audio/Document`, Slack `ev.Files`, Discord `msg.Attachments`, WhatsApp `msg.Attachments`).
2. **Validate** — for each attachment:
   - Normalize filename via `artifacts.NormalizeFilename()`.
   - Detect kind via `artifacts.DetectKind()`.
   - Check `MaxMediaBytes` — if 0, mark as "disabled by config".
   - If `MaxMediaBytes > 0` and file size exceeds it, mark as "too large".
   - If `Artifacts` store is nil, mark as "storage unavailable".
3. **Download** — fetch file data from the platform:
   - **Telegram**: `getFile()` to get the file path, then `downloadFile()` via `https://api.telegram.org/file/bot<token>/<path>`.
   - **Slack**: `downloadPrivateFile()` via the private download URL, with `Authorization: Bearer <token>`.
   - **Discord**: `downloadAttachment()` via the attachment URL (no auth needed).
   - **WhatsApp**: `decodeBridgeAttachment()` decodes base64 inline data.
4. **Save** — `artifacts.Store.SaveNamed()` persists the data.
5. **Mark** — on success, generate a marker like `[image: photo.jpg]`. On failure, generate `[image: photo.jpg - <reason>]`.
6. **Compose** — `channels.ComposeMessageText(text, markers)` joins the user's text with markers, each on a new line. For example:

```
see image
[image: photo.jpg]
```

The resulting `content` string is published as the event `Message`. The attachments list is stored in `meta["attachments"]`.

## Telegram Media Types

`internal/channels/telegram/telegram.go:298`

| Telegram type | Field | Detection | Artifact kind | API endpoint |
|---|---|---|---|---|
| Photo | `msg.Photo` | Takes largest size (last) | `image` | `/getFile` |
| Voice note | `msg.Voice` | FileUniqueID as filename | `audio` (.ogg) | `/getFile` |
| Audio file | `msg.Audio` | FileName from metadata | `audio` | `/getFile` |
| Document | `msg.Document` | FileName from metadata | `file` | `/getFile` |

Media groups (`media_group_id`) are noted in metadata but processed one update at a time.

## Slack File Types

`internal/channels/slack/slack.go:453`

Files arrive in the `files` array on the event. Each file has:

- `id`, `name`, `mimetype`, `filetype`, `size`
- `url_private` and `url_private_download` — authenticated download URLs

The channel prefers `url_private_download` over `url_private`. Downloads use `Authorization: Bearer <BotToken>`.

Files bypass the mention requirement — if a message has files but no mention, it is still processed.

## Discord Attachment Types

`internal/channels/discord/discord.go:477`

Attachments arrive in `msg.Attachments` with:

- `url`, `filename`, `content_type`, `size`

Downloads are a simple HTTP GET to the URL (no auth required). Limits are enforced via `io.LimitReader` and a post-read size check.

## WhatsApp Bridge Attachments

`internal/channels/whatsapp/whatsapp.go:202`

Attachments arrive from the bridge as inline base64 data:

- `data_base64` — file content as base64
- `filename`, `mime`, `kind`, `size_bytes`
- `path` — local path, not used for inbound

The bridge encodes files inline. Path-only attachments are rejected as "invalid media payload".

## Media Text Composition

`internal/channels/media.go`

`ComposeMessageText(text, markers)` combines user text with markers:

```go
func ComposeMessageText(text string, markers []string) string {
    parts := make([]string, 0, len(markers)+1)
    if strings.TrimSpace(text) != "" {
        parts = append(parts, strings.TrimSpace(text))
    }
    for _, marker := range markers {
        marker = strings.TrimSpace(marker)
        if marker == "" { continue }
        parts = append(parts, marker)
    }
    return strings.TrimSpace(strings.Join(parts, "\n"))
}
```

`MediaPaths(meta)` extracts the `media_paths` key from a meta map, supporting both `[]string` and `[]any` value types.

## Outbound Media Delivery

Each channel sends media differently:

### Telegram Outbound (`telegram.go:447`)

- `telegramSendSpec()` maps file type to endpoint (sendPhoto / sendVoice / sendAudio / sendDocument).
- Only the first file gets the caption text.
- Uses multipart form upload with the file content.
- Supports `reply_to_message_id` for threaded replies.

### Slack Outbound (`slack.go:341`)

- Two-step process: `files.getUploadURLExternal` to get upload URL and file ID, then `files.completeUploadExternal` to attach files to a message.
- Files are uploaded raw with `Content-Type: application/octet-stream`.
- Supports `thread_ts` for threading and `initial_comment` for caption text.

### Discord Outbound (`discord.go:365`)

- Multipart form with `payload_json` field containing text content and message reference, plus `files[0]`, `files[1]` etc. for each attachment.
- Each file part is read and copied into the multipart writer.

### WhatsApp Outbound (`whatsapp.go:252`)

- Files are read from disk, base64-encoded inline.
- Sent as `bridgeAttachment` objects in the JSON `send` command payload over WebSocket.
- Local `Path` is stripped — only `DataBase64`, `Filename`, `Mime`, `Kind`, and `SizeBytes` are sent.

## Media Size Limits

The `MaxMediaBytes` field on each channel controls whether media is allowed and the maximum size:

- **0** — media handling is disabled entirely ("disabled by config").
- **>0** — maximum allowed file size in bytes. Exceeded files get "too large" markers.
- Downloads use `io.LimitReader(resp.Body, limit+1)` to avoid reading oversized files, followed by a post-read size check.
- Default download limit when `MaxMediaBytes` is not set: 25 MB.
