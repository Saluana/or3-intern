# Mini Apps Requirements

## Overview

Mini apps are local app projects that OR3 can create, run, stop, and proxy. They live inside the workspace under `.apps/<app-id>/` and can use any stack (Vue, Nuxt, Next, Go, Rust, Python, Bun, Node, static HTML, etc.) as long as they follow a small contract defined in `app.json`.

OR3 does not care what stack is inside the app. It only cares about the manifest, the process lifecycle, the assigned port, and the data directory.

Scope assumptions:

- Mini apps are trusted local code created by the agent or the user, not untrusted third-party packages.
- The safety boundary targets accidental corruption, port conflicts, and hostile remote access, not malicious local code.
- V1 does not require containerization, sandboxing, or resource limits beyond process supervision and localhost binding.
- The agent creates apps using normal file tools; OR3 provides lifecycle tools and the supervisor.
- `or3-app` consumes mini apps through the OR3 reverse proxy, not by connecting to raw app ports.

## Requirements

### Requirement 1: App manifest contract

**Engineering objective:** Define a minimal `app.json` manifest that tells OR3 how to run any app, regardless of stack.

#### Acceptance Criteria

1. WHEN an app directory contains `.apps/<app-id>/app.json` THEN OR3 SHALL parse and validate the manifest before starting the app.
2. WHEN the manifest is valid THEN it SHALL contain at minimum: `schemaVersion`, `id`, `name`, and `commands` (with at least `dev` or `start`).
3. WHEN the manifest is missing required fields THEN OR3 SHALL reject it with a clear validation error naming the missing field.
4. WHEN the manifest specifies optional fields (`runtime`, `commands.install`, `commands.build`, `server.portEnv`, `server.healthPath`, `server.readyPattern`) THEN OR3 SHALL honor them.
5. WHEN two apps share the same `id` THEN OR3 SHALL reject the duplicate.
6. WHEN the manifest `id` does not match the containing directory name THEN OR3 SHALL reject it with a clear error.
7. WHEN `schemaVersion` is absent THEN OR3 SHALL assume `1`.
8. WHEN `schemaVersion` is greater than the supported version THEN OR3 SHALL reject the manifest with a forward-compatibility error.

### Requirement 1A: App ID validation

**Engineering objective:** Prevent path traversal, collisions, and ambiguous names by enforcing a strict app ID charset.

#### Acceptance Criteria

1. WHEN an app ID is validated THEN it SHALL match `^[a-z0-9][a-z0-9-]{0,63}$`.
2. WHEN an app ID contains `..`, path separators, uppercase letters, underscores, or leading hyphens THEN it SHALL be rejected.
3. WHEN discovery finds a directory under `.apps/` whose name does not match the ID charset THEN it SHALL be skipped and logged as a warning.
4. WHEN `create_mini_app` is called with an invalid ID THEN it SHALL return a validation error before touching the filesystem.

### Requirement 2: Port assignment and isolation

**Engineering objective:** OR3 assigns each running app a unique localhost port from a bounded range, preventing port conflicts and direct public exposure.

#### Acceptance Criteria

1. WHEN an app starts THEN OR3 SHALL assign it an available port from a configurable range (default `49152..49252`).
2. WHEN no ports are available THEN OR3 SHALL return a clear error indicating the app pool is full.
3. WHEN an app stops THEN OR3 SHALL release its port for reuse.
4. WHEN OR3 assigns a port THEN it SHALL inject `PORT`, `OR3_APP_PORT`, and (when `server.portEnv` is set) the custom env name into the app process, all with the same assigned port value.
5. WHEN an app stops THEN OR3 SHALL persist the last-used port in SQLite as `last_port` for UX hints, but the runtime `port` field SHALL be cleared to `0`.
6. WHEN the service restarts THEN all runtime port assignments SHALL be cleared; `last_port` is retained only as a hint for future allocation attempts.
7. WHEN a port from the pool is already occupied by a non-OR3 process THEN the allocator SHALL skip it and try the next available port.

