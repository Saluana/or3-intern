# Workspace Context

Workspace context is gathered before each model call to give the agent awareness of relevant files in the workspace directory. It combines priority files, recent memory journal entries, and query-relevant file excerpts.

Source: `internal/memory/workspace_context.go`

## BuildWorkspaceContext

`BuildWorkspaceContext()` (`workspace_context.go:56-140`) is the main entry point. It takes a `WorkspaceContextConfig` and a query string, and returns a formatted context string (or empty string if no content is found).

### Configuration

```go
type WorkspaceContextConfig struct {
    WorkspaceDir string  // root directory
    MaxFileBytes int     // default: 12KB
    MaxResults   int     // default: 6
    MaxChars     int     // default: 1600
    Now          time.Time
}
```

### Candidate Sources

Results are gathered from three sources, in order:

1. **Priority Markdown Files** (`workspace_context.go:111-116`) — Always checked: `README.md`, `TODO.md`, `TASKS.md`, `PLAN.md`, `STATUS.md`, `NOTES.md`, `PROJECT.md`. These are statically included regardless of query.

2. **Recent Memory Journal Entries** (`workspace_context.go:117-119`) — Checks the `memory/` subdirectory of the workspace. Prefers today's and yesterday's journal entries (format `2006-01-02.md`). Falls back to the two most recently modified `.md` files in the directory.

3. **Query-Relevant Files** (`workspace_context.go:120-125`) — Walks the workspace (up to 200 files) looking for `.md` and `.txt` files whose content or path contains query tokens. Excludes hidden directories (`.*`, `node_modules`, `vendor`, `artifacts`).

### Scoring

For query-relevant files, scoring works through `workspaceExcerpt()` (`workspace_context.go:317-356`):
- Token found in file path: +6 points
- Token found in file content: +3 points per token
- A recency bonus is added via `workspaceRecencyScore()`:
  - <= 24 hours: +3
  - <= 7 days: +2
  - <= 30 days: +1
  - Older: 0

Results are sorted by score (descending), then mod time (newest first), then path.

### Caching

Results are cached for 5 seconds (`workspaceContextCacheTTL` at line 20). The cache key includes the root directory, query, and all config parameters. This avoids re-scanning the filesystem on every call within the same burst.

### Output Format

The output is a formatted string:

```
Startup workspace context gathered before the model call.
1) [relative/path.md] First line or excerpt from file...
2) [other/file.md] Content excerpt...
```

Each entry is limited to one line (320 chars) via `workspaceOneLine()` (`workspace_context.go:428-434`). The entire context is capped at `MaxChars` (default 1600) and truncated with `…[truncated]` if needed.

## File Safety

`workspaceSafePath()` (`workspace_context.go:301-315`) ensures files are within the workspace root by:
1. Resolving to absolute path
2. Following symlinks
3. Computing relative path from root
4. Rejecting paths that escape the root (e.g., `..` traversal)

## Token Extraction

`workspaceQueryTokens()` (`workspace_context.go:375-399`) extracts lowercase alphanumeric tokens (>= 3 chars) from the query, filtering out common stop words (`the`, `and`, `for`, `with`, `that`, `this`, `from`, `into`, `what`, `when`, `where`, `have`, `just`, `please`, `about`).

## Bootstrap File Exclusion

`isBootstrapWorkspaceFile()` (`workspace_context.go:410-418`) excludes system files (`SOUL.MD`, `AGENTS.MD`, `TOOLS.MD`, `IDENTITY.MD`, `MEMORY.MD`, `HEARTBEAT.MD`) from query-relevant search, since these are always known to the agent.
