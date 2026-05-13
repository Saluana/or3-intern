# Event Streaming

Events are streamed using Server-Sent Events (SSE). Each event has a type and a JSON payload.

## Event Types

| Event | When It Happens |
|---|---|
| `turn_start` | A new turn begins |
| `turn_finish` | A turn completes |
| `tool_call` | The agent calls a tool |
| `tool_result` | A tool returns its result |
| `job_update` | A background job reports progress |
| `approval_request` | A tool needs user approval |
| `error` | Something went wrong |
| `stream_chunk` | Partial text output from the agent |

## How Streaming Works

The client opens an SSE connection. Events are sent as they happen. The client can show progress in real time.

For chat turns, `stream_chunk` events contain partial text. The client appends each chunk to build the full response. `tool_call` and `tool_result` events let the client show what the agent is doing.

For jobs, `job_update` events show progress. The client can show a progress bar or status text.

## Client Code Example

```javascript
const events = new EventSource("/api/v1/turns?stream=true");
events.addEventListener("stream_chunk", (e) => {
  appendText(JSON.parse(e.data).content);
});
events.addEventListener("tool_call", (e) => {
  showToolCall(JSON.parse(e.data));
});
```