### Requirement 3: Process supervision

**Engineering objective:** OR3 starts, stops, restarts, and monitors app processes with bounded log capture and correct process-group teardown.

#### Acceptance Criteria

1. WHEN `start_mini_app` is called THEN OR3 SHALL run the manifest's `dev` command (or `start` if `dev` is absent) through a shell (`sh -c` on Unix) with the app directory as the working directory.
2. WHEN the app process starts THEN OR3 SHALL inject the child environment defined in Requirement 3A.
3. WHEN the app process starts THEN OR3 SHALL place it in its own process group (`Setpgid: true`) so that stop can signal the entire tree.
4. WHEN the app process exits unexpectedly THEN OR3 SHALL mark the app as `stopped` with the exit code and persist the last log tail to disk.
5. WHEN `stop_mini_app` is called THEN OR3 SHALL send SIGTERM to the process group (negative PID), wait up to `StopGraceSecs` (default 5), then SIGKILL the group if any process remains.
6. WHEN `restart_mini_app` is called THEN OR3 SHALL stop and then start the app, attempting to reuse the `last_port` when available.
7. WHEN an app is running THEN OR3 SHALL capture stdout and stderr into a bounded in-memory ring buffer (default 64KB).
8. WHEN `mini_app_logs` is called THEN OR3 SHALL return the most recent log lines from the ring buffer (if running) or the persisted log tail (if stopped).
9. WHEN an app stops (cleanly or via crash) THEN OR3 SHALL persist the last 8KB of log output to `<data-dir>/.or3-last.log` for post-restart debugging.

### Requirement 3A: Child environment

**Engineering objective:** App processes receive a useful, bounded environment that does not leak OR3 internals.

#### Acceptance Criteria

1. WHEN an app process starts THEN OR3 SHALL inject: `PATH`, `HOME`, `TMPDIR`, `PORT`, `OR3_APP_PORT`, `OR3_APP_ID`, `OR3_APP_DATA_DIR`, `OR3_APP_URL`, and `HOST=127.0.0.1`.
2. WHEN `server.portEnv` is set to a non-empty value (e.g. `VITE_PORT`) THEN OR3 SHALL also set that env name to the assigned port.
3. WHEN `OR3_APP_URL` is injected THEN it SHALL be `http://127.0.0.1:<service-port>/apps/<app-id>` with no trailing slash.
4. WHEN the child environment is built THEN OR3 SHALL reuse the existing `ChildEnvAllowlist` config and add mini-app-specific entries (`NODE_ENV`, `LANG`, `LC_ALL`, `UV_*` when relevant).
5. WHEN the child environment is built THEN service secrets, master keys, auth tokens, database paths, and approval keys SHALL NOT be included.

### Requirement 4: App data directory

**Engineering objective:** Each app receives an isolated data directory for storage, separate from the app source code.

#### Acceptance Criteria

1. WHEN an app starts THEN OR3 SHALL create `<data-root>/<app-id>/` under the configured data root (default `~/.or3-intern/mini-apps/`).
2. WHEN the app process runs THEN OR3 SHALL inject `OR3_APP_DATA_DIR` pointing to this directory.
3. WHEN the app writes files to `OR3_APP_DATA_DIR` THEN those files SHALL persist across app restarts.
4. WHEN `delete_mini_app` is called THEN OR3 SHALL remove both the app source directory (`.apps/<app-id>`) and the app data directory.
5. WHEN the data root is not configured THEN OR3 SHALL fall back to `~/.or3-intern/mini-apps/`.

### Requirement 5: Reverse proxy with path stripping

**Engineering objective:** OR3 proxies HTTP requests to running mini apps through the existing service, stripping the `/apps/<app-id>` prefix so apps see root `/`.

#### Acceptance Criteria

