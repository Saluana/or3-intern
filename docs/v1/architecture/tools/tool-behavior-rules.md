# Tool Behavior Rules

The tool behavior system provides advice, metadata, and classification for all tools.

## Tool name constants

All tool names are defined as constants in `tool_behavior.go`:

```
read_artifact, read_file, search_file, write_file, edit_file, list_dir,
memory_set_pinned, memory_add_note, memory_search, memory_recent, memory_get_pinned,
send_message, read_skill, run_skill, run_skill_script,
exec, spawn_subagent, web_fetch, web_fetch_markdown, web_search, cron
```

Source: `internal/tools/tool_behavior.go:8-30`

## Advice providers

Each tool implements `AdviceProvider` with a `FailureAdvice` method. When a tool fails, the advice provider returns helpful messages to guide the AI agent.

The advice is registered in a global map during `init()` via `registerBuiltInAdviceProviders`.

Source: `internal/tools/tool_behavior.go:36-64`

## Metadata declarations

Each tool type declares its metadata groups:

| Tool | Groups |
|------|--------|
| ReadArtifact | read |
| ReadFile | read |
| SearchFile | read |
| WriteFile | write |
| EditFile | write |
| ListDir | read |
| Memory* (all 5) | memory, read |
| SendMessage | channels |
| ReadSkill | skills, read |
| RunSkill | skills, exec |
| RunSkillScript | skills, exec |
| ExecTool | exec |
| SpawnSubagent | service |
| WebFetch | web |
| WebFetchMarkdown | web |
| WebSearch | web |
| CronTool | cron |

Source: `internal/tools/tool_behavior.go:181-412`

## Tool classification helpers

Helper functions classify tools by behavior:

- `IsExecutionToolName(name)` - exec, run_skill, run_skill_script
- `IsWriteToolName(name)` - write_file, edit_file
- `IsWebFetchToolName(name)` - web_fetch, web_fetch_markdown
- `IsWebToolName(name)` - web_fetch, web_fetch_markdown, web_search

Source: `internal/tools/tool_behavior.go:104-134`

## Common failure advice patterns

### Exec tool advice
- Prefer program + args over command strings
- After approval required: retry the exact same call so the token matches
- Shell disabled: split pipelines into separate direct calls
- Program not allowed: use a read-only tool instead

Source: `internal/tools/tool_behavior.go:349-371`

### Memory tool advice
- If memory is unavailable, continue without persistence
- For empty queries: provide a concrete query
- For empty text: provide the fact or note explicitly

Source: `internal/tools/tool_behavior.go:139-149`

### Web fetch advice
- Use a full URL (not a search query)
- For unsupported content: try raw=true
- For render failures: retry without render=true

Source: `internal/tools/tool_behavior.go:166-179`

### Skill execution advice
- Use read_skill first to inspect instructions
- After approval required: wait and retry with the same params or plan_id
- For skill not found: use the exact installed skill name

Source: `internal/tools/tool_behavior.go:151-164`
