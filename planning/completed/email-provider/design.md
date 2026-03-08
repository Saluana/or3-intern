# Overview

The email integration fits naturally into the current architecture as another external channel managed by `internal/channels.Manager` and started from `cmd/or3-intern/main.go` during `serve`.

The design intentionally mirrors the existing Telegram/Slack/Discord/WhatsApp pattern:

- config lives in `internal/config`
- startup wiring happens in `cmd/or3-intern/main.go`
- inbound email becomes `bus.Event`
- outbound email uses the existing `Deliver` path
- session/history/memory behavior stays inside existing `internal/agent`, `internal/db`, and `internal/memory` flows

This keeps the feature bounded and avoids introducing a new service layer, webhook server, or mailbox-specific database model.

# Affected areas

- `internal/config/config.go`
  - add `EmailChannelConfig`, defaults, env overrides, and access validation
- `cmd/or3-intern/main.go`
  - register/start the email channel when enabled
- `internal/channels/email/email.go`
  - implement IMAP polling, message parsing, sender filtering, and SMTP delivery
- `internal/channels/channels.go`
  - no structural changes expected, but email will be another registered channel
- `internal/tools/message.go`
  - likely no schema change required; existing `channel` and `to` fields already fit email delivery
- `internal/agent/runtime.go`
  - no special-case flow should be needed beyond normal event handling and payload persistence
- `internal/db/store.go`
  - no schema migration expected; reply metadata can live in message payload JSON
- `README.md`
  - document configuration, env vars, consent, and sender restrictions
- tests under `internal/channels/email`, `internal/config`, and possibly `cmd/or3-intern`

# Control flow / architecture

## Inbound flow

1. `serve` starts the email channel through the existing channel manager.
2. The email channel validates config and begins a bounded IMAP polling loop.
3. Each poll:
   - selects the configured mailbox
   - searches unread or otherwise targeted messages
   - fetches a bounded number of candidates
   - deduplicates by UID/message-id
   - parses sender, subject, date, and readable body text
4. Accepted mail is converted into a `bus.Event`:
   - `Type`: existing user-message style event path
   - `Channel`: `email`
   - `SessionKey`: deterministic sender-derived key
   - `From`: normalized sender email
   - `Message`: readable email transcript
   - `Meta`: sender/subject/message-id/date/uid
5. The existing worker/runtime path handles the turn, stores history, performs retrieval, and optionally replies.

## Outbound flow

1. Runtime/tooling uses `send_message` with `channel="email"` and a recipient address.
2. `channels.Manager.DeliverWithMeta` routes to the email channel.
3. The email channel builds an SMTP message from:
   - recipient
   - configured sender identity
   - generated or overridden subject
   - prior threading metadata when replying
4. The channel sends the message using SMTP with TLS/SSL options from config.
5. Errors are returned through the normal channel delivery path.

## Reply-threading behavior

The email channel should track the latest subject and message-id per sender identity in memory for the live process, while also persisting enough metadata in stored messages to restore context after restart if needed.

A pragmatic v1 approach is:

- keep a small in-memory cache for fast replies during the current process lifetime
- persist inbound message metadata in `messages.payload_json`
- on outbound reply, use the most recent stored inbound metadata for that sender/session if the live cache is empty

That avoids a new SQLite table while still surviving restarts.

# Data and persistence

## SQLite changes

None required for v1.

Existing `messages.payload_json` is sufficient for bounded email metadata such as:

```json
{
  "channel": "email",
  "from": "user@example.com",
  "meta": {
    "sender_email": "user@example.com",
    "subject": "Project update",
    "message_id": "<abc@example.com>",
    "uid": "3812",
    "date": "Fri, 7 Mar 2026 12:34:56 +0000"
  }
}
```

This preserves reply context without introducing mailbox-specific schema.

## Config / env changes

Add an email channel config shaped consistently with the existing channel config model:

