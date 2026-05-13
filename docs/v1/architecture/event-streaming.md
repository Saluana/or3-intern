# Event Streaming

The service API uses Server-Sent Events (SSE) for progressive output and reconnectable job/runner streams. Each event is emitted as:

```text
event: <type>
data: <json>
```

## Event Types

| Event | When It Happens |
|---|---|
| `queued` | A job was accepted |
| `started` | A job began executing |
| `completion` | The runtime produced final assistant text |
| `tool_call` | The agent calls a tool |
| `tool_result` | A tool returns its result |
| `approval_required` | A tool needs user approval before continuing |
| `completed` | A job completed |
| `failed` | A job failed |
| `aborted` | A job was canceled |
| `error` | Something went wrong |
| `done` | Runner-chat stream reached a terminal turn state |

## How Streaming Works

Foreground turns stream when the request includes `Accept: text/event-stream`:

```http
POST /internal/v1/turns
Accept: text/event-stream
```

Long-running jobs expose a reconnect-friendly stream:

```http
GET /internal/v1/jobs/{jobId}/stream
```

Runner chat turns expose both durable event history and a stream:

```http
GET /internal/v1/runner-chat/sessions/{sessionId}/turns/{turnId}/events?after_seq=42
GET /internal/v1/runner-chat/sessions/{sessionId}/turns/{turnId}/stream?after_seq=42
```

## Client Code Example

```javascript
const events = new EventSource("/internal/v1/jobs/job_123/stream");

events.addEventListener("tool_call", (e) => {
  showToolCall(JSON.parse(e.data));
});

events.addEventListener("completion", (e) => {
  renderFinalText(JSON.parse(e.data).final_text);
});
```

## Reconnect Rules

- Capture IDs immediately: `X-Or3-Job-Id`, runner chat `session_id`, and `turn_id`.
- Job streams replay the in-memory snapshot before live events.
- Runner chat streams use durable event sequence numbers, so reconnect with `after_seq`.
