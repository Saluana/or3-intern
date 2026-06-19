---
name: reminders
description: Create one-shot and recurring reminders using the or3-intern cron CLI.
---

# Reminders

Create reminders that fire at a specific time or on a recurring schedule. Use this when someone says "remind me to X" — it is a simpler wrapper around the cron skill.

Reminders are cron jobs with `kind: at` (one-shot) or `kind: cron`/`kind: every` (recurring) schedules. When a reminder fires, it tells the runner to deliver the message to the user.

## How it works

1. You create a reminder with `or3-intern cron add --json`
2. At the scheduled time, or3-intern runs the task
3. The runner delivers the reminder message

No auth tokens, no service setup, no curl required.

## CLI commands

### "Remind me in 20 minutes to call Sarah" (one-shot)

```bash
# Calculate "20 minutes from now" in epoch milliseconds
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
      "task": "Remind the user: it is time to call Sarah. Be brief and friendly."
    }
  }
}
ENDJSON
)"
```

Output:
```
id:       a1b2c3d4e5
name:     Call Sarah
enabled:  true
schedule: at 2026-06-18T15:20:00-07:00
runner:   opencode
task:     Remind the user: it is time to call Sarah. Be brief and friendly.
next:     2026-06-18T15:20:00-07:00
last:     - (-)
```

### "Remind me every day at 10pm to change the bed sheets" (recurring)

```bash
or3-intern cron add --json '{
  "name": "Change bed sheets",
  "schedule": { "kind": "cron", "expr": "0 22 * * *" },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Remind the user: time to change the bed sheets!"
    }
  }
}'
```

### "Remind me every hour to stand up" (interval-based)

```bash
or3-intern cron add --json '{
  "name": "Stand up reminder",
  "schedule": { "kind": "every", "every_ms": 3600000 },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Remind the user: stand up, stretch, and take a quick break!"
    }
  }
}'
```

### "Remind me every weekday at 9am" (cron expression)

```bash
or3-intern cron add --json '{
  "name": "Morning check-in",
  "schedule": { "kind": "cron", "expr": "0 9 * * 1-5", "tz": "America/New_York" },
  "payload": {
    "kind": "runner_run",
    "agent_run": {
      "runner_id": "opencode",
      "task": "Good morning! Time for your daily check-in."
    }
  }
}'
```

### List all reminders

```bash
or3-intern cron list
```

### Delete a reminder

```bash
or3-intern cron remove a1b2c3d4e5
```

Output: `removed a1b2c3d4e5`

### Cancel all reminders on a topic

List first to find the IDs, then delete each one:

```bash
# Find bed-related reminders
or3-intern cron list | grep -i bed | awk '{print $1}'
```

Then delete each ID:

```bash
or3-intern cron remove REPLACE_WITH_JOB_ID
```

## Time conversion reference

| You want | Calculation |
|----------|-------------|
| "in N minutes" | `python3 -c "import time; print(int((time.time() + N*60) * 1000))"` |
| "in N hours" | `python3 -c "import time; print(int((time.time() + N*3600) * 1000))"` |
| "tomorrow at 10am" | `python3 -c "from datetime import datetime,timedelta;t=datetime.now()+timedelta(days=1);t=t.replace(hour=10,minute=0,second=0,microsecond=0);print(int(t.timestamp()*1000))"` |
| "next Monday at 9am" | `python3 -c "from datetime import datetime,timedelta;today=datetime.now();da=(7-today.weekday()+0)%7 or 7;t=(today+timedelta(days=da)).replace(hour=9,minute=0,second=0,microsecond=0);print(int(t.timestamp()*1000))"` |

## See also

For advanced scheduling (custom runner options, meta data, service API), see the **cron** skill.
