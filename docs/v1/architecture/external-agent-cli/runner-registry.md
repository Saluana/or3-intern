# Runner Registry

The runner registry maps runner IDs to their specs and adapters. It is defined in `internal/agentcli/registry.go`.

## RunnerSpec

Each runner has a spec (`internal/agentcli/runners.go:118-127`) that describes:

- **ID** — unique identifier like `"opencode"` or `"codex"`
- **DisplayName** — human-readable name like `"OpenCode"`
- **Binary** — the CLI executable name (e.g. `"opencode"`)
- **VersionArgs** — arguments to check the version (e.g. `["--version"]`)
- **AuthCheck** — optional command to verify authentication (e.g. `["auth", "list"]`)
- **Supports** — capability flags

## RunnerSupports

Each runner declares which features it supports (`internal/agentcli/runners.go:100-110`):

- `StructuredOutput` — produces JSON or JSONL output
- `StreamingJSON` — can stream JSON events
- `ModelFlag` — accepts a `--model` flag
- `PermissionsMode` — supports permission modes
- `SafeSandboxFlag` — has a safe sandbox option
- `DangerousBypassFlag` — can bypass approvals and sandboxing
- `StdinPrompt` — accepts the prompt on stdin
- `Chat` — chat-specific capabilities

## RunnerRegistry

A `RunnerRegistry` (`internal/agentcli/registry.go:137-144`) holds:

- `specs` — map from `RunnerID` to `RunnerSpec`
- `adapters` — map from `RunnerID` to `RunnerAdapter`
- `detectCache` — cached detection results with timestamps

## Creating a Registry

`NewDefaultRegistry()` (`internal/agentcli/registry.go:308-321`) creates the production registry with all five standard runners and their adapters:

```go
specs := AllRunners()
return NewRunnerRegistry(specs, []RunnerAdapter{
    &OpenCodeAdapter{spec: openCodeSpec},
    &CodexAdapter{spec: codexSpec},
    &ClaudeAdapter{spec: claudeSpec},
    &GeminiAdapter{spec: geminiSpec},
})
```

## RunnerAdapter Interface

Each adapter implements the `RunnerAdapter` interface (`internal/agentcli/runners.go:185-192`):

- `ID() RunnerID`
- `DisplayName() string`
- `Spec() RunnerSpec`
- `Detect(ctx, opts) RunnerInfo`
- `BuildCommand(req) (CommandSpec, error)`

## Building Commands

When `Manager.executeRun()` needs to run a task, it calls `m.Registry.BuildCommand(req)` (`internal/agentcli/registry.go:261-268`), which finds the right adapter and calls its `BuildCommand` method. Each adapter builds a `CommandSpec` with the binary path, arguments, working directory, and output mode for its specific CLI.

## Runtime Backends

OpenCode and Codex can now run through native runtime backends before falling back to the CLI adapters:

- `auto` mode tries native first and emits a bounded `runtime.warning` event before CLI fallback.
- `native` mode treats native runtime errors as terminal instead of falling back.
- `cli` mode preserves the original adapter/process behavior.

OpenCode supports explicit loopback attach/reuse with `agentCLI.nativeServerUrls.opencode`. Config validation rejects non-loopback URLs; a healthy configured endpoint is classified as `external` ownership and is reused even if the `opencode` binary is not installed locally. If no configured endpoint is healthy, the runtime can lazily start `opencode serve` on loopback and classify it as `managed`.

Codex uses `codex app-server --listen stdio://` per turn. It is not started by `or3-intern service`, service start scripts, or restart scripts.

The compatibility `CLIRuntimeBackend` wraps existing chat adapters so fallback behavior remains explicit and testable without changing command construction.

## Detection Cache

Detection results are cached with a TTL of 30 seconds (constant `agentCLIDetectCacheTTL` in `internal/agentcli/manager.go:28`). When a run is enqueued, the manager checks the cache first. If the cache is stale or missing, it triggers an async refresh via `RefreshDetectAsync` (`internal/agentcli/registry.go:219-246`).
