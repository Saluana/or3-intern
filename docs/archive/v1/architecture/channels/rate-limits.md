# Rate Limits

Source: `internal/channels/rate_limit.go`, per-channel rate limit handling

Each external channel handles platform rate limiting in its Deliver path. The shared `channels` package provides formatting utilities.

## Shared Rate Limit Utilities

`internal/channels/rate_limit.go`

### FormatRateLimitError

```go
func FormatRateLimitError(service string, retryAfter time.Duration, detail string) error
```

Creates a consistent error message:

- With retryAfter: `"<service> rate limited: retry after <duration> (<detail>)"`
- Without retryAfter: `"<service> rate limited: <detail>"`
- Minimal: `"<service> rate limited"`

Duration is rounded to milliseconds.

### ParseRetryAfterSeconds

```go
func ParseRetryAfterSeconds(raw string) time.Duration
```

Parses a string like `"3"` or `"1.5"` into a `time.Duration`. Returns 0 for empty or invalid values. Used primarily for Slack's `Retry-After` header.

## Telegram Rate Limiting

`internal/channels/telegram/telegram.go:284-296`

When the Telegram API returns HTTP 429 (Too Many Requests), the response body contains:

```json
{
    "ok": false,
    "description": "Too Many Requests: retry later",
    "parameters": {
        "retry_after": 2
    }
}
```

`telegramRateLimitError()` parses this body and calls `FormatRateLimitError("telegram", retryAfterSeconds, description)`. The `retry_after` field is in integer seconds.

Rate limit errors are surfaced in the `postJSON()` method and propagate through `Deliver()`.

## Slack Rate Limiting

`internal/channels/slack/slack.go:221-223, 247-249`

When the Slack API returns HTTP 429, the `Retry-After` response header contains the number of seconds to wait (as an integer string, e.g. `"3"`).

Both `postJSON()` and `postForm()` handle this:

```go
if resp.StatusCode == http.StatusTooManyRequests {
    return rootchannels.FormatRateLimitError("slack",
        rootchannels.ParseRetryAfterSeconds(resp.Header.Get("Retry-After")), "")
}
```

Slack does not include a detail message, so the error is `"slack rate limited: retry after 3s"`.

## Discord Rate Limiting

`internal/channels/discord/discord.go:292-302`

When the Discord API returns HTTP 429, the response body contains:

```json
{
    "message": "You are being rate limited.",
    "retry_after": 1.5
}
```

`discordRateLimitError()` parses this body and calls `FormatRateLimitError("discord", retryAfter, message)`. The `retry_after` field is a float64 in seconds.

Rate limit errors are surfaced in `getJSON()` and `postJSON()`, and propagate through `Deliver()`.

## WhatsApp Rate Limiting

`internal/channels/whatsapp/whatsapp.go`

The WhatsApp channel does not have built-in rate limit handling. Outbound messages are sent via `conn.WriteJSON()` on the bridge WebSocket. Rate limiting would need to be handled by the bridge process.

## Email Rate Limiting

`internal/channels/email/email.go`

The email channel does not implement rate limiting. It uses standard IMAP/SMTP protocols which do not have the same rate-limit semantics as HTTP APIs. Polling interval (minimum 5 seconds, default 30 seconds) provides natural throttling.

## CLI Channel

The CLI channel does not have rate limiting — it writes directly to stdout.

## How Rate Limit Errors Are Surfaced

When `Deliver()` returns a rate limit error, it propagates to the agent runtime. The runtime can:

1. Retry the delivery after the suggested delay.
2. Report the error to the user.
3. Drop the message.

Rate limit errors are distinguished from other API errors by their format — they always contain the string `"rate limited"` in the error message.
