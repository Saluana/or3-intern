# Execute Tool (exec)

The exec tool runs local programs. It is the primary way agents interact with the system.

## Tool name: `exec`

Source: `internal/tools/exec.go:20-33` (ExecTool struct)

## Capability levels

- **Program + args** (no command field): `CapabilityGuarded`
- **Legacy shell command**: `CapabilityPrivileged`
- **Service requests with shell command**: `CapabilityGuarded` (because service requests get special handling)

Source: `internal/tools/exec.go:77-92`

## Parameters

- `program` (string) - executable name or path
- `args` (string array) - arguments passed directly (no shell parsing)
- `command` (string, legacy) - shell command string (requires `enableLegacyShell`)
- `cwd` (string) - working directory (must be inside `RestrictDir` if set)
- `timeoutSeconds` (integer) - optional timeout override

Source: `internal/tools/exec.go:60-72`

## Execution flow

1. If `command` is set for a service request, it's parsed with `parseServiceDirectCommand` which rejects shell syntax
2. Validates that program or command is provided
3. For service requests: checks allowed programs and role
4. Checks `enableLegacyShell` / `disableShell` for legacy commands
5. Blocks known dangerous patterns in legacy shell commands (rm -rf, mkfs, dd, shutdown, reboot, etc.)
6. Resolves and validates the working directory
7. Builds child environment from whitelist + context env + PATH
8. Resolves the executable path, checks against allowed programs list
9. Evaluates with the approval broker
10. Runs the command (via bubblewrap if sandbox is enabled, otherwise directly)
11. Returns bounded stdout/stderr previews (default 12000/8000 bytes)

Source: `internal/tools/exec.go:124-288`

## Environment building

`BuildChildEnv` builds the child environment by:
- Selecting only allowed environment variables from the parent
- Adding environment variables from the tool context
- Appending to PATH if `PathAppend` is set

Source: `internal/tools/env.go` and usage in `exec.go:175`

## Program allowlist

When `AllowedPrograms` is set, only listed programs can be executed. Programs are matched by name (if no path separator in the request) or by full canonical path.

Source: `internal/tools/exec.go:397-421` (allowedProgram)

## Service request restrictions

Service requests (from API calls, not chat):
- Must use program+args, not shell commands
- Shell syntax (pipes, redirects, `$()`) is rejected
- `AllowedPrograms` must be non-empty
- Only "operator" and "admin" roles can execute
- The `which` command is supported for executable lookup

Source: `internal/tools/exec.go:144-155`, `internal/tools/exec.go:194-196`, `internal/tools/exec.go:290-301`

## Legacy shell safety net

The tool has a blocklist of dangerous patterns (`rm -rf`, `mkfs`, `dd `, `shutdown`, `reboot`, `poweroff`, `:(){`, `>|`, `chown -R /`, `chmod -R 777 /`). These are substring matches intended to catch accidents, not a security boundary.

Source: `internal/tools/exec.go:94-100` (defaultBlockedPatterns)
