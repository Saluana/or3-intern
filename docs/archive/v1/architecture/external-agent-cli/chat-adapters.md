# Chat Adapters

Chat adapters convert between OR3 Intern's internal chat model and each external CLI's native format. They are defined in `internal/agentcli/chat_adapters.go`.

## RunnerChatAdapter Interface

This extends `RunnerAdapter` with chat-specific methods (`internal/agentcli/runners.go:253-264`):

- `BuildChatCommand(req) (CommandSpec, error)` — builds a command for a single chat turn
- `NormalizeChatEvent(raw AgentRunEvent) []RunnerChatEvent` — converts raw runner events to normalized chat events

## NativeRunnerChatAdapter Interface

Extends `RunnerChatAdapter` with session extraction (`internal/agentcli/runners.go:266-274`):

- `ExtractNativeSessionRef(event AgentRunEvent) (string, bool)` — pulls a stable session reference from runner output

## Continuation Modes

Two modes control how chat context is carried between turns (`internal/agentcli/runners.go:200-212`):

### Replay Mode (`ContinuationReplay`)
The replay prompt is built from prior turn history. Each turn includes the user message and assistant response. The prompt is bounded by:
- Maximum 12 turns (`replayMaxTurns`)
- Maximum 48 KiB total (`replayMaxBytes`)

This mode works with all runners and is the default when native session support is unavailable.

### Native Mode (`ContinuationNative`)
The runner resumes its own native session. Only used when the adapter implements `NativeRunnerChatAdapter` and the spec declares `ChatNativeSession`, `ChatResume`, and `ChatSessionRefExtractable`.

## Replay Prompt Building

`BuildReplayPrompt` in `internal/agentcli/chat_prompt.go:20-57` constructs the prompt:

```
System: This conversation is being replayed for context. ...
System: Earlier turns were truncated to fit context limits.

--- Previous turns ---
User: <previous message 1>
Assistant: <previous response 1>
User: <previous message 2>
Assistant: <previous response 2>
--- End previous turns ---

User: <new message>
```

Turns are filtered to only include completed ones. They are accumulated from newest to oldest until the byte limit is reached, then reversed to chronological order. If even the newest turn exceeds the limit, it is truncated from the front.

## Runner-Specific Chat Commands

### OpenCode (`internal/agentcli/chat_adapters.go:45-73`)
```
opencode run --format json [--session <id>] [--model <model>] <prompt>
```
Native mode passes `--session`. Otherwise passes the replay prompt.

### Codex (`internal/agentcli/chat_adapters.go:75-80`)
Replay mode: falls through to the one-shot `BuildCommand`.
Native mode: `codex exec resume --json --skip-git-repo-check [--model <m>] <session_ref> <task>`.

### Claude (`internal/agentcli/chat_adapters.go:82-101`)
Replay mode: falls through to the one-shot `BuildCommand`.
Native mode: adds `--resume <session_ref>` to the command args.

### Gemini (`internal/agentcli/chat_adapters.go:103-183`)
Always uses `--output-format stream-json` for chat. Native mode adds `--resume <session_ref>`.

## Event Normalization

Each adapter's `NormalizeChatEvent` converts raw structured/output events into `RunnerChatEvent` values. The normalized event types include:

- `text_delta` — streaming assistant text
- `reasoning_delta` — streaming reasoning/thinking text
- `content.delta` — generic content delta with a stream kind
- `item.started`, `item.updated`, `item.completed` — tool call lifecycle events
- `turn.proposed.delta`, `turn.plan.updated`, `turn.diff.updated` — plan and diff events
- `turn.completed` — turn finished
- `request.opened`, `request.resolved` — approval/permission requests
- `runtime.warning`, `runtime.error` — warnings and errors
- `runner_output` — fallback for unrecognized events

Events that cannot be normalized are wrapped in `runner_output` events so they remain visible for diagnostics. Suppressed events (like lifecycle noise) produce nil so they are not stored.

## Session Ref Extraction

Each adapter extracts native session refs differently:

- **OpenCode**: Searches for `sessionID`/`sessionId`/`session_id` in any JSON payload
- **Codex**: Looks for `type: "thread.started"` events with `thread_id`/`threadId`
- **Claude**: Looks for `type: "system"` with `subtype: "init"` and `session_id`/`sessionId`
- **Gemini**: Looks for `type: "init"` events with `session_id`/`sessionId`

Extracted session refs are persisted to the `runner_chat_sessions.native_session_ref` column so they survive process restarts.
