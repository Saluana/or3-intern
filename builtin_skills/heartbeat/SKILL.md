---
name: heartbeat
description: Manage the autonomous heartbeat ticker — recurring tasks that or3-intern checks on a timer.
---

# Heartbeat

The heartbeat is a timer that wakes or3-intern on a regular interval (default: every 30 minutes) and tells the runner to review a task file called `HEARTBEAT.md`. Use this when someone wants standing instructions checked automatically — "keep an eye on this every time you check in."

## How it works

1. A timer fires every N minutes (configurable, minimum 1 minute)
2. or3-intern reads the `HEARTBEAT.md` file from the workspace (or configured path)
3. If the file contains active instructions (bullet points, numbered lists, plain text), the runner is triggered
4. The runner reads the instructions and acts on them
5. Headings (`#` lines) and HTML comments (`<!-- -->`) are ignored — only real content counts

## Prerequisites

- `or3-intern serve` must be running (heartbeat only runs during `serve`, not `chat` or `service`)
- Heartbeat must be enabled in config (disabled by default)

## Where HEARTBEAT.md lives

The file is searched in this order. The first one found wins:

1. `<workspace>/HEARTBEAT.md` (uppercase)
2. `<workspace>/heartbeat.md` (lowercase)
3. The path configured in `~/.or3-intern/config.json` under `heartbeat.tasksFile`

The workspace is usually the directory where or3-intern was started.

## Edit the HEARTBEAT.md file

HEARTBEAT.md is just a markdown file. The runner should read it, modify it, or create it using normal file operations.

### Add a standing instruction

```markdown
# Heartbeat

## Keep an eye on

- Check for uncommitted changes and report anything important
- Review any failed test runs in the last 24 hours
- Surface any blockers the user should know about
```

**Important rules:**
- Bullet items (`- text`) count as active instructions
- Numbered lists count as active instructions
- Plain paragraphs count as active instructions
- Headings (`#`, `##`, etc.) are ignored
- HTML comments (`<!-- text -->`) are ignored
- Empty lines are ignored
- If the file contains ONLY headings and comments, the heartbeat is silent (no runner trigger)

### Remove an instruction

Delete the line(s). For example, to stop checking test runs:

```markdown
# Heartbeat

## Keep an eye on

- Check for uncommitted changes and report anything important
- Surface any blockers the user should know about
```

### Silence the heartbeat temporarily (without disabling it)

Wrap the active content in HTML comments:

```markdown
# Heartbeat

<!--
- Check for uncommitted changes
- Review any failed tests
-->
```

Now the file has only headings and comments — the heartbeat will fire but the runner will see nothing to do and stay quiet.

### Add structured tasks (JSON format)

For more complex automation, use a JSON object with a `"tasks"` array:

````markdown
# Heartbeat

```json
{
  "tasks": [
    { "description": "Check for uncommitted changes", "priority": "high" },
    { "description": "Run the test suite and report results", "priority": "medium" }
  ]
}
```
````

The runner will receive these as structured tasks instead of free text.

## Enable or disable the heartbeat

The heartbeat is **disabled by default**. Enable it by editing the config file.

### Check current status

```bash
grep -A5 '"heartbeat"' ~/.or3-intern/config.json 2>/dev/null || echo "Not found in config"
```

Output if enabled:
```json
"heartbeat": {
  "enabled": true,
  "intervalMinutes": 30,
  "tasksFile": "/path/to/HEARTBEAT.md",
  "sessionKey": "heartbeat:default"
}
```

### Enable the heartbeat

Edit `~/.or3-intern/config.json` and set `"heartbeat.enabled"` to `true`:

```json
{
  "heartbeat": {
    "enabled": true,
    "intervalMinutes": 30,
    "tasksFile": "HEARTBEAT.md",
    "sessionKey": "heartbeat:default"
  }
}
```

Or use the configure CLI:

```bash
or3-intern configure --set heartbeat.enabled=true
or3-intern configure --set heartbeat.intervalMinutes=30
```

Then restart `or3-intern serve`.

### Disable the heartbeat

```bash
or3-intern configure --set heartbeat.enabled=false
```

Or set `"enabled": false` in the config file directly. Then restart `or3-intern serve`.

## Change the check interval

Default: 30 minutes. Minimum: 1 minute.

```json
{
  "heartbeat": {
    "intervalMinutes": 15
  }
}
```

Or via CLI:

```bash
or3-intern configure --set heartbeat.intervalMinutes=15
```

Then restart `or3-intern serve`.

## Change the session key

Default: `"heartbeat:default"`. The session key keeps heartbeat history separate from chat history.

```json
{
  "heartbeat": {
    "sessionKey": "heartbeat:my-project"
  }
}
```

## Set a custom tasks file path

By default, the workspace `HEARTBEAT.md` is used. You can point to any file:

```json
{
  "heartbeat": {
    "tasksFile": "/home/user/.or3-intern/standing-tasks.md"
  }
}
```

## Verify the heartbeat is working

```bash
# After starting or3-intern serve, check status
or3-intern status --advanced
```

Look for output containing `heartbeat=%t` (enabled) or `heartbeat=%f` (disabled).

## Template: Complete HEARTBEAT.md for daily monitoring

```markdown
# Heartbeat

Use this note for standing work OR3 should review every time it checks in.

## Keep an eye on

- Review the most important open work and anything overdue.
- Look for blockers, urgent issues, or follow-ups that need attention.
- Surface the clearest next steps when something important changes.

## How to work

- Keep updates short, practical, and easy to act on.
- Ignore anything already finished or no longer relevant.
- If there is nothing important to do, stay quiet.
```

## Common scenarios

### "Check my inbox every 2 hours during work hours"

```json
{
  "heartbeat": {
    "enabled": true,
    "intervalMinutes": 120
  }
}
```

Then in `HEARTBEAT.md`:

```markdown
# Heartbeat

- Check for any urgent emails and summarize them
- Flag anything that needs a response within the next 2 hours
```

### "Run a health check every 15 minutes"

```json
{
  "heartbeat": {
    "enabled": true,
    "intervalMinutes": 15
  }
}
```

Then in `HEARTBEAT.md`:

```markdown
# Heartbeat

- Check if the web server is responding
- Check disk space usage
- Report if anything is abnormal
```

## Error troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Heartbeat never fires | Disabled in config | Set `"enabled": true` in `~/.or3-intern/config.json` |
| Heartbeat fires but runner does nothing | HEARTBEAT.md has only headings/comments | Add bullet items or text (not just `#` lines) |
| Heartbeat fires too often | `intervalMinutes` is too low | Increase it (minimum is 1 minute) |
| Heartbeat file not found | Wrong path | Check `tasksFile` config or create `HEARTBEAT.md` in the workspace |
| "I want heartbeat to stop" | User wants to disable it | Set `"enabled": false` in config, or replace all HEARTBEAT.md content with comments |
