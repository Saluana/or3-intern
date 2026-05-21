# AGENTS.md

This file orients AI coding agents working in `or3-intern`. The README is the user-facing quick start; this file is for engineering navigation and validation.

## Project Shape

`or3-intern` is a Go CLI/service runtime with SQLite persistence, memory retrieval, connected app integrations, automation triggers, skills, approvals, secure connections, and an internal HTTP API consumed by `or3-app`.

Important directories:

- `cmd/or3-intern`: CLI entrypoint, command handlers, service HTTP routes, auth middleware, TUI/configure flows, and broad service tests.
- `internal/agent`: core agent runtime, prompt assembly, tool policy, streaming, jobs, skills, and subagents.
- `internal/agentcli`: external runner integrations and runner-chat support.
- `internal/approval`: approval broker and device token handling.
- `internal/auth`: passkeys, auth sessions, step-up, and auth audit.
- `internal/config`: config loading, defaults, dotenv handling, readiness validation.
- `internal/db`: SQLite schema and stores.
- `internal/memory`: hybrid memory/doc retrieval, embeddings, consolidation, scheduler.
- `internal/secureconn`: secure device enrollment, certificates, secure session claims, action authorization, replay protection, and relay protocol helpers.
- `internal/tools`: runtime tool implementations and metadata.
- `docs`: user and architecture documentation. `docs/v1` contains the current v1 docs set.
- `scripts`: local helper scripts, including CLI install and service restart helpers.

## Common Commands

Use ordinary Go commands from the repo root.

```bash
go test ./...
go test ./cmd/or3-intern
go test ./cmd/or3-intern ./internal/secureconn
go run ./cmd/or3-intern version
go run ./cmd/or3-intern service
go run ./cmd/or3-intern chat
```

Install the CLI when you need the `or3-intern` binary in your shell:

```bash
./scripts/install-cli.sh
```

Run `gofmt` on touched Go files before finishing.

## Service API And App Integration

`or3-app` normally talks to `or3-intern service`. The companion repo is commonly checked out at:

```text
/Users/brendon/Documents/or3-app
/Users/brendon/Documents/or3-intern
```

Service route and auth touch points:

- `cmd/or3-intern/service_routes.go`
- `cmd/or3-intern/service_auth.go`
- `cmd/or3-intern/service_auth_policy.go`
- `cmd/or3-intern/service_middleware.go`
- `cmd/or3-intern/service_secure_connections.go`
- `cmd/or3-intern/service_test.go`
- `cmd/or3-intern/service_auth_rollout_test.go`
- `cmd/or3-intern/service_secure_connections_test.go`

Docs for the app-facing surface:

- `docs/api-reference.md`
- `docs/v1/user-guide/app-integration/or3-app-connection-guide.md`
- `docs/v1/user-guide/app-integration/bootstrap.md`
- `docs/v1/architecture/security/secure-connections/secure-connections-api.md`

When changing a service endpoint, auth policy, pairing, secure connections, or response contract, check `or3-app` callers too.

## Auth And Pairing Notes

There are several auth paths. Keep them conceptually separate:

- Shared service secret: service/client automation credentials.
- Legacy paired-device token: compatibility device auth from pairing flows.
- Auth session/passkey: owner/user auth and recent step-up.
- Secure connection enrollment/session: certificate-backed device trust and server-issued secure session claims.

Route sensitivity is centralized in `serviceRouteRequirementForRequest`. If you change a route requirement, update `service_auth_rollout_test.go`.

Secure connection action policy is in `internal/secureconn/authorization.go`. Secure session lifecycle is in `internal/secureconn/session.go`, with service endpoints in `service_secure_connections.go`.

## Testing Guidance

Pick the narrowest useful test first, then broaden if you touch shared behavior.

Examples:

```bash
go test ./cmd/or3-intern -run TestServiceRouteRequirementForRequest_SensitivityMatrix
go test ./cmd/or3-intern -run Secure
go test ./internal/secureconn
go test ./internal/agent
go test ./internal/db
```

Run `go test ./...` for broad changes, but expect it to take longer because this repo has many integration-style tests.

## Development Cautions

- Do not weaken service auth gates casually. Many routes intentionally require session and recent step-up.
- Keep public/unauthenticated pairing and bootstrap routes narrowly scoped.
- Preserve existing JSON field names in service contracts. App-facing v1 APIs use snake_case.
- Do not leak secrets/tokens in logs, errors, audit payloads, or tests.
- Use existing config/default helpers instead of adding environment-only behavior.
- Keep SQLite migrations and schema changes backward-compatible.
- Avoid broad refactors when fixing route, auth, or runtime behavior. The command/service surface has many tests and compatibility assumptions.

## Cross-Repo Workflow

For `or3-app` integration work:

1. Update the service behavior here.
2. Run focused Go tests for the touched service/auth/runtime area.
3. Update `or3-app` composables/proxy/types/tests as needed.
4. Run focused app tests through Vitest, not direct `bun test`.
5. Document any intentional contract change in `docs/`.

Do not assume `localhost` works for remote devices. Phones and other devices need a LAN/Tailscale/reachable address; `127.0.0.1` only works when app and service run on the same computer.
