# Sandbox Tool Integration

The sandbox system is not a standalone tool - it is integrated into the exec and skill execution tools.

## BubblewrapConfig

Every exec-related tool has a `BubblewrapConfig` embedded:

```go
type BubblewrapConfig struct {
    Enabled        bool
    BubblewrapPath string   // default: "bwrap"
    AllowNetwork   bool     // default: false (network blocked)
    WritablePaths  []string // paths writable inside sandbox
}
```

Source: `internal/tools/sandbox.go:12-17`

## When sandbox applies

Sandbox wrapping is attempted for both:
- The exec tool (program+args and legacy shell commands)
- The skill execution tools (run_skill, run_skill_script)

If sandbox is not enabled, the tool falls back to running the command directly.

Source: `internal/tools/exec.go:248-254` (exec sandbox), `internal/tools/skill_run.go:372-382` (skill sandbox)

## How sandbox invocation works

`commandWithSandbox` builds a `bwrap` invocation that:
1. Creates a new Linux namespace with PID isolation and tmpfs root
2. Optionally isolates network (`--unshare-net`)
3. Mounts system directories read-only: /bin, /sbin, /usr, /lib, /lib64, /etc, /opt, /run/current-system/sw
4. Mounts specified writable paths with read-write access
5. Mounts the working directory (read-only unless covered by writable paths)
6. Mounts the executable's directory (read-only unless covered)
7. Creates parent directories for all mount points
8. Sets the working directory and passes the command

Source: `internal/tools/sandbox.go:21-77`

## Read-only paths

The sandbox provides these paths read-only by checking if they exist on the host:
- `/bin`, `/sbin` - basic executables
- `/usr` - most system software
- `/lib`, `/lib64` - shared libraries
- `/etc` - system configuration
- `/opt` - optional software
- `/run/current-system/sw` - NixOS system profile

Source: `internal/tools/sandbox.go:79-90`

## Path coverage checking

`sandboxPathCovered` checks if a path is already covered by an existing writable mount or the working directory mount. This prevents duplicate bind mounts.

Source: `internal/tools/sandbox.go:120-140`

## Safety mode integration

- Balanced mode: sandbox disabled
- Locked-down mode: sandbox enabled, bubblewrap path set to default
- Hosted-service scenario: same as locked-down

Source: `internal/safetymode/safetymode.go:158,166-168`
