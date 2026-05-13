# Runtime Security Profiles

Runtime profiles are preset configurations that set security defaults for different deployment scenarios. They are defined in `config.ProfileSpec`.

## Available profiles

Five named profiles exist. The following are referenced in the codebase:

- **`local-dev`** - development use on a single machine
- **`single-user-hardened`** - single user with approvals and guarded tools
- **`hosted-service`** - self-hosted service for trusted group
- **`hosted-no-exec`** - hosted service with shell and privileged tools disabled
- **`hosted-remote-sandbox-only`** - exec only allowed inside sandbox

Source: `internal/controlplane/controlplane.go:674-701` (CollectCapabilitiesReportWithMCPStatus)

## Profile effects

Each profile controls these capabilities:

- **Hosted** - whether the profile runs as a hosted service
- **ForbidPrivilegedTools** - blocks skill execution and other privileged tools
- **RequireSandboxForExec** - exec only works when sandbox is enabled
- **ForbidExecShell** - disables legacy shell commands

Source: `internal/controlplane/controlplane.go:682-686` (CapabilitiesReport fields)

## Validation

The doctor engine checks that the current config matches the profile's requirements. For hosted profiles, it requires the secret store enabled, audit logging enabled and strict, and network policies configured.

Source: `internal/doctor/engine_runtime.go:15-106` (runtimeProfileFindings)

## Profile enforcement

The capabilities report shows whether actions are available based on the profile:
- `ExecAvailable` requires `GuardedTools` on and (sandbox enabled or no sandbox required)
- `ShellModeAvailable` requires `GuardedTools`, `PrivilegedTools`, `EnableExecShell` all on, and no profile bans
- `SkillExecEnabled` requires `EnableExec`, `PrivilegedTools`, and no profile ban

Source: `internal/controlplane/controlplane.go:682-685`
