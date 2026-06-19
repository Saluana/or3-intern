---
name: cron
description: Schedule, list, update, and remove automated runner jobs by time or interval.
---

# Cron

Create automated runner jobs that fire on a schedule. Use this when someone asks for a daily report, a weekly check, a one-shot reminder, or any recurring task.

## How it works

Cron jobs are managed through the `or3-intern cron` CLI. Every job has:

- A **schedule** that tells or3-intern when to fire (once at a time, every N milliseconds, or via cron expression)
- A **payload** that tells or3-intern what to do when it fires (run a runner agent task)
- A **name** and optional **session key** for organization

When a job fires, or3-intern enqueues a runner agent task. The runner executes it and reports back.

## CLI commands (recommended)

These work directly — no service, no auth tokens, no curl required.

### Create a cron job

Pass the job as JSON (same format as the service API):

```bash
or3-intern cron add --json '{
  "name": "Daily review",
  "schedule": { "kind": "every", "every_ms": 3600000 },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Review all uncommitted changes"
    }
  }
}'
```

Or pipe it from stdin:

```bash
echo '{"name":"test","schedule":{"kind":"cron","expr":"0 22 * * *"},"payload":{"kind":"runner_run","agent_run":{"runner_id":"opencode","task":"Remind: change bed sheets"}}}' | or3-intern cron add --stdin
```

Output (shows the created job with auto-generated ID):
```
id:       a1b2c3d4e5
name:     Daily review
enabled:  true
schedule: every 1h0m0s
runner:   opencode
task:     Review all uncommitted changes
next:     2026-06-18T14:00:00-07:00
last:     - (-)
```

### List all jobs

```bash
or3-intern cron list
```

Output:
```
a1b2c3d4e5  Daily review          enabled   next=2026-06-18T14:00:00-07:00
f6g7h8i9j0  Bed sheets reminder   enabled   next=2026-06-18T22:00:00-07:00
```

### Show a single job

```bash
or3-intern cron show a1b2c3d4e5
```

### Delete a job

```bash
or3-intern cron remove a1b2c3d4e5
```

Output: `removed a1b2c3d4e5`

### Pause a job (disable without deleting)

```bash
or3-intern cron pause a1b2c3d4e5
```

Output: `paused a1b2c3d4e5 (Daily review)`

### Resume a paused job

```bash
or3-intern cron resume a1b2c3d4e5
```

Output: `resumed a1b2c3d4e5 (Daily review)`

### Run a job immediately

Validates the job and shows what it would do. The job actually fires when `or3-intern serve` or `or3-intern service` is running.

```bash
or3-intern cron run a1b2c3d4e5
```

Output:
```
job "Daily review" (a1b2c3d4e5) would fire:
  runner: opencode
  task:   Review all uncommitted changes
ran a1b2c3d4e5
```

Force-run a disabled job:

```bash
or3-intern cron run a1b2c3d4e5 --force
```

## JSON reference for `or3-intern cron add --json`

The JSON object supports these fields:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | no | auto-generated ID | Human-readable name |
| `enabled` | no | `true` | Start as enabled or paused |
| `delete_after_run` | no | `false` | Remove after it fires (for one-shots) |
| `schedule.kind` | yes | — | `"at"`, `"every"`, or `"cron"` |
| `schedule.at_ms` | for `at` | — | Unix epoch milliseconds |
| `schedule.every_ms` | for `every` | — | Milliseconds between runs (min 1000) |
| `schedule.expr` | for `cron` | — | Cron expression (e.g. `"0 22 * * *"`) |
| `schedule.tz` | no | — | IANA timezone (e.g. `"America/New_York"`) |
| `payload.kind` | no | `"runner_run"` | Payload type (only `runner_run` supported) |
| `payload.session_key` | no | `"cron:default"` | Session key for isolated history |
| `payload.agent_run.runner_id` | yes | — | Which runner: `"opencode"`, `"codex"`, etc. |
| `payload.agent_run.task` | yes | — | The prompt/task for the runner |
| `payload.agent_run.timeout_seconds` | no | 900 | Max seconds before timeout |
| `payload.agent_run.cwd` | no | workspace | Working directory |
| `payload.agent_run.model` | no | default | Model override |
| `payload.agent_run.mode` | no | `"review"` | `"review"`, `"safe_edit"`, `"sandbox_auto"` |
| `payload.agent_run.isolation` | no | `"host_readonly"` | `"host_readonly"`, `"host_workspace_write"`, etc. |
| `payload.agent_run.max_turns` | no | — | Max conversation turns |

## Schedule reference

### `kind: "every"` — repeating interval

```bash
or3-intern cron add --json '{
  "name": "Hourly code review",
  "schedule": { "kind": "every", "every_ms": 3600000 },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Review all uncommitted changes and summarize what happened in the last hour"
    }
  }
}'
```

### `kind: "cron"` — cron expression

