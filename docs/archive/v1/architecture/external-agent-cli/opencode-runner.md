# OpenCode Runner

OpenCode is the primary external runner. It receives special treatment because it is also the open-source CLI that OR3 Intern is built to complement.

## OpenCodeAdapter

Defined in `internal/agentcli/registry.go:370-397`. The adapter builds this command:

```
opencode run --format json [--model <model>] [--dangerously-skip-permissions] <task>
```

- `--format json` ensures structured JSON output
- `--model` is passed when a model is specified
- `--dangerously-skip-permissions` is added in `sandbox_auto` mode

Output mode is `OutputJSON` (a single JSON object, not streaming lines).

## OpenCode Permissions

`internal/agentcli/opencode_permissions.go` handles OpenCode's external directory permissions.

### OPENCODE_CONFIG_CONTENT

Instead of writing a config file, OR3 passes configuration through the `OPENCODE_CONFIG_CONTENT` environment variable. This variable holds a JSON document that pre-authorizes specific external directories:

```json
{
  "permission": {
    "external_directory": {
      "/path/to/workspace/*": "allow",
      "/path/to/skills/*": "allow"
    }
  }
}
```

### Pre-Authorized Directories

`OpenCodeExternalDirectoriesFromConfig` (`internal/agentcli/opencode_permissions.go:16-45`) builds the list of directories that OpenCode can access:

- `cfg.WorkspaceDir` — the user's workspace
- `cfg.AllowedDir` — configured allowed directory
- `cfg.Skills.ManagedDir` — managed skills directory
- `cfg.Skills.Load.GlobalDir` — global skills directory (unless disabled)
- `cfg.Skills.Load.ExtraDirs` — extra skill directories

Each directory is resolved to an absolute path and deduplicated.

## External Directory Bypass

The `Manager.OpenCodeExternalDirectories` field holds OR3-owned directories that OpenCode can access outside the current working directory. These directories are passed as environment variables when the process starts (`internal/agentcli/manager.go:748-757`).

If a chat turn includes a `runner_permission` allowing access to an additional directory, that directory is appended to the pre-authorized list for that run only.

## OpenCode Session

OpenCode supports native session resume. When `--session <id>` is passed, OpenCode continues an existing conversation rather than starting fresh.

The `OpenCodeAdapter.ExtractNativeSessionRef` method (`internal/agentcli/chat_adapters.go:272-278`) extracts the session ID from OpenCode's output by scanning for `sessionID`, `sessionId`, or `session_id` fields in JSON events.

## Chat Events

OpenCodeAdapter normalizes chat events in `NormalizeChatEvent` (`internal/agentcli/chat_adapters.go:185-208`):

- `type: "text"` → `text_delta` event
- `type: "assistant"` / `"assistant_message"` → `text_delta` event
- `type: "message.part.delta"` → `text_delta` event
- `type: "message.part.updated"` → `text_delta` or tool lifecycle event
- `type: "tool_use"` → tool lifecycle event
- `type: "permission.asked"` → `request.opened` event
- `type: "session.error"` → `runtime.error` event
- `type: "session.status"` with idle → `turn.completed` event

Suppressed events (returned as nil): `step_start`, `step_finish`, `step.completed`, `step.started`, `session.updated`.
