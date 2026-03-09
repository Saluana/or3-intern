# 1. Secret storage

- [x] (R1, R5) Add config for encrypted secret storage in `internal/config/config.go` and document a local key source plus backward-compatible plaintext fallback.
- [x] (R1, R5) Add SQLite helpers in `internal/db` for versioned encrypted secret records if secrets move out of `config.json`.
- [x] (R1) Update provider, channel, MCP, and webhook startup wiring to resolve secrets without logging plaintext values.

# 2. Audit trail

- [x] (R2, R5) Add an append-only `audit_events` schema and DB helpers in `internal/db` for bounded event persistence.
- [x] (R2) Implement record hashing/signing or HMAC chaining plus verification helpers in a small internal package or `internal/db`.
- [x] (R2) Emit audit records from sensitive flows such as secret changes, privileged exec, skill approval/install, and pairing/profile updates.

# 3. Access profiles

- [x] (R3) Add config types for named access profiles and default profile assignment rules in `internal/config/config.go`.
- [x] (R3) Enforce effective-profile checks in `internal/agent/runtime.go`, `internal/agent/subagents.go`, and risky tool paths under `internal/tools/`.
- [x] (R3) Ensure child subagents inherit an equal-or-more-restrictive profile and add regression tests.

# 4. Trusted-host policy

- [x] (R4) Extend `internal/tools/web.go` with trusted-host allowlists or pattern matching on initial URLs, redirects, and resolved IPs.
- [x] (R4) Apply the same outbound host policy to `internal/mcp/manager.go`, provider HTTP clients where appropriate, and channel integrations that initiate external requests.
- [x] (R4) Add focused tests for deny-by-policy, redirect bypass attempts, and loopback/private-address handling.

# 5. Validation and migration

- [x] (R1-R5) Add config and DB migration tests covering mixed-mode startup, additive schema changes, and backward-compatible loading.
- [x] (R1-R5) Update operator docs in `README.md` with rollout order, key-management expectations, and strict-vs-compatibility modes.

# 6. Out of scope

- [x] No cloud KMS, distributed key escrow, or external audit service in Phase 3.
- [x] No full enterprise RBAC or multi-tenant isolation model.
- [x] No replacement of SQLite with a separate security datastore.
