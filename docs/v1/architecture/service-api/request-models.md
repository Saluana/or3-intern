# Request and Response Models

The service API uses JSON for request and response bodies unless a route explicitly streams SSE, upgrades to WebSocket, or downloads raw file content.

## Canonical Fields

```json
{
  "session_key": "sess_abc123",
  "message": "Hello, agent!",
  "meta": {
    "request_id": "req_123",
    "workspace_id": "ws_123"
  }
}
```

- `session_key` identifies the conversation for turns.
- `parent_session_key` identifies the parent conversation for subagents or agent runs.
- `meta` carries correlation fields through job lifecycle events.
- `profile_name`, `allowed_tools`, `restrict_tools`, and `approval_token` are accepted by execution routes that need scoped runtime behavior.

Use snake_case for new clients. Some camelCase aliases are still accepted for compatibility.

## Tool Policy Compatibility

Turns and subagents can either send explicit `allowed_tools` / `restrict_tools` fields or a `tool_policy` object. Conflicting canonical and alias fields keep the canonical snake_case value and may emit `X-Or3-Request-Warning`.

## Common Response Fields

```json
{
  "job_id": "job_abc123",
  "type": "turn",
  "status": "completed",
  "events": []
}
```

Job-shaped responses expose `job_id`, `status`, `type`, timestamps, optional `final_text`, and lifecycle `events`. Route-specific responses use typed fields such as `items`, `servers`, `session`, `turn`, `job`, or `runners`.

## Error Response

```json
{
  "error": "session_key and message are required",
  "code": "validation_failed",
  "request_id": "req_abc123"
}
```

Errors that pass through the service boundary include a stable `code` when possible. Some errors include `recovery`, `retry_after_seconds`, `approval_id`, or route-specific context.

## Pagination

List endpoints mostly use bounded `limit` query parameters instead of cursor pagination:

```http
GET /internal/v1/subagents?status=terminal&limit=50
GET /internal/v1/chat-sessions?include_archived=true&limit=20
GET /internal/v1/runner-chat/sessions/{id}/turns/{turnId}/events?after_seq=42&limit=200
```

Streaming/event-history endpoints use monotonically increasing sequence numbers (`after_seq`) when reconnecting.
