# Overview

This plan adds Model Context Protocol (MCP) integration so `or3-intern` can connect to external MCP servers and expose their tools through the existing tool loop.

Scope covers:

- configuring one or more MCP servers in `config.json`
- connecting to those servers from the Go runtime
- wrapping remote MCP tools as native `internal/tools.Tool` instances
- enforcing safe-by-default transport, timeout, and output constraints

Assumptions:

- v1 should favor deterministic startup registration over a large dynamic control plane
- MCP tools should look like normal tools to the runtime once registered
- failures to connect to individual servers should not prevent the whole app from starting unless explicitly configured otherwise

# Requirements

## 1. Add MCP server configuration to the existing tool/runtime config

The system shall support declarative MCP server configuration in `config.json` with disabled-by-default behavior.

### Acceptance criteria

- config supports zero or more named MCP servers under an existing repo-aligned config area
- each server config supports the minimal transport details needed for stdio and HTTP-based MCP transports
- config supports per-server enable/disable flags and bounded tool timeout settings
- existing configs remain backward compatible when no MCP settings are present
- secrets such as headers or env values remain in config/env only and are not copied into prompts

## 2. Connect configured MCP servers and register their tools

The system shall connect to enabled MCP servers and expose their tools through the normal tool registry.

### Acceptance criteria

- startup or first-use registration yields MCP-backed tools visible in the runtime tool definitions
- each remote MCP tool is exposed with a stable local tool name derived from server name plus remote tool name
- tool descriptions and input schemas are sourced from the MCP server when available
- connection failure for one server does not prevent other MCP servers from registering
- the runtime continues functioning with native tools even when all MCP servers fail to connect

## 3. Execute MCP tools within existing bounded tool-loop semantics

The system shall run MCP tools as normal tool calls subject to the repo's existing execution limits.

### Acceptance criteria

- MCP tool execution uses the same runtime/tool loop path as built-in tools
- per-call execution is bounded by a configured timeout
- returned text participates in the existing oversized-output handling path rather than bypassing it
- remote tool failures surface as normal tool errors or bounded failure text without crashing the process

## 4. Keep transports safe by default

The system shall enforce conservative MCP transport behavior.

### Acceptance criteria

- stdio transport is supported as the preferred default for local trusted integrations
- HTTP transports are explicit and validated, with conservative defaults around insecure URLs and timeouts
- server process env passthrough is explicit rather than inheriting all ambient environment by default
- connection setup and tool execution avoid unbounded retries or reconnection storms in v1
- configuration makes it clear when the user is enabling networked MCP access

## 5. Preserve current runtime architecture

The system shall fit into the existing single-process CLI/runtime model.

### Acceptance criteria

- no new daemon, queue, or sidecar service is introduced inside this repo
- MCP integration plugs into the existing registry/runtime wiring from `cmd/or3-intern/main.go`
- channel handling, session keys, memory retrieval, and SQLite persistence continue unchanged when MCP is enabled
- no SQLite migration is required for v1 MCP support

## 6. Support startup visibility and operator debugging

The system shall make MCP registration observable without leaking secrets.

### Acceptance criteria

- startup logs identify which MCP servers connected and how many tools were registered
- failed server connections produce concise bounded error logs without printing secret values
- docs explain the supported transports and expected server configuration shape

## 7. Add focused regression coverage

The system shall include tests appropriate for transport wrappers and registry integration.

### Acceptance criteria

- config parsing and normalization are covered by tests
- MCP wrapper behavior is covered for naming, schema propagation, timeout, and error handling
- registry/runtime tests verify that registered MCP tools appear in tool definitions and execute through the normal path
- native tool registration behavior remains unchanged when MCP is disabled

# Non-functional constraints

- Favor a small integration layer inside existing packages over a generic plugin framework
- Keep memory and goroutine usage bounded; avoid background reconnect loops in v1
- Preserve deterministic startup behavior as much as possible within remote-connectivity constraints
- Treat remote MCP servers as untrusted external systems; do not assume they are local or safe
- Reuse existing tool output bounding and artifact spilling instead of inventing a new result channel
- Do not weaken workspace restrictions, command limits, or secret handling because MCP is enabled