1. WHEN a request arrives at `/apps/<app-id>/path` on the OR3 service THEN OR3 SHALL proxy it to `http://127.0.0.1:<assigned-port>/path` (prefix stripped).
2. WHEN the request path is exactly `/apps/<app-id>` (no trailing slash) THEN OR3 SHALL redirect to `/apps/<app-id>/` to preserve relative URL semantics.
3. WHEN the target app is not running THEN OR3 SHALL return `502 Bad Gateway` with a safe error message.
4. WHEN the target app does not exist THEN OR3 SHALL return `404 Not Found`.
5. WHEN the proxy forwards a request THEN it SHALL require the same OR3 session/auth as other service routes.
6. WHEN the proxy forwards a request THEN it SHALL inject `X-Or3-App-Id`, `X-Forwarded-Prefix: /apps/<app-id>`, `X-Forwarded-For`, and `X-Forwarded-Proto`.
7. WHEN WebSocket upgrade requests arrive at `/apps/<app-id>/*` THEN OR3 SHALL proxy them to the app's assigned port with the same path stripping.
8. WHEN SSE or streaming responses are returned by the app THEN OR3 SHALL not buffer the response body (flush immediately).
9. WHEN the proxy forwards a request THEN it SHALL apply the same body-size limits as other service routes and a configurable proxy timeout (default 30s for non-streaming).

### Requirement 5A: Proxy path contract for apps

**Engineering objective:** Define a single supported model for how apps handle being served under a path prefix, so Vite, Next, Nuxt, and static servers all work.

#### Acceptance Criteria

1. WHEN an app is proxied THEN the app SHALL receive requests as if it is running at root `/` (prefix is stripped by OR3).
2. WHEN an app generates asset URLs or API calls THEN it SHOULD use relative paths (e.g. `/api/data`, `/assets/style.css`) or read `X-Forwarded-Prefix` to construct absolute URLs.
3. WHEN the agent creates a Vite/Vue app THEN it SHALL set `base: '/apps/<app-id>/'` in the Vite config so asset URLs are correct.
4. WHEN the agent creates a Next.js app THEN it SHALL set `basePath: '/apps/<app-id>'` in `next.config.js`.
5. WHEN the agent creates a Nuxt app THEN it SHALL set `app.baseURL: '/apps/<app-id>/'` in `nuxt.config`.
6. WHEN an app's dev server supports HMR via WebSocket THEN the HMR WebSocket path SHALL work through the OR3 proxy because the proxy strips the prefix and forwards WebSocket upgrades.
7. WHEN documentation is provided THEN it SHALL include example manifest and config snippets for at least one static server, one SPA dev server, and one backend server.

### Requirement 6: Health checking

**Engineering objective:** OR3 periodically checks whether running apps are responsive and reports their status.

#### Acceptance Criteria

1. WHEN an app is running and specifies `server.healthPath` THEN OR3 SHALL periodically HTTP GET that path on `127.0.0.1:<assigned-port>`.
2. WHEN the health check succeeds THEN the app status SHALL remain `running`.
3. WHEN the health check fails 3 consecutive times THEN the app status SHALL become `unhealthy`.
4. WHEN an app specifies `server.readyPattern` THEN OR3 SHALL scan stdout for that pattern during startup and mark the app `running` only after it appears.
5. WHEN no `readyPattern` is specified THEN OR3 SHALL mark the app `running` after the process starts successfully and the first health check passes (or immediately if no `healthPath`).
6. WHEN health checks are performed THEN OR3 SHALL only connect to `127.0.0.1`, never to `0.0.0.0` or any non-loopback address.

### Requirement 7: Agent tools

**Engineering objective:** Provide agent tools for the full mini app lifecycle, integrated into the existing tool registry.

#### Acceptance Criteria

