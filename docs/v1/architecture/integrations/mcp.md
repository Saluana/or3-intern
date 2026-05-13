# MCP Integration

MCP (Model Context Protocol) integration connects OR3 Intern to external servers that expose tools. The integration is in `internal/mcp/manager.go`.

## Manager

```go
type Manager struct {
    servers    map[string]config.MCPServerConfig
    sessions   map[string]session
    tools      []remoteToolSpec
    failures   map[string]string
    hostPolicy security.HostPolicy
}
```

Created with `NewManager(servers)`. Uses the `modelcontextprotocol/go-sdk` library.

## Transports

Three transports are supported (`internal/mcp/manager.go:442-462`):

### Stdio

The server is launched as a subprocess. The `CommandTransport` runs the configured command with arguments and communicates over stdin/stdout.

```json
{
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem"],
  "env": {"HOME": "/home/user"}
}
```

The child process environment is built using `tools.BuildChildEnv` with the configured `ChildEnvAllowlist` and `env` map.

### SSE (Server-Sent Events)

Connects over HTTP using SSE. The endpoint URL must be reachable from the OR3 service process.

```json
{
  "transport": "sse",
  "url": "https://api.example.com/mcp",
  "headers": {"Authorization": "Bearer token"}
}
```

### Streamable HTTP

Connects over HTTP with the streamable transport variant. Supports automatic reconnection via `MaxRetries: -1`.

```json
{
  "transport": "streamablehttp",
  "url": "https://api.example.com/mcp"
}
```

## Connection Lifecycle

### Connect (`internal/agentcli/manager.go:171-223`)

1. Only connects once (skips if tools or sessions already exist)
2. Iterates enabled servers sorted by name
3. For HTTP-based transports, validates the endpoint against host policy
4. Establishes a session via `connectSessionWithPolicy`
5. Lists all remote tools via `ListTools` (handles pagination via cursor)
6. Filters out nil tools and those with empty names
7. Assigns local tool names in the format `mcp_<server>_<tool>`
8. Duplicate local names are skipped (logged as warnings)

### Refresh (`internal/agentcli/manager.go:227-241`)

Replaces the server configuration and reconnects without restarting the process. Closes existing sessions first.

### ReconnectWithBackoff (`internal/agentcli/manager.go:244-278`)

Retries failed servers with exponential backoff (up to 5 seconds max delay). Useful for servers that are slow to start.

### Close (`internal/agentcli/manager.go:317-330`)

Closes all active sessions and clears discovered tools.

## Tool Registration

Discovered tools are registered into the `tools.Registry` via `RegisterTools` (`internal/agentcli/manager.go:306-314`). Each remote tool becomes a `RemoteTool` implementing the `tools.Tool` interface.

## RemoteTool

```go
type RemoteTool struct {
    tools.Base
    localName   string
    serverName  string
    remoteName  string
    description string
    parameters  map[string]any
    timeout     time.Duration
    session     session
}
```

- `Name()` returns the local name (`mcp_<server>_<tool>`)
- `Description()` returns the remote description or a synthesized fallback
- `Parameters()` returns a cloned JSON schema
- `Execute(ctx, params)` calls the remote tool and converts the result to text
- `Capability()` returns `CapabilityGuarded`

## Result Conversion

`resultToText` (`internal/agentcli/manager.go:604-620`) converts MCP tool results to plain text:

- `TextContent` → the text value
- `ImageContent` → `[image content omitted mime=... bytes=N]`
- `AudioContent` → `[audio content omitted mime=... bytes=N]`
- `ResourceLink` → `[resource link uri=... name=...]`
- `EmbeddedResource` → the text if available, otherwise resource metadata
- Unknown content → JSON representation

Results are truncated at 64 KB (`maxResultChars`).

## Host Policy

For HTTP-based transports, the `HostPolicy` validates endpoints before connection. This prevents the MCP manager from connecting to unauthorized hosts.

## Server Status

`ServerStatus()` returns a per-server status snapshot showing whether each server is connected, how many tools it provides, and any error messages. Status is computed from config, active sessions, and failure records.
