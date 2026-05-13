# Runner Detection

OR3 Intern checks whether each external CLI tool is installed and ready before using it. Detection is defined in `internal/agentcli/detect.go`.

## Detect Function

`Detect(ctx, spec, opts)` (`internal/agentcli/detect.go:11-104`) runs three checks for each runner:

### 1. Binary Lookup

The function calls `ResolveExecutable(spec.Binary, opts.Env)` to find the binary on `PATH`. If the binary is not found, the runner status is set to `RunnerStatusMissing`.

### 2. Version Probe

If the spec has `VersionArgs`, detection runs the version command with a 2-second timeout. On success, the first line of output is stored as the runner's version string.

Gemini CLI has special handling: if `--version` fails, detection falls back to `--help` (`internal/agentcli/detect.go:52-71`).

### 3. Auth Check

If the spec has an `AuthCheck`, detection runs the auth command with the configured timeout (default 3 seconds). If the command succeeds, auth is marked `AuthReady`. If it fails, the status is set to `RunnerStatusAuthMissing`.

Gemini CLI always gets `AuthUnknown` since it has no consistent auth check (`internal/agentcli/detect.go:99-101`).

## Runner Statuses

Defined in `internal/agentcli/runners.go:40-52`:

| Status | Meaning |
|--------|---------|
| `available` | Binary found, version and auth OK |
| `missing` | Binary not found on PATH |
| `not_executable` | Binary exists but cannot run |
| `auth_missing` | Binary found but not authenticated |
| `auth_unknown` | Cannot determine auth state |
| `unsupported_version` | Version check produces error |
| `disabled_by_config` | Disabled in config |
| `error` | Detection failed with an error |

## Special Cases

**OR3 Intern** (`RunnerOR3`) always returns `available` with `AuthReady` — it is the built-in runner and needs no external check (`internal/agentcli/detect.go:21-24`).

**Disabled runners** are checked against `opts.DisabledRunners` before any binary lookup. If the runner ID matches a disabled entry, detection returns `RunnerStatusDisabledByConfig` immediately (`internal/agentcli/detect.go:26-31`).

## Async Detection

Detection runs asynchronously at startup via `RefreshAllAsync` (`internal/agentcli/registry.go:248-259`). Each runner is detected in its own goroutine. Results are cached in the registry's `detectCache` map with timestamps.

When a run is enqueued, the manager checks the cached detection result. If the cache is stale (older than 30 seconds, defined as `agentCLIDetectCacheTTL` in `internal/agentcli/manager.go:28`), it triggers a background refresh and proceeds optimistically.

## DetectOptions

Detection uses `DetectOptions` (`internal/agentcli/runners.go:194-199`) that carry:

- `WorkDir` — directory for the version/auth probe commands
- `Env` — filtered environment for the probe commands
- `DisabledRunners` — list of runner IDs disabled by config
