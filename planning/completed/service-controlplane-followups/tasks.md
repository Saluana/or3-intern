# 1. Add deferred HTTP parity

- [x] [Req 1, 2] Extend `internal/controlplane/controlplane.go` with shared embeddings operations reused from `cmd/or3-intern/embeddings_cmd.go`.
- [x] [Req 1, 2] Extend `internal/controlplane` with shared audit status/verify operations backed by the existing audit logger/security setup.
- [x] [Req 1, 2] Extend `internal/controlplane` with shared scope link/list/resolve operations backed by the existing SQLite scope helpers.
- [x] [Req 1, 2] Add thin `service` routes/handlers in `cmd/or3-intern/service.go` for the approved embeddings, audit, and scope endpoints.

# 2. Thin the CLI wrappers

- [x] [Req 2] Refactor `cmd/or3-intern/embeddings_cmd.go` to call shared control-plane methods instead of owning the core logic.
- [x] [Req 2] Move the inline `audit` and `scope` operation bodies out of `cmd/or3-intern/main.go` into shared helpers/control-plane calls while keeping CLI output unchanged.

# 3. Extract the next bootstrap seam

- [x] [Req 3] Identify the smallest reusable startup/wiring slice in `cmd/or3-intern/main.go` and extract it into an internal helper package or helper set.
- [x] [Req 3] Update `chat`, `serve`, and `service` startup paths to use that helper without changing command boundaries or validation order.
- [x] [Req 3] Add regression tests for command bootstrap boundaries and any newly extracted helper behavior.

# 4. Lock down compatibility and docs

- [x] [Req 4] Add route/auth/method/decode tests in `cmd/or3-intern/service_test.go` for each new endpoint.
- [x] [Req 4] Add fixture-backed or compatibility-style tests for stable status/control responses in `cmd/or3-intern/service_contract_test.go` where response shape stability matters.
- [x] [Req 4] Add/update `internal/controlplane/controlplane_test.go` coverage for the new shared operations.
- [x] [Req 4] Update `docs/api-reference.md` and `docs/cli-reference.md` to document the new parity endpoints and reaffirm `serve` vs `service` responsibilities.

# 5. Out of scope

- [ ] Do not merge `serve` and `service`.
- [ ] Do not add a second runtime/application stack for HTTP.
- [ ] Do not force local-only workflows into HTTP if the existing security posture cannot support them cleanly.
