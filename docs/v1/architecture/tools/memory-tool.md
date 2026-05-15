# Memory Tools

Five tools manage long-term memory for AI agents.

## memory_set_pinned

Name: `memory_set_pinned` | Capability: `safe` | Group: `memory, read`

Stores a durable key-value fact that appears in every prompt. For persistent facts, rules, or user preferences.

Parameters:
- `key` (required) - short key like "user_name" or "coding_style"
- `content` (required) - the fact or rule
- `scope` - optional: "global" to share across sessions

Source: `internal/tools/memory.go:16-48`

## memory_add_note

Name: `memory_add_note` | Capability: `safe` | Group: `memory, read`

Adds a semantic memory note with embedding-based retrieval. The note text is embedded using the configured embedding model and stored for vector search.

Parameters:
- `text` (required) - the memory note
- `tags` - comma-separated tags for filtering
- `source_message_id` - message this note came from
- `scope` - optional: "global" to share across sessions

Source: `internal/tools/memory.go:50-98`

## memory_search

Name: `memory_search` | Capability: `safe` | Group: `memory, read`

Searches memory using hybrid semantic (vector) and keyword (FTS) retrieval.

Parameters:
- `query` (required) - topic or fact to find
- `topK` - max results
- `scope` - optional: "global" to search only shared memory

Source: `internal/tools/memory.go:100-154`

Uses a `memory.Retriever` with configurable vector K, FTS K, and top-K limits.

## memory_recent

Name: `memory_recent` | Capability: `safe` | Group: `memory, read`

Fetches recent conversation messages from the linked session scope. For immediate conversational context, not durable storage.

Parameters:
- `limit` - number of recent messages

Source: `internal/tools/memory.go:156-190`

## memory_get_pinned

Name: `memory_get_pinned` | Capability: `safe` | Group: `memory, read`

Reads pinned memory entries for the current session (including global ones).

Parameters:
- `key` - optional: fetch only one key
- `scope` - optional: "global" for shared memory only

Source: `internal/tools/memory.go:192-237`

## Scope handling

All memory tools accept a `scope` parameter. If set to "global", the operation targets shared memory visible to all sessions. Otherwise, it targets the current session's scope (from context).

Source: `internal/tools/memory.go:239-244` (memoryScopeFromParams)

## Text compaction

Memory text is compacted (whitespace normalized) and truncated with "..." when exceeding the character limit.

Source: `internal/tools/memory.go:260-269` (compactMemoryText)
