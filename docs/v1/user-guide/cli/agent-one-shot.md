# Agent (One-Shot)

`agent` runs one foreground turn and exits. It is the non-interactive counterpart to `chat`.

```bash
or3-intern agent -m "What is the weather in Tokyo?"
or3-intern agent -m "Summarize this file" -s review
```

## Supported flags

| Flag | Description |
| --- | --- |
| `-m <message>` | Message to send to the agent. Required. |
| `-s <session>` | Session key to continue or group work under one session identity |
| `--approval-token <token>` | Attach a one-shot approval token |

## Session behavior

If you omit `-s`, the command uses the configured default session key. It does not automatically create a guaranteed one-off random session every time.

Use an explicit session key when you want:

- repeated scripted calls to share context
- a dedicated review or automation session
- predictable memory/history grouping

## Good uses

- shell scripts and automation
- CI or local task runners
- quick foreground questions without opening chat
- approval-token resume flows
