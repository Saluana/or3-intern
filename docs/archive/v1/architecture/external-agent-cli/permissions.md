# Permission Model

Runners can request filesystem access beyond their working directory. The permission system is defined in `internal/agentcli/runner_permissions.go`.

## RunnerPermissionRequest

```go
type RunnerPermissionRequest struct {
    RunnerID   string
    Kind       string
    Access     string
    TargetPath string
}
```

- `Kind` — always `"filesystem"` (only kind supported)
- `Access` — `"read"` or `"write"`
- `TargetPath` — filesystem path the runner wants to access

## Detection Methods

Permissions are detected from runner output, not from direct API calls.

### OpenCode Permission Detection

`detectOpenCodePermissionRequest` (`internal/agentcli/runner_permissions.go:86-103`) watches stderr output for a specific pattern:

```
permission requested: external_directory (/some/path); auto-rejecting
```

This regex (`openCodeExternalDirectoryPermissionPattern`) captures the path. The trailing `*` is stripped to get the directory. The access is always `read`.

### Codex Permission Detection

`detectCodexPermissionRequest` (`internal/agentcli/runner_permissions.go:105-121`) scans the final text output. When Codex hits a sandbox write boundary, it outputs:

```
can't write to `/some/path`
```

The regex (`codexSandboxWriteDeniedPattern`) captures the path. The access is always `write`. The path is resolved to its parent directory if it points to a file.

## Permission Normalization

`NormalizeRunnerPermissionRequest` (`internal/agentcli/runner_permissions.go:29-42`) cleans up a permission request:

1. Trims whitespace from all fields
2. Defaults `Kind` to `"filesystem"` if empty
3. Defaults `Access` to `"read"` if empty
4. Runs `filepath.Clean` on the target path
5. Rejects paths that collapse to `"."` or `"/"`

The normalized path must be meaningful — requesting access to root or current directory is rejected.

## Approval Flow

When a permission is detected during a chat turn, the `ChatManager` evaluates it through the approval broker:

1. `maybeCaptureRunnerPermission` (`internal/agentcli/chat_manager.go:512-528`) catches OpenCode permissions from stderr events
2. `maybeCaptureCodexRunnerPermission` (`internal/agentcli/chat_manager.go:530-539`) catches Codex permissions from final text
3. `appendRunnerApprovalRequired` (`internal/agentcli/chat_manager.go:541-580`) sends the permission to the approval broker
4. If approval is required, the turn is set to `status = "approval_required"` and an approval event is appended

The turn only completes once the approval is granted. Until then, it shows as pending.

## Pre-Authorization via Meta

Permissions can be pre-approved by passing a `runner_permission` field in the request meta. The `ChatManager.StartTurn` method (`internal/agentcli/chat_manager.go:481-509`) checks for pre-approved permissions before enqueuing the run:

```json
{
  "runner_permission": {
    "runner_id": "codex",
    "kind": "filesystem",
    "access": "write",
    "target_path": "/some/external/dir"
  }
}
```

If the approval token matches an existing approval request, the permission is granted inline.
