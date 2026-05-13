# Documentation Audit

Last audited: 2026-05-13

## Scope

This audit compared `docs/v1` against the current `or3-intern` package layout, command dispatch, and service route registration.

Checked areas:

- `cmd/or3-intern` command tiers and service route specs
- `internal` package groups
- `docs/v1` architecture, user-guide, operations, and reference files
- service contract fixtures under `cmd/or3-intern/testdata/service_contract`
- Markdown links inside `docs/v1`

## Coverage Status

Strong coverage already exists for:

- getting started and first-run setup
- CLI overview plus major operator commands
- core runtime internals
- tools and tool policy
- storage and database tables
- memory and document indexing
- channels
- automation
- diagnostics/doctor/startup validation
- security, approvals, auth, passkeys, network policy, sandboxing, and audit
- external agent CLI runners
- integrations, including providers, skills, ClawHub, artifacts, and MCP internals
- app integration user guide for common route families

## Weak Areas Found and Fixed

1. **Stale service API architecture pages**

   Several `architecture/service-api/*` pages described an older `/api/v1` contract. They now document the live `/internal/v1` route families, job model, file root/path model, runner chat, approvals, auth, middleware, and request models.

2. **Missing MCP management route docs**

   Added architecture and app-integration docs for `/internal/v1/mcp/servers/*`, including list/add/delete/test behavior and restart semantics.

3. **Missing control-plane documentation**

   Added a control-plane/ServiceApp page explaining how HTTP handlers, runtime, jobs, auth, approvals, embeddings, audit, and scope are bridged.

4. **Missing event bus documentation**

   Added a dedicated event bus page for channel and automation event flow.

5. **Missing UX state/copy documentation**

   Added docs for `internal/uxstate`, `internal/uxcopy`, and `internal/uxformat`, which are important to consumer-grade status/settings/approval behavior.

6. **Missing compatibility-helper documentation**

   Added docs for canonical snake_case fields, alias fallback, and warning behavior.

7. **Monitoring docs referenced old root endpoints**

   Updated monitoring docs to use `/internal/v1/health`, `/internal/v1/readiness`, and `/internal/v1/capabilities`.

## Remaining Follow-ups

These are not blockers, but they are worth documenting next:

- add short CLI guide pages for `serve`, `service`, `audit`, `capabilities`, `connect-device`, and `migrate-jsonl`
- add app-integration pages for skills, agent runs, embeddings/audit/scope, and app bootstrap/action routes
- keep `reference/command-reference.md` synced with root help topics when command help changes
- consider generating a service route index from `service_routes.go` to prevent future route drift

## Verification

Markdown links inside `docs/v1` were checked after the audit. No broken relative links were found.
