# File Operations Tools

Three tools handle file operations: read, write, and directory listing.

## read_file

Name: `read_file` | Capability: `safe` | Group: `read`

Reads a UTF-8 text file from the allowed workspace.

Parameters:
- `path` (required) - file path inside the allowed read root
- `mode` - one of: preview, full, range, grep, outline (default: preview)
- `startLine`, `endLine` - for range mode (1-based, inclusive)
- `pattern` - for grep mode (substring or regex)
- `maxBytes` - max bytes returned (default: 65536)

Source: `internal/tools/files.go:220-275`

Modes:
- **preview** - first `maxBytes` bytes of the file
- **full** - same as preview but with truncation advice
- **range** - specific line range
- **grep** - lines matching a pattern
- **outline** - lines starting with `#`, `func `, `type `, `class `, or `def `

## search_file

Name: `search_file` | Capability: `safe` | Group: `read`

Searches one file for a pattern and returns matching lines.

Parameters:
- `path` (required) - file to search
- `pattern` (required) - regex or literal substring
- `maxBytes` - max output bytes (default: 65536)

Source: `internal/tools/files.go:312-344`

## write_file

Name: `write_file` | Capability: `guarded` | Group: `write`

Creates or replaces a file in the allowed write root.

Parameters:
- `path` (required) - destination path
- `content` (required) - complete new contents
- `mkdirs` (boolean) - create parent directories

Source: `internal/tools/files.go:346-384`

## edit_file

Name: `edit_file` | Capability: `guarded` | Group: `write`

Edits a file by applying find/replace operations. Max file size: 10 MB.

Parameters:
- `path` (required) - existing file path
- `edits` (required) - array of {find, replace, count} objects

Source: `internal/tools/files.go:386-456`

## list_dir

Name: `list_dir` | Capability: `safe` | Group: `read`

Lists files and folders in a directory. Sorted with directories first.

Parameters:
- `path` (required) - directory path
- `max` - max entries (default: 80)

Source: `internal/tools/files.go:458-537`

## Path safety

All file tools validate paths against a root directory. The root is resolved to its canonical path (symlinks followed) before checking. The path validation prevents traversal with `..` and symlink escapes using `os.SameFile` checks after opening.

Source: `internal/tools/files.go:28-101` (safePath, safeWritePath, safePathForRoot, validatePathInRoot)
