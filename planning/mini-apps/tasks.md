# Mini Apps Tasks

## 1. Config and schema foundation

- [ ] 1.1 Add `MiniAppsConfig` struct to `internal/config/types.go` with fields: `Enabled`, `PortRangeStart`, `PortRangeEnd`, `DataDir`, `HealthIntervalSecs`, `HealthTimeoutSecs`, `LogBufferBytes`, `InstallTimeoutSecs`, `BuildTimeoutSecs`, `StopGraceSecs`, `ReadyTimeoutSecs`, `RescanIntervalSecs`, `ProxyTimeoutSecs`. Requirements: 1, 2
- [ ] 1.2 Add `MiniApps MiniAppsConfig` field to the `Config` struct in `internal/config/types.go`. Requirements: 1
- [ ] 1.3 Add default values in `internal/config/defaults.go`: `Enabled: true`, `PortRangeStart: 49152`, `PortRangeEnd: 49252`, `DataDir: filepath.Join(root, "mini-apps")`, `HealthIntervalSecs: 10`, `HealthTimeoutSecs: 3`, `LogBufferBytes: 65536`, `InstallTimeoutSecs: 120`, `BuildTimeoutSecs: 120`, `StopGraceSecs: 5`, `ReadyTimeoutSecs: 30`, `RescanIntervalSecs: 30`, `ProxyTimeoutSecs: 30`. Requirements: 2, 3, 4, 6, 8
- [ ] 1.4 Add env overrides in `internal/config/env.go`: `OR3_MINI_APPS_ENABLED`, `OR3_MINI_APPS_DATA_DIR`. Requirements: 1
- [ ] 1.5 Add `mini_apps` table migration in `internal/db/db.go` `migrate()`: columns `id`, `name`, `runtime`, `status`, `port`, `last_port`, `pid`, `started_at`, `stopped_at`, `exit_code`, `last_error`, `installed`, `install_hash`, `manifest_json`, `workspace`, `updated_at`. Add indexes on `(status, updated_at)` and `(workspace)`. Requirements: 2, 3
- [ ] 1.6 Add config validation in `internal/config/validate.go`: port range start < end, log buffer > 0, timeouts > 0, rescan interval > 0. Requirements: 1, 2

## 2. Core supervisor package (`internal/miniapp/`)

