# Overview

MCP support fits best as a thin integration layer between startup configuration and the existing `internal/tools.Registry`.

The repo already has the right shape for this:

- tools are registered centrally in `cmd/or3-intern/main.go`
- runtime only needs tool schemas plus an `Execute` implementation
- output bounding and artifact spilling already happen after tool execution
- config is centralized and JSON/env driven

The design therefore adds:

- MCP server config types in `internal/config`
- a small MCP client/manager package responsible for connecting to configured servers
- tool wrappers that implement the existing `internal/tools.Tool` interface
- startup wiring that registers wrappers before the runtime begins handling work

This avoids introducing a second registry model, plugin framework, or persistent control-plane service.

# Affected areas

- `internal/config/config.go`
  - add MCP config structs, defaults, and any env override handling
- `cmd/or3-intern/main.go`
  - construct an MCP manager/client set and register tools into the main tool registry
- `internal/tools/registry.go`
  - may need small safety changes if runtime registration timing requires synchronization
- `internal/tools`
  - add MCP wrapper types implementing the existing tool interface
- `internal/mcp` or `internal/tools/mcp`
  - add transport/client management for stdio and HTTP MCP transports
- `README.md`
  - document config shape, transport options, and safety notes
- tests in `internal/config`, `internal/tools`, and the new MCP package

# Control flow / architecture

## Preferred v1 architecture: startup registration

1. `cmd/or3-intern/main.go` loads config.
2. Before worker loops start, startup builds the base tool registry.
3. If MCP servers are configured:
   - connect to each enabled server
   - list available tools
   - wrap each tool as a local `tools.Tool`
   - register wrappers into the same registry used by runtime
4. Runtime uses the final registry with both native and MCP-backed tools.

This approach is preferred because it preserves deterministic tool definitions during runtime and avoids concurrent mutation of the current unsynchronized registry.

## Why not lazy per-turn registration in v1?

Lazy connect/retry is attractive for resilience, but it adds extra complexity in this repo:

- tool schemas would change after runtime startup
- the current registry has no concurrency control for late registration
- multiple workers could race tool registration or duplicate connections

A simpler repo-aligned first pass is:

- connect/register during startup
- log partial failure and continue
- allow reconnect-on-restart rather than implementing a live reconnect manager immediately

## Tool wrapper flow

Each MCP remote tool is wrapped into a local Go type that implements the existing tool interface:

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any
    Execute(ctx context.Context, params map[string]any) (string, error)
    Schema() map[string]any
}
```

Wrapper responsibilities:

- generate a stable local name such as `mcp_<server>_<tool>`
- expose remote description and JSON schema
- call the remote tool with provided arguments
- enforce timeout and output shaping
- return combined text content or a bounded fallback representation

# Data and persistence

## SQLite changes

None required.

MCP support is purely runtime/tooling state in v1.

## Config changes

The most repo-aligned place is under `ToolsConfig`, because MCP adds tool sources rather than channels or providers.

Suggested shape:

```go
type MCPServerConfig struct {
    Enabled               bool              `json:"enabled"`
    Transport             string            `json:"transport"`
    Command               string            `json:"command"`
    Args                  []string          `json:"args"`
    Env                   map[string]string `json:"env"`
    URL                   string            `json:"url"`
    Headers               map[string]string `json:"headers"`
    ToolTimeoutSeconds    int               `json:"toolTimeoutSeconds"`
    ConnectTimeoutSeconds int               `json:"connectTimeoutSeconds"`
    AllowInsecureHTTP     bool              `json:"allowInsecureHttp"`
}

type ToolsConfig struct {
    BraveAPIKey         string                     `json:"braveApiKey"`
    WebProxy            string                     `json:"webProxy"`
    ExecTimeoutSeconds  int                        `json:"execTimeoutSeconds"`
    RestrictToWorkspace bool                       `json:"restrictToWorkspace"`
    PathAppend          string                     `json:"pathAppend"`
    MCPServers          map[string]MCPServerConfig `json:"mcpServers"`
}
```

Supported transports for v1:

- `stdio`
- `sse`
- `streamableHttp`

Safe defaults:

- no servers configured by default
- per-tool timeout defaults to a bounded value such as 30 seconds
- connect timeout defaults to a bounded value such as 10 seconds
- insecure plain HTTP disabled by default except possibly localhost if explicitly allowed
- server env is explicit; ambient environment inheritance is not automatic

# Interfaces and types

## MCP package

Add a focused internal package, for example `internal/mcp`, to isolate transport concerns from generic tool code.

Suggested responsibilities:

```go
type Manager struct {
    Servers map[string]config.MCPServerConfig
}

func (m *Manager) RegisterTools(ctx context.Context, reg *tools.Registry) error
func (m *Manager) Close() error
```

The manager should:

- connect to each enabled server
- keep any session/client handles needed for execution
- create wrapper instances for listed tools
- support graceful cleanup on shutdown

## Tool wrappers

A wrapper should preserve remote schema as closely as possible:

```go
type RemoteTool struct {
    serverName   string
    toolName     string
    description  string
    parameters   map[string]any
    timeout      time.Duration
    caller       RemoteCaller
}
```

The wrapper should avoid repo-specific special cases beyond naming, timeout, and output conversion.

## Registry considerations

If registration occurs strictly before worker startup, `internal/tools/registry.go` may not need API changes.

If implementation reality requires concurrent access, add a minimal mutex to the registry rather than creating a separate MCP registry abstraction.

# Failure modes and safeguards

- Invalid config
  - reject unsupported transport values and malformed server definitions during startup normalization
- Connection failure
  - log the failed server, skip its tools, continue loading others
- Tool listing failure
  - treat as a server-level registration failure
- Tool timeout
  - return bounded timeout error text or normal Go error
- Unsupported content blocks
  - stringify unsupported MCP result segments rather than failing the whole call
- Secret leakage
  - never print configured headers, auth values, or env secrets in logs
- Unsafe URLs
  - reject insecure or malformed HTTP endpoints unless explicitly allowed by config
- Shutdown cleanup
  - close MCP sessions/clients during process stop without hanging indefinitely

# Testing strategy

Use focused Go tests rather than broad end-to-end infrastructure.

## Unit tests

- `internal/config/config_test.go`
  - defaults and normalization for MCP config
  - transport validation and safe HTTP rules
- `internal/mcp/..._test.go`
  - tool wrapper naming
  - schema propagation
  - timeout behavior
  - stringification of result blocks
- `internal/tools/..._test.go`
  - registry integration and visibility in tool definitions

## Integration-style tests

- add a small fake MCP stdio server or stub transport adapter to simulate:
  - successful tool registration
  - multiple tools from one server
  - one failed server plus one successful server
  - tool execution through the normal registry path

## Regression coverage

- verify `cmd/or3-intern/main.go` still builds the normal registry unchanged when MCP is disabled
- verify runtime tool execution remains stable for existing native tools
- verify no SQLite or session behavior changes are introduced by enabling MCP
