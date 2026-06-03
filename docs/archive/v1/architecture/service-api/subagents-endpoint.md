# Subagents Endpoint

Subagent endpoints create and inspect background tasks run by OR3's internal subagent manager.

## Create Subagent

`POST /internal/v1/subagents`

```json
{
  "parent_session_key": "sess_abc123",
  "task": "Review the service API docs for stale route names",
  "allowed_tools": ["read_file", "search_file"],
  "restrict_tools": true,
  "timeout_seconds": 120,
  "meta": {
    "request_id": "req_abc123"
  }
}
```

Returns `202 Accepted` with:

```json
{
  "job_id": "subagent_job_123",
  "child_session_key": "sess_child_123",
  "status": "queued"
}
```

## Get Subagent Status

`GET /internal/v1/subagents/{jobId}`

Returns the persisted subagent job snapshot, final text when available, and reconstructed events.

## List Subagents

`GET /internal/v1/subagents`

Query parameters:

- `status`
- `parent_session_key`
- `limit`

The special status filter `terminal` is accepted by the database layer for completed/failed/interrupted jobs.

## Abort

Subagents are aborted through the shared job route:

```http
POST /internal/v1/jobs/{jobId}/abort
```

There is no `DELETE /internal/v1/subagents/{id}` route in v1.

## Use Cases

- bounded background research
- codebase exploration that should not block the foreground turn
- file-processing or review tasks with a separate child session
