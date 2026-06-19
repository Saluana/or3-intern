---
name: runner
description: Diagnose runner availability, check configuration, and detect which external AI coding assistants are installed.
---

# Runner

Check which external AI coding assistants (runners) are available, verify their configuration, and troubleshoot setup issues. Use this when the agent or cron skills fail with "runner not found" or when you need to confirm which runners work.

## Check available runners

```bash
or3-intern status --runners
```

Sample output:
```
opencode    available
codex       missing (not found on PATH)
claude      available
gemini      missing (not found on PATH)
```

## Check runner configuration

```bash
grep -A10 '"runners"' ~/.or3-intern/config.json
```

Sample output:
```json
"runners": {
  "default": "opencode",
  "disabledRunners": [],
  "defaultMode": "safe_edit",
  "defaultIsolation": "host_workspace_write",
  "maxConcurrent": 1,
  "maxQueued": 16,
  "defaultTimeoutSeconds": 900
}
```

## Key config fields

| Field | Default | Description |
|-------|---------|-------------|
| `runners.default` | — | Runner used when none is specified (`"opencode"`, `"codex"`, etc.) |
| `runners.disabledRunners` | `[]` | Array of runner IDs to disable |
| `runners.defaultMode` | `"safe_edit"` | Default mode for runner runs |
| `runners.defaultIsolation` | `"host_workspace_write"` | Default isolation level |
| `runners.maxConcurrent` | `1` | How many runs execute at once |
| `runners.defaultTimeoutSeconds` | `900` | Default timeout per run |

## Supported runners

| Runner ID | Binary | Notes |
|-----------|--------|-------|
| `opencode` | `opencode` | Primary/recommended runner |
| `codex` | `codex` | OpenAI Codex CLI |
| `claude` | `claude` | Anthropic Claude Code |
| `gemini` | `gemini` | Google Gemini CLI |

## Diagnose a missing runner

If a runner shows as "missing (not found on PATH)", check:

```bash
# Is the binary installed?
which opencode  # or codex, claude, gemini

# If not found, tell the user to install it
# opencode: https://github.com/github/opencode
# codex: pip install codex-cli
# claude: npm install -g @anthropic/claude-code
```

## Run detailed diagnostics

```bash
or3-intern doctor --runners
```

This shows version info, auth status, and compatibility for each detected runner.

## Set a default runner

```bash
or3-intern configure --set runners.default=opencode
```

## Disable a runner

```bash
or3-intern configure --set runners.disabledRunners='["codex","claude"]'
```

## Verify runner can execute tasks

```bash
or3-intern agent -m "Say hello and respond with 'Runner is working'"
```

## See also

- **agent** skill: run one-shot tasks on a runner
- **cron** skill: schedule runner tasks on a timer
