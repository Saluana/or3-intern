# Overview

This follow-up keeps building on the current `internal/controlplane` package instead of widening the architecture. The smallest repo-aligned approach is:

1. lift the remaining machine-useful CLI-only operations into shared control-plane methods,
2. add thin `service` endpoints on top of those methods,
3. add fixture-backed regression coverage for the newer stable responses, and
4. extract only the bootstrap seams in `main.go` that are already shared in spirit.

This fits the current Go CLI + SQLite runtime because it reuses the existing runtime, DB, audit logger, approval broker, and job registry instead of creating a second application stack.

# Affected areas

- `internal/controlplane` — add reusable embeddings, audit, and scope operations; keep method contracts Go-native and transport-agnostic.
- `cmd/or3-intern/service.go` — add thin HTTP handlers and route wiring for any approved parity endpoints.
- `cmd/or3-intern/embeddings_cmd.go`, `cmd/or3-intern/main.go` (scope/audit paths) — switch CLI code to shared operations where sensible.
- `cmd/or3-intern/main.go` and adjacent startup helpers — extract common runtime/bootstrap wiring into a focused helper package or helper set.
- `cmd/or3-intern/service_test.go`, `cmd/or3-intern/service_contract_test.go`, `internal/controlplane/controlplane_test.go` — add parity and compatibility coverage.
- `docs/api-reference.md`, `docs/cli-reference.md` — document newly supported endpoints and the preserved command boundary.

# Control flow / architecture

`service` should remain a thin transport layer:

- HTTP handler authenticates, validates method/body, and maps request fields.
- Handler calls `controlplane.Service` for the actual operation.
- Shared control-plane code calls existing DB, audit, provider, memory, or scope helpers.
- CLI commands call the same control-plane methods and keep only flag parsing plus human-readable output.

For bootstrap reuse, extract a small internal helper around runtime/service wiring (for example config-validated construction of provider, jobs, subagent manager, audit/approval dependencies), but keep command dispatch in `main.go`.

# Data and persistence

No new persistence layer is needed.

- Embeddings parity should reuse the current memory/doc embedding state in SQLite and the existing provider fingerprint logic.
- Audit parity should reuse the existing audit logger and append-only audit verification flow.
- Scope parity should reuse the existing session-link data already stored in SQLite.
- Schema changes are not expected for the first pass; explicitly avoid migrations unless a concrete API gap requires one.
- Config/env changes are not expected.

# Interfaces and types

Prefer extending `internal/controlplane.Service` with small methods such as:

```go
func (s *Service) GetEmbeddingStatus(ctx context.Context) (EmbeddingStatusReport, error)
func (s *Service) RebuildEmbeddings(ctx context.Context, target string) (EmbeddingRebuildResult, error)
func (s *Service) GetAuditStatus(ctx context.Context) (AuditStatusReport, error)
func (s *Service) VerifyAudit(ctx context.Context) (AuditVerifyResult, error)
func (s *Service) LinkSessionScope(ctx context.Context, sessionKey, scopeKey string) error
func (s *Service) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error)
func (s *Service) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error)
```

The exact names can vary, but the shape should separate reusable application logic from CLI/HTTP formatting.

# Failure modes and safeguards

- Missing runtime/provider/DB dependencies should return explicit service-unavailable or bad-request style errors through the transport layer.
- Rebuild endpoints must stay bounded and should surface provider failures without hiding partial progress.
- Audit verify endpoints must preserve the current strict/hosted posture and never weaken startup validation.
- Scope endpoints must preserve session isolation rules and reject malformed identifiers cleanly.
- Bootstrap extraction must not silently change command ordering, startup gating, or which commands are handled before runtime bootstrap.

# Testing strategy

- Add `internal/controlplane` unit tests for new embeddings, audit, and scope methods.
- Add `service` route tests for auth, methods, request decoding, and response shapes.
- Add contract/fixture coverage for stable singleton responses such as health/readiness/capabilities plus any new status endpoints that are meant to stay machine-stable.
- Keep broader validation focused: use existing service tests first, then targeted Go tests for the extracted bootstrap helpers.
