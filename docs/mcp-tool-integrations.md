# MCP tool integrations

## Overview

MCP support is optional and disabled by default. Configured servers are registered during startup, and their tools are exposed to the model as normal local tools with stable names such as `mcp_<server>_<tool>`.

## Configuration shape

MCP servers are configured under `tools.mcpServers`.

Per-server settings include:

- `enabled`
- `transport`
- `command`
- `args`
- `env`
- `childEnvAllowlist`
- `url`
- `headers`
- `connectTimeoutSeconds`
- `toolTimeoutSeconds`
- `allowInsecureHttp`

## Supported transports

- `stdio`
- `sse`
- `streamableHttp`

## Safety notes

The README documents these safety rules:

- prefer `stdio` for local trusted servers
- plain `http://` endpoints are rejected unless `allowInsecureHttp=true`, and even then only for loopback or localhost targets
- stdio MCP servers inherit only the configured child environment allowlist plus explicit per-server `env` entries
- MCP tool calls go through the existing tool loop, timeout handling, and artifact spill behavior
- when `security.network` is enabled, MCP HTTP transports must satisfy the global trusted-host policy
- hosted-profile startup and skill inventory enumeration fail closed for remote MCP HTTP endpoints that do not satisfy the effective host policy

## Operational notes

v1 intentionally does not include:

- live reconnect loops
- hot-add or hot-remove of MCP tools
- SQLite persistence for tool catalogs
- a separate MCP gateway service

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/mcp/`
- `internal/tools/`
- `internal/config/config.go`
