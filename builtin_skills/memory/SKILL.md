---
name: memory
description: Recall and store durable OR3 memory through the or3-intern memory CLI.
---

# Memory

Use OR3 memory when you need durable facts, preferences, or decisions that should survive across turns and sessions.

## Injected context vs active recall

OR3 may inject `memory_context` blocks (pinned, retrieved, digest) into compiled prompts. On native continuation follow-up turns, a bounded memory refresh may also appear before the user task.

Use active recall when:

- You need a fact that may not be in the current prompt.
- You want to verify what OR3 remembers before acting.
- You are about to store something durable.

Rely on injected context when the needed fact is already visible in the prompt.

## Session vs global scope

- Default commands scope memory to the session key you pass with `--session`.
- Add `--global` for facts that should be shared across sessions (preferences, identity, cross-project lessons).

## Durable notes (`add-note`)

Store only durable information: preferences, decisions, facts, and project lessons.

Do not store scratch work, transient plans, secrets, API keys, or raw command output.

```bash
or3-intern memory add-note --session cli:default "User prefers concise answers"
or3-intern memory add-note --session cli:default --global --tags preference "Timezone is Pacific"
```

JSON output:

```json
{"id": 42, "warning": ""}
```

## Recall (`search`)

```bash
or3-intern memory search --session cli:default --top-k 5 "deployment preference"
or3-intern memory search --session cli:default --global "timezone"
```

JSON output:

```json
{"hits": [{"source": "note", "score": 0.91, "text": "..."}], "warning": ""}
```

When embeddings are unavailable, search still works with keyword recall and returns a `warning` field.

## Pinned memory

Pinned keys are always eligible for prompt injection. Use them sparingly for facts that must appear in future prompts.

```bash
or3-intern memory pinned get --session cli:default
or3-intern memory pinned get --session cli:default --key project_name
or3-intern memory pinned set --session cli:default --key project_name "or3-intern"
or3-intern memory pinned set --session cli:default --global --key timezone "America/Los_Angeles"
```

`pinned get` JSON:

```json
{"entries": [{"key": "project_name", "content": "or3-intern"}]}
```

`pinned set` JSON:

```json
{"ok": true}
```
