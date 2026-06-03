# Job Lifecycle

Jobs let the agent work in the background. They go through these states:

## States

- **Pending** — the job was created but has not started yet
- **Running** — the agent is actively processing the job
- **Completed** — the job finished successfully
- **Failed** — the job stopped because of an error
- **Cancelled** — a user cancelled the job

## Lifecycle

1. A job is created with a type and input data. It starts as **Pending**.
2. The runtime picks up the job and sets it to **Running**.
3. While running, the job can stream output. You can check progress.
4. If the job succeeds, it moves to **Completed**.
5. If there is an error, it moves to **Failed**. Error details are stored.
6. A user can cancel a job at any time. It moves to **Cancelled**.

## Persistence

Jobs are stored in SQLite. They survive service restarts. You can check on a job days later and still get the result.

## Streaming

Jobs can stream output while running. The service API supports SSE for job progress. The CLI shows a progress indicator.