- [ ] 2.1 Create `internal/miniapp/id.go` with `ValidateAppID(id string) error`. Use `regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)`. Reject empty, `..`, path separators, uppercase, underscores, leading hyphens. Requirements: 1A
- [ ] 2.2 Create `internal/miniapp/manifest.go` with `Manifest`, `ManifestCommands`, `ManifestServer` structs (no `ManifestStorage` for V1). Include `SchemaVersion int` field. Implement `ParseManifest(path string) (*Manifest, error)` and `(m *Manifest) Validate() error`. Validate calls `ValidateAppID`. Reject `schemaVersion > SupportedSchemaVersion`. Requirements: 1, 1A
- [ ] 2.3 Create `internal/miniapp/ringbuf.go` with `ringBuffer` struct. Implement `newRingBuffer(size int)`, `Write(p []byte) (int, error)`, `Lines(n int) []string`, `Tail(maxBytes int) []byte`, `Reset()`. Requirements: 3
- [ ] 2.4 Create `internal/miniapp/portalloc.go` with `portAllocator` struct. Implement `newPortAllocator(start, end int)`, `Acquire(appID string, preferredPort int) (int, error)`, `Release(port int)`, `IsAvailable(port int) bool`. Use `net.Listen("tcp", "127.0.0.1:port")` to verify availability. `Acquire` tries `preferredPort` first when non-zero and available. Requirements: 2
- [ ] 2.5 Create `internal/miniapp/store.go` with SQLite CRUD helpers: `upsertMiniApp`, `getMiniApp`, `listMiniApps(workspace string)`, `updateMiniAppStatus`, `updateMiniAppPort`, `clearStaleRunningApps(workspace string)`. All queries filter by `workspace` column. Requirements: 2, 3
- [ ] 2.6 Create `internal/miniapp/env.go` with `buildChildEnv(cfg, appID, port, dataDir, servicePort, portEnv string) []string`. Build base env from existing `ChildEnvAllowlist`, add `PATH`, `HOME`, `TMPDIR`, `PORT`, `OR3_APP_PORT`, `OR3_APP_ID`, `OR3_APP_DATA_DIR`, `OR3_APP_URL`, `HOST=127.0.0.1`, `NODE_ENV=development`, `LANG`, `LC_ALL`. When `portEnv` is non-empty, add `<portEnv>=<port>`. Requirements: 3A
- [ ] 2.7 Create `internal/miniapp/supervisor.go` with `Supervisor` struct, `AppState`, `AppStatus` constants, `StartOptions`, `AuditLogger` interface, and `managedApp` internal type (with per-app `sync.Mutex`). Implement `NewSupervisor`, `Scan`, `List`, `Get`, `Create`, `Start`, `Stop`, `Restart`, `Delete`, `Logs`, `ResolvePort`, `Shutdown`. Requirements: 3, 4, 8, 11
- [ ] 2.8 Implement process start logic in `supervisor.go`: run command via `sh -c`, set `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`, inject child env via `buildChildEnv`, set working directory to app dir, wire stdout/stderr to ring buffer, start health check goroutine. Requirements: 3, 3A, 4, 10, 12
- [ ] 2.9 Implement process stop logic: `syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)` to process group, wait `StopGraceSecs`, `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` if still alive. Persist log tail (8KB) to `<data-dir>/.or3-last.log`. Update state, release port, clear `port` but preserve `last_port`. Requirements: 3
- [ ] 2.10 Implement install/build step execution with timeouts (`InstallTimeoutSecs`, `BuildTimeoutSecs`). Store SHA-256 hash of `commands.install` in `.or3-installed` marker file and SQLite `install_hash`. Skip install when hash matches and `!opts.Reinstall`. Re-run install when hash changes or `Reinstall: true`. Do NOT use bubblewrap/sandbox. Requirements: 10
- [ ] 2.11 Implement health check goroutine: periodic HTTP GET to `http://127.0.0.1:<port><healthPath>`, track consecutive failures, transition to `unhealthy` after 3 failures. Loopback-only. Requirements: 6, 12
- [ ] 2.12 Implement ready pattern detection: poll ring buffer for `readyPattern` during startup (100ms interval), transition to `running` when found, timeout after `ReadyTimeoutSecs`. Requirements: 6
- [ ] 2.13 Implement `Scan()`: `os.ReadDir` on `<workspace>/.apps/`, skip symlinked directories, validate directory names against app ID charset, parse each `app.json`, reconcile with SQLite state (filter by workspace). Requirements: 8
- [ ] 2.14 Implement periodic rescan goroutine: ticker at `RescanIntervalSecs`, call `Scan()`, detect new/removed/changed apps. Do not affect running apps. Stop on `Shutdown()`. Requirements: 8
- [ ] 2.15 Implement `Logs(id, lines)`: return lines from ring buffer when app is running, read from `<data-dir>/.or3-last.log` when stopped. Requirements: 3
- [ ] 2.16 Implement `Shutdown()`: stop all running apps gracefully (process group SIGTERM/SIGKILL), release all ports, stop rescan goroutine. Requirements: 3
- [ ] 2.17 Implement idempotent start/stop: `Start` on `running`/`starting` returns success with current status. `Stop` on `stopped` returns success. Requirements: 11
- [ ] 2.18 Implement audit event emission: call `AuditLogger.Log` for `mini_app.start`, `mini_app.stop`, `mini_app.delete`, `mini_app.error` when audit logger is non-nil. Requirements: 7

## 3. Agent tools (`internal/tools/miniapp.go`)

- [ ] 3.1 Define `MiniAppSupervisor` interface in `internal/tools/miniapp.go` matching the supervisor methods needed by tools (including `StartOptions`). Requirements: 7
- [ ] 3.2 Implement `CreateMiniAppTool`: accepts `id`, `name`, `manifest` (JSON object). Validates ID via `ValidateAppID`. Creates `.apps/<id>/` and writes `app.json`. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7, 1A
- [ ] 3.3 Implement `ListMiniAppsTool`: returns all discovered apps with id, name, status, port (or `last_port` when stopped), runtime. `CapabilitySafe`, group `ToolGroupRead`. Requirements: 7
- [ ] 3.4 Implement `StartMiniAppTool`: accepts `id` and optional `reinstall` boolean. Delegates to supervisor with `StartOptions`. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7, 10
- [ ] 3.5 Implement `StopMiniAppTool`: accepts `id`, delegates to supervisor. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.6 Implement `RestartMiniAppTool`: accepts `id`, delegates to supervisor. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.7 Implement `MiniAppStatusTool`: accepts `id`, returns detailed status including port, last_port, PID, uptime, health, last log lines. `CapabilitySafe`, group `ToolGroupRead`. Requirements: 7
- [ ] 3.8 Implement `MiniAppLogsTool`: accepts `id` and optional `lines` (default 50), returns log output. `CapabilitySafe`, group `ToolGroupRead`. Requirements: 7
- [ ] 3.9 Implement `DeleteMiniAppTool`: accepts `id`, delegates to supervisor. When `ApprovalBroker` is non-nil and runtime profile requires it, request approval before deleting. `CapabilityGuarded`, group `ToolGroupExec`. Requirements: 7
- [ ] 3.10 Add tool metadata reporting (`Metadata()`) for all mini app tools with correct groups (`ToolGroupRead` for read tools, `ToolGroupExec` for lifecycle tools). Requirements: 7

