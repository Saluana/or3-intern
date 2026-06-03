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
| `-s <conversation>` | Conversation identity to continue or group related work |
| `--approval-token <token>` | Attach a one-shot approval token |

## Conversation behavior

If you omit `-s`, the command uses the configured default conversation identity. It does not automatically create a guaranteed one-off random conversation every time.

Use an explicit conversation identity when you want:

- repeated scripted calls to share context
- a dedicated review or automation conversation
- predictable memory/history grouping

## Good uses

- shell scripts and automation
- CI or local task runners
- quick foreground questions without opening chat
- approval-token resume flows
