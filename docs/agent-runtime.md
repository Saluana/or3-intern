# Agent runtime

## Shared runtime model

`or3-intern` uses one shared runtime model across:

- `chat`
- `agent`
- `serve`
- `service`
- channel adapters
- autonomous triggers
- subagent jobs

That means turns, tool calls, memory retrieval, quotas, and session handling behave consistently no matter how a task enters the system.

## High-level flow

1. A foreground or background entrypoint receives work
2. The runtime resolves the session key and related context
3. Prompt bootstrap files and retrieved memory are injected
4. The model runs a turn
5. Tool calls execute through the shared tool registry
6. Results are persisted to SQLite-backed history and memory stores
7. Foreground callers receive the response immediately, while background jobs stream or persist status updates

## Foreground entrypoints

- `chat` for local interactive use
- `agent -m` for one-shot turns
- `service` for authenticated HTTP callers

## Background entrypoints

- `serve` for external channels
- webhook triggers
- file-watch triggers
- heartbeat turns
- cron jobs
- subagent jobs

## Streaming

The CLI `chat` command supports live token streaming from the provider. The internal service API can also stream job output over SSE for callers that send `Accept: text/event-stream`.

## Session model

Every turn runs inside a session key, such as:

- `cli:default`
- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `email:<address>`
- `whatsapp:<chat-id>`
- `heartbeat:default`

Session keys are used for history isolation, memory retrieval, and optional scope linking.

## Subagents

Subagents are optional background jobs governed by the same runtime and hardening controls. Configuration includes:

- `subagents.enabled`
- `subagents.maxConcurrent`
- `subagents.maxQueued`
- `subagents.taskTimeoutSeconds`

## Related documentation

- [Memory and context](memory-and-context.md)
- [Triggers and automation](triggers-and-automation.md)
- [Internal service API reference](api-reference.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/agent/`
- `cmd/or3-intern/main.go`
- `cmd/or3-intern/service.go`
