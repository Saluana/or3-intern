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
2. WHEN the manifest is valid THEN it SHALL contain at minimum: `id`, `name`, `commands` (with at least `dev` or `start`), and `server.portEnv`.
3. WHEN the manifest is missing required fields THEN OR3 SHALL reject it with a clear validation error.
4. WHEN the manifest specifies optional fields (`runtime`, `commands.install`, `commands.build`, `server.healthPath`, `server.readyPattern`, `storage.mode`) THEN OR3 SHALL honor them.
5. WHEN two apps share the same `id` THEN OR3 SHALL reject the duplicate.

### Requirement 2: Port assignment and isolation

**Engineering objective:** OR3 assigns each running app a unique localhost port from a bounded range, preventing port conflicts and direct public exposure.

#### Acceptance Criteria

1. WHEN an app starts THEN OR3 SHALL assign it an available port from a configurable range (default `49152..49252`).
2. WHEN no ports are available THEN OR3 SHALL return a clear error indicating the app pool is full.
3. WHEN an app stops THEN OR3 SHALL release its port for reuse.
4. WHEN OR3 assigns a port THEN it SHALL inject `PORT` and `OR3_APP_PORT` environment variables into the app process.
5. WHEN an app is running THEN OR3 SHALL track the port assignment in memory and persist the mapping in SQLite for restart recovery.

### Requirement 3: Process supervision

**Engineering objective:** OR3 starts, stops, restarts, and monitors app processes with bounded log capture.

#### Acceptance Criteria

1. WHEN `start_mini_app` is called THEN OR3 SHALL run the manifest's `dev` command (or `start` if `dev` is absent) as a child process with the app directory as the working directory.
2. WHEN the app process starts THEN OR3 SHALL inject `PORT`, `OR3_APP_PORT`, `OR3_APP_ID`, and `OR3_APP_DATA_DIR` into the child environment.
3. WHEN the app process exits unexpectedly THEN OR3 SHALL mark the app as `stopped` with the exit code and last log lines.
4. WHEN `stop_mini_app` is called THEN OR3 SHALL send SIGTERM, wait up to 5 seconds, then SIGKILL if the process has not exited.
5. WHEN `restart_mini_app` is called THEN OR3 SHALL stop and then start the app, preserving the same port assignment when possible.
6. WHEN an app is running THEN OR3 SHALL capture stdout and stderr into a bounded ring buffer (default 64KB).
7. WHEN `mini_app_logs` is called THEN OR3 SHALL return the most recent log lines from the ring buffer.

### Requirement 4: App data directory

**Engineering objective:** Each app receives an isolated data directory for storage, separate from the app source code.

#### Acceptance Criteria

1. WHEN an app starts THEN OR3 SHALL create `or3-data/mini-apps/<app-id>/` under the configured data root (default `~/.or3-intern/mini-apps/`).
2. WHEN the app process runs THEN OR3 SHALL inject `OR3_APP_DATA_DIR` pointing to this directory.
3. WHEN the app writes files to `OR3_APP_DATA_DIR` THEN those files SHALL persist across app restarts.
4. WHEN `delete_mini_app` is called THEN OR3 SHALL remove both the app source directory (`.apps/<app-id>`) and the app data directory.
5. WHEN the data root is not configured THEN OR3 SHALL fall back to `~/.or3-intern/mini-apps/`.

### Requirement 5: Reverse proxy

**Engineering objective:** OR3 proxies HTTP requests to running mini apps through the existing service, requiring OR3 authentication.

#### Acceptance Criteria

1. WHEN a request arrives at `/apps/<app-id>/*` on the OR3 service THEN OR3 SHALL proxy it to `http://127.0.0.1:<assigned-port>/*`.
2. WHEN the target app is not running THEN OR3 SHALL return `502 Bad Gateway` with a safe error message.
3. WHEN the target app does not exist THEN OR3 SHALL return `404 Not Found`.
4. WHEN the proxy forwards a request THEN it SHALL require the same OR3 session/auth as other service routes.
5. WHEN the proxy forwards a request THEN it SHALL inject `X-Or3-App-Id` and forward standard proxy headers (`X-Forwarded-For`, `X-Forwarded-Proto`).
6. WHEN WebSocket upgrade requests arrive at `/apps/<app-id>/*` THEN OR3 SHALL proxy them to the app's assigned port.

### Requirement 6: Health checking

**Engineering objective:** OR3 periodically checks whether running apps are responsive and reports their status.

#### Acceptance Criteria

1. WHEN an app is running and specifies `server.healthPath` THEN OR3 SHALL periodically HTTP GET that path on the assigned port.
2. WHEN the health check succeeds THEN the app status SHALL remain `running`.
3. WHEN the health check fails 3 consecutive times THEN the app status SHALL become `unhealthy`.
4. WHEN an app specifies `server.readyPattern` THEN OR3 SHALL scan stdout for that pattern during startup and mark the app `running` only after it appears.
5. WHEN no `readyPattern` is specified THEN OR3 SHALL mark the app `running` after the process starts successfully and the first health check passes (or immediately if no `healthPath`).

