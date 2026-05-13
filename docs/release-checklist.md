# Release checklist

Run these checks before cutting a release that includes service/app contract changes.

## Required validation

- `go test ./...`
- `bun run typecheck` in `or3-app`
- `bun run test` in `or3-app`
- `bun run build` in `or3-app`
- Manual app smoke against a local `or3-intern service`

## Manual smoke focus

- Pair the app, start a session, and send a foreground turn.
- Start a subagent/agent-run flow and confirm `job_id`, `parent_session_key`, and `child_session_key` remain snake_case in app state.
- Trigger or mock bootstrap warnings for `integration_quarantined`, `legacy_context_mode`, and `embedding_fingerprint_mismatch` and confirm they are visible but non-blocking for warning severity.
- Exercise `settings`, `connect-device`, `pairing approve-code`, and `devices list` to confirm canonical CLI guidance is consistent.
- Confirm non-2xx service responses include `error`, `code`, and `request_id`.