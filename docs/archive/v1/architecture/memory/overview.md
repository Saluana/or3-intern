# Memory Overview

OR3 Intern's memory system gives the agent long-term recall across chat sessions. It combines vector search, full-text search (FTS), lexical matching, and recency scoring to find relevant information. It also automatically summarizes old conversation messages into durable memory notes.

## What Gets Stored

Memory is stored in the `memory_notes` table (`internal/db/db.go:112`). Each row has:

- **text** — The note content
- **embedding** — A float32 vector blob for semantic search
- **embed_fingerprint** — Identifies which embedding model produced the vector
- **kind** — The type of memory (`summary`, `fact`, `preference`, `goal`, `procedure`, `decision`, `warning`, `episode`, `note`)
- **tags** — Comma-separated tags (e.g. `"consolidation"`)

Memory kinds are defined in `internal/db/store.go:19-31`.

## How It Works

1. **Consolidation** — After enough new messages accumulate in a chat session, the `Consolidator` sends the conversation transcript to an LLM. The LLM extracts durable facts, preferences, goals, procedures, decisions, and warnings. These are saved as typed memory notes with embeddings (`internal/memory/consolidate.go`).

2. **Retrieval** — When the agent needs context, the `Retriever` runs hybrid search: vector similarity via sqlite-vec, FTS via SQLite FTS5, lexical token overlap, and recency decay. Results are scored, deduplicated, and diversified (`internal/memory/retrieve.go`).

3. **Document Indexing** — Workspace `.md` and `.txt` files can be indexed into `memory_docs` for FTS retrieval. The `DocIndexer` walks configured roots and updates rows when files change (`internal/memory/docs.go`).

4. **Workspace Context** — On startup, the agent gathers context from workspace files, recent memory journal entries, and query-relevant files (`internal/memory/workspace_context.go`).

5. **Scheduler** — The `Scheduler` debounces consolidation runs per session so only one runs at a time. If a new trigger arrives while a run is in progress, it sets a dirty flag to re-run after the current one finishes (`internal/memory/scheduler.go`).

## Key Files

| File | Purpose |
|------|---------|
| `internal/memory/consolidate.go` | LLM-driven conversation summarization |
| `internal/memory/retrieve.go` | Hybrid retrieval (vector + FTS + lexical + recency) |
| `internal/memory/vector.go` | Vector packing, cosine similarity, vector search wrapper |
| `internal/memory/docs.go` | Document indexing and retrieval |
| `internal/memory/workspace_context.go` | Workspace context gathering |
| `internal/memory/scheduler.go` | Consolidation run scheduler |
| `internal/db/store.go` | Core DB operations for memory tables |
