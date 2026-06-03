# External Agent CLI Overview

OR3 Intern can delegate tasks to other AI coding CLIs. This system is called the External Agent CLI, or "runners." It lets OR3 use tools like OpenCode, Codex, Claude Code, and Gemini CLI to perform work on its behalf.

## Why Runners Exist

OR3 Intern is one AI agent among many. Sometimes another CLI tool is better suited for a specific task. Runners let OR3:

- Ask OpenCode to explore a codebase
- Use Codex to run commands in a sandbox
- Delegate to Claude Code with its permission model
- Run reviews through Gemini CLI

Each runner is treated as a background job. OR3 enqueues the task, the runner processes it, and OR3 collects the result.

## How Runners Work

The runner system is managed by `internal/agentcli/manager.go`. The `Manager` struct owns a worker pool, a registry of runner specs, a process manager, and a database connection. The flow is:

1. **Register** — each supported CLI tool is defined as a `RunnerSpec` in `internal/agentcli/runners.go`
2. **Detect** — on startup, OR3 checks which runners are installed and ready (`internal/agentcli/detect.go`)
3. **Enqueue** — when a task is submitted, it is validated and stored in the database
4. **Execute** — workers claim queued runs and launch the runner as a subprocess (`internal/agentcli/process.go`)
5. **Stream** — output is captured line-by-line and emitted as events (`internal/agentcli/stream.go`)
6. **Extract** — the final text result is pulled from structured output (`internal/agentcli/result_extract.go`)
7. **Finalize** — the run is marked complete, succeeded, or failed

## Supported Runners

Defined in `internal/agentcli/runners.go:13-19`:

| ID | Display Name | Binary |
|----|-------------|--------|
| `opencode` | OpenCode | `opencode` |
| `codex` | Codex | `codex` |
| `claude` | Claude Code | `claude` |
| `gemini` | Gemini CLI | `gemini` |
| `or3-intern` | OR3 Intern | (internal) |

## Key Files

- `internal/agentcli/manager.go` — main runner manager: enqueue, workers, finalization
- `internal/agentcli/registry.go` — runner specs, adapters, detection cache
- `internal/agentcli/runners.go` — type definitions, interfaces, runner constants
- `internal/agentcli/detect.go` — binary detection and auth checking
- `internal/agentcli/process.go` — subprocess spawning and management
- `internal/agentcli/stream.go` — output streaming and structured parsing
- `internal/agentcli/result_extract.go` — final text extraction from runner output
- `internal/agentcli/chat_adapters.go` — chat-specific command building and event normalization
- `internal/agentcli/chat_manager.go` — chat turn lifecycle management
- `internal/agentcli/chat_prompt.go` — replay prompt construction
- `internal/agentcli/runner_permissions.go` — permission detection for runners
- `internal/agentcli/opencode_permissions.go` — OpenCode-specific permission config
- `internal/agentcli/cwd.go` — working directory resolution and validation
- `internal/agentcli/env.go` — child process environment building
- `internal/agentcli/executable.go` — binary resolution from PATH
