# Turns

A turn is submitted through `POST /internal/v1/turns`.

## Canonical request shape

Use snake_case fields such as:

- `session_key`
- `message`
- optional `tool_policy`
- optional `meta`
- optional `profile_name`

Compatibility aliases are still accepted, but new clients should send snake_case only.

## Synchronous JSON mode

Without SSE, the route waits for completion and returns JSON.

## Streaming mode

To stream the turn, send:

```http
POST /internal/v1/turns
Accept: text/event-stream
```

## Important headers

- `X-Or3-Job-Id` — the job ID for the turn
- `X-Request-Id` — echoed into the error envelope and useful for tracing
- `X-Approval-Token` and `X-Or3-Approval-Token` — accepted approval-token header aliases

## Compatibility notes

If a request sends both canonical snake_case and a conflicting alias value, the snake_case value wins and the response can include `X-Or3-Request-Warning`.

Use this route for actual execution. Use `chat-sessions` for chat metadata and browsing.
