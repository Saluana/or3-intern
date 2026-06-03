# Event Bus

> **Runner-first:** Channels, cron, heartbeat, and triggers still publish events on the bus. Serve-mode workers route most user-facing work to **external runners** via the runner turn orchestrator when `agentCLI.enabled` is true. The legacy `Runtime.Handle` path remains as a fallback when no orchestrator is configured.

The event bus is a single-process fan-out bus used by channels and automation to hand work to the runtime.

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
- Cron runner dispatches `agent_cli_run` payloads (or legacy `agent_turn` events that migrate to runner chat when agent CLI is enabled).
- Webhook and filewatch triggers publish automation events.
- Heartbeat publishes periodic task prompts.
- Serve-mode workers consume bus events and call `RunnerTurnOrchestrator.HandleBusEvent` when configured, otherwise `Runtime.Handle` (legacy).

Service API runner chat and agent CLI runs do not enter through the bus; they call the chat manager and agent CLI manager directly so HTTP job observation and cancellation stay tied to the request's job ID.
