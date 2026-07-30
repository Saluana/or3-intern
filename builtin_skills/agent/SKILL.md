---
name: agent
description: Run one-shot runner agent tasks — fire and forget, or wait for results.
---

# Agent

Run a one-shot task on a runner. Use this when someone says "run X" or "do Y" — it delegates work to a runner (opencode, codex, claude, gemini) and waits for the result.

Unlike cron jobs and heartbeat (which run on schedules), agent runs are **immediate**: you create them, the runner executes, and you get the result.

## Two ways to run an agent

| Method | Best for | Command |
|--------|----------|---------|
| CLI (`or3-intern agent`) | Simple one-shot tasks from the terminal | `or3-intern agent -m "task"` |
| API (`POST /internal/v1/runner-runs`) | More control (mode, isolation, model, timeout) | `curl` with auth token |

## Method 1: CLI (simplest)

The `or3-intern agent` command runs one turn on the configured runner.

### Basic usage

```bash
or3-intern agent -m "Review all uncommitted changes in this workspace"
```

The runner executes the task, produces output, and the result is sent back through whatever channel the user is using (CLI, telegram, slack, etc.).

### With a custom session key

```bash
or3-intern agent -m "Summarize the latest merge request" -s "session:review-42"
```

The session key keeps related conversations together. If omitted, the default session key from config is used.

### Forcing a specific runner

The `or3-intern agent` command uses whatever runner is configured as default. If you need a specific runner, note that the CLI does not accept a `--runner` flag. The session key must be configured to use the desired runner. See the `runner` skill for diagnostics.

### Prerequisites (CLI)

- `or3-intern` must be configured with a valid runner (check: `or3-intern status --runners`)
- A default runner must be set in config: `runners.default` (e.g., `"opencode"`)
- The runner binary must be on PATH (check: `which opencode`)

### Expected behavior

The CLI blocks until the runner completes. Output is displayed in the terminal. If the task takes too long, it may time out (default: 900 seconds).

## Method 2: Service API (more control)

Use the runner runs API for fine-grained control over mode, isolation, model, timeout, and working directory.

### Prerequisites (API)

Same as the cron skill:
1. `or3-intern service` must be running on `127.0.0.1:9100` (default)
2. Service secret with operator role: `or3-intern configure --set service.sharedSecretRole=operator`
3. You need the service secret from `~/.or3-intern/config.json`

### Setup: Get the auth token

```bash
TOKEN=$(python3 -c "
import hmac, json, base64, os, time
secret = 'REPLACE_WITH_YOUR_SECRET'
iat = int(time.time())
nonce = os.urandom(12).hex()
claims = json.dumps({'iat': iat, 'nonce': nonce}).encode()
payload = base64.urlsafe_b64encode(claims).rstrip(b'=').decode()
sig = hmac.new(secret.encode(), payload.encode(), 'sha256').hexdigest()
print(f'{payload}.{sig}')
")
PORT=9100
```

### Create a runner run (basic)

```bash
curl -s -X POST "http://127.0.0.1:${PORT}/internal/v1/runner-runs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{
    "parent_session_key": "agent:cli",
    "runner_id": "opencode",
    "task": "Review the code in src/ for any bugs",
    "mode": "review",
    "isolation": "host_readonly"
  }'
```

Expected response (HTTP 202):
```json
{
  "job_id": "job-runner-ab12cd34ef56",
  "run_id": "rr_ab12cd34ef567890",
  "status": "queued"
}
```

### Create a runner run (full options)

```bash
curl -s -X POST "http://127.0.0.1:${PORT}/internal/v1/runner-runs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{
    "parent_session_key": "agent:code-review",
    "runner_id": "opencode",
    "task": "Run a full security audit on the repository",
    "timeout_seconds": 1800,
    "cwd": "/home/user/projects/myapp",
    "model": "claude-sonnet-4-20250514",
    "mode": "review",
    "isolation": "host_readonly",
    "max_turns": 10,
    "meta": {
      "trigger": "user_request",
      "priority": "high"
    }
  }'
```

### Check run status

```bash
curl -s "http://127.0.0.1:${PORT}/internal/v1/runner-runs/RUN_ID" \
  -H "Authorization: Bearer ${TOKEN}"
```

Replace `RUN_ID` with the `run_id` from the create response.

