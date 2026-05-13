# Sandboxing (Bubblewrap)

OR3 Intern can run commands inside a bubblewrap sandbox for process isolation.

## BubblewrapConfig

The sandbox configuration has:
- `Enabled` - whether sandboxing is active
- `BubblewrapPath` - path to the `bwrap` binary (default: "bwrap")
- `AllowNetwork` - whether the sandboxed process can access the network
- `WritablePaths` - paths that should be writable inside the sandbox

Source: `internal/tools/sandbox.go:12-17`

## Sandbox setup

When a command is run with sandbox enabled, `commandWithSandbox` builds the bubblewrap invocation:

1. Resolves the executable path
2. Creates a new namespace with: `--die-with-parent --new-session --unshare-pid --proc /proc --dev /dev --tmpfs /`
3. If `AllowNetwork` is false (default), adds `--unshare-net`
4. Mounts standard read-only paths: `/bin`, `/sbin`, `/usr`, `/lib`, `/lib64`, `/etc`, `/opt`, `/run/current-system/sw`
5. Mounts writable paths with `--bind` (read-write)
6. Mounts the current working directory as read-only (unless covered by a writable path)
7. Mounts the directory containing the executable as read-only (unless covered)
8. Sets the working directory with `--chdir`

Source: `internal/tools/sandbox.go:21-77`

## Read-only paths

The sandbox always mounts these paths read-only:
- `/bin`, `/sbin`, `/usr`, `/lib`, `/lib64`, `/etc`, `/opt`
- `/run/current-system/sw` (NixOS support)

Source: `internal/tools/sandbox.go:79-90`

## Parent directory creation

For each mounted path, the sandbox also creates parent directories with `--dir` arguments. This ensures the mount points exist. Directories are tracked to avoid duplicates.

Source: `internal/tools/sandbox.go:92-118`

## Integration with exec tool

The exec tool calls `commandWithSandbox` when running any command (both program+args and legacy shell). If bubblewrap is not enabled, it returns `errSandboxNotEnabled` and the tool falls back to running the command directly.

Source: `internal/tools/exec.go:248-254`

## Safety mode integration

The locked-down safety mode enables sandboxing and sets the bubblewrap path to the default value if not configured. The relaxed mode disables sandboxing.

Source: `internal/safetymode/safetymode.go:166-168` (locked-down sandbox) and `internal/safetymode/safetymode.go:158` (relaxed sandbox)

## Doctor validation

The doctor engine checks:
- If `bubblewrapPath` is empty when sandbox is needed (`privileged-exec.bubblewrap_path_empty`)
- If bubblewrap is missing from the system (`privileged-exec.bubblewrap_missing`)

Source: `internal/doctor/fix.go:56-59,183-198`
