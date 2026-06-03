# Cron Automation Workflow

Cron automation is controlled through `/internal/v1/cron/*`.

## 1. Check scheduler availability

```http
GET /internal/v1/cron/status
```

This tells you whether cron is enabled in config and whether the scheduler is actually available.

## 2. Create a job

```http
POST /internal/v1/cron/jobs
```

Cron jobs are persisted scheduler definitions, not just one-off text prompts. Treat them as durable configuration.

## 3. Browse and edit jobs

Use:

- `GET /internal/v1/cron/jobs`
- `GET /internal/v1/cron/jobs/{id}`
- `PATCH /internal/v1/cron/jobs/{id}`
- `DELETE /internal/v1/cron/jobs/{id}`

## 4. Control execution

Use action routes:

- `POST /internal/v1/cron/jobs/{id}/run`
- `POST /internal/v1/cron/jobs/{id}/pause`
- `POST /internal/v1/cron/jobs/{id}/resume`

## Operational tips

- use a dedicated session identity for scheduled work when you want history separated from normal chat
- verify `cron/status` before exposing scheduling UI
- expect structured `503` responses when the scheduler is configured off or unavailable
