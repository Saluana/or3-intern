# Jobs

There is no generic `POST /jobs` submission route in v1. Jobs are created by other route families and then observed through `/internal/v1/jobs/*`.

## Common job creators

- `POST /internal/v1/turns` — foreground turns still receive a job ID
- `POST /internal/v1/subagents` — explicit background subagent work
- some approval resumptions create a `resume_job_id`

## Job observation routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/jobs/{jobId}` | Fetch the current job snapshot |
| `GET /internal/v1/jobs/{jobId}/stream` | Attach to live SSE lifecycle events |
| `POST /internal/v1/jobs/{jobId}/abort` | Request cancellation |

## Immediate job IDs

Turn submission responses and SSE opens include `X-Or3-Job-Id`. Persist it immediately so the client can reconnect or resume monitoring later.

## Good client pattern

1. submit work through `turns` or `subagents`
2. capture the job ID from headers or response payloads
3. poll `jobs/{jobId}` or attach to `jobs/{jobId}/stream`
4. call `abort` if the user cancels
