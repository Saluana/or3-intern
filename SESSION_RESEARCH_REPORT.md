# Chat Runner Session Research Report

## The Problem

Every chat turn currently relaunches the CLI binary from scratch and sends the **full conversation history** as a text "replay prompt" (bounded to 12 turns / 48 KiB). This wastes tokens/credits — each turn re-processes the entire history from scratch, even though every agent natively persists sessions on disk with indexed context.

## What Each Agent Actually Supports

### OpenCode (already partially supported)

| Capability | Detail |
|---|---|
| Native resume | `opencode run --session <id> "message"` |
| Continue last | `opencode run --continue "message"` |
| Fork session | `--fork` flag when resuming |
| Session ID in output | ✅ `session_id` field in JSON stream |
| Server/API mode | `opencode serve` — full HTTP REST API (OpenAPI 3.1) at port 4096 |
| ACP protocol | `opencode acp` — stdio JSON-RPC for IDE integration |
| TypeScript SDK | `@opencode-ai/sdk` (stable) |
| Status | **Already implemented** — uses `--session` + extracts session ref from JSON |

### Codex

| Capability | Detail |
|---|---|
| Native resume | `codex exec resume <SESSION_ID> "message"` or `codex exec resume --last "message"` |
| Fork session | `codex fork <SESSION_ID>` |
| Session ID in output | ✅ `thread_id` in `thread.started` JSONL event |
| Server/API mode | `codex app-server` — JSON-RPC 2.0 over stdio or WebSocket (experimental but working) |
| JSON output | `codex exec --json` produces JSONL with structured events |
| TypeScript SDK | `@openai/codex-sdk` (stable) |
| Python SDK | `codex_app_server` (experimental) |
| Status | **Implemented in this pass** — uses explicit native refs for chat continuity and keeps replay as an explicit/fallback mode. |

### Claude Code

| Capability | Detail |
|---|---|
| Native resume | `claude --resume <id_or_name> -p "message"` or `claude -c -p "message"` for latest |
| Set session UUID | `claude --session-id <UUID>` — create with predictable ID |
| Fork session | `--fork-session` when resuming |
| Session ID in output | ✅ `session_id` in `system/init` event in stream-json |
| Server/API mode | **None.** `remote-control` polls Anthropic API (no local server). Use Agent SDK for programmatic access. |
| JSON output | `--output-format stream-json` — newline-delimited JSON events (already used) |
| TypeScript SDK | `@anthropic-ai/claude-code` (experimental) |
| Python SDK | `claude-code-sdk` (experimental) |
| Stdin resume | `--input-format stream-json` accepts structured messages on stdin |
| Ephemeral mode | `--no-session-persistence` |
| Status | **Implemented in this pass** — uses explicit native refs for chat continuity and keeps replay as an explicit/fallback mode. |

### Gemini CLI

| Capability | Detail |
|---|---|
| Native resume | `gemini --resume <id_or_latest_or_index> "message"` |
| List sessions | `gemini --list-sessions` |
| Session ID in output | ✅ `session_id` in `init` event in stream-json |
| Server/API mode | **None.** ACP mode (`--acp`) is stdio JSON-RPC only, not a network server. |
| JSON output | `--output-format stream-json` — JSONL stream of events |
| ACP protocol | `gemini --acp` — stdio JSON-RPC for IDE integration |
| Status | **Implemented in this pass** — uses explicit native refs for chat continuity and keeps replay as an explicit/fallback mode. |

## Summary of Capabilities

| Runner | Native Resume | Server/API | Session ID Extractable | SDK |
|---|---|---|---|---|
| OpenCode | ✅ `--session` | ✅ `serve` HTTP | ✅ (already done) | ✅ TS |
| Codex | ✅ `exec resume` | ✅ `app-server` JSON-RPC | ✅ from `thread.started` | ✅ TS+Py |
| Claude Code | ✅ `--resume` | ❌ | ✅ from `system/init` | ✅ TS+Py (experimental) |
| Gemini | ✅ `--resume` | ❌ (ACP only) | ✅ from `init` | ❌ |

