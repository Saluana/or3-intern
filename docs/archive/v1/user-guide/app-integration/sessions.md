# Sessions

The app-facing session metadata API lives under `/internal/v1/chat-sessions/*`.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/chat-sessions` | List chat session metadata |
| `POST /internal/v1/chat-sessions` | Create or upsert chat session metadata |
| `GET /internal/v1/chat-sessions/{sessionKey}` | Read one chat session |
| `PATCH /internal/v1/chat-sessions/{sessionKey}` | Rename or archive a session |
| `GET /internal/v1/chat-sessions/{sessionKey}/messages` | Read persisted messages |
| `POST /internal/v1/chat-sessions/{sessionKey}/fork` | Fork from an anchor point |

## Important distinction

`session_key` is still the execution identity used by turns and memory. `chat-sessions` is the metadata layer used by app UX for titles, archive state, browsing, and message history.

## Create shape

Creating a chat session currently requires `session_key` and can also include metadata such as title and runner labeling.

## Filters and browsing

The list route supports query options such as:

- `host_id`
- `runner_id`
- `include_archived`
- `only_archived`
- `q`
- `limit`

Use this API for app-side chat history management. Use `turns` for actual execution.
