# 1. Config surface

- [ ] (R1, R4, R5) Add `MCPServerConfig` and `ToolsConfig.MCPServers` to `internal/config/config.go` with disabled-by-default behavior.
- [ ] (R1, R4) Normalize defaults for transport, connect timeout, tool timeout, and insecure HTTP handling.
- [ ] (R1, R7) Add config tests in `internal/config/config_test.go` for backward compatibility, validation, and safe URL rules.

# 2. MCP client/manager package

- [ ] (R2, R4, R5) Add a focused package such as `internal/mcp` to own server connections, tool discovery, and cleanup.
- [ ] (R2, R4) Implement stdio transport first-class support.
- [ ] (R1, R4) Implement explicit HTTP transport support for `sse` and `streamableHttp` with conservative URL validation and bounded timeouts.
- [ ] (R2, R6) Add startup logging hooks that report successful tool counts and bounded failures per server.

# 3. Tool wrappers and registry integration

- [ ] (R2, R3) Add MCP-backed tool wrapper types implementing the existing `internal/tools.Tool` interface.
- [ ] (R2, R3) Preserve remote tool description and JSON schema in local tool definitions.
- [ ] (R3, R5) Register MCP tools into the normal registry before worker startup in `cmd/or3-intern/main.go`.
- [ ] (R5) Add minimal registry synchronization only if startup-time registration proves insufficient.

# 4. Execution semantics

- [ ] (R3, R4) Enforce per-call timeouts and bounded error handling for MCP tool execution.
- [ ] (R3) Convert MCP result content into bounded text that flows through the existing runtime/artifact spill logic.
- [ ] (R3, R5) Ensure MCP tool failures do not crash the runtime or block native tool execution.

# 5. Lifecycle wiring

- [ ] (R2, R5, R6) Update `cmd/or3-intern/main.go` to create the MCP manager, register tools before serving traffic, and close MCP resources on shutdown.
- [ ] (R2, R6) Decide and document whether connection failures are soft-fail by default or configurable as hard-fail; implement the chosen startup behavior consistently.

# 6. Tests

- [ ] (R2, R3, R7) Add unit tests for MCP wrapper naming, schema propagation, timeout handling, and result conversion.
- [ ] (R2, R7) Add integration-style tests with a fake/stdio MCP server or transport stub covering successful and partial-failure registration.
- [ ] (R3, R7) Extend runtime/registry-adjacent tests to verify MCP tools appear in tool definitions and execute via the normal path.

# 7. Documentation

- [ ] (R6) Update `README.md` with MCP config examples, supported transports, startup behavior, and safety notes.
- [ ] (R4, R6) Document explicit network/URL trust implications and the recommendation to prefer stdio for local trusted servers.

# 8. Out of scope

- [ ] No live reconnect manager or dynamic hot-add/hot-remove of MCP tools in the first pass.
- [ ] No new plugin marketplace, UI, or separate MCP gateway service.
- [ ] No persistence of MCP tool catalogs in SQLite.
- [ ] No automatic inheritance of the full ambient process environment into MCP server processes.
