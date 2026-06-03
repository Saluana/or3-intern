# Event types

OR3 Intern uses events for streaming responses and internal communication.

## Streaming events

These events are sent during a chat turn.

| Event | When it happens |
|---|---|
| `turn_start` | A new turn begins |
| `turn_finish` | The turn is complete |
| `tool_call` | The agent wants to call a tool |
| `tool_result` | A tool call returned a result |
| `job_update` | A background job changed state |
| `approval_request` | The agent needs approval for a tool |
| `error` | Something went wrong |
| `stream_chunk` | A piece of text from the AI response |

## Event bus events

These are used internally and can be useful for custom integrations.

| Event | When it happens |
|---|---|
| `session_created` | A new session started |
| `session_ended` | A session ended |
| `message_received` | A user message arrived |
| `message_sent` | An agent response was sent |
| `tool_registered` | A new tool was registered |
| `channel_connected` | A channel connected |
| `channel_disconnected` | A channel disconnected |

Events follow this format:

```json
{
  "type": "tool_call",
  "data": {
    "tool": "Exec",
    "args": {
      "command": "ls -la"
    }
  },
  "session_id": "...",
  "timestamp": "..."
}
```
