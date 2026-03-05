# or3-intern (v1)

Go rewrite of nanobot with SQLite persistence + hybrid long-term memory retrieval.

## Quick start

1) Create config (auto-created on first run):
```bash
or3-intern -config ~/.or3-intern/config.json version
```

2) Set provider key:
```bash
export OR3_API_KEY="..."
# or OPENAI_API_KEY if using default config
```

3) Run interactive chat:
```bash
or3-intern chat
```

## Commands

- `or3-intern chat` interactive CLI
- `or3-intern agent -m "hello"` one-shot
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`

## Notes

- Uses SQLite with WAL + single-connection for deterministic low-RAM operation.
- History is always fetched with `LIMIT` and never full-scanned.
- Hybrid memory retrieval: pinned + vector (cosine) + FTS keyword search.

## Dependencies

This repo uses external Go modules (SQLite driver + cron parser). If you're building in an offline environment, you must vendor modules ahead of time.
