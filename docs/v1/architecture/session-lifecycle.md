# Session Lifecycle

Sessions represent conversations with the agent. They go through these stages:

## Stages

- **Created** — a new conversation starts. A session key is generated.
- **Active** — messages are being exchanged. The session is in use.
- **Archived** — the session is old or too large. It is stored but not in active memory.
- **Deleted** — the session is permanently removed.

## Automatic Management

Sessions are managed automatically based on age and size. Old sessions get archived. Very old sessions may be deleted. This keeps the database from growing forever.

## Scopes

Sessions can be linked with "scopes." A scope is a shared memory space. Multiple sessions in the same scope can access the same memories. This lets the agent remember things across different conversations, even on different devices.

## Manual Control

You can archive or delete sessions manually with the CLI. You can also change the scope of a session.
