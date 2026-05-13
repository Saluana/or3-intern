# External Agent Runner Workflow

There are two external-agent workflows now. Pick the right one.

## Option A: background external runs

Use the `agent-runs` family when you want a queued background run on an external agent CLI.

Useful routes:

- `GET /internal/v1/agent-runners`
- `POST /internal/v1/agent-runs`
- `GET /internal/v1/agent-runs`
- `GET /internal/v1/agent-runs/{id}`
- `GET /internal/v1/agent-runs/{id}/events?after_seq=N`

This is the right choice for delegated work that should continue without an interactive turn-by-turn UI.

## Option B: interactive runner chat

Use `runner-chat` when the app needs a live session with one external runner.

Useful routes:

- `POST /internal/v1/runner-chat/sessions`
- `POST /internal/v1/runner-chat/sessions/{id}/turns`
- `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/stream`

## Why the split matters

- `agent-runs` are queued background executions
- `runner-chat` is interactive, turn-oriented, resumable chat with one runner

Do not document these as the old single `POST /runner` flow.
