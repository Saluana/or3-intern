# Cron Runner / Dispatcher

The cron runner (dispatcher) executes cron jobs when they fire. It converts cron job payloads into bus events or agent CLI runs.

## Dispatcher

The `Dispatcher` struct holds:
- `Bus` - the event bus for publishing agent turn events
- `DefaultSessionKey` - session key used when the job doesn't specify one
- `AgentCLI` - enqueuer for agent CLI runs (optional)

Source: `internal/cronrunner/dispatcher.go:18-22`

## Run method

The dispatcher is a `cron.Runner` (a function). When called with a job, it dispatches based on payload kind:

- `agent_turn` / `system_event` → publish to event bus
- `agent_cli_run` → enqueue via AgentCLI manager
- anything else → error

Source: `internal/cronrunner/dispatcher.go:32-41`

## Publishing agent turns

For `agent_turn` and `system_event` payloads:
1. Uses the payload message or generates a default ("cron job: <name>")
2. Resolves the session key (payload's session key or the default)
3. Publishes a `bus.Event` with type `EventCron`
4. Includes the channel, recipient, message, and job ID in the event

If the event bus is full (channel buffer full), the dispatch returns an error.

Source: `internal/cronrunner/dispatcher.go:43-63`

## Enqueuing agent CLI runs

For `agent_cli_run` payloads:
1. Requires `AgentCLI` enqueuer to be configured
2. Builds an `agentcli.AgentRunRequest` from the payload's `agent_run` fields
3. Calls `AgentCLI.Enqueue` to create the run
4. Returns the enqueued job ID and run ID

Source: `internal/cronrunner/dispatcher.go:65-91`

## Construction

`New(b, defaultSessionKey, agentCLI)` creates a `cron.Runner`. The event bus is required (panics if nil). The agent CLI enqueuer is optional.

Source: `internal/cronrunner/dispatcher.go:24-29`
