# Runner Chat Endpoints

Runner chat lets the app hold an interactive session with an external AI CLI such as OpenCode, Codex, Claude Code, Gemini CLI, or the internal OR3 runner.

## List Chat-Selectable Runners

`GET /internal/v1/chat-runners`

Returns runner cards suitable for a chat transport selector. `or3-intern` is always present; other runners appear when agent CLI support is enabled and detection succeeds.

## Create Session

`POST /internal/v1/runner-chat/sessions`

```json
{
  "app_session_key": "svc:app-session",
  "runner_id": "opencode",
  "continuation_mode": "replay",
  "model": "",
  "mode": "review",
  "isolation": "host_workspace_write",
  "cwd": "/path/to/workspace",
  "max_turns": 8
}
```

Returns `201 Created` with runner chat session metadata.

## Read Session and Turns

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/runner-chat/sessions/{id}` | Read session metadata |
| `GET /internal/v1/runner-chat/sessions/{id}/turns` | List turns |
| `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}` | Read one turn |

## Start Turn

`POST /internal/v1/runner-chat/sessions/{id}/turns`

```json
{
  "user_message": "Review the settings MCP server form",
  "timeout_seconds": 300,
  "runner_permission": {
    "runner_id": "opencode",
    "kind": "filesystem",
    "access": "workspace_write",
    "target_path": "/path/to/workspace"
  }
}
```

Returns `202 Accepted` with `session_id`, `turn_id`, `job_id`, and `status`.

## Events and Streaming

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/events` | Fetch durable event history with `after_seq` and `limit` |
| `GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/stream` | Attach to SSE output; flushes persisted events then polls |
| `POST /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/abort` | Abort a running turn |

## How It Works

Runner chat is backed by `internal/agentcli/chat_manager.go` and persisted in the runner chat database tables. Events are durable, so clients can reconnect using `after_seq`.

When runner permissions require approval, `POST .../turns` can return `409` with `code: "approval_required"` and `approval_id`.
