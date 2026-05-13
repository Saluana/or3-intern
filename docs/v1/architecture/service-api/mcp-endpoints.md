# MCP Endpoints

MCP server management is exposed under `/internal/v1/mcp/servers/*`. These routes are operator-only. Reads require an authenticated operator session; add, delete, and test require a recent passkey step-up because they can persist or execute MCP server configuration.

## List Servers

`GET /internal/v1/mcp/servers`

Returns configured servers with their current runtime status. `env` and `headers` values are redacted as `"configured"` when present; clients can post the same placeholder back to preserve the existing secret value.

```json
{
  "servers": [
    {
      "name": "local",
      "config": {
        "enabled": true,
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem"]
      },
      "status": {
        "connected": true,
        "toolCount": 3,
        "tools": ["read_file", "list_directory"]
      }
    }
  ]
}
```

## Add or Replace Server

`POST /internal/v1/mcp/servers`

```json
{
  "name": "local",
  "config": {
    "enabled": true,
    "transport": "stdio",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
    "env": {},
    "headers": {},
    "connectTimeoutSeconds": 10,
    "toolTimeoutSeconds": 60
  }
}
```

The handler normalizes the config, applies the global child environment allowlist when no server-specific allowlist is set, validates the full MCP server map, saves config, and returns:

```json
{
  "ok": true,
  "config_path": "/Users/example/.or3-intern/config.json",
  "restartRequired": true
}
```

The running service does not hot-register new MCP tools from this route. Restart the service after add/delete.

When replacing an existing server, `env` or `headers` entries with the value `"configured"` preserve the previous stored value for that key. Omit a key to remove it, or provide a new value to replace it.

## Delete Server

`DELETE /internal/v1/mcp/servers/{name}`

Deletes the named server from config and returns `restartRequired: true`.

## Test Server

`POST /internal/v1/mcp/servers/{name}/test`

Creates an isolated MCP manager for the named configured server, applies host policy for HTTP transports, connects, lists tools, and then closes the test manager. For `stdio` servers this can launch the configured command, so this route follows the same recent step-up requirement as config mutation.

Success:

```json
{
  "ok": true,
  "toolCount": 2,
  "tools": [
    {"name": "mcp_local_read_file"},
    {"name": "mcp_local_list_directory"}
  ]
}
```

Failure still returns `200` with `ok: false` and a bounded `error` string so settings UIs can show connection failures without treating the HTTP request itself as broken.

## Related Architecture

- [MCP integration](../integrations/mcp.md)
- [Tools registry](../tools/registry.md)
- [Network policy](../security/network-policy.md)
