# Cron

Cron automation is managed under `/internal/v1/cron/*`.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/cron/status` | Return whether cron is enabled and whether the scheduler is available |
| `GET /internal/v1/cron/jobs` | List scheduled jobs |
| `POST /internal/v1/cron/jobs` | Create a job |
| `GET /internal/v1/cron/jobs/{id}` | Read one job |
| `PATCH` or `PUT /internal/v1/cron/jobs/{id}` | Replace one job definition |
| `POST /internal/v1/cron/jobs/{id}/run` | Run immediately |
| `POST /internal/v1/cron/jobs/{id}/pause` | Pause |
| `POST /internal/v1/cron/jobs/{id}/resume` | Resume |
| `DELETE /internal/v1/cron/jobs/{id}` | Delete |

## Request shape

Create/update accepts either a top-level cron job object or `{ "job": { ... } }`.

In practice, jobs are richer than just `task + schedule`. They carry the persisted scheduler payload used by OR3 Intern, including task kind and task data.

## Good client flow

1. call `cron/status`
2. if `available` is true, list jobs
3. create or update jobs through `cron/jobs`
4. use `run`, `pause`, and `resume` actions for control

If the cron service is disabled or unavailable, the API returns `503` with structured JSON instead of pretending the scheduler exists.
