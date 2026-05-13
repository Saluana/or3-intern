# Files

The v1 file API is root-and-path based. It does not use opaque `file_id` routes for browsing.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/files/roots` | List available roots such as workspace, allowed folder, artifacts, or current directory |
| `GET /internal/v1/files/list` | List a directory under one root |
| `GET /internal/v1/files/search` | Search under a root |
| `GET /internal/v1/files/stat` | Fetch metadata for one path |
| `GET /internal/v1/files/read` | Read file content as text |
| `GET /internal/v1/files/download` | Download raw file content |
| `PUT /internal/v1/files/write` | Write or replace text content |
| `POST /internal/v1/files/upload` | Upload one file into a writable root |
| `POST /internal/v1/files/mkdir` | Create a directory |
| `POST /internal/v1/files/delete` | Disabled in v1 and returns `403` |

## Mental model

Every operation is scoped by:

- a `root_id`
- a relative path inside that root

The available roots depend on runtime config. Common IDs include `workspace`, `allowed`, `artifacts`, and sometimes `computer` or `cwd`.

## Why this matters

This model keeps file access bounded to configured roots instead of exposing arbitrary host paths or a global file-ID registry.

## Good UI pattern

1. call `files/roots`
2. let the user pick a root
3. use `list` / `search` / `stat`
4. use `read` or `download` depending on content type
5. use `write`, `upload`, or `mkdir` only in writable roots
