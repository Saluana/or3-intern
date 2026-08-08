# AGENTS.md

This file orients AI coding agents working in `or3-intern`. The README is the user-facing quick start; this file is for engineering navigation and validation.

## Project Shape

`or3-intern` is a Go runner control-plane with SQLite persistence, memory retrieval, connected app integrations, automation triggers, skills catalog metadata, approvals, secure connections, and an internal HTTP API consumed by `or3-app`.

Important directories:

- `cmd/or3-intern`: CLI entrypoint, command handlers, service HTTP routes, auth middleware, TUI/configure flows, and broad service tests.
- `internal/agentcli`: external runner integrations and runner-chat support.
- `internal/app`: runner turn orchestration, bootstrap context, and service app wiring.
- `internal/approval`: approval broker and device token handling.
- `internal/auth`: passkeys, auth sessions, step-up, and auth audit.
- `internal/config`: config loading, defaults, dotenv handling, readiness validation.
- `internal/db`: SQLite schema and stores.
- `internal/memory`: hybrid memory/doc retrieval, embeddings, consolidation, scheduler.
- `internal/memorysvc`: typed memory bridge operations for runners and service APIs.
- `internal/requestctx`: request-scoped identity, approval token, and delivery context helpers.
- `internal/secureconn`: secure device enrollment, certificates, secure session claims, action authorization, replay protection, and relay protocol helpers.
- `internal/tools`: slim MCP tool interface plus child-env/path helpers retained for process spawning.
- `docs`: user and architecture documentation. Legacy v1 docs live under `docs/archive/v1`.
- `scripts`: local helper scripts, including CLI install, service restart, and runner-first forbidden-symbol checks.

## Common Commands

Use ordinary Go commands from the repo root.

```bash
go test ./...
go test ./cmd/or3-intern
go test ./cmd/or3-intern ./internal/secureconn
go run ./cmd/or3-intern version
go run ./cmd/or3-intern service
go run ./cmd/or3-intern chat
bash scripts/check-runner-first-forbidden.sh
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
- `cmd/or3-intern/service_runner_chat.go`
- `cmd/or3-intern/service_test.go`
- `cmd/or3-intern/service_auth_rollout_test.go`
- `cmd/or3-intern/service_secure_connections_test.go`

Docs for the app-facing surface:

- `docs/api-reference.md`
- `docs/migration-runner-first.md`
- `docs/app-connection.md`
- `docs/connect-release-status.md` (current Connect launch decision and version contract)

When changing a service endpoint, auth policy, pairing, secure connections, or response contract, check `or3-app` callers too.

## Auth And Pairing Notes

There are several auth paths. Keep them conceptually separate:

- Shared service secret: service/client automation credentials.
- Legacy paired-device token: compatibility device auth from pairing flows.
- Auth session/passkey: owner/user auth and recent step-up.
