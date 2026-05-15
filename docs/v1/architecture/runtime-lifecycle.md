# Runtime Lifecycle

The runtime builds in phases at startup. Each phase has its own file in `cmd/or3-intern/`.

## Phase 1: Build Config

The config builder loads `config.json` from `~/.or3-intern/config.json`. It applies environment variable overrides and validates all values. If the config is missing required fields (like an API key), the runtime will not start.

## Phase 2: Build Storage

The storage builder opens the SQLite database. It runs migrations to create tables if needed. It sets WAL mode and other pragmas for performance.

## Phase 3: Build Security

The security builder loads encrypted secrets from the database. It sets up auth keys and session validation. This phase prepares the safety system.

## Phase 4: Build Integrations

The integrations builder connects to MCP servers, loads skills, and starts channels. Each integration goes through a lifecycle: available, starting, ready, quarantined, or stopped.

## Phase 5: Build Runtime

The runtime builder creates the agent runtime with all dependencies. This is the final step. Once the runtime is ready, commands can start processing turns.

## Startup Order

Config -> Storage -> Security -> Integrations -> Runtime

Each phase depends on the previous one. If any phase fails, startup stops and the error is reported.
