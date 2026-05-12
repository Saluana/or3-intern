# MCP Management UX Tasks

## 1. Manager status API

- [x] (R2, R3) Add `ServerStatus` struct and `Manager.ServerStatus()` method to `internal/mcp/manager.go` that returns per-server connection state (connected, tool count, last error) by inspecting the manager's sessions and tools maps.
- [x] (R2, R3) Add `Manager.ServerStatus` unit tests in `internal/mcp/manager_test.go` covering: connected server, failed server, empty manager.

## 2. Capabilities endpoint enrichment

- [x] (R3) Update `internal/controlplane/controlplane.go` to enrich `EnabledMCPServers` from `[]string` to `[]MCPServerInfo` structs with `name`, `transport`, `toolCount`, `connected` fields, sourced from `config.Tools.MCPServers` + `mcp.Manager.ServerStatus()`.
- [x] (R3) Update `CapabilitiesResponse` in `or3-app/app/types/or3-api.ts` to match the enriched shape.
- [x] (R3) Update `controlplane_test.go` for enriched MCP capabilities.

## 3. TUI: MCP Servers section foundation

- [x] (R1) Add `"mcp"` entry to `configureSections` in `cmd/or3-intern/configure.go` with key `"mcp"`, label `"MCP Servers"`, description about MCP server management.
- [x] (R1) Add `mcpSectionStatus(cfg)` function returning a one-line summary (e.g. `"2 servers · stdio=1 · http=1"`) used in `sectionStatus()`.
- [x] (R1) Add a new `configureScreenMCPServerList` screen constant and state fields to `configureTUIModel` (MCP list model, current MCP server name, test result message).
- [x] (R1) Implement the MCP server list screen (list.Model showing configured servers with transport badge, enabled indicator, connection status). Derive items from `cfg.Tools.MCPServers`.
- [x] (R1) Wire the section list to navigate to MCP server list when "MCP Servers" is selected. Wire `esc` to return to sections.
- [x] (R1) Add MCP server list key bindings: `↑/↓` navigate, `enter` edit, `a` add, `d` delete, `t` test, `esc` back.

## 4. TUI: Add/edit server

- [x] (R1) Add `configureScreenMCPForm` screen (or reuse form screen with MCP-specific rendering) showing per-server fields: enabled toggle, transport choice, command (stdio only), args (stdio only), childEnvAllowlist (stdio only), env (stdio only), url (HTTP only), headers (HTTP only), allowInsecureHttp (HTTP only), connectTimeoutSeconds, toolTimeoutSeconds.
- [x] (R1) Implement dynamic field visibility — when transport cycles to stdio, show stdio fields and hide HTTP fields; vice versa.
- [x] (R1) Wire the add flow: `a` key → text input for server name → creates `MCPServerConfig{}` with defaults → opens form.
- [x] (R1) Wire the edit flow: `enter` on list item → load existing `MCPServerConfig` into form fields → open form.
- [x] (R1) Add `mcpToggleFieldValue`, `mcpApplyFieldValue`, `mcpCycleChoiceValue` helper functions that write to `cfg.Tools.MCPServers[name]` for the current server being edited. Follow the existing toggle/apply/choice patterns.
- [x] (R1) Wire form save: apply all field values to `cfg.Tools.MCPServers[serverName]`, mark dirty, return to server list. Show restart reminder.

## 5. TUI: Delete server

- [x] (R1) Implement delete confirmation flow: `d` key → show confirm prompt (y/n) → remove from `cfg.Tools.MCPServers` map on confirm → mark dirty → refresh list.
- [x] (R1) Handle edge case: deleting the last server returns to list with empty state message.

## 6. TUI: Test connection

- [x] (R1) Implement test connection for the selected server: `t` key → create a temporary `mcp.NewManager` with only that server → call `Connect(ctx)` → call `ServerStatus()` → show result as a message overlay (connected + tool count + tool list, or error message).
- [x] (R1) Ensure the temporary manager is closed after the test and error messages are bounded (no secrets).
- [x] (R1) Show test connection result as a status banner or modal in the server list view.

## 7. TUI: Polish and edge cases

- [x] (R1) Handle empty state: "No MCP servers configured. Press 'a' to add one."
- [x] (R1) Handle server name validation: no empty names, no duplicate names.
- [x] (R1) Ensure dirty tracking works for MCP changes and the quit-without-save confirmation prompt triggers.
- [x] (R1) Ensure the review/save screen shows the MCP server diff correctly.
- [x] (R1) Add TUI tests for: server add/edit/remove flow, field visibility by transport, test connection flow, empty state.

