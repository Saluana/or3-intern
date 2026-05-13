# Turns Endpoint

`POST /internal/v1/turns` submits a foreground agent turn. Internally every turn is registered as a job, so clients can wait for JSON, stream immediately, or reconnect through the job endpoints.

## Request

```json
{
  "session_key": "sess_abc123",
  "message": "What files are in my project?",
  "meta": {
    "request_id": "req_abc123"
  },
  "profile_name": "default",
  "allowed_tools": ["list_dir", "read_file"],
  "restrict_tools": true
}
```

Required fields:

- `session_key`
- `message`

Optional fields include `meta`, `profile_name`, `allowed_tools`, `restrict_tools`, `tool_policy`, `approval_token`, and replay-tool-call fields.

## JSON Mode

Without SSE, the route waits for completion and returns a job-shaped JSON response.

```json
{
  "job_id": "job_abc123",
  "type": "turn",
  "status": "completed",
  "final_text": "Your project has src/, docs/, and tests/ directories.",
  "events": []
}
```

## Streaming Mode

To stream the same turn, send `Accept: text/event-stream`:

```http
POST /internal/v1/turns
Accept: text/event-stream
```

The response includes `X-Or3-Job-Id`. The stream first flushes existing job events, then sends new events until the job reaches a terminal status.

## Lifecycle Events

Common event types include:

- `queued`
- `started`
- `tool_call`
- `tool_result`
- `completion`
- `approval_required`
- `completed`
- `failed`
- `aborted`

`X-Request-Id`, `X-Workspace-Id`, and `X-Network-Session-Id` headers are copied into lifecycle metadata when present.

## Approval Resume

If a tool returns approval-required, the turn job completes with `approval_required`. Approving the request may return a `resume_job_id` that continues the original session with the issued approval token.
