# MCP Management UX Design

## Overview

The existing MCP integration is engine-only — `internal/mcp/manager.go` connects servers at startup and registers tools, but there is no management UI. This design adds management surfaces to the configure TUI, service API, and or3-app, following existing patterns.

The key insight: MCP servers are already part of `config.Tools.MCPServers` (a `map[string]MCPServerConfig`), saved to `config.json`. The new surfaces read from and write to this config map. No new persistence layer needed.

## Affected areas

### or3-intern (Go backend)

- `cmd/or3-intern/configure.go` — add "MCP Servers" section to `configureSections`, add section status, add field building
- `cmd/or3-intern/configure_tui.go` — add server list screen, add/edit server form, delete confirmation, test connection flow
- `cmd/or3-intern/service_mcp.go` — new file: MCP REST endpoints (list, add, remove, test)
- `cmd/or3-intern/main.go` — wire MCP service endpoints (already has `mcpManager` available)
- `cmd/or3-intern/service_*.go` — wire the `/internal/v1/mcp/` routes
- `internal/controlplane/controlplane.go` — enrich capabilities `EnabledMCPServers` with transport and tool count
- `internal/mcp/manager.go` — add `ServerStatus()` method that returns per-server connection state for the API/UI

### or3-app (Vue/Nuxt frontend)

- `app/types/or3-api.ts` — type `mcpServers` properly (replace `unknown[]`)
- `app/components/computer/ComputerOverviewCard.vue` — show MCP summary when servers are configured
- `app/settings/simpleSettings.ts` — add `connections` section controls for MCP
- `app/composables/useMCP.ts` — new composable for MCP server CRUD + test
- `app/pages/settings/` — MCP server management page (add/edit/list cards)
- `app/components/settings/` — MCP server card, add/edit form component

## Control flow / architecture

```
┌─────────────────────────────────────────────────────┐
│                   or3-app (Vue)                      │
│  ComputerOverviewCard → MCP summary                  │
│  Settings → "Add-ons" section → MCP CRUD + test     │
│      │  GET /internal/v1/mcp/servers                │
│      │  POST /internal/v1/mcp/servers               │
│      │  DELETE /internal/v1/mcp/servers/{name}      │
│      │  POST /internal/v1/mcp/servers/{name}/test   │
│      │  GET /internal/v1/capabilities (enriched)     │
│      ▼                                               │
│  or3-intern HTTP service (service_mcp.go)            │
│      │  reads/writes config.Tools.MCPServers         │
│      │  uses mcp.Manager for test connections        │
│      │  config.Save() to persist changes             │
│      ▼                                               │
│  config.json ←→ config.Tools.MCPServers              │
│      │                                               │
│      ▼                                               │
│  mcp.Manager (startup connect, tool registration)    │
│      │                                               │
│      ▼                                               │
│  tools.Registry (existing tool loop)                  │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│            or3-intern configure TUI                  │
│  Sections list → "MCP Servers" → Server list screen  │
│      │  Add (prompt name → form)                     │
│      │  Edit (select → form with fields)             │
│      │  Delete (confirm → remove from map)            │
│      │  Test (connect → show result)                 │
│      ▼                                               │
│  config.Save() to config.json                        │
└─────────────────────────────────────────────────────┘
```

## TUI design

### New screen: `configureScreenMCPServerList`

Between the sections list screen and the form screen, a new screen shows:
- Title: "MCP Servers"
- List of configured servers, each row showing:
  - Server name (bold)
  - Transport badge (stdio/sse/streamableHttp)
  - Enabled/disabled indicator
  - Connection status (connected ✓ / failed ✗ / unknown ?)
- Empty state: "No MCP servers configured. Press 'a' to add one."
- Key bindings:
  - `↑/↓` — navigate servers
  - `enter` — edit selected server (opens per-server form)
  - `a` — add new server (prompts for name, then opens form)
  - `d` — delete selected server (confirm prompt)
  - `t` — test connection for selected server
  - `esc` — back to sections list

### Per-server edit screen: reuses `configureScreenForm`

When editing a server, the existing form screen is used with the server name shown as a subtitle. Fields are:
- `enabled` — toggle
- `transport` — choice: stdio / sse / streamableHttp
- **stdio-only fields:**
  - `command` — text (path to executable)
  - `args` — text (space-separated, parsed to []string)
  - `childEnvAllowlist` — text (comma-separated)
  - `env` — text (KEY=VAL, semicolon-separated, parsed to map)
- **HTTP-only fields (sse/streamableHttp):**
  - `url` — text
  - `headers` — text (KEY: VAL, semicolon-separated, parsed to map)
  - `allowInsecureHttp` — toggle
- Common:
  - `connectTimeoutSeconds` — text (int)
  - `toolTimeoutSeconds` — text (int)

Dynamic field visibility: only show fields relevant to the selected transport. When the user cycles the transport choice, the visible fields change.

### Integration with existing save flow

MCP server changes are applied to `cfg.Tools.MCPServers` in-memory and saved via `config.Save()`. The existing review screen (`configureScreenReview`) shows a summary of changed fields including MCP changes. The existing save flow works unchanged.

### New TUI section registration

Add to `configureSections` in `configure.go`:
```go
{Key: "mcp", Label: "MCP Servers", Description: "Model Context Protocol server connections and tool registration"},
```

Add section status in `sectionStatus()`:
```go
case "mcp":
    return mcpSectionStatus(cfg)
```

## API design

### `GET /internal/v1/mcp/servers`

