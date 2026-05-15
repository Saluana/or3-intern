# cron
Use the `cron` tool to add/list/remove/run/status scheduled jobs.

For normal OR3 turns, create jobs with `payload.kind="agent_turn"` and a `payload.message`.

For external agent CLI jobs, create jobs with `payload.kind="agent_cli_run"` and `payload.agent_run` containing `runner_id` and `task`. Scheduled external agent runs default to `mode="review"` and `isolation="host_readonly"` when those fields are omitted.
