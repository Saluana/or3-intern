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

## Native runner behavior

OpenCode and Codex are native-first when their runtime mode is `auto`, with CLI fallback preserved:

- OpenCode can reuse a configured local server from `agentCLI.nativeServerUrls.opencode` or lazily start `opencode serve` on loopback.
- Codex starts `codex app-server --listen stdio://` only when a turn needs it.
- Service start/restart commands only start OR3 Intern itself; they do not eagerly start OpenCode or Codex helper runtimes.
- Runner discovery includes runtime status, ownership, fallback reason, available models, and the configured/default model so the app can show model choices per runner.

Native permission requests are converted into OR3 runner-permission approvals. Approving a request and retrying the turn passes the approved permission back through the runner chat metadata; denied requests remain blocked and are shown in the approval UI with the raw technical payload available for inspection.

## Use runner chat when

- the app needs an interactive session with an external agent CLI
- you want turn-by-turn UI rather than one queued background run
- you need resumable events and abort support for that interaction
