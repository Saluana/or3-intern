# Mini Apps Tasks

## 1. Config and schema foundation

- [ ] 1.1 Add `MiniAppsConfig` struct to `internal/config/types.go` with fields: `Enabled`, `PortRangeStart`, `PortRangeEnd`, `DataDir`, `HealthIntervalSecs`, `HealthTimeoutSecs`, `LogBufferBytes`, `InstallTimeoutSecs`, `BuildTimeoutSecs`, `StopGraceSecs`, `ReadyTimeoutSecs`. Requirements: 1, 2
- [ ] 1.2 Add `MiniApps MiniAppsConfig` field to the `Config` struct in `internal/config/types.go`. Requirements: 1
- [ ] 1.3 Add default values in `internal/config/defaults.go`: `Enabled: true`, `PortRangeStart: 49152`, `PortRangeEnd: 49252`, `DataDir: filepath.Join(root, "mini-apps")`, `HealthIntervalSecs: 10`, `HealthTimeoutSecs: 3`, `LogBufferBytes: 65536`, `InstallTimeoutSecs: 120`, `BuildTimeoutSecs: 120`, `StopGraceSecs: 5`, `ReadyTimeoutSecs: 30`. Requirements: 2, 3, 4, 6
- [ ] 1.4 Add env overrides in `internal/config/env.go`: `OR3_MINI_APPS_ENABLED`, `OR3_MINI_APPS_DATA_DIR`. Requirements: 1
- [ ] 1.5 Add `mini_apps` table migration in `internal/db/db.go` `migrate()`: columns `id`, `name`, `runtime`, `status`, `port`, `pid`, `started_at`, `stopped_at`, `exit_code`, `last_error`, `installed`, `manifest_json`, `updated_at`. Add index on `(status, updated_at)`. Requirements: 2, 3
- [ ] 1.6 Add config validation in `internal/config/validate.go`: port range start < end, log buffer > 0, timeouts > 0. Requirements: 1, 2

## 2. Core supervisor package (`internal/miniapp/`)

- [ ] 2.1 Create `internal/miniapp/manifest.go` with `Manifest`, `ManifestCommands`, `ManifestServer`, `ManifestStorage` structs. Implement `ParseManifest(path string) (*Manifest, error)` and `(m *Manifest) Validate() error`. Requirements: 1
- [ ] 2.2 Create `internal/miniapp/ringbuf.go` with `ringBuffer` struct. Implement `newRingBuffer(size int)`, `Write(p []byte) (int, error)`, `Lines(n int) []string`, `Reset()`. Requirements: 3
- [ ] 2.3 Create `internal/miniapp/portalloc.go` with `portAllocator` struct. Implement `newPortAllocator(start, end int)`, `Acquire(appID string) (int, error)`, `Release(port int)`, `IsAvailable(port int) bool`. Use `net.Listen` to verify port availability. Requirements: 2
- [ ] 2.4 Create `internal/miniapp/store.go` with SQLite CRUD helpers: `upsertMiniApp`, `getMiniApp`, `listMiniApps`, `updateMiniAppStatus`, `clearStaleRunningApps`. Requirements: 2, 3
- [ ] 2.5 Create `internal/miniapp/supervisor.go` with `Supervisor` struct, `AppState`, `AppStatus` constants, and `managedApp` internal type. Implement `NewSupervisor`, `Scan`, `List`, `Get`, `Create`, `Start`, `Stop`, `Restart`, `Delete`, `Logs`, `ResolvePort`, `Shutdown`. Requirements: 3, 4, 8
- [ ] 2.6 Implement process start logic in `supervisor.go`: build `exec.Cmd` from manifest commands, inject environment (`PORT`, `OR3_APP_PORT`, `OR3_APP_ID`, `OR3_APP_DATA_DIR`, `OR3_APP_URL`), set working directory to app dir, wire stdout/stderr to ring buffer, start health check goroutine. Requirements: 3, 4, 10
- [ ] 2.7 Implement process stop logic: SIGTERM, wait `StopGraceSecs`, SIGKILL if needed, update state, release port. Requirements: 3
- [ ] 2.8 Implement install/build step execution with timeouts (`InstallTimeoutSecs`, `BuildTimeoutSecs`), install marker file (`.or3-installed`), and failure handling (mark `error`, capture output). Requirements: 10
- [ ] 2.9 Implement health check goroutine: periodic HTTP GET to `healthPath`, track consecutive failures, transition to `unhealthy` after 3 failures. Requirements: 6
- [ ] 2.10 Implement ready pattern detection: scan ring buffer for `readyPattern` during startup, transition to `running` when found, timeout after `ReadyTimeoutSecs`. Requirements: 6
- [ ] 2.11 Implement `Scan()`: walk `<workspace>/.apps/`, parse each `app.json`, reconcile with SQLite state. Requirements: 8
- [ ] 2.12 Implement `Shutdown()`: stop all running apps gracefully, release all ports. Requirements: 3

