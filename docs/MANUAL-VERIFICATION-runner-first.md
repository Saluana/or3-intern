# Manual verification: runner chat

Run on a machine with authenticated OpenCode and Codex installations.

## Service / CLI

1. `or3-intern doctor` — reports OpenCode install/auth and default runner readiness.
2. `or3-intern chat` — sends a message; turn is handled by the default external runner (not built-in tool loop).
3. `GET /internal/v1/chat-runners` — lists selectable runners and each runner's
   live `models`; Codex model IDs match app-server `model/list` exactly.
4. Enqueue a cron `runner_run` job — appears in agent CLI run history.
5. Trigger heartbeat with `HEARTBEAT.md` present — autonomous turn uses runner orchestrator.

## OR3 Chat

1. Open Agents, connect the host, and choose **New agent**.
2. Open composer settings. Verify Codex and OpenCode show their current models
   with friendly provider labels.
3. Choose a non-default model such as `gpt-5.6-luna`. Send a short instruction
   and confirm the service receives that exact ID, without an added provider
   prefix.
4. Send a follow-up and confirm it appears in the same conversation.
5. In review or safe-edit mode, request a protected file or command action.
   Confirm one inline approval card appears.
6. Approve once. The button disables while submitting, the same native turn
   resumes, and the card stays visibly approved.
7. Repeat and deny. The protected action does not run and the card stays
   visibly denied.
8. Reload OR3 and reopen both sessions. Transcript, model, terminal status, and
   approval decisions reconstruct from the canonical host history.
9. Confirm normal screens show no request IDs, endpoints, headers, raw JSON, or
   stack traces.
10. If a runner fails before step 5 with a credit/authentication error, treat
    approval verification for that runner as blocked by its provider account;
    do not record the approval path as passed.
