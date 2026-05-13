# Background Task Workflow

The current background-work path is `subagents`, not a generic `POST /jobs` route.

## 1. Submit background work

Use the service API:

```http
POST /internal/v1/subagents
```

This queues a background subagent job instead of waiting for a foreground turn to finish.

## 2. Capture the job ID

Background work is tracked through the job registry. Persist the returned job ID so the client can reconnect later.

## 3. Observe progress

Use:

- `GET /internal/v1/jobs/{jobId}`
- `GET /internal/v1/jobs/{jobId}/stream`

## 4. Cancel if needed

```http
POST /internal/v1/jobs/{jobId}/abort
```

## Good uses

- long-running research
- file processing that should survive UI disconnects
- delegated work where foreground waiting would be annoying

## Related but different

- `or3-intern agent -m ...` is a foreground one-shot turn
- `agent-runs` are background runs for external agent CLIs
- `runner-chat` is interactive turn-by-turn external-agent work
