# Event Bus

The event bus is a single-process fan-out bus used by channels and automation to hand work to the agent runtime.

## Package

`internal/bus`

## Event Model

```go
type Event struct {
    Type       EventType
    SessionKey string
    Channel    string
    From       string
    Message    string
    Meta       map[string]any
}
```

Event types:

| Type | Source |
| --- | --- |
| `user_message` | CLI and external channels |
| `cron` | scheduled cron jobs |
| `heartbeat` | heartbeat service |
| `system` | internal system events |
| `webhook` | webhook trigger server |
| `file_change` | filewatch trigger |

## Fan-out Behavior

`Bus.Subscribe()` creates a per-subscriber buffered channel. `Publish()` attempts non-blocking delivery to every subscriber. If a subscriber buffer is full, that subscriber's event is dropped and `Publish()` returns false.

The deprecated `Channel()` method is retained for worker-pool queue semantics where multiple workers split work instead of all receiving every event.

## Where It Is Used

- Channels publish inbound platform messages.
- Cron runner dispatches scheduled turns or agent-run payloads.
- Webhook and filewatch triggers publish automation events.
- Heartbeat publishes periodic task prompts.
- Serve-mode workers consume bus events and call `Runtime.Handle`.

Service API turns do not normally enter through the bus; they call ServiceApp and runtime directly so HTTP job observation and cancellation stay tied to the request's job ID.
