# MCP Servers

MCP server settings are managed under `/internal/v1/mcp/servers/*`.

## Routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/mcp/servers` | List configured servers with runtime status |
| `POST /internal/v1/mcp/servers` | Add or replace one server config |
| `DELETE /internal/v1/mcp/servers/{name}` | Remove one server config |
| `POST /internal/v1/mcp/servers/{name}/test` | Try connecting and list discovered tools |

## Add Shape

```json
{
  "name": "local-files",
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

Supported transports are `stdio`, `sse`, and `streamablehttp`.

## UI Notes

- Add/delete responses include `restartRequired: true`.
- Test failures return `200` with `ok: false` and a human-readable `error`.
- Server names are path-escaped in delete/test URLs.
- HTTP transports are checked against the configured network host policy.