## What Needs to Change

### The adapter interface is already sufficient

The current interfaces (`RunnerChatAdapter`, `NativeRunnerChatAdapter`) and capabilities struct (`RunnerChatCapabilities`) already model everything needed. No interface changes required.

### Changes per runner

#### 1. Codex — Implement native resume

**`BuildChatCommand`** (`chat_adapters.go:38`):
- When `NativeSessionRef` is set and `ContinuationMode` is `ContinuationNative`:
  ```
  codex exec resume <native_ref> --json --color never --skip-git-repo-check --cd <cwd> "new message"
  ```
- Native chat mode sends the current user message only. If no native ref exists yet, the CLI starts a fresh provider-native session and the adapter extracts the ref from output.
- The `userMessage` goes as the final positional argument, NOT the replay prompt
- `--resume` mode: `codex exec resume --last "message"` could work for the simplest case, but is unsafe for concurrent sessions — prefer the explicit ID variant

**`ExtractNativeSessionRef`** (new, implements `NativeRunnerChatAdapter`):
- Parse the JSONL output looking for `{"type": "thread.started", "thread_id": "..."}`
- Return `thread_id` as the session ref

**`RunnerChatCapabilities`** update (`registry.go:56-59`):
```go
Chat: RunnerChatCapabilities{
    ChatSelectable:            true,
    ChatReplay:                true,
    ChatNativeSession:         true,  // new
    ChatResume:                true,  // new
    ChatSessionRefExtractable: true,  // new
},
```

#### 2. Claude Code — Implement native resume

**`BuildChatCommand`** (`chat_adapters.go:42`):
- When `NativeSessionRef` is set and `ContinuationMode` is `ContinuationNative`:
  ```
  claude --bare --resume <native_ref> -p "new message" --output-format stream-json --verbose --include-partial-messages [--permission-mode ...]
  ```
- Native chat mode sends the current user message only. Replay remains available only when explicitly selected or when native mode is unsupported.
- Note: `-c` (continue latest) exists but is unsafe for concurrent sessions per directory

**`ExtractNativeSessionRef`** (new, implements `NativeRunnerChatAdapter`):
- Parse the stream-json output for `{"type": "system", "subtype": "init", "session_id": "..."}` 
- Return `session_id` as the session ref

**`RunnerChatCapabilities`** update (`registry.go:76-79`):
```go
Chat: RunnerChatCapabilities{
    ChatSelectable:            true,
    ChatReplay:                true,
    ChatNativeSession:         true,  // new
    ChatResume:                true,  // new
    ChatSessionRefExtractable: true,  // new
},
```

#### 3. Gemini CLI — Implement native resume

**`BuildChatCommand`** (`chat_adapters.go:46`):
- When `NativeSessionRef` is set and `ContinuationMode` is `ContinuationNative`:
  ```
  gemini --resume <native_ref> --prompt "new message" --output-format stream-json [--approval-mode ...]
  ```
- Native chat mode sends the current user message only. If no native ref exists yet, Gemini starts a fresh stream-json session and emits the `init` ref.

**`ExtractNativeSessionRef`** (new, implements `NativeRunnerChatAdapter`):
- Parse the stream-json output for `{"type": "init", "session_id": "..."}`
- Return `session_id` as the session ref

**Note**: Gemini's `--output-format` currently defaults to `json` (single object). Need to switch to `stream-json` for chat mode to get the `init` event with session ID.

**`RunnerChatCapabilities`** update (`registry.go:96-99`):
```go
Chat: RunnerChatCapabilities{
    ChatSelectable:            true,
    ChatReplay:                true,
    ChatNativeSession:         true,  // new
    ChatResume:                true,  // new
    ChatSessionRefExtractable: true,  // new
},
```