## 3. Agent tools (`internal/tools/miniapp.go`)

- [ ] 3.1 Define `MiniAppSupervisor` interface in `internal/tools/miniapp.go` matching the supervisor methods needed by tools. Requirements: 7
- [ ] 3.2 Implement `CreateMiniAppTool`: accepts `id`, `name`, `manifest` (JSON object), creates `.apps/<id>/app.json` using workspace path. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.3 Implement `ListMiniAppsTool`: returns all discovered apps with id, name, status, port, runtime. `CapabilitySafe`, group `ToolGroupRead`. Requirements: 7
- [ ] 3.4 Implement `StartMiniAppTool`: accepts `id`, delegates to supervisor. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.5 Implement `StopMiniAppTool`: accepts `id`, delegates to supervisor. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.6 Implement `RestartMiniAppTool`: accepts `id`, delegates to supervisor. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.7 Implement `MiniAppStatusTool`: accepts `id`, returns detailed status. `CapabilitySafe`, group `ToolGroupRead`. Requirements: 7
- [ ] 3.8 Implement `MiniAppLogsTool`: accepts `id` and optional `lines` (default 50), returns log output. `CapabilitySafe`, group `ToolGroupRead`. Requirements: 7
- [ ] 3.9 Implement `DeleteMiniAppTool`: accepts `id`, delegates to supervisor. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.10 Add tool metadata reporting (`Metadata()`) for all mini app tools with appropriate groups. Requirements: 7

## 4. Service routes and proxy (`cmd/or3-intern/`)

- [ ] 4.1 Create `cmd/or3-intern/service_miniapp.go` with `handleMiniApps` handler dispatching GET list, GET status, POST start/stop/restart, GET logs, DELETE. Requirements: 9
- [ ] 4.2 Create `cmd/or3-intern/service_miniapp_proxy.go` with `handleMiniAppProxy` handler using `httputil.ReverseProxy`. Extract app-id from path, resolve port, proxy request. Return 404 for missing apps, 502 for stopped apps. Requirements: 5
- [ ] 4.3 Add WebSocket upgrade support to the proxy handler: detect `Upgrade: websocket` header, hijack connection, establish backend connection, bidirectional copy. Requirements: 5
- [ ] 4.4 Update `cmd/or3-intern/service_routes.go`: add `{Path: "/internal/v1/mini-apps", Subtree: true, Handler: server.handleMiniApps}` and `{Path: "/apps/", Subtree: true, Handler: server.handleMiniAppProxy}`. Requirements: 5, 9
- [ ] 4.5 Add mini app routes to `serviceRouteRequirementForRequest` in `cmd/or3-intern/service_auth.go`: `/internal/v1/mini-apps` requires session auth, `/apps/` requires session auth. Requirements: 5, 9
- [ ] 4.6 Add proxy response headers: `X-Or3-App-Id`, standard `X-Forwarded-For`, `X-Forwarded-Proto`. Requirements: 5

## 5. Wire supervisor into service startup

- [ ] 5.1 Add `miniappSupervisor` field to `serviceServer` struct in `cmd/or3-intern/service.go`. Requirements: 3
- [ ] 5.2 Create the supervisor in `runServiceCommandWithBrokerOptionsCronMCPAndChannels` when workspace is configured and `MiniApps.Enabled` is true. Pass config, workspace path, and DB. Requirements: 3
- [ ] 5.3 Call `supervisor.Scan()` after creation to discover existing apps. Requirements: 8
- [ ] 5.4 Call `supervisor.Shutdown()` during graceful shutdown (before HTTP server shutdown). Requirements: 3
- [ ] 5.5 Add lazy init pattern (`miniapp()`) on `serviceServer` matching existing `app()` and `control()` patterns. Requirements: 3

