# Streaming

v1 streaming is split across several route families. There is not one universal `?stream=true` convention anymore.

## Foreground turn streaming

Use:

```http
POST /internal/v1/turns
Accept: text/event-stream
```

The same route returns normal JSON when you do not request SSE.

## Job streaming

Use:

```http
GET /internal/v1/jobs/{jobId}/stream
```

This is the reconnect-friendly stream for long-running work.

## Terminal streaming

Terminal sessions support both:

- `GET /internal/v1/terminal/sessions/{sessionId}/stream` for SSE
- `POST /internal/v1/terminal/sessions/{sessionId}/ws-ticket` followed by `GET .../ws` for WebSocket attachment

## Runner chat streaming

Runner chat turns expose:

```http
GET /internal/v1/runner-chat/sessions/{sessionId}/turns/{turnId}/stream
```

and a reconnectable event history endpoint at `.../events`.

## Practical advice

- capture IDs immediately (`X-Or3-Job-Id`, session IDs, turn IDs)
- use SSE for progressive UI
- fall back to polling the corresponding read endpoint when reconnecting or recovering
