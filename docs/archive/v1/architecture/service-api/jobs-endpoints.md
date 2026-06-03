# Jobs Endpoints

Jobs are the common observation surface for turns, subagents, approved-resume work, and agent-runner jobs. There is no generic `POST /internal/v1/jobs` route in v1; jobs are created by other route families.

## Job Creators

- `POST /internal/v1/turns`
- `POST /internal/v1/subagents`
- `POST /internal/v1/agent-runs`
- approval actions that return `resume_job_id`
- runner chat turns, which expose both runner-chat turn IDs and job IDs

Capture job IDs from response payloads or the `X-Or3-Job-Id` header.

## Get Job Status

`GET /internal/v1/jobs/{jobId}`

Reads the live job snapshot first. If the live registry no longer has it, the service attempts to return persisted subagent, agent-runner, or service-job history.

## Stream Job Events

`GET /internal/v1/jobs/{jobId}/stream`

Attaches to the job's SSE stream. The server flushes known events first, then sends new events until the job reaches a terminal status.

## Abort Job

`POST /internal/v1/jobs/{jobId}/abort`

Requests cancellation for abortable work. Completed or non-abortable jobs return a conflict-style response.

## Job Statuses

- queued — accepted and waiting to start
- running — being processed
- completed — finished successfully
- failed — stopped with an error
- aborted — canceled by user or caller
- approval_required — paused until an approval request is resolved
- subagent-specific terminal statuses such as succeeded/interrupted can appear in persisted snapshots
