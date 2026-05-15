# Files Endpoints

The v1 file API is root-and-path based. It does not expose arbitrary host paths or opaque file IDs for browsing.

## Routes

| Route | Method | Purpose |
| --- | --- | --- |
| `/internal/v1/files/roots` | GET | List available roots |
| `/internal/v1/files/list` | GET | List a directory under a root |
| `/internal/v1/files/search` | GET | Search under a root |
| `/internal/v1/files/stat` | GET | Fetch metadata for one path |
| `/internal/v1/files/read` | GET | Read text content |
| `/internal/v1/files/download` | GET | Download raw file content |
| `/internal/v1/files/write` | PUT | Write or replace text content |
| `/internal/v1/files/upload` | POST | Upload one file into a writable root |
| `/internal/v1/files/mkdir` | POST | Create a directory |
| `/internal/v1/files/delete` | POST | Disabled in v1; returns `403` |

## Common Query Fields

- `root_id` — one of the roots returned by `files/roots`
- `path` — relative path inside that root
- route-specific fields such as `q`, `limit`, `offset`, or `max_bytes`

Common root IDs include `workspace`, `allowed`, `artifacts`, `computer`, and `cwd`, depending on config.

## Read Example

```http
GET /internal/v1/files/read?root_id=workspace&path=notes/todo.md
```

```json
{
  "root_id": "workspace",
  "path": "notes/todo.md",
  "name": "todo.md",
  "mime_type": "text/markdown",
  "size": 1204,
  "writable": true,
  "content": "# Todo\n"
}
```

## Write Model

Write, upload, and mkdir are allowed only inside writable roots. Path resolution rejects absolute paths, `..` escapes, and symlink escapes from the selected root.

Deletion is intentionally disabled in v1.