```go
type EmailChannelConfig struct {
    Enabled             bool     `json:"enabled"`
    OpenAccess          bool     `json:"openAccess"`
    ConsentGranted      bool     `json:"consentGranted"`
    AllowedSenders      []string `json:"allowedSenders"`
    DefaultTo           string   `json:"defaultTo"`
    AutoReplyEnabled    bool     `json:"autoReplyEnabled"`
    PollIntervalSeconds int      `json:"pollIntervalSeconds"`
    MarkSeen            bool     `json:"markSeen"`
    MaxBodyChars        int      `json:"maxBodyChars"`
    SubjectPrefix       string   `json:"subjectPrefix"`
    FromAddress         string   `json:"fromAddress"`
    IMAPMailbox         string   `json:"imapMailbox"`
    IMAPHost            string   `json:"imapHost"`
    IMAPPort            int      `json:"imapPort"`
    IMAPUseSSL          bool     `json:"imapUseSSL"`
    IMAPUsername        string   `json:"imapUsername"`
    IMAPPassword        string   `json:"imapPassword"`
    SMTPHost            string   `json:"smtpHost"`
    SMTPPort            int      `json:"smtpPort"`
    SMTPUseTLS          bool     `json:"smtpUseTLS"`
    SMTPUseSSL          bool     `json:"smtpUseSSL"`
    SMTPUsername        string   `json:"smtpUsername"`
    SMTPPassword        string   `json:"smtpPassword"`
}
```

Recommended env overrides:

- `OR3_EMAIL_IMAP_HOST`
- `OR3_EMAIL_IMAP_PORT`
- `OR3_EMAIL_IMAP_USERNAME`
- `OR3_EMAIL_IMAP_PASSWORD`
- `OR3_EMAIL_SMTP_HOST`
- `OR3_EMAIL_SMTP_PORT`
- `OR3_EMAIL_SMTP_USERNAME`
- `OR3_EMAIL_SMTP_PASSWORD`
- `OR3_EMAIL_FROM_ADDRESS`

Defaults should remain conservative:

- disabled by default
- polling every 30-60 seconds unless configured lower bound applies
- `autoReplyEnabled=false`
- `consentGranted=false`
- `maxBodyChars` bounded to avoid huge prompts/events

## Session and memory scope

Use deterministic sender-based session keys:

- `email:user@example.com`

This aligns with the existing `platform:id` convention already used for other channels. Scope linking remains an explicit user choice through existing scope commands.

# Interfaces and types

## Channel shape

The email integration should implement the existing channel interface directly:

```go
type Channel struct {
    Config EmailChannelConfig
}

func (c *Channel) Name() string
func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error
func (c *Channel) Stop(ctx context.Context) error
func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error
```

## Internal helpers

Expected helpers inside `internal/channels/email/email.go`:

- config validation
- IMAP polling loop
- bounded search/fetch logic
- MIME body extraction
- sender normalization/filtering
- SMTP send helper
- subject/thread reconstruction helper

## Metadata contract

Inbound email should place bounded metadata in `bus.Event.Meta` so it is persisted automatically with the message payload.

Suggested keys:

- `sender_email`
- `subject`
- `message_id`
- `uid`
- `date`

# Failure modes and safeguards

- Invalid config
  - startup should return actionable validation errors when enabled config is incomplete
- Missing consent
  - channel should not start and should log a concise warning
- IMAP polling failure
  - transient errors should be retried on the next poll without crashing the process
- SMTP delivery failure
  - return a normal delivery error to the caller; do not terminate the polling loop
- Duplicate inbound messages
  - rely on mailbox flags plus bounded UID/message-id dedupe
- Oversized or malformed bodies
  - truncate content and skip unreadable MIME parts instead of failing the whole poll cycle
- Unauthorized senders
  - drop inbound messages that fail allowlist/open-access checks
- Secret leakage
  - never log passwords or include connection details in prompts or user-visible runtime text

# Testing strategy

Use Go `testing` package coverage focused on the new channel package and startup/config integration.

## Unit tests

- `internal/config/config_test.go`
  - default config values
  - env overrides
  - access validation for enabled email channel
- `internal/channels/email/email_test.go`
  - sender normalization and allowlist behavior
  - subject threading helpers
  - plain-text and HTML body extraction
  - IMAP result parsing and UID dedupe logic
  - SMTP request construction and reply metadata behavior

## Integration-style tests

- use `httptest`, fake listeners, or in-memory stubs where practical to simulate polling and delivery behavior without real mail servers
- verify that inbound email becomes `bus.Event` with correct session key and metadata
- verify that outbound `Deliver` sends the expected message envelope and threading headers

## Regression coverage

- `cmd/or3-intern` tests or focused startup tests should verify the channel manager registers email only when enabled
- existing channels and `send_message` behavior should continue to pass unchanged when email is disabled