### Requirement 7: Agent tools

**Engineering objective:** Provide agent tools for the full mini app lifecycle, integrated into the existing tool registry.

#### Acceptance Criteria

1. WHEN `create_mini_app` is called with an app ID and manifest THEN OR3 SHALL create `.apps/<app-id>/app.json` and the app directory.
2. WHEN `list_mini_apps` is called THEN OR3 SHALL return all discovered apps with their current status (`stopped`, `starting`, `running`, `unhealthy`, `error`).
3. WHEN `start_mini_app` is called THEN OR3 SHALL validate the manifest, assign a port, run install/build if specified, and start the dev/start command.
4. WHEN `stop_mini_app` is called THEN OR3 SHALL stop the running process and release the port.
5. WHEN `restart_mini_app` is called THEN OR3 SHALL stop and start the app.
6. WHEN `mini_app_status` is called THEN OR3 SHALL return detailed status including port, PID, uptime, health, and last log lines.
7. WHEN `mini_app_logs` is called THEN OR3 SHALL return the recent log output from the ring buffer.
8. WHEN `delete_mini_app` is called THEN OR3 SHALL stop the app if running, remove the app directory, and remove the data directory.
9. WHEN any mini app tool is called THEN it SHALL respect the existing tool capability and approval model (`CapabilityGuarded`).

### Requirement 8: App discovery and scanning

**Engineering objective:** OR3 automatically discovers apps in the workspace `.apps/` directory on startup and when requested.

#### Acceptance Criteria

1. WHEN the service starts THEN OR3 SHALL scan `<workspace>/.apps/` for directories containing `app.json`.
2. WHEN a new `app.json` appears in `.apps/` THEN OR3 SHALL detect it on the next scan or when `list_mini_apps` is called.
3. WHEN an `app.json` is malformed THEN OR3 SHALL report the app as `error` with the validation message, without crashing.
4. WHEN the workspace directory is not configured THEN mini app discovery SHALL be disabled and tools SHALL return a clear error.

### Requirement 9: Service API for or3-app

**Engineering objective:** Expose mini app management through the existing service HTTP API for or3-app consumption.

#### Acceptance Criteria

1. WHEN `GET /internal/v1/mini-apps` is called THEN OR3 SHALL return all discovered apps with status, port, and metadata.
2. WHEN `POST /internal/v1/mini-apps/<app-id>/start` is called THEN OR3 SHALL start the app.
3. WHEN `POST /internal/v1/mini-apps/<app-id>/stop` is called THEN OR3 SHALL stop the app.
4. WHEN `POST /internal/v1/mini-apps/<app-id>/restart` is called THEN OR3 SHALL restart the app.
5. WHEN `GET /internal/v1/mini-apps/<app-id>/logs` is called THEN OR3 SHALL return recent log lines.
6. WHEN `DELETE /internal/v1/mini-apps/<app-id>` is called THEN OR3 SHALL delete the app.
7. WHEN any mini app service endpoint is called THEN it SHALL require the same auth as other service routes.

### Requirement 10: Install and build steps

**Engineering objective:** OR3 runs install and build commands before starting an app when the manifest specifies them.

#### Acceptance Criteria

1. WHEN `commands.install` is specified and the app has not been installed yet THEN OR3 SHALL run the install command before starting the app.
2. WHEN `commands.build` is specified THEN OR3 SHALL run the build command after install and before start.
3. WHEN install or build fails THEN OR3 SHALL mark the app as `error` with the failure output and SHALL NOT start the app.
4. WHEN install/build commands run THEN they SHALL use the same bounded execution model as the exec tool (timeout, output limit).
5. WHEN an app has already been installed (marker file exists) THEN OR3 SHALL skip install unless explicitly requested.

## Non-functional constraints

- **Deterministic single-process behavior:** The mini app supervisor runs inside the existing or3-intern service process. No external process managers, Docker, or sidecar services.
- **Low memory usage:** Log ring buffers are bounded (default 64KB per app). Port assignments and app state are small SQLite rows. Health checks use short timeouts.
- **Bounded loops/output:** Install and build commands have configurable timeouts (default 120s). Log capture is a fixed-size ring buffer. Health checks run on a fixed interval (default 10s).
- **SQLite safety:** App state table is additive. No migration of existing tables. Port assignments are reconciled on startup (stale entries cleared).
- **Secure handling of files, network access, and secrets:**
  - Apps bind to `127.0.0.1` only; the proxy enforces OR3 auth.
  - App processes receive a restricted environment (no service secrets, no master keys).
  - App data directories are isolated per app ID.
  - The proxy blocks requests to non-existent or stopped apps.
- **Backward compatibility:** No changes to existing service routes, config fields, SQLite tables, or tool behavior. Mini apps are purely additive.
- **Workspace boundary:** Apps live inside `<workspace>/.apps/`. The agent uses existing file tools to create app source code. The supervisor only manages apps within the workspace.