## 4. Service routes and proxy (`cmd/or3-intern/`)

- [ ] 4.1 Create `cmd/or3-intern/service_miniapp.go` with `handleMiniApps` handler dispatching: GET list, POST create, GET status, POST start (with optional `reinstall` body), POST stop, POST restart, GET logs, DELETE. Requirements: 9
- [ ] 4.2 Create `cmd/or3-intern/service_miniapp_proxy.go` with `handleMiniAppProxy` handler. Extract app-id, redirect `/apps/<id>` to `/apps/<id>/`, strip prefix from path, resolve port, proxy via `httputil.ReverseProxy`. Return 404 for missing, 502 for stopped. Requirements: 5, 5A
- [ ] 4.3 Implement proxy `Director` function: set `Host` to `127.0.0.1:<port>`, inject `X-Or3-App-Id`, `X-Forwarded-Prefix: /apps/<app-id>`, `X-Forwarded-For`, `X-Forwarded-Proto`. Strip `/apps/<app-id>` from `r.URL.Path`. Requirements: 5, 5A
- [ ] 4.4 Add WebSocket upgrade support: detect `Upgrade: websocket` header, hijack client connection, dial backend at `127.0.0.1:<port>`, bidirectional copy. Apply same path stripping to the backend dial path. Requirements: 5
- [ ] 4.5 Set `FlushInterval: -1` on the reverse proxy for SSE/streaming support. Apply `ProxyTimeoutSecs` via `ModifyResponse` or context timeout for non-streaming requests. Requirements: 5
- [ ] 4.6 Update `cmd/or3-intern/service_routes.go`: add `{Path: "/internal/v1/mini-apps", Subtree: true, Handler: server.handleMiniApps}` and `{Path: "/apps/", Subtree: true, Handler: server.handleMiniAppProxy}`. Requirements: 5, 9
- [ ] 4.7 Add mini app routes to `serviceRouteRequirementForRequest` in `cmd/or3-intern/service_auth.go`: `/internal/v1/mini-apps` requires session auth, `/apps/` requires session auth. Requirements: 5, 9

## 5. Wire supervisor into service startup

- [ ] 5.1 Add `miniappSupervisor` field to `serviceServer` struct in `cmd/or3-intern/service.go`. Requirements: 3
- [ ] 5.2 Create the supervisor in `runServiceCommandWithBrokerOptionsCronMCPAndChannels` when workspace is configured and `MiniApps.Enabled` is true. Pass config, workspace path, DB, and audit logger (from `control().Audit`). Requirements: 3
- [ ] 5.3 Call `supervisor.Scan()` after creation to discover existing apps. Requirements: 8
- [ ] 5.4 Call `supervisor.Shutdown()` during graceful shutdown (before HTTP server shutdown). Requirements: 3
- [ ] 5.5 Add lazy init pattern (`miniapp()`) on `serviceServer` matching existing `app()` and `control()` patterns. Requirements: 3

## 6. Register mini app tools in the tool registry

- [ ] 6.1 Update `buildToolRegistryWithOptions` in `cmd/or3-intern/main.go` to accept a `miniapp.Supervisor` parameter (or nil when disabled). Requirements: 7
- [ ] 6.2 Register all 8 mini app tools when the supervisor is non-nil. Pass workspace path for `CreateMiniAppTool`, pass `ApprovalBroker` for `DeleteMiniAppTool`. Requirements: 7
- [ ] 6.3 Thread the supervisor through the call chain: `runServiceCommand` -> `buildRuntimeTools` closure -> `buildToolRegistryWithOptions`. Requirements: 7

## 7. Tests

