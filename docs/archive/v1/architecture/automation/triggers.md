# Trigger System

The trigger system handles external events that wake the OR3 agent. It has two trigger types: webhooks and file watches.

## Shared metadata

Triggers produce events with structured metadata for the agent to understand where the event came from.

### StructuredEvent

Each trigger event includes a `StructuredEvent` in its metadata:
- `type` - event type (e.g., "webhook", "file_change")
- `source` - trigger source name
- `trusted` - whether the event comes from a trusted source
- `details` - source-specific information

Source: `internal/triggers/triggers.go:20-26` (StructuredEvent)

### StructuredTasks

Trigger payloads can contain structured task definitions. These are JSON objects or code-fenced blocks that specify tool calls the agent should execute.

Source: `internal/triggers/structured_tasks.go` (StructuredTaskEnvelope, ParseStructuredTasksText)

### TriggerMeta

A simple metadata struct for trigger source information:
- `Source` - "webhook" or "filewatch"
- `Path` - file path (for file-change events)
- `Route` - URL route (for webhook events)
- `Headers` - limited header subset (for webhook events)

Source: `internal/triggers/triggers.go:10-15`

## Event bus integration

Both webhook and filewatch triggers publish events to the internal event bus. The agent runtime listens for these events and processes them.

## Config

Trigger configuration lives in `config.Config`:
- `Triggers.Webhook` - webhook server settings
- `Triggers.FileWatch` - file watcher settings

## Startup

Both triggers are started during server startup if their config is enabled. The webhook server listens on a TCP port; the file watcher starts a polling loop.

Source: `internal/triggers/webhook.go:33-60` (Start), `internal/triggers/filewatch.go:43-53` (Start)
