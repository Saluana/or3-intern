# Migration notes

These notes cover current app clients, operators, and deploy scripts.

## Service request payloads

- `/internal/v1` request and response keys are snake_case.
- App-facing identifiers include `session_key`, `parent_session_key`, `child_session_key`, `job_id`, `approval_id`, and `timeout_seconds`.

## Environment and compose deployments

- `.env` loading is opt-in. Set `OR3_LOAD_DOTENV=true` when a local development run should load a nearby `.env` file; existing shell variables still win.
- dotenv values are runtime-only and are not automatically written to `config.json`.
- Keep compose files aligned with `config.json` paths for `dbPath`, `artifactsDir`, `workspaceDir`, and service bind settings; avoid moving runtime storage without copying the existing SQLite and artifact directories.

## Integration warnings

- Incomplete channel or MCP integration setup can be quarantined so the core service continues running.
- Quarantined integrations appear in status/bootstrap warning payloads as `integration_quarantined` and should be visible in the app without blocking normal load unless severity is `error`.

## Legacy context settings

- Older `context` fields remain readable for config compatibility, but OR3 no longer presents or edits them as local settings.
- Runner-specific prompt context and tool permissions are managed by the selected runner. Use `or3-intern capabilities` to inspect the effective execution model.

## Embeddings

- If the embedding provider/model fingerprint changes, bootstrap/status can report `embedding_fingerprint_mismatch`.
- Rebuild embeddings from the memory tools after provider/model changes so search and recall use vectors from the current configuration.
