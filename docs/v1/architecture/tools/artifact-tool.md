# Artifact Tool

Name: `read_artifact` | Capability: `safe` | Group: `read`

Reads content from an artifact that was created by a previous tool call. Artifacts are used when tool output is too large to return directly.

## Parameters

- `artifact_id` (required) - artifact identifier from a previous tool result
- `maxBytes` - max bytes to read (uses runtime default if not set)
- `offset` - byte offset to start reading from (for chunked reading)

Source: `internal/tools/artifact.go:10-57`

## When artifacts are created

Tools create artifacts when their output exceeds the configured byte budget. Examples:
- `read_file` with a very large file
- `web_fetch` with HTML content converted to Markdown
- Other tools that produce large results

The tool result includes an artifact ID that can be used with `read_artifact`.

## Reading artifacts

The read is session-scoped. Only artifacts from the same session can be read. The store supports capped reads (offset + max bytes) and reports whether the read was truncated (more content available).

Source: `internal/tools/artifact.go:47-56`

## Output format

```
artifact_id: <id>
session_key: <session>
mime: <content-type>
size_bytes: <total>
offset: <start>
read_bytes: <amount>

<content>...[truncated]
```

Source: `internal/tools/artifact.go:55-56`
