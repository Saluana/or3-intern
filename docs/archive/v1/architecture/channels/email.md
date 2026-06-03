# Email Channel

Source: `internal/channels/email/email.go`

The email channel polls an IMAP inbox for new messages and sends replies via SMTP. It is the only channel that does not use WebSockets or HTTP polling — it uses raw TCP connections with TLS.

## Configuration

The channel uses `config.EmailChannelConfig`:

| Field | Purpose |
|---|---|
| `Enabled` | Master enable/disable switch |
| `ConsentGranted` | User must explicitly consent to email access |
| `IMAPHost` | IMAP server hostname |
| `IMAPPort` | IMAP port |
| `IMAPUsername` | IMAP login username |
| `IMAPPassword` | IMAP login password |
| `IMAPUseSSL` | Use TLS from connection start |
| `IMAPMailbox` | Mailbox to poll (default `"INBOX"`) |
| `MarkSeen` | Mark fetched messages as seen |
| `SMTPHost` | SMTP server hostname |
| `SMTPPort` | SMTP port |
| `SMTPUsername` | SMTP auth username |
| `SMTPPassword` | SMTP auth password |
| `SMTPUseSSL` | Use TLS from connection start |
| `SMTPUseTLS` | Use STARTTLS after connecting |
| `PollIntervalSeconds` | Poll interval (default 30s, minimum 5s) |
| `MaxBodyChars` | Maximum characters to extract from email body |
| `SubjectPrefix` | Prefix for reply subjects (default `"Re:"`) |
| `FromAddress` | Explicit from address (falls back to SMTP/IMAP username) |
| `DefaultTo` | Fallback recipient address |
| `AutoReplyEnabled` | Whether auto-reply is turned on |
| `InboundPolicy` | Access control (deny/pairing/allowlist/open) |
| `OpenAccess` | Allow all inbound senders |
| `AllowedSenders` | Allowlist of email addresses |
| `InboundPolicy` | Sender access control |

Requirements (`email.go:238`):
- IMAP host, username, password must be set.
- SMTP host, username, password must be set.
- SMTP auth requires TLS or SSL (plaintext auth over unencrypted connection is rejected).
- Either `openAccess`, `pairing`, or `allowedSenders` must be configured.

## Consent Gate

If `ConsentGranted` is `false`, the `Start()` method logs a message and returns nil without error. This is a privacy safety feature — no IMAP connection is made without user consent.

## Connection Lifecycle

### Start (`email.go:91`)

1. Returns immediately if `ConsentGranted` is false.
2. Validates all required configuration.
3. Creates a child context with cancellation.
4. Launches `pollLoop()` in a background goroutine.

### Poll Loop (`email.go:160`)

- Calls `pollOnce()` immediately, then on a ticker interval.
- Minimum interval: 5 seconds. Default: 30 seconds.
- Exits when context is cancelled.

### Stop (`email.go:122`)

Cancels the poll context. In-flight IMAP/SMTP operations are cancelled via context-aware connection watching.

## Inbound: Fetching Messages

### IMAP Fetch (`email.go:265`)

`fetchViaIMAP()` uses the `github.com/emersion/go-imap/v2` library:

1. Dials an IMAP connection (TCP or TLS), with TLS handshake if `IMAPUseSSL` is set.
2. Logs in with username/password.
3. Selects the mailbox (default `INBOX`).
4. Searches for unseen messages (`NotFlag: Seen`).
5. Fetches at most `maxFetchBatch` (20) messages, newest first.
6. For each message, fetches the UID, envelope, and full body.
7. Optionally marks fetched messages as seen.
8. Parses raw email with `parseRawEmail()`.

### Email Parsing (`email.go:496`)

`parseRawEmail()` extracts:

- **From** — normalized email address via `normalizeAddress()`.
- **Subject** — decoded from MIME encoded-words (e.g., `=?UTF-8?Q?...?=`).
- **Message-ID** — for threading.
- **Date** — parsed from RFC 5322 format.
- **Body** — plain text is preferred. If no plain text part exists, HTML is converted to text via `htmlToText()`. Attachments are skipped. Quoted-printable and base64 transfer encodings are decoded.

### Deduplication (`email.go:760-812`)

Email uses two layers of deduplication:

1. **In-memory** — a ring buffer of the last 4096 processed keys (prefixed with `uid:` or `msgid:`). Messages already in this buffer are skipped.
2. **Database-persisted** — checks the last 200 messages in the database for the same UID or Message-ID. This prevents re-processing after restarts.

### Thread Tracking (`email.go:814`)

The channel remembers the subject and Message-ID for each sender in `threadBySender`. When delivering a reply, it looks up the thread state and also searches historical messages in the database (`email.go:834`). This enables proper `In-Reply-To` and `References` headers in outbound emails.

### Access Control (`email.go:689`)

Uses `shared.AllowInboundIdentity()` with `AllowedSenders` as the allowlist and `normalizeAddress` for identity normalization. `OpenAccessOverridesAllowlist` is set to `true` — open access bypasses the allowlist, unlike other channels.

## Outbound: Sending Messages

### Deliver (`email.go:135`)

1. Normalizes the recipient address.
2. Looks up the thread state for that sender (in-memory + DB).
3. Determines the subject: uses meta `subject` if provided, otherwise prefixes the original subject with `SubjectPrefix` (default `"Re:"`), preserving existing `Re:` prefixes.
4. Builds an `OutboundMessage` with `To`, `From`, `Subject`, `Text`, and `InReplyTo`.
5. Calls `sendMail()`.

### SMTP Send (`email.go:383`)

`sendViaSMTP()`:

1. Validates SMTP auth + transport security (must use TLS or STARTTLS if auth is configured).
2. Builds the raw email message with `buildOutboundMessage()`:
   - Headers: From, To, Subject (MIME Q-encoded), Date, Message-ID, MIME-Version.
   - Body: `text/plain; charset=UTF-8` with `quoted-printable` transfer encoding.
   - If threading info exists: In-Reply-To and References headers.
3. Connects via TCP, optionally upgrades to TLS (STARTTLS or direct SSL).
4. Authenticates with `PLAIN` auth.
5. Sends `MAIL FROM`, `RCPT TO`, `DATA`, then the raw message bytes.
6. Quits the SMTP session.

### Context Cancellation

Both IMAP and SMTP connections are monitored via `watchConnContext()` (`email.go:938`). When the context is cancelled, the TCP connection is closed, causing any in-progress read/write to fail and return.

## Auto-Reply Policy

`AutoReplyEnabled` is included in the event metadata. The runtime (not the channel) decides whether to suppress replies when auto-reply is disabled. The channel itself always delivers regardless of this setting.