Returns:
```json
{
  "servers": [
    {
      "name": "filesystem",
      "config": { ...MCPServerConfig... },
      "status": {
        "connected": true,
        "toolCount": 5,
        "tools": ["mcp_filesystem_read_file", "mcp_filesystem_write_file", ...],
        "lastError": ""
      }
    }
  ]
}
```

The `status` object comes from the running `mcp.Manager` when available (for currently connected servers). For servers added since the last startup, `connected` is `false` with a note that a restart is needed.

### `POST /internal/v1/mcp/servers`

Request body:
```json
{
  "name": "filesystem",
  "config": { ...MCPServerConfig... }
}
```

- If the server name does not exist, it's added to `cfg.Tools.MCPServers`
- If the server name exists, its config is replaced (update)
- The config is validated through the same validation as config loading (`validateMCPServers`)
- Config is saved to disk
- Returns `{ "ok": true, "restartRequired": true }`

### `DELETE /internal/v1/mcp/servers/{name}`

- Removes the named server from `cfg.Tools.MCPServers`
- Saves config to disk
- Returns `{ "ok": true }`

### `POST /internal/v1/mcp/servers/{name}/test`

- Creates a temporary MCP manager with just this server
- Attempts to connect and list tools
- Returns:
  ```json
  {
    "ok": true,
    "toolCount": 5,
    "tools": [
      {"name": "read_file", "description": "Read a file from the filesystem"},
      ...
    ]
  }
  ```
- On failure, returns `{ "ok": false, "error": "..." }` with a bounded error message (no secrets)
- The temporary manager is closed after the test

### Capabilities enrichment

Existing `GET /internal/v1/capabilities` already returns `enabledMcpServers` as a `[]string`. Enrich it to:
```json
{
  "mcpServers": [
    {
      "name": "filesystem",
      "transport": "stdio",
      "toolCount": 5,
      "connected": true
    }
  ]
}
```

### Auth

All MCP endpoints require operator-level auth (`requireServiceRole(w, r, approval.RoleOperator)`), consistent with configure endpoints.

## App design

### Types (or3-api.ts)

Replace `mcpServers?: unknown[]` with:
```typescript
export interface MCPServerInfo {
  name: string
  transport: string
  toolCount: number
  connected: boolean
}

export interface MCPServerDetail extends MCPServerInfo {
  config: MCPServerConfig
  tools: string[]
  lastError?: string
}
```

### Computer overview

When `capabilities.mcpServers` has entries, show a summary line under the connection stats:
"MCP: 2 add-ons active" or "MCP: 1 of 2 connected" with a green/amber pill.

### Settings — "Add-ons" section

Add to `simpleSettings.ts`:
- New control kind: `connection-card` (reuse or extend the existing one used by channels)
- New section key or extend the existing `connections` section
- Controls for each configured MCP server (dynamically loaded from API)

The MCP management in settings consists of:
1. **List view**: Cards for each configured server (name, transport, connection status, tool count)
2. **Add button**: Opens a form (modal or new page)
3. **Server card tap**: Opens detail view with test button and config display
4. **Delete**: Swipe-to-delete or long-press menu

### New composable: `useMCP.ts`

```typescript
export function useMCP() {
  const servers = ref<MCPServerDetail[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function loadServers()
  async function addServer(name: string, config: MCPServerConfig): Promise<void>
  async function removeServer(name: string): Promise<void>
  async function testServer(name: string): Promise<MCPServerTestResult>
}
```

### Config shape for the app

The app uses `MCPServerConfig` matching the backend Go struct:
```typescript
export interface MCPServerConfig {
  enabled: boolean
  transport: 'stdio' | 'sse' | 'streamablehttp'
  command?: string
  args?: string[]
  env?: Record<string, string>
  childEnvAllowlist?: string[]
  url?: string
  headers?: Record<string, string>
  connectTimeoutSeconds: number
  toolTimeoutSeconds: number
  allowInsecureHttp: boolean
}
```

## Manager status tracking

The `mcp.Manager` currently tracks sessions and tools but doesn't expose per-server status to external callers. Add:

```go
type ServerStatus struct {
    Connected bool     `json:"connected"`
    ToolCount int      `json:"toolCount"`
    Tools     []string `json:"tools"`
    LastError string   `json:"lastError,omitempty"`
}

func (m *Manager) ServerStatus() map[string]ServerStatus
```

This method iterates the manager's sessions and tools maps to compile per-server status, returning `{connected: false}` for enabled servers that failed to connect.

## Testing strategy

### Go tests (or3-intern)

- `service_mcp_test.go` — test MCP CRUD endpoints with auth, test endpoint with mock manager
- `configure_tui_test.go` or `configure_test.go` — test new MCP section fields, status line
- `manager_test.go` — test `ServerStatus()` method
- `controlplane_test.go` — test enriched capabilities with MCP details

### App tests (or3-app)

- `useMCP.test.ts` — test composable against mock API responses
- `mcp-management.test.ts` — test server card component rendering

## Failure modes and safeguards

- **Invalid config saved via TUI/API**: Validation is applied before save (same `validateMCPServers` as startup)
- **Secrets in logs**: Test endpoint errors are bounded; headers and env values are never included in error messages
- **Concurrent edits**: No locking needed — config operations are serial, and the last save wins
- **Restart reminder**: All save operations return `restartRequired: true` and the UI shows a prominent note
- **Transport field visibility**: The TUI and API only show transport-relevant fields (no URL field for stdio, no command field for HTTP)
