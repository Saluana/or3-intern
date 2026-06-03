# Tools System Overview

The tools system provides the runtime interface that AI agents use to perform actions. Every tool implements the `Tool` interface and is registered in a `Registry`.

## Tool interface

Every tool must implement:
- `Name() string` - unique tool name
- `Description() string` - what the tool does
- `Parameters() map[string]any` - JSON Schema for parameters
- `Execute(ctx, params) (string, error)` - run the tool
- `Schema() map[string]any` - OpenAI function-calling schema

Source: `internal/tools/tools.go:25-31` (Tool interface)

## Capability levels

Each tool has a capability level that controls when approval is needed:

- **safe** - no approval needed (e.g., read_file, memory_search)
- **guarded** - may need policy checks (e.g., exec with program+args, write_file)
- **privileged** - requires elevated approval (e.g., exec with shell commands, run_skill)

Source: `internal/tools/tools.go:15-22` (CapabilityLevel)

Tools can report their capability statically (`CapabilityReporter`), per-params (`CapabilityForParamsReporter`), or context-aware (`CapabilityForContextParamsReporter`).

Source: `internal/tools/tools.go:34-75` (capability resolution)

## Tool registry

The `Registry` stores tools by name and provides lookup, listing, and filtered cloning. When executing a tool, it checks the tool guard from context before running.

Source: `internal/tools/registry.go:12-207`

## Tool groups

Tools are organized into groups for filtering and metadata:
- **read** - read_file, search_file, list_dir, read_artifact, read_skill
- **memory** - memory_set_pinned, memory_add_note, memory_search, memory_recent, memory_get_pinned
- **write** - write_file, edit_file
- **exec** - exec, run_skill, run_skill_script
- **web** - web_fetch, web_search
- **cron** - cron
- **skills** - read_skill, run_skill, run_skill_script
- **channels** - send_message
- **mcp** - MCP-provided tools
- **service** - spawn_subagent
- **plan** - create_plan, update_plan, complete_plan_task, remove_plan (agent runtime; see [plan tools](plan-tools.md))

Source: `internal/tools/registry.go:17-29` (ToolGroup constants)

## Tool result format

Tools return results using `ToolResult` encoded to JSON. The result includes:
- `Kind` - result type (file_read, exec, web_fetch, etc.)
- `OK` - whether the operation succeeded
- `Summary` - one-line description
- `Preview` - bounded content preview
- `Stats` - structured metadata about the result

Source: `internal/tools/result.go` and usage in tool Execute methods

## Advice providers

Each tool implements `AdviceProvider` to give helpful messages when it fails. The advice helps the AI agent correct its call and retry.

Source: `internal/tools/tool_behavior.go:32-34` (AdviceProvider interface) and per-tool FailureAdvice methods