Useful patterns:
- `"0 9 * * 1-5"` = weekdays at 9:00 AM
- `"0 */2 * * *"` = every 2 hours
- `"0 22 * * *"` = every day at 10:00 PM
- `"0 0 * * 0"` = every Sunday at midnight

```bash
or3-intern cron add --json '{
  "name": "Daily standup summary",
  "schedule": { "kind": "cron", "expr": "0 9 * * 1-5", "tz": "America/New_York" },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Summarize yesterday progress and any blockers in the workspace"
    }
  }
}'
```

### `kind: "at"` — one-shot (auto-deletes after firing)

```bash
# Calculate "tomorrow at 10pm" in epoch milliseconds
AT_MS=$(python3 -c "from datetime import datetime,timedelta;t=datetime.now()+timedelta(days=1);t=t.replace(hour=22,minute=0,second=0,microsecond=0);print(int(t.timestamp()*1000))")

or3-intern cron add --json "$(cat <<ENDJSON
{
  "name": "Deploy to production",
  "delete_after_run": true,
  "schedule": { "kind": "at", "at_ms": ${AT_MS} },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Deploy the latest build to production"
    }
  }
}
ENDJSON
)"
```

## Common templates

### "Remind me every day at 10pm to change the bed sheets"

```bash
or3-intern cron add --json '{
  "name": "Change bed sheets reminder",
  "schedule": { "kind": "cron", "expr": "0 22 * * *" },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Remind the user to change their bed sheets. Be friendly and brief."
    }
  }
}'
```

### "Run a code review every Monday at 9am"

```bash
or3-intern cron add --json '{
  "name": "Weekly code review",
  "schedule": { "kind": "cron", "expr": "0 9 * * 1", "tz": "America/New_York" },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Review all changes from the past week and flag any issues",
      "mode": "review",
      "isolation": "host_readonly"
    }
  }
}'
```

### "Remind me in 20 minutes to call Sarah"

```bash
AT_MS=$(python3 -c "import time; print(int((time.time() + 20*60) * 1000))")

or3-intern cron add --json "$(cat <<ENDJSON
{
  "name": "Call Sarah",
  "delete_after_run": true,
  "schedule": { "kind": "at", "at_ms": ${AT_MS} },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Remind the user: time to call Sarah. Keep it short."
    }
  }
}
ENDJSON
)"
```

## Service API (advanced)

For programmatic access when the HTTP service is running, use the API directly. Requires `or3-intern service` to be running on `127.0.0.1:9100` (default).

### Auth token

You need the service secret from `~/.or3-intern/config.json` and operator-level role:

```bash
or3-intern configure --set service.sharedSecretRole=operator
```

Generate a bearer token (valid for 5 minutes):

```bash
TOKEN=$(python3 -c "
import hmac, json, base64, os, time
secret = 'YOUR_SECRET_HERE'
iat = int(time.time())
nonce = os.urandom(12).hex()
claims = json.dumps({'iat': iat, 'nonce': nonce}).encode()
payload = base64.urlsafe_b64encode(claims).rstrip(b'=').decode()
sig = hmac.new(secret.encode(), payload.encode(), 'sha256').hexdigest()
print(f'{payload}.{sig}')
")
PORT=9100
```

### API endpoints

| Method | Path | Action |
|--------|------|--------|
| **GET** | `/internal/v1/cron/jobs` | List all jobs |
| **POST** | `/internal/v1/cron/jobs` | Create a job |
| **GET** | `/internal/v1/cron/jobs/{id}` | Get one job |
| **PUT** | `/internal/v1/cron/jobs/{id}` | Update a job |
| **DELETE** | `/internal/v1/cron/jobs/{id}` | Delete a job |
| **POST** | `/internal/v1/cron/jobs/{id}/run` | Run now |
| **POST** | `/internal/v1/cron/jobs/{id}/pause` | Disable |
| **POST** | `/internal/v1/cron/jobs/{id}/resume` | Re-enable |

Example:

```bash
curl -s -X POST "http://127.0.0.1:${PORT}/internal/v1/cron/jobs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{"job": {"name": "API test", "schedule": {"kind": "every", "every_ms": 60000}, "payload": {"kind": "runner_run", "agent_run": {"runner_id": "opencode", "task": "hello"}}}}'
```

## Error troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `cron error: unexpected end of JSON input` | Empty or invalid JSON | Check the JSON syntax |
| `invalid cron expression` | Bad cron expr | Use standard cron format like `"0 22 * * *"` |
| `agent_run.runner_id is required` | Missing runner_id | Add `"runner_id": "opencode"` to `agent_run` |
| `agent_run.task is required` | Missing task | Add the task text to `agent_run.task` |
| `job with id ... already exists` | Duplicate ID | Omit `id` to have one auto-generated |
| Job stays `paused` after creation | `enabled` was not set | Set `"enabled": true` in the JSON (or omit it — defaults to true) |
