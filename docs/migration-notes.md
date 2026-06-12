# Migration notes

These notes cover current app clients, operators, and deploy scripts.

## Service request payloads

- `/internal/v1` request and response keys are snake_case.
- App-facing identifiers include `session_key`, `parent_session_key`, `child_session_key`, `job_id`, `approval_id`, and `timeout_seconds`.

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
