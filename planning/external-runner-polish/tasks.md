# External Runner Polish Tasks

## 1. Verify current assumptions

- [x] Re-run focused tests around `internal/agentcli` runner chat and note current baseline failures. Requirements: 1, 2, 3.
- [ ] Validate CLI syntax locally for Codex `exec resume <id>`, Claude `--resume <id> -p`, and Gemini `--resume <id>` before flipping capabilities. Requirements: 1, 2. **Partially complete:** Codex is now installed and `codex exec resume --help` was validated; Claude and Gemini binaries are still blocked on local PATH.
- [x] Update or annotate `SESSION_RESEARCH_REPORT.md` if any runner command/session event differs from the report. Requirements: 1, 2.

## 2. Native session continuity

- [x] Make native-capable chat sessions prefer `ContinuationNative` by default while keeping explicit replay as a fallback/user option. Requirements: 1, 2.
- [x] Update `CodexAdapter.BuildChatCommand` to use explicit native resume when `ContinuationNative` and `NativeSessionRef` are set. Requirements: 1.
- [x] Update `ClaudeAdapter.BuildChatCommand` to use explicit native resume with stream-json output when a native ref exists. Requirements: 1.
- [x] Update `GeminiAdapter.BuildChatCommand` to use explicit native resume and stream-json output when a native ref exists. Requirements: 1.
- [x] Add `ExtractNativeSessionRef` implementations for Codex, Claude, and Gemini. Requirements: 2.
- [x] Add tests proving first native turns and resumed native turns send only `UserMessage`, not the full replay prompt. Requirements: 1, 2.
- [x] Flip `registry.go` native capability flags only for runners with passing command and extraction tests. Requirements: 1, 2, 3.

## 3. Canonical runtime events

- [x] Add a small canonical event helper/type set in `internal/agentcli` for `content.delta`, item lifecycle, request lifecycle, plan/diff, warning, error, and turn completion payloads. Requirements: 4, 5, 6.
- [x] Extend Codex normalization for JSONL events: assistant deltas, reasoning deltas, command/file output, plan updates, approval requests/resolution, diffs, and errors. Requirements: 4, 5.
- [x] Extend Claude normalization for stream-json events: assistant text, reasoning when available, tool lifecycle summaries, TodoWrite/plan updates, session errors, and final result. Requirements: 4, 5.
- [x] Extend Gemini normalization for stream-json events: assistant text, session lifecycle, tool/output summaries where available, and errors. Requirements: 4, 5.
- [x] Map tool categories into `command_execution`, `file_change`, `mcp_tool_call`, `web_search`, `collab_agent_tool_call`, `dynamic_tool_call`, or `unknown` instead of flattening everything into assistant text. Requirements: 5.
- [x] Keep legacy `text_delta`, `runner_output`, and `completion` event fields stable for existing app consumers. Requirements: 4, 6.

## 4. Service and app contract

- [x] Ensure `controlplane.BuildRunnerChatEventResponse` passes canonical payload JSON through unchanged. Requirements: 6.
- [x] Add service tests proving event list and SSE responses expose canonical payloads plus legacy fields. Requirements: 6.
- [x] Document the app-facing event contract in the plan or docs near runner chat APIs so `or3-app` can implement one renderer path. Requirements: 6.
- [x] Coordinate an `or3-app` follow-up to map canonical payloads into native chat message/activity/approval/plan/diff UI components. Requirements: 5, 6. Implemented in `or3-app` runner event normalization.

## 5. Safety and regression coverage

- [x] Add tests that unknown provider events remain bounded `runner_output` diagnostics. Requirements: 7.
- [x] Add tests that native resume never uses "latest" / process-global continuation commands. Requirements: 1, 7.
- [x] Add tests for two concurrent sessions with distinct native refs. Requirements: 3, 7.
- [x] Add tests that command/file/tool output remains bounded and does not leak secrets through raw payloads. Requirements: 5, 7.
- [x] Run `go test ./internal/agentcli ./internal/db ./internal/controlplane ./cmd/or3-intern` and then `go test ./...` if focused tests pass. Requirements: 7.

## Out of scope

- [x] Do not add Codex `app-server`, OpenCode SDK server mode, Claude Agent SDK, persistent subprocess registries, or a new WebSocket server in this phase.
- [x] Do not redesign `runner_chat_*` SQLite tables unless canonical event querying proves necessary.
- [x] Do not replace existing OR3 chat/session/memory behavior; external runners should integrate into it.