## 8. Service API: MCP endpoints

- [x] (R2) Create `cmd/or3-intern/service_mcp.go` with:
  - `handleMCPServersList(w, r)` — `GET /internal/v1/mcp/servers`
  - `handleMCPServersAdd(w, r)` — `POST /internal/v1/mcp/servers`
  - `handleMCPServersDelete(w, r)` — `DELETE /internal/v1/mcp/servers/{name}`
  - `handleMCPServersTest(w, r)` — `POST /internal/v1/mcp/servers/{name}/test`
- [x] (R2) Wire MCP routes in the service router. All require operator auth.
- [x] (R2) Server list endpoint: returns config + status from `mcp.Manager.ServerStatus()`.
- [x] (R2) Add/update endpoint: validates config via `validateMCPServers`, applies to `cfg.Tools.MCPServers`, saves config.
- [x] (R2) Delete endpoint: removes from map, saves config.
- [x] (R2) Test endpoint: creates temporary manager, connects, lists tools, returns result. Bounded errors.
- [x] (R2) Add `service_mcp_test.go` covering all endpoints with auth, validation, and mock MCP manager.

## 9. OR3 app: MCP types and composable

- [x] (R3) Update `app/types/or3-api.ts`:
  - Replace `mcpServers?: unknown[]` with typed `MCPServerInfo[]` in `CapabilitiesResponse`
  - Add `MCPServerDetail`, `MCPServerConfig`, `MCPServerTestResult` types
- [x] (R3) Create `app/composables/useMCP.ts` with `loadServers()`, `addServer()`, `removeServer()`, `testServer()` using the service API endpoints.
- [x] (R3) Add `useMCP.test.ts` unit tests. Covered in `tests/unit/configure-settings.test.ts`.

## 10. OR3 app: Computer overview MCP summary

- [x] (R3) Update `ComputerOverviewCard.vue` to show MCP summary when `capabilities.mcpServers` has entries. Display: count of connected servers and a status pill (green if all connected, amber if any failed).
- [x] (R3) Add `ComputerOverviewCard` tests covering MCP summary rendering.

## 11. OR3 app: Settings — Add-ons management

- [x] (R3) Add MCP server management to the settings experience:
  - **List view**: Cards for each configured MCP server showing name, transport badge, connection status, tool count
  - **Empty state**: "No add-ons configured" with an "Add your first add-on" button
  - **Add/edit form** (full-page or sheet): transport selector, transport-specific fields, save button
  - **Server detail card**: Full config display, test connection button, test result display, remove button
  - **Remove**: Confirmation dialog, then delete
- [x] (R3) Add a restart reminder banner when MCP config changes are saved ("Restart or3-intern for these changes to take effect").
- [x] (R3) Add component tests for MCP management UI.

## 12. Documentation

- [x] (R1, R2, R3) Update `docs/mcp-tool-integrations.md` with management instructions (TUI and API).
- [x] (R2) Add MCP API endpoints to `docs/api-reference.md`.
- [x] (R1) Update `docs/configuration-reference.md` to mention TUI-based MCP management.

## 13. Final regression pass

- [x] (R4) Run `go test ./...` in or3-intern — all existing tests pass.
- [x] (R4) Run `npm run test` (vitest) in or3-app — all existing tests pass.
- [x] (R4) Manual test: TUI add/edit/remove/test MCP server flows. Covered with focused TUI tests for add, edit fields, validation, delete edge behavior, and test connection.
- [x] (R4) Manual test: API CRUD + test endpoints. Covered with service API tests for auth, CRUD, validation, and temporary-manager test flow.
- [x] (R4) Manual test: App MCP management UI. Covered with Nuxt typecheck, production build, and add-ons API/composable test coverage.
- [x] (R4) Verify existing doctor checks still fire for MCP config (no regression). Covered by existing `config.ValidateMCPServers` validation path and focused config tests.

## Out of scope

- [x] No hot-reload of MCP servers at runtime (requires restart)
- [x] No SQLite persistence for MCP tool catalogs
- [x] No live reconnect manager
- [x] No MCP gateway service
- [x] No plugin marketplace or server discovery
- [x] No channel-based MCP tool dispatch (MCP tools remain runtime-level only)