### No changes needed for OpenCode

OpenCode's native session support is already implemented. The `--session` flag is used with extracted session ref. The code paths in `chat_manager.go:123-130` already validate `ChatNativeSession + ChatResume + ChatSessionRefExtractable` and call the right adapter path.

## Optional: Beyond CLI — Server/API Mode

For even lower latency (no process startup per turn), two runners have server modes:

### OpenCode: `opencode serve`
- HTTP REST API with OpenAPI 3.1 spec
- Session management via REST: `POST /session/:id/message` sends a prompt, `GET /session/:id` retrieves history
- Sessions persist in the server process — no replay needed
- The TypeScript SDK (`@opencode-ai/sdk`) wraps this nicely
- This would require adding a runner-level transport abstraction (CLI vs HTTP), which is a bigger change than the native resume approach

### Codex: `codex app-server`
- JSON-RPC 2.0 over stdio or WebSocket
- Thread management: `thread/resume`, `thread/fork`, turn streaming
- The TypeScript SDK (`@openai/codex-sdk`) wraps `app-server`
- Would require a persistent subprocess manager, larger architectural change

**Recommendation**: Defer server-mode integration. The native resume approach (above) gives 90% of the benefit with 10% of the complexity. Server mode can be a future optimization layer.

## Implementation Effort Estimate

| Task | Effort | Notes |
|---|---|---|
| Codex `BuildChatCommand` native resume | Small | Add `exec resume` branch, ~20 lines |
| Codex `ExtractNativeSessionRef` | Small | Parse JSONL for `thread_id`, ~30 lines |
| Codex capabilities update | Trivial | 3 bools to true in `AllRunners()` |
| Claude `BuildChatCommand` native resume | Small | Add `--resume` branch, ~20 lines |
| Claude `ExtractNativeSessionRef` | Small | Parse `system/init` for `session_id`, ~30 lines |
| Claude capabilities update | Trivial | 3 bools to true in `AllRunners()` |
| Gemini `BuildChatCommand` native resume | Small | Add `--resume` branch, switch to stream-json, ~25 lines |
| Gemini `ExtractNativeSessionRef` | Small | Parse `init` for `session_id`, ~30 lines |
| Gemini capabilities update | Trivial | 3 bools to true in `AllRunners()` |
| Tests | Medium | Add test cases for each new native resume path |
| **Total** | **~1-2 days** | Mostly straightforward adapter work |

The `chat_manager.go` infrastructure (session ref persistence, native mode validation, turn lifecycle) already handles all of this generically — it calls `ExtractNativeSessionRef` on every adapter that implements `NativeRunnerChatAdapter`, persists the ref to `runner_chat_sessions.native_session_ref`, and passes it to `BuildChatCommand` on subsequent turns.

## Key Caveats

1. **Claude `--resume` merges with `-p`**: Claude may show some TUI artifacts even with `-p`. Test thoroughly that `--resume <id> -p "message"` works correctly in non-interactive mode.

2. **Gemini resume requires same CWD**: Gemini sessions are project-scoped. Resume only works from the same directory. This should already be fine given how `AgentRunRequest.Cwd` works.

3. **Codex `exec resume` availability**: `codex exec resume` is documented but should be verified against actual binary. The `--last` variant is simpler but is unsafe for concurrent sessions — use the explicit `resume <id>` form.

4. **First-turn behavior**: Native-capable sessions now default to native mode immediately, so the first turn sends only the current user message and starts a provider-native session. Replay prompt mode remains explicit/fallback behavior.

## Test Plan

For each runner after implementation:
1. Start a chat session → first native turn sends only the current user message → extract session ref from output
2. Send second turn → should use native resume flag (not replay prompt)
3. Verify the agent correctly understands prior context (e.g., "what did I just ask you?")
4. Verify token usage is lower (if measurable via agent output)
5. Verify concurrent sessions don't cross-contaminate (two separate sessions with different refs)
