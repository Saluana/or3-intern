# Migration notes

These notes cover the current core-simplification compatibility boundary for app clients, operators, and deploy scripts.

## Service request payloads

- `/internal/v1` remains backward compatible: snake_case request and response keys are canonical, and existing camelCase aliases are still accepted.
- If a request sends both snake_case and camelCase fields with different values, the snake_case value wins. The response includes `X-Or3-Request-Warning` so clients can log or surface the conflict without breaking the request.
- App-facing identifiers such as `session_key`, `parent_session_key`, `child_session_key`, `job_id`, `approval_id`, `timeout_seconds`, and `tool_policy` remain unchanged.

## Environment and compose deployments

- `.env` loading is additive. Existing shell variables are preserved, so compose or supervisor-provided values override `.env` entries.
- Set `OR3_LOAD_DOTENV=false` when a deployment must ignore `.env` files entirely.
- Keep compose files aligned with `config.json` paths for `dbPath`, `artifactsDir`, `workspaceDir`, and service bind settings; avoid moving runtime storage without copying the existing SQLite and artifact directories.

## Integration warnings

- Incomplete channel or MCP integration setup can be quarantined so the core service continues running.
- Quarantined integrations appear in status/bootstrap warning payloads as `integration_quarantined` and should be visible in the app without blocking normal load unless severity is `error`.

## Context defaults

- Older configs without a `context` section run in legacy context mode and emit `legacy_context_mode` warnings.
- Use `or3-intern settings` for normal updates or `or3-intern configure --section context` for targeted edits to write the modern context defaults.

## Embeddings

- If the embedding provider/model fingerprint changes, bootstrap/status can report `embedding_fingerprint_mismatch`.
- Rebuild embeddings from the memory tools after provider/model changes so search and recall use vectors from the current configuration.