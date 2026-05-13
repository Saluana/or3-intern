# Tool Registry

The tool registry manages all available tools at runtime.

## Registration

Tools are registered with `Registry.Register(tool)` which stores them by name. When a tool implements `AdviceProvider`, it is also registered in the global advice provider map.

Source: `internal/tools/registry.go:45-50`

## Lookup

`Registry.Get(name)` returns a tool by name. Returns nil if the tool is not found.

Source: `internal/tools/registry.go:52-56`

## Listing

`Registry.Names()` returns sorted tool names. `Registry.Definitions()` returns all tool schemas sorted by name.

Source: `internal/tools/registry.go:58-82`

## Metadata

`Registry.Metadata(name)` returns `ToolMetadata` for a tool from the registry. If the tool implements `MetadataReporter`, its metadata is used. Otherwise, metadata is inferred from the tool's capability level.

Source: `internal/tools/registry.go:84-106`

## Cloning

`Registry.CloneSelected(allowedNames)` creates a new registry with only the named tools. `Registry.CloneFiltered(allowed)` is similar but takes a slice instead of a map.

Source: `internal/tools/registry.go:108-177`

## Execution

`Registry.Execute(ctx, name, argsJSON)` parses JSON args and executes the tool. `Registry.ExecuteParams(ctx, name, params)` takes already-parsed params. Before execution, the tool guard from context is invoked to check if the action is allowed.

Source: `internal/tools/registry.go:179-207`

## Context values

The execution context carries:
- `ToolGuard` - function that checks if a tool invocation is allowed
- `RequestSource` - whether the request is local (chat) or service (API)
- `RequesterIdentity` - the actor and role making the request
- `ActiveProfile` - the access profile for the current context
- `Session` - the session identifier
- `ApprovalToken` - an approval token for authorized retries
- Environment variables to add to child processes

Source: `internal/tools/context.go` (context value accessors)
