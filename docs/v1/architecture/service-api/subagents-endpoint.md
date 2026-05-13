# Subagents Endpoint

Endpoints for managing subagents. They let apps create and monitor parallel agent tasks.

## Create Subagent

`POST /api/v1/subagents`

```json
{
  "session_key": "sess_abc123",
  "instructions": "Search the web for latest AI news",
  "tools": ["web_search"]
}
```

Returns the subagent ID and status.

## Get Subagent Status

`GET /api/v1/subagents/:id`

Returns the subagent's current status and any results.

## List Subagents

`GET /api/v1/subagents?session_key=sess_abc123`

Lists all subagents for a session.

## Cancel Subagent

`DELETE /api/v1/subagents/:id`

Cancels a running subagent.

## Use Cases

- Research tasks (multiple searches at once)
- File processing (process multiple files in parallel)
- Code review (review multiple files simultaneously)
