# Migrate OpenClaw

`migrate-openclaw` imports selected files and memory content from a local OpenClaw agent directory.

```bash
or3-intern migrate-openclaw ~/.openclaw/agents/main
or3-intern migrate-openclaw --scope global ~/agent-export
```

## What it imports

The current command imports:

- `SOUL.md`
- `IDENTITY.md`
- `MEMORY.md`
- `USER.md`
- OpenClaw daily notes and dreams as durable memory notes

It does not promise a full migration of sessions, secrets, runtime config, or managed skills.

## Flags

| Flag | Description |
| --- | --- |
| `--scope <scope-key>` | Memory scope for imported daily notes; defaults to global shared memory |
| `--embed-max-bytes <n>` | Maximum bytes per imported memory chunk before embedding |

## Operational notes

- the command is repeatable
- previously imported OpenClaw daily notes for the same source agent are replaced before fresh notes are inserted
- after large imports, `or3-intern embeddings status` or `or3-intern embeddings rebuild memory` can help verify memory vectors are aligned with the current provider/model
