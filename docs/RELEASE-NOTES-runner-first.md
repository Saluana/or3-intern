# Release Notes: Runner-First Execution

- New turns require an external runner.
- Runner orchestration is the only supported execution path.
- `or3-intern agent -m` enqueues runner chat work.
- `/internal/v1/chat-runners` lists configured external runners.
- Scheduled jobs use `runner_run`.