## 6. Register mini app tools in the tool registry

- [ ] 6.1 Update `buildToolRegistryWithOptions` in `cmd/or3-intern/main.go` to accept a `miniapp.Supervisor` parameter (or nil when disabled). Requirements: 7
- [ ] 6.2 Register all 8 mini app tools when the supervisor is non-nil. Pass workspace path for `CreateMiniAppTool`. Requirements: 7
- [ ] 6.3 Thread the supervisor through the call chain: `runServiceCommand` -> `buildRuntimeTools` closure -> `buildToolRegistryWithOptions`. Requirements: 7

## 7. Tests

- [ ] 7.1 Add manifest parsing tests in `internal/miniapp/manifest_test.go`: valid manifest, missing id, missing commands, invalid JSON, empty portEnv. Requirements: 1
- [ ] 7.2 Add ring buffer tests in `internal/miniapp/ringbuf_test.go`: write/read, overflow wrap, line extraction, concurrent writes. Requirements: 3
- [ ] 7.3 Add port allocator tests in `internal/miniapp/portalloc_test.go`: acquire, release, exhaustion, concurrent acquire, release idempotency. Requirements: 2
- [ ] 7.4 Add SQLite store tests in `internal/miniapp/store_test.go`: upsert, get, list, update status, clear stale, idempotent migration. Requirements: 2, 3
- [ ] 7.5 Add supervisor integration tests in `internal/miniapp/supervisor_test.go`: create app, start with real `sleep` process, stop, restart, delete, log capture, status transitions. Requirements: 3, 4, 8
- [ ] 7.6 Add health check tests: mock HTTP server, pass/fail transitions, consecutive failure threshold. Requirements: 6
- [ ] 7.7 Add service route tests in `cmd/or3-intern/service_miniapp_test.go`: list, start, stop, restart, logs, delete endpoints with mock supervisor. Requirements: 9
- [ ] 7.8 Add proxy tests in `cmd/or3-intern/service_miniapp_proxy_test.go`: proxy to test server, 502 for stopped, 404 for missing, header injection. Requirements: 5
- [ ] 7.9 Add WebSocket proxy test: upgrade, bidirectional message passing. Requirements: 5
- [ ] 7.10 Add mini app routes to `TestServiceRouteRequirementForRequest_SensitivityMatrix` in `cmd/or3-intern/service_auth_rollout_test.go`. Requirements: 5, 9
- [ ] 7.11 Add tool tests in `internal/tools/miniapp_test.go`: each tool's Execute with mock supervisor, error handling, parameter validation. Requirements: 7

## 8. Config validation and readiness

- [ ] 8.1 Update `internal/config/readiness.go` to include mini app readiness: when `MiniApps.Enabled` and workspace is set, verify `.apps/` directory can be created. Requirements: 8
- [ ] 8.2 Add mini app config to `internal/doctor` engine: check data dir exists or can be created, port range is valid. Requirements: 4

## 9. Documentation

- [ ] 9.1 Add `docs/v1/features/mini-apps.md` with user-facing documentation: what mini apps are, how to create them, the manifest format, the proxy URL pattern, data directory usage. Requirements: 1, 4, 5
- [ ] 9.2 Add mini app tool descriptions to the agent bootstrap or TOOLS.md guidance so the agent knows when and how to use them. Requirements: 7

## Out of scope

- Container/sandbox isolation (V2 consideration)
- Resource limits (CPU, memory, disk quotas)
- App marketplace or sharing between users
- Auto-restart on crash (agent or user must restart in V1)
- App-to-app communication
- Custom domain or TLS termination for apps
- App build caching or incremental builds
- Multi-workspace app support (V1 is single-workspace)
- or3-app UI components for mini app management (separate or3-app planning)
- `@or3/mini-ui` or `@or3/mini-sdk` npm packages (separate effort)
