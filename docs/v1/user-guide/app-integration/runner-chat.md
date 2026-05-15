# Runner Chat

Runner chat is the interactive external-agent session API. It is different from the background `agent-runs` queue.

## Main routes

| Route | Purpose |
| --- | --- |
| `POST /internal/v1/runner-chat/sessions` | Create a runner chat session |
| `GET /internal/v1/runner-chat/sessions/{id}` | Read session metadata |
| `GET /internal/v1/runner-chat/sessions/{id}/turns` | List turns |
| `POST /internal/v1/runner-chat/sessions/{id}/turns` | Start a turn |
| `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}` | Read one turn |
| `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/events` | Fetch durable event history |
| `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/stream` | Attach to SSE output |
| `POST /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/abort` | Abort a running turn |

## Session creation

Creating a runner chat session currently requires:

- `app_session_key`
- `runner_id`

Optional fields include continuation mode, model, mode, isolation, cwd, and max turns.

## Use runner chat when

- the app needs an interactive session with an external agent CLI
- you want turn-by-turn UI rather than one queued background run
- you need resumable events and abort support for that interaction
