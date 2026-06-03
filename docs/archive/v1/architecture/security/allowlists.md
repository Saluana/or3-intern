# Allowlist System

Allowlists let operators approve a class of actions once, so the same action won't need approval again.

## How allowlists work

When an operator approves a request with "always allow", the broker creates an allowlist entry from the request's subject. Future actions matching that allowlist entry are approved automatically without operator intervention.

Source: `internal/approval/requests.go:128-174` (createAllowlistFromRequest)

## Allowlist structure

Each allowlist entry has:

- **Domain** - the subject type (exec, skill_exec, runner_permission)
- **Scope** - limits where the allowlist applies (host ID, tool name, profile, agent)
- **Matcher** - defines what the allowlist matches (varies by domain)
- **ExpiresAt** - optional expiration timestamp (0 means never)
- **DisabledAt** - when the entry was disabled (0 means active)

Source: `internal/approval/allowlist.go:14-31` (AddAllowlist)

## Scope matching

A scope matches when all non-empty fields of the stored scope equal the corresponding fields of the current action:

- `HostID` - which host the action runs on
- `Tool` - which tool is being used
- `Profile` - which access profile is active
- `Agent` - which requester agent is acting

Source: `internal/approval/allowlist.go:73-94` (allowlistScopeMatches)

## Exec allowlist matchers

For exec actions, the matcher checks (source: `internal/approval/allowlist.go:96-128`):
- `ExecutablePath` - exact path to the executable
- `PathGlob` - glob pattern for the executable path
- `Argv` - exact argument list
- `WorkingDir` - exact working directory
- `WorkingDirPref` - working directory prefix
- `ScriptHash` - hash of the legacy shell command

At least one field must be set (executable, path, argv, working dir, or script).

## Skill exec allowlist matchers

For skill execution, the matcher checks (source: `internal/approval/allowlist.go:129-162`):
- `SkillID` - skill identifier
- `Version` - installed version
- `Origin` - where the skill came from
- `TrustState` - permission state
- `PlanHash` - frozen plan hash
- `ScriptHash` - script content hash
- `EnvBindingHash` - environment binding hash
- `TimeoutSeconds` - execution timeout

## Runner permission matchers

For runner permissions, the matcher checks (source: `internal/approval/allowlist.go:163-193`):
- `RunnerID` - runner identifier
- `PermissionKind` - e.g., "filesystem"
- `Access` - "read" or "write"
- `TargetPath` - exact path
- `PathPrefix` - path prefix

## Removing allowlists

Allowlists are removed by setting `DisabledAt` (soft delete). The entry remains for audit purposes but is ignored in matching.

Source: `internal/approval/allowlist.go:34-40` (RemoveAllowlist)
