# Agent Runtime

The agent runtime (`internal/agent/runtime.go`) is the heart of OR3 Intern. It manages everything the agent does.

## What It Does

- Runs the turn processing loop (input -> prompt -> provider -> tools -> output)
- Holds references to all subsystems (tools, memory, config, providers)
- Handles orchestration between prompt building, tool calls, and response generation
- Supports both synchronous (wait for complete response) and streaming (SSE) modes

## Key Responsibilities

The runtime decides what happens next in each turn. It sends the prompt to the provider. It processes the response. If the response includes tool calls, the runtime executes them and sends the results back to the provider. This loop continues until the agent produces a final text response.

## Subsystem References

The runtime holds pointers to:
- **Tool registry** — all available tools
- **Memory system** — vector search and FTS
- **Config** — current configuration
- **Provider** — the AI provider client
- **Security** — auth and approval system
- **Storage** — database connections

## Modes

The runtime works in two modes:
- **Sync mode** — waits for the full response and returns it
- **Stream mode** — sends events as they happen (SSE)

Both modes use the same internal loop. The difference is how results are delivered.