- [ ] 7.1 Add app ID validation tests in `internal/miniapp/id_test.go`: valid IDs, uppercase rejection, `..` rejection, path separator rejection, too long, empty, leading hyphen. Requirements: 1A
- [ ] 7.2 Add manifest parsing tests in `internal/miniapp/manifest_test.go`: valid manifest, missing id, missing commands, invalid JSON, schema version 1 default, unsupported schema version, `portEnv` optional, ID validation delegated to `ValidateAppID`. Requirements: 1, 1A
- [ ] 7.3 Add ring buffer tests in `internal/miniapp/ringbuf_test.go`: write/read, overflow wrap, line extraction, tail extraction, concurrent writes. Requirements: 3
- [ ] 7.4 Add port allocator tests in `internal/miniapp/portalloc_test.go`: acquire, release, exhaustion, concurrent acquire, preferred port reuse, preferred port fallback when occupied, release idempotency. Requirements: 2
- [ ] 7.5 Add SQLite store tests in `internal/miniapp/store_test.go`: upsert, get, list with workspace filter, update status, clear stale, idempotent migration, `last_port` preservation across status changes. Requirements: 2, 3
- [ ] 7.6 Add supervisor integration tests in `internal/miniapp/supervisor_test.go`: create app, start with real `python3 -m http.server` process, verify loopback binding, stop with process group kill, verify no orphan processes, restart, delete, log capture, persisted log tail, status transitions, idempotent start/stop, concurrent start on different apps. Requirements: 3, 4, 8, 11, 12
- [ ] 7.7 Add install hash tests: hash computation, hash change triggers reinstall, `Reinstall: true` forces reinstall, same hash skips install. Requirements: 10
- [ ] 7.8 Add health check tests: mock HTTP server on loopback, pass/fail transitions, consecutive failure threshold, loopback-only verification. Requirements: 6, 12
- [ ] 7.9 Add child env tests: verify all required vars present, `portEnv` custom var, no secrets leaked, `HOST=127.0.0.1` always set. Requirements: 3A
- [ ] 7.10 Add service route tests in `cmd/or3-intern/service_miniapp_test.go`: list, create, start (with reinstall), stop, restart, logs, delete endpoints with mock supervisor. Requirements: 9
- [ ] 7.11 Add proxy tests in `cmd/or3-intern/service_miniapp_proxy_test.go`: path stripping (`/apps/test/foo` -> `/foo`), redirect (`/apps/test` -> `/apps/test/`), 502 for stopped, 404 for missing, header injection (`X-Forwarded-Prefix`, `X-Or3-App-Id`). Requirements: 5, 5A
- [ ] 7.12 Add WebSocket proxy test: upgrade, bidirectional message passing, path stripping on WS path. Requirements: 5
- [ ] 7.13 Add SSE proxy test: streaming response flush, no buffering. Requirements: 5
- [ ] 7.14 Add mini app routes to `TestServiceRouteRequirementForRequest_SensitivityMatrix` in `cmd/or3-intern/service_auth_rollout_test.go`. Requirements: 5, 9
- [ ] 7.15 Add tool tests in `internal/tools/miniapp_test.go`: each tool's Execute with mock supervisor, error handling, parameter validation, capability level assertions (Safe for read, Guarded for lifecycle). Requirements: 7

## 8. Config validation, readiness, and doctor

- [ ] 8.1 Update `internal/config/readiness.go` to include mini app readiness: when `MiniApps.Enabled` and workspace is set, verify `.apps/` directory can be created and data dir is writable. Requirements: 8
- [ ] 8.2 Add mini app checks to `internal/doctor` engine: data dir exists or can be created, port range is valid, port range does not conflict with service listen port, workspace `.apps/` is accessible. Requirements: 4

## 9. Documentation

- [ ] 9.1 Add `docs/v1/features/mini-apps.md` with user-facing documentation: what mini apps are, app ID rules, the manifest format (with `schemaVersion`), the proxy URL pattern and path-stripping contract, data directory usage, framework-specific base path config (Vite, Next, Nuxt, Go, static), `OR3_APP_URL` format. Requirements: 1, 4, 5, 5A
- [ ] 9.2 Add a sample `app.json` and minimal "hello world" app under `docs/v1/features/mini-apps-examples/` for agent bootstrap reference. Include examples for: Vue+Vite, static HTML, Go net/http. Requirements: 1, 5A
- [ ] 9.3 Add mini app tool descriptions to the agent bootstrap or TOOLS.md guidance so the agent knows when and how to use them. Document capability levels and approval requirements. Requirements: 7
- [ ] 9.4 Document `.apps/` in `.gitignore` recommendations: source code can be committed, data dir should not, `.or3-installed` marker should not. Requirements: 4

## Out of scope

- Container/sandbox isolation (V2 consideration)
- Resource limits (CPU, memory, disk quotas)
- Max concurrent running apps beyond the port pool size (100 default is the implicit limit)
- App marketplace or sharing between users
- Auto-restart on crash (agent or user must restart in V1)
- Pinned apps that auto-start on service boot (V1.5 consideration; `last_port` hint is preserved for this)
- App-to-app communication
- Custom domain or TLS termination for apps
- App build caching or incremental builds
- Multi-workspace app support (V1 is single-workspace; workspace column enables future filtering)
- or3-app UI components for mini app management (separate or3-app planning)
- `@or3/mini-ui` or `@or3/mini-sdk` npm packages (separate effort)
- State events / internal event bus for status changes (V2; or3-app polls or rescans in V1)
- Windows process signals (V1 targets Unix; `Setpgid`/negative-PID kill are Unix-only)
- CORS configuration (apps are responsible for their own CORS; the proxy does not inject CORS headers in V1)
- File-system notify (inotify/fsevents) for instant discovery; periodic rescan is sufficient for V1
- Multi-instance same app ID (impossible by design: one directory per ID, one process per directory)
