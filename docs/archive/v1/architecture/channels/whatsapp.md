# WhatsApp Channel

Source: `internal/channels/whatsapp/whatsapp.go`

The WhatsApp channel connects to an external bridge service over WebSocket. or3-intern does not connect to WhatsApp directly — a separate bridge process handles the WhatsApp protocol and exposes a WebSocket endpoint that or3-intern talks to.

## Configuration

The channel uses `config.WhatsAppBridgeConfig`:

| Field | Purpose |
|---|---|
| `BridgeURL` | WebSocket URL of the WhatsApp bridge |
| `BridgeToken` | Bearer token for bridge authentication |
| `DefaultTo` | Fallback recipient for delivery |
| `InboundPolicy` | Access control (deny/pairing/allowlist/open) |
| `OpenAccess` | Allow all inbound messages |
| `AllowedFrom` | Allowlist of sender IDs |
| `IsolatePeers` | Give each sender their own session |

Requirements (`whatsapp.go:47`): `BridgeURL` must be set.

## Bridge URL Normalization

`BridgeURL(base)` (`whatsapp.go:300`) parses the configured base URL and ensures the path is `/ws`. For example, `http://bridge:8080` becomes `ws://bridge:8080/ws`.

## Connection Lifecycle

### Start (`whatsapp.go:47`)

1. Validates that `BridgeURL` is configured.
2. Calls `connect()` which dials the WebSocket with optional Bearer token auth.
3. Launches `readLoop()` in a background goroutine.

### Stop (`whatsapp.go:66`)

Cancels the read loop context and closes the WebSocket. Sets `closed = true`.

## Bridge Protocol

### Inbound Message Format (`whatsapp.go:192`)

Messages received from the bridge have this structure:

```go
type inboundMessage struct {
    Type        string             // "message"
    ID          string
    Chat        string             // group ID or chat JID
    From        string             // sender phone number
    Text        string
    IsGroup     bool
    Attachments []bridgeAttachment
}
```

Only messages with `Type == "message"` are processed.

### Attachment Format (`whatsapp.go:202`)

```go
type bridgeAttachment struct {
    Path       string // local path (not used for inbound)
    DataBase64 string // base64-encoded file data
    Filename   string
    Mime       string
    Kind       string // "image", "audio", "document", etc.
    SizeBytes  int64
}
```

Attachments from the bridge come as inline base64 data. The bridge is expected to encode files into `data_base64`. Path-only attachments are rejected (marked as "invalid media payload").

### Deduplication

Uses message `ID` if present, otherwise a composite key of `chat|from|text` (`whatsapp.go:325`).

### Session Keys

- Default: `whatsapp:<chat_id>`
- With `IsolatePeers`: `whatsapp:<chat_id>:<from_id>`

If `Chat` is empty but `From` is set, `From` is used as the session target.

## Outbound: Sending Messages

### Deliver (`whatsapp.go:83`)

`Deliver()` sends a `send` command over the WebSocket using `conn.WriteJSON()`:

```go
cmd := map[string]any{
    "type": "send",
    "to":   target,
    "text": text,
}
```

- Uses `to` or falls back to `DefaultTo`.
- If media paths are present, calls `outboundAttachments()` to read and base64-encode files.
- Extra meta fields are forwarded in the command payload (except `media_paths` which is handled separately).

### Outbound Attachments (`whatsapp.go:252`)

`outboundAttachments()` reads files from disk and encodes them as inline `bridgeAttachment` structs:

1. Stats each file and checks `MaxMediaBytes`.
2. Reads the file content.
3. Detects the MIME type and artifact kind.
4. Base64-encodes the data.
5. Returns attachments with `DataBase64`, `Filename`, `Mime`, `Kind`, and `SizeBytes` — but without `Path` (local paths are not sent).

### Inbound Attachment Decoding (`whatsapp.go:281`)

`decodeBridgeAttachment()` decodes base64 data from bridge attachments. It checks decoded size against `maxBytes` before and after decoding. Returns an error if the data is too large or base64 is invalid.

## Access Control

Uses `shared.AllowInboundIdentity()` with `AllowedFrom` as the allowlist. Supports all four policies (deny, pairing, allowlist, open).

## Testing Support

`NewTestDialer()` (`whatsapp.go:312`) returns a WebSocket dialer with a 5-second handshake timeout, used in tests to validate bridge connectivity.
