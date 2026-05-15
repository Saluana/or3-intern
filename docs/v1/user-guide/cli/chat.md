# Chat

`chat` is the interactive terminal chat UI. It is also the default command when you run `or3-intern` with no command name.

```bash
or3-intern
or3-intern chat
```

## What it does

Chat uses the configured runtime, tools, memory, skills, and approvals. It is the easiest way to work locally with OR3 Intern in a terminal.

## Useful in-chat commands

The current chat flow includes useful slash commands such as:

- `/commands` — show available local chat commands
- `/status` — show message counts, context pressure, and related runtime details
- `/new` — archive the current session into memory and start a fresh live conversation
- `/exit` or `/quit` — leave chat

You can also leave with `Ctrl+C`.

## Sessions

Chat runs under a stable session identity so conversation history can build over time. Use `/new` when you want to preserve memory but clear the live thread for a fresh conversation.

## Before you start

If chat seems blocked by config or safety posture, run:

```bash
or3-intern status
or3-intern doctor
```
