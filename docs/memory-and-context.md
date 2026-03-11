# Memory and context

## Persistent history and retrieval

`or3-intern` stores conversation history in SQLite and uses a hybrid retrieval pipeline for long-term context.

The README describes the retrieval stack as:

- pinned context
- vector similarity search
- FTS keyword search

This is meant to keep retrieval precise without needing to scan full histories on every turn.

## Context sources

A turn can draw context from several places:

- session history stored in SQLite
- `IDENTITY.md` for stable agent identity
- `MEMORY.md` for standing context and preferences
- document index excerpts from configured roots
- autonomous task context from `HEARTBEAT.md`
- scope-linked session history when session scopes are used

## Bootstrap files

Three markdown files are especially important:

- `IDENTITY.md` — the agent's role, style, and identity
- `MEMORY.md` — standing facts and preferences that should always be available
- `HEARTBEAT.md` — autonomous work instructions used only for heartbeat, cron, webhook, and file-watch turns

`HEARTBEAT.md` is reread for each autonomous turn so edits apply without restarting `serve`.

## Document index

The optional document index lets the runtime retrieve relevant excerpts from local files and inject them into the prompt.

Supported configuration keys include:

- `docIndex.enabled`
- `docIndex.roots`
- `docIndex.maxFiles`
- `docIndex.maxFileBytes`
- `docIndex.maxChunks`
- `docIndex.embedMaxBytes`
- `docIndex.refreshSeconds`
- `docIndex.retrieveLimit`

Supported file types in the README include:

- `.md`, `.txt`
- `.go`, `.py`, `.js`, `.ts`
- `.json`, `.yaml`, `.toml`
- `.sh`

## Consolidation

The top-level config also exposes message consolidation settings such as:

- `consolidationEnabled`
- `consolidationWindowSize`
- `consolidationMaxMessages`
- `consolidationMaxInputChars`
- `consolidationAsyncTimeoutSeconds`

These settings help keep long-running sessions manageable.

## Session scopes

Session scopes let multiple session keys share one conversation history.

Example use cases:

- link a Telegram chat and a Discord channel to the same project scope
- keep a shared memory thread across multiple delivery channels
- move work between channels without losing context

Commands:

```bash
or3-intern scope link telegram:12345 my-project
or3-intern scope link discord:67890 my-project
or3-intern scope list my-project
or3-intern scope resolve telegram:12345
```

## Related documentation

- [Agent runtime](agent-runtime.md)
- [CLI reference](cli-reference.md)
- [Triggers and automation](triggers-and-automation.md)

## Related code

- `internal/memory/`
- `internal/db/`
- `internal/scope/`