1. WHEN `create_mini_app` is called with an app ID and manifest THEN OR3 SHALL validate the ID, create `.apps/<app-id>/`, and write `app.json`.
2. WHEN `list_mini_apps` is called THEN OR3 SHALL return all discovered apps with their current status (`stopped`, `starting`, `running`, `unhealthy`, `error`).
3. WHEN `start_mini_app` is called on an already `running` or `starting` app THEN OR3 SHALL return success with the current status (idempotent).
4. WHEN `stop_mini_app` is called on an already `stopped` app THEN OR3 SHALL return success (idempotent).
5. WHEN `start_mini_app` is called THEN OR3 SHALL validate the manifest, assign a port, run install/build if needed, and start the dev/start command.
6. WHEN `restart_mini_app` is called THEN OR3 SHALL stop and start the app.
7. WHEN `mini_app_status` is called THEN OR3 SHALL return detailed status including port, PID, uptime, health, and last log lines.
8. WHEN `mini_app_logs` is called THEN OR3 SHALL return the recent log output (ring buffer if running, persisted tail if stopped).
9. WHEN `delete_mini_app` is called THEN OR3 SHALL stop the app if running, remove the app directory, and remove the data directory.
10. WHEN read-only tools (`list_mini_apps`, `mini_app_status`, `mini_app_logs`) are called THEN they SHALL use `CapabilitySafe` and belong to `ToolGroupRead`.
11. WHEN lifecycle tools (`create_mini_app`, `start_mini_app`, `stop_mini_app`, `restart_mini_app`, `delete_mini_app`) are called THEN they SHALL use `CapabilityGuarded` and belong to `ToolGroupExec`.
12. WHEN `delete_mini_app` is called THEN OR3 SHALL emit an audit event (`mini_app.delete`) and require approval through the existing `ApprovalBroker` when the runtime profile requires it.
13. WHEN `start_mini_app` or `stop_mini_app` is called THEN OR3 SHALL emit audit events (`mini_app.start`, `mini_app.stop`) when audit logging is available.

### Requirement 8: App discovery and scanning

**Engineering objective:** OR3 automatically discovers apps in the workspace `.apps/` directory on startup and when requested.

#### Acceptance Criteria

1. WHEN the service starts THEN OR3 SHALL scan `<workspace>/.apps/` for directories containing `app.json`.
2. WHEN a new `app.json` appears in `.apps/` THEN OR3 SHALL detect it on the next periodic rescan (default every 30s) or when `list_mini_apps` is called.
3. WHEN an `app.json` is malformed THEN OR3 SHALL report the app as `error` with the validation message, without crashing.
4. WHEN discovery finds a directory whose name does not match the app ID charset THEN it SHALL skip it and log a warning.
5. WHEN the workspace directory is not configured THEN mini app discovery SHALL be disabled and tools SHALL return a clear error.

### Requirement 9: Service API for or3-app

**Engineering objective:** Expose mini app management through the existing service HTTP API for or3-app consumption.

#### Acceptance Criteria

1. WHEN `GET /internal/v1/mini-apps` is called THEN OR3 SHALL return all discovered apps with status, port (or `last_port` hint when stopped), and metadata.
2. WHEN `POST /internal/v1/mini-apps` is called with a manifest body THEN OR3 SHALL create the app (matching `create_mini_app` tool behavior).
3. WHEN `POST /internal/v1/mini-apps/<app-id>/start` is called THEN OR3 SHALL start the app. Accept optional `reinstall: true` to force re-running install.
4. WHEN `POST /internal/v1/mini-apps/<app-id>/stop` is called THEN OR3 SHALL stop the app.
5. WHEN `POST /internal/v1/mini-apps/<app-id>/restart` is called THEN OR3 SHALL restart the app.
6. WHEN `GET /internal/v1/mini-apps/<app-id>/logs` is called THEN OR3 SHALL return recent log lines.
7. WHEN `DELETE /internal/v1/mini-apps/<app-id>` is called THEN OR3 SHALL delete the app.
8. WHEN any mini app service endpoint is called THEN it SHALL require the same auth as other service routes.

### Requirement 10: Install and build steps

**Engineering objective:** OR3 runs install and build commands before starting an app when the manifest specifies them, with proper invalidation.

#### Acceptance Criteria