Expected response:
```json
{
  "id": "rr_ab12cd34ef567890",
  "status": "running",
  "runner_id": "opencode",
  "task": "Review the code in src/ for any bugs",
  "requested_at": 1718233200000,
  "started_at": 1718233201000
}
```

Status values: `queued`, `starting`, `running`, `succeeded`, `failed`, `aborted`, `timed_out`, `approval_required`.

### List recent runs

```bash
curl -s "http://127.0.0.1:${PORT}/internal/v1/runner-runs?limit=10" \
  -H "Authorization: Bearer ${TOKEN}"
```

Filter by session:
```bash
curl -s "http://127.0.0.1:${PORT}/internal/v1/runner-runs?parent_session_key=agent:code-review" \
  -H "Authorization: Bearer ${TOKEN}"
```

Filter by status:
```bash
curl -s "http://127.0.0.1:${PORT}/internal/v1/runner-runs?status=running" \
  -H "Authorization: Bearer ${TOKEN}"
```

Expected response:
```json
{
  "items": [
    { "id": "rr_ab12cd34ef567890", "status": "succeeded", ... },
    { "id": "rr_ff11aa22bb33cc44", "status": "failed", ... }
  ]
}
```

### Get run events (stdout, stderr, tool calls)

```bash
curl -s "http://127.0.0.1:${PORT}/internal/v1/runner-runs/RUN_ID/events" \
  -H "Authorization: Bearer ${TOKEN}"
```

Expected response:
```json
{
  "events": [
    { "type": "started", "timestamp_ms": 1718233201000 },
    { "type": "stdout", "data": "Analyzing source files...", "timestamp_ms": 1718233202000 },
    { "type": "completion", "data": "{\"result\": \"No bugs found\"}", "timestamp_ms": 1718233250000 }
  ]
}
```

## Agent run fields reference

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `parent_session_key` | yes | — | Which session to associate this run with |
| `runner_id` | yes | — | `"opencode"`, `"codex"`, `"claude"`, `"gemini"` |
| `task` | yes | — | The prompt/task for the runner |
| `timeout_seconds` | no | 900 | Max seconds before timeout |
| `cwd` | no | workspace | Absolute path to working directory |
| `model` | no | config default | Model override |
| `mode` | no | `"safe_edit"` | `"review"`, `"safe_edit"`, `"sandbox_auto"` |
| `isolation` | no | `"host_workspace_write"` | `"host_readonly"`, `"host_workspace_write"`, `"sandbox_workspace_write"`, `"sandbox_dangerous"` |
| `max_turns` | no | — | Max conversation turns |
| `meta` | no | — | Arbitrary key-value metadata |

## Mode + isolation combinations

| Use case | mode | isolation |
|----------|------|-----------|
| Read-only code review | `"review"` | `"host_readonly"` |
| Edit files in workspace | `"safe_edit"` | `"host_workspace_write"` |
| Dangerous operations | `"sandbox_auto"` | `"sandbox_dangerous"` |

## Common scenarios

### "Run a code review on src/"

```bash
or3-intern agent -m "Review all files in src/ for bugs, performance issues, and security vulnerabilities. Be thorough."
```

### "Summarize the latest git log"

```bash
or3-intern agent -m "Show me the last 10 git commits and summarize what changed"
```

### "Run a task on another runner with specific settings"

```bash
curl -s -X POST "http://127.0.0.1:${PORT}/internal/v1/runner-runs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{
    "parent_session_key": "agent:deployment",
    "runner_id": "codex",
    "task": "Deploy the staging branch to the staging server",
    "mode": "safe_edit",
    "isolation": "host_workspace_write",
    "max_turns": 5
  }'
```

## Check available runners

```bash
or3-intern status --runners
```

Sample output:
```
opencode    available
codex       available
claude      missing (not found on PATH)
gemini      missing (not found on PATH)
```

## Error troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| "runner orchestration unavailable" | Runner manager not initialized | Ensure a runner is configured and the binary is on PATH |
| "unknown command: agent" | Very old version | Upgrade or3-intern |
| Run stays `queued` forever | Worker pool is full | Wait for a running task to complete (default: 1 concurrent worker) |
| Run stays `running` but no output | Runner is stuck or waiting | Abort the run and retry with a simpler task |
| `404` on runner-runs | Wrong run ID | Double-check the `run_id` from the create response |
| `401 Unauthorized` | Token expired | Generate a fresh token |

## See also

- **cron** skill: schedule agent tasks to run on a timer
- **runner** skill: diagnose runner availability and configuration
