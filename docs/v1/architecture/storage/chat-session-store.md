# Chat Session Store

The chat session store manages chat session metadata, message listing, session forking, and archive/rename operations.

Source: `internal/db/chat_session_store.go`

## Data Model

### ChatSessionMeta (`chat_session_store.go:24-46`)

Tracks all metadata about a chat session beyond the raw messages:

| Field | Description |
|-------|-------------|
| SessionKey | Primary key, links to `sessions.key` |
| HostID | The execution host |
| Title | User-friendly title |
| RunnerID | Which runner handles this session |
| RunnerLabel | Display label for the runner |
| RunnerChatSessionID | Linked runner chat session |
| RunnerContinuationMode | `"replay"` or other mode |
| RunnerModel | Model used by the runner |
| RunnerMode | Agent mode |
| RunnerIsolation | Isolation setting |
| RunnerCwd | Working directory |
| MessageCount | Number of messages in the session |
| LastMessagePreview | First 160 chars of the last message |
| LastMessageAt | Timestamp of the last message |
| ParentSessionKey | If this session was forked from another |
| ForkAnchorMessageID | Which message was the fork point |
| ForkedFromRunnerID | Runner ID of the source session |
| ForkStrategy | Fork mode (default: `"replay"`) |
| Archived | Whether the session is archived |
| CreatedAt / UpdatedAt | Timestamps in ms |

### ChatMessage (`chat_session_store.go:59-66`)

A lightweight message view for chat history paging:

```go
type ChatMessage struct {
    ID          int64
    SessionKey  string
    Role        string
    Content     string
    PayloadJSON string
    CreatedAt   int64
}
```

### ChatMessagePage (`chat_session_store.go:69-72`)

```go
type ChatMessagePage struct {
    Messages   []ChatMessage
    NextCursor int64  // 0 when no more pages
}
```

## Operations

### UpsertChatSessionMeta (`chat_session_store.go:97-155`)

Inserts or merges metadata. Uses `COALESCE(NULLIF(excluded.field,''), existing.field)` so empty fields in the input don't overwrite existing non-empty values. Runs in a transaction.

### GetChatSessionMeta (`chat_session_store.go:158-165`)

Returns metadata for a session. Returns `ErrChatSessionNotFound` if the row doesn't exist.

### ListChatSessions (`chat_session_store.go:168-208`)

Lists sessions with filtering:

```go
type ChatSessionListFilter struct {
    HostID         string
    RunnerID       string
    IncludeArchive bool
    OnlyArchived   bool
    Search         string  // matches title or last message preview
    Limit          int     // default: 50, max: 200
}
```

Results are ordered by `updated_at DESC`.

### RenameChatSession (`chat_session_store.go:211-224`)

Sets the session title. Returns `ErrChatSessionNotFound` if no rows were affected.

### ArchiveChatSession (`chat_session_store.go:227-244`)

Sets the `archived` flag. Default limit for unarchived-only listing.

### ListChatMessages (`chat_session_store.go:248-277`)

Returns a page of messages after a given `afterID`, in chronological order. Uses limit+1 to determine if there's a next page (returns the last ID as `NextCursor`). Default limit: 100, max: 500.

### ForkChatSession (`chat_session_store.go:296-451`)

Copies messages from a source session into a new session. Steps:

1. Validates the anchor message belongs to the source session.
2. If the anchor is an incomplete assistant message (status streaming/pending) and `AllowIncompleteAnchor` is false, returns `ErrForkAnchorIncomplete`.
3. If allowed, walks back to the last complete message before the anchor.
4. Copies all messages up to the effective anchor into the new session.
5. Strips sensitive payload keys (approval tokens, runner output, secrets, etc.) via `sanitizeForkPayload()`.
6. Inherits runner config from the source session metadata (model, mode, isolation, cwd).
7. Creates new `chat_session_meta` with fork ancestry recorded.

Sensitive keys stripped from forked payloads: `approval_token`, `approval_tokens`, `runner_output`, `raw_output`, `child_env`, `env`, `secrets`, `bearer`, `authorization`.

### ForkChatSessionRequest (`chat_session_store.go:280-291`)

```go
type ForkChatSessionRequest struct {
    SourceSessionKey      string
    NewSessionKey         string
    AnchorMessageID       int64
    TargetRunnerID        string
    Title                 string
    AllowIncompleteAnchor bool
    ForkStrategy          string  // default: "replay"
}
```

## Error Types

- `ErrChatSessionNotFound` — Session metadata row missing
- `ErrInvalidForkAnchor` — Anchor message doesn't exist in the source session
- `ErrForkAnchorIncomplete` — Anchor points to an in-progress assistant message
