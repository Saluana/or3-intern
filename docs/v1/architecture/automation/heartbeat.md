# Heartbeat Service

The heartbeat service periodically reads a tasks file (HEARTBEAT.md) and publishes review events on the event bus.

## Configuration

- `enabled` - whether the heartbeat service runs
- `intervalMinutes` - how often to check (default: 30, minimum: 1)
- `tasksFile` - path or name of the tasks file (default: HEARTBEAT.md)
- `sessionKey` - session key for published events

Source: `internal/heartbeat/service.go:34-48` (Service struct)

## How it works

1. A ticker fires at the configured interval
2. On each tick, the service reads the tasks file
3. If the file has active (non-comment, non-empty) instructions, an event is published
4. Only one event is in flight at a time (guarded by an atomic flag)

Source: `internal/heartbeat/service.go:103-211`

## Tasks file

The tasks file is searched in order:
1. `<workspace>/HEARTBEAT.md`
2. `<workspace>/heartbeat.md`
3. The configured path

Source: `internal/heartbeat/service.go:263-283` (candidatePaths)

## Active instructions detection

`HasActiveInstructions` scans the text and ignores:
- Empty lines
- HTML comments (`<!-- ... -->`)
- Markdown heading lines (starting with `#`)

If any non-empty, non-comment, non-heading line exists, the file is considered active.

Source: `internal/heartbeat/service.go:236-261`

## Published event

The heartbeat event includes:
- Type: `EventHeartbeat`
- Default channel: "system"
- Default from: "heartbeat"
- Seed message: "Review HEARTBEAT.md and execute any active recurring tasks."
- Metadata: heartbeat flag, done callback (clears in-flight flag), tasks path, structured event
- Structured tasks parsed from the tasks file if present

Source: `internal/heartbeat/service.go:183-210`

## In-flight guard

The `inFlight` atomic boolean prevents multiple heartbeat events from being processed simultaneously. The done callback stored in event metadata clears the flag when the agent finishes processing.

Source: `internal/heartbeat/service.go:163-211`

## Lifecycle

- `Start()` launches two goroutines: ticker and publisher
- `Stop()` cancels the context and waits for both goroutines to drain

Source: `internal/heartbeat/service.go:60-101`
