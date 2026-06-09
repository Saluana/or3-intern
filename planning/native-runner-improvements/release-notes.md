# Native Runner Improvements Release Notes

## Manual Verification

- OpenCode startup: with `agentCLI.runtimeMode.opencode=auto`, start a runner chat on OpenCode and confirm `/internal/v1/chat-runners` reports native readiness, endpoint, providers, and discovered models. Stop the OpenCode server and confirm auto mode falls back to CLI before a mutating turn begins.
- Codex app-server chat: start a Codex runner chat and confirm the app-server session starts over `stdio://`, emits a native thread ref, streams assistant output, and can run a second turn without respawning on the success path.
- Approval continuation: trigger a native approval request, approve it from the app, and confirm OR3 records `approval_response` with `route=native` and the runner continues without a manual retry. Repeat with the runtime stopped and confirm the approval-token fallback route is recorded.
- Abort: start a long-running native Codex/OpenCode turn, press stop, and confirm the runtime receives an abort/interrupt and the turn finalizes without leaving an active request ref.
- CLI fallback: set `agentCLI.runtimeMode.{opencode,codex}=cli` and confirm both runners still use the existing CLI adapters. Restore `auto` after verification.

## Rollout Notes

- Native runtime mode remains `auto` by default for OpenCode and Codex.
- Existing CLI command adapters remain available and are used when native startup/probing fails in auto mode.
- App runner/model pickers now consume backend runtime metadata for readiness, fallback guidance, defaults, model options, and native health.
- Activity logs now display canonical native observability events including config warnings, model reroutes, skill invocations, token usage, and approval responses.
