# cron
Use the `cron` tool to add/list/remove/run/status scheduled jobs.

Create scheduled runner jobs with `payload.kind="runner_run"` and `payload.runner_run` containing `runner_id` and `task`. Scheduled runner jobs default to `mode="review"` and `isolation="host_readonly"` when those fields are omitted.