1. WHEN `commands.install` is specified and the app has not been installed yet THEN OR3 SHALL run the install command before starting the app.
2. WHEN `commands.build` is specified THEN OR3 SHALL run the build command after install and before start.
3. WHEN install or build fails THEN OR3 SHALL mark the app as `error` with the failure output and SHALL NOT start the app.
4. WHEN install/build commands run THEN they SHALL use the same bounded execution model as the exec tool (timeout, output limit).
5. WHEN an app has already been installed THEN OR3 SHALL skip install unless the install command has changed (detected via stored command hash) or `reinstall: true` is passed.
6. WHEN install succeeds THEN OR3 SHALL store a hash of the install command string in the `.or3-installed` marker file.
7. WHEN the install command in the manifest changes compared to the stored hash THEN OR3 SHALL re-run install on the next start.
8. WHEN install/build commands run THEN they SHALL NOT use the exec tool's sandbox/bubblewrap (mini app install runs with the same trust level as the agent's file tools).

### Requirement 11: Idempotency and concurrency

**Engineering objective:** Define clear semantics for repeated and concurrent lifecycle operations.

#### Acceptance Criteria

1. WHEN `start` is called on an app that is already `starting` or `running` THEN OR3 SHALL return success with the current status and port.
2. WHEN `stop` is called on an app that is already `stopped` THEN OR3 SHALL return success.
3. WHEN `start` and `delete` are called concurrently on the same app THEN the per-app mutex SHALL serialize them; one SHALL succeed and the other SHALL return an appropriate error.
4. WHEN `delete` is called on a `running` app THEN OR3 SHALL stop it first, then delete.
5. WHEN two apps are started simultaneously THEN they SHALL each receive distinct ports without conflict.
6. WHEN the supervisor manages multiple apps THEN it SHALL use per-app mutexes for lifecycle operations, allowing concurrent operations on different apps.

### Requirement 12: Localhost-only binding enforcement

**Engineering objective:** Ensure apps bind to loopback only, preventing direct network access that bypasses the OR3 auth proxy.

#### Acceptance Criteria

1. WHEN an app process starts THEN OR3 SHALL inject `HOST=127.0.0.1` into the child environment.
2. WHEN health checks are performed THEN OR3 SHALL connect to `127.0.0.1:<port>` only.
3. WHEN the proxy forwards requests THEN it SHALL connect to `127.0.0.1:<port>` only.
4. WHEN documentation is provided THEN it SHALL state that apps MUST bind to `127.0.0.1` and that binding to `0.0.0.0` will not provide additional access because the proxy enforces auth.

## Non-functional constraints

- **Deterministic single-process behavior:** The mini app supervisor runs inside the existing or3-intern service process. No external process managers, Docker, or sidecar services.
- **Low memory usage:** Log ring buffers are bounded (default 64KB per app). Persisted log tails are 8KB. Port assignments and app state are small SQLite rows. Health checks use short timeouts.
- **Bounded loops/output:** Install and build commands have configurable timeouts (default 120s). Log capture is a fixed-size ring buffer. Health checks run on a fixed interval (default 10s).
- **SQLite safety:** App state table is additive. No migration of existing tables. Runtime port assignments are cleared on startup; `last_port` is retained as a hint only.
- **Secure handling of files, network access, and secrets:**
  - Apps bind to `127.0.0.1` only (enforced via `HOST` env var and loopback-only proxy/health connections).
  - App processes receive a restricted environment (no service secrets, no master keys).
  - App data directories are isolated per app ID.
  - The proxy blocks requests to non-existent or stopped apps.
  - App IDs are validated against a strict charset to prevent path traversal.
- **Backward compatibility:** No changes to existing service routes, config fields, SQLite tables, or tool behavior. Mini apps are purely additive.
- **Workspace boundary:** Apps live inside `<workspace>/.apps/`. The agent uses existing file tools to create app source code. The supervisor only manages apps within the workspace.
- **Workspace changes:** If `workspaceDir` changes, the supervisor operates only on the new workspace. Stale SQLite rows from a previous workspace are ignored (filtered by workspace path) but not deleted.
