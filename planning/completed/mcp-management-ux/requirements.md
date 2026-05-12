# Overview

MCP engine support is fully implemented (`internal/mcp/manager.go`, connected at startup, tools flow through the normal tool loop). However, there is zero UI for managing MCP servers in either the `or3-intern` CLI TUI or the `or3-app` frontend. Users must hand-edit `config.json` to add/remove servers and have no visibility into connection status.

This plan adds MCP server management to both surfaces.

Scope covers:

- MCP server list/add/edit/remove in the `or3-intern configure` TUI
- MCP server management API endpoints for the app
- MCP server visibility and management in `or3-app` settings
- Connection testing (on-demand health check)

Assumptions:

- No hot-reload yet — config changes still require restart (consistent with v1 scope)
- The existing `internal/mcp/manager.go` and `MCPServerConfig` struct are the source of truth
- The `me.md` UX recommendation to call them "Add-ons" in the app is followed for consumer-facing language, while the TUI and API retain "MCP Servers" for technical clarity
- Existing transport safety rules (reject insecure HTTP, deny-by-default enforcement) remain in place

# Requirements

## 1. TUI: MCP Servers section in `or3-intern configure`

The configure TUI SHALL include a new "MCP Servers" section that allows viewing, adding, editing, removing, and testing configured MCP servers.

### Acceptance criteria

- A new "MCP Servers" entry appears in the configure section list, showing a summary line (e.g. `2 servers · stdio=1 · http=1 · 0 failing`)
- Selecting the section opens a server list view showing each configured server with its name, transport type, enabled status, and connection health (from the most recent startup or on-demand test)
- Users can navigate the server list, add a new server, select one to edit its fields, remove one, or test its connection
- Adding a server prompts for a name, then opens a per-server form with transport-specific fields:
  - Common: enabled toggle, transport choice (stdio/sse/streamableHttp), tool timeout, connect timeout
  - stdio: command, args, child env allowlist, env vars
  - HTTP: URL, headers, allow insecure HTTP toggle
- Editing a server reuses the same form, pre-filled with current values
- Removing a server shows a confirmation prompt before deletion
- Testing a server connects, lists available tools, and displays success + tool count or an error message
- Changes are written to config.json through the existing save path
- The "Tools" section status line gains an MCP summary (e.g. `2 MCP servers`)

## 2. API: MCP server management endpoints

The service SHALL expose REST endpoints for listing, adding, removing, and testing MCP servers, following existing service API conventions.

### Acceptance criteria

- `GET /internal/v1/mcp/servers` returns the list of configured MCP servers with config details and last-known connection status (tool count, last error) derived from the current Manager state
- `POST /internal/v1/mcp/servers` adds or updates a named MCP server config and saves it to config.json
- `DELETE /internal/v1/mcp/servers/{name}` removes a server config and saves
- `POST /internal/v1/mcp/servers/{name}/test` connects, lists tools, and returns success + tool list or a bounded error message
- All endpoints require operator-level auth (consistent with configure endpoints)
- The capabilities endpoint (`GET /internal/v1/capabilities`) SHALL include richer MCP server details beyond just names: transport type and tool count per server

## 3. OR3 app: MCP visibility and management

The app SHALL surface MCP server status in the computer overview and provide a management interface in settings.

### Acceptance criteria

- The Computer Overview card SHALL show a summary of MCP servers (count, connected/failing breakdown) when any are configured, sourced from the capabilities endpoint
- The Settings home page gains an "Add-ons" control that shows configured MCP servers as summary cards (or "No add-ons configured" empty state)
- Tapping into an MCP server card shows its detail: transport type, tool count, last connection status, connection test button
- The app SHALL support adding a new MCP server through a guided form with transport-appropriate fields
- The app SHALL support removing an MCP server with confirmation
- The app SHALL support testing an MCP server connection and displaying the result
- All write operations are gated on pairing/auth (existing session protection)

## 4. Safety and regression

### Acceptance criteria

- Adding/editing/removing MCP servers through the TUI or API does not affect the current running Manager state (no hot-reload) — users are informed a restart is needed for changes to take effect
- Existing transport safety validation (insecure HTTP rejection, hosted-profile enforcement) continues to apply to configs saved through the new management surfaces
- The existing doctor checks for MCP (engine_mcp.go) are not affected
- Existing tool loop, runtime, and native tools remain unchanged
- No new SQLite migrations are required for v1 management features
- Tests cover: TUI server add/edit/remove flow, API endpoint authorization and CRUD, config save/load round-trip for MCP servers
