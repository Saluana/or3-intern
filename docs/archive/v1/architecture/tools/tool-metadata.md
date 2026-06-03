# Tool Metadata Format

Each tool in the registry has metadata describing its groups and capabilities.

## ToolMetadata struct

```go
type ToolMetadata struct {
    Groups       []string  // e.g., ["read"], ["exec"], ["skills", "exec"]
    Capabilities []string  // e.g., ["safe"], ["guarded"], ["privileged"]
    Hidden       bool      // whether the tool should be hidden from agents
}
```

Source: `internal/tools/registry.go:31-35`

## Metadata reporters

Tools that implement `MetadataReporter` return their metadata explicitly. Tools that don't implement it get inferred metadata based on their capability level.

Source: `internal/tools/registry.go:37-39` (MetadataReporter), `internal/tools/registry.go:141-146` (inferToolMetadata)

## Group normalization

Groups are normalized to lowercase, deduplicated, and sorted alphabetically.

Source: `internal/tools/registry.go:123-139` (normalizeGroups)

## Built-in tool metadata

Tool metadata is declared in `tool_behavior.go` via methods on each tool type:

| Tool | Groups |
|------|--------|
| read_artifact | read |
| read_file | read |
| search_file | read |
| write_file | write |
| edit_file | write |
| list_dir | read |
| memory_set_pinned | memory, read |
| memory_add_note | memory, read |
| memory_search | memory, read |
| memory_recent | memory, read |
| memory_get_pinned | memory, read |
| send_message | channels |
| read_skill | skills, read |
| run_skill | skills, exec |
| run_skill_script | skills, exec |
| exec | exec |
| spawn_subagent | service |
| web_fetch | web |
| web_fetch_markdown | web |
| web_search | web |
| cron | cron |

Source: `internal/tools/tool_behavior.go:181-412` (Metadata methods)

## MCP tool metadata

MCP-provided tools are in the "mcp" group. The metadata scanner checks MCP tools for suspicious patterns in their names and descriptions.

Source: `internal/tools/metadata_scanner.go:58-96` (FilterSuspiciousExternalTools)
