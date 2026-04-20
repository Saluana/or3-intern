# 1. Add configuration and startup posture

- [x] [Req 2, Req 10, Req 11] Extend `internal/config/config.go` with `Security.Approvals`, per-domain modes, TTLs, and a stable `hostId` default that stays lightweight.
- [x] [Req 2, Req 11] Add config validation and defaults tests in `internal/config/config_test.go` so missing approval config remains backward compatible and unknown modes fail clearly.
- [x] [Req 2, Req 10] Update `cmd/or3-intern/startup_validation.go` and `cmd/or3-intern/doctor.go` to warn or refuse unsafe combinations such as exposed service mode with approvals enabled but no signing key, or `ask`/`allowlist` modes without a working broker.

# 2. Add SQLite persistence for pairing and approvals

- [x] [Req 3, Req 8, Req 11, Req 13] Add additive migrations in `internal/db/db.go` for `paired_devices`, `pairing_requests`, `approval_requests`, `approval_allowlists`, and `approval_tokens` plus the minimum supporting indexes.
- [x] [Req 3, Req 8, Req 10, Req 11] Add focused DB helpers in new files such as `internal/db/approval_store.go` and tests in `internal/db/approval_store_test.go` for create/list/update/expire/revoke operations.
- [x] [Req 11] Add reopen/migration regression coverage so an existing database upgrades cleanly and the new tables survive restart.

# 3. Implement the internal broker package

- [x] [Req 1, Req 5, Req 6, Req 8, Req 10, Req 13] Create `internal/approval` with domain enums, canonical subject structs, deterministic hashing helpers, allowlist matchers, and broker orchestration.
- [x] [Req 3, Req 4, Req 10] Implement pairing-code generation, hashed storage, opaque device-token issuance, device-token authentication, revocation, and rotation in the broker.
- [x] [Req 5, Req 6, Req 10, Req 13] Implement short-lived HMAC-signed approval tokens whose claims bind request ID, subject hash, host ID, and expiry.
- [x] [Req 1, Req 9] Route all approval and pairing state transitions through broker methods that also emit audit events via the existing `security.AuditLogger`.
- [x] [Req 5, Req 6, Req 8] Add unit tests in `internal/approval/*_test.go` for subject hashing, allowlist matching, token verification, duplicate request reuse, and expiration.

# 4. Wire host-local enforcement into tool execution

- [x] [Req 7] Extend `internal/tools/context.go` with the minimal approval context needed for execution, such as a one-shot approval token and requester identity.
- [x] [Req 1, Req 7] Install an approval-aware `ToolGuard` during runtime/tool registry construction in `cmd/or3-intern/main.go` so sensitive checks remain centralized.
- [x] [Req 5, Req 7, Req 10] Update `internal/tools/exec.go` to build a canonical exec subject and fail closed before subprocess launch when approval is missing, denied, expired, or mismatched.
- [x] [Req 6, Req 7, Req 10] Update `internal/tools/skill_exec.go` to build a canonical skill-exec subject and fail closed before script execution when approval is missing, denied, expired, or mismatched.
- [x] [Req 7, Req 8, Req 9] Add execution audit callbacks so start/block/complete/fail events include subject hash and host ID without duplicating a second audit system.
- [x] [Req 5, Req 6, Req 7] Add integration tests in `internal/tools/exec_test.go` and `internal/tools/skill_exec_test.go` for approval-required, subject-mismatch, approve-once, and allowlist flows.

# 5. Add CLI-first operator workflows

- [x] [Req 12] Add `cmd/or3-intern/approvals_cmd.go` and `cmd/or3-intern/approvals_cmd_test.go` for `list`, `show`, `approve`, `deny`, and allowlist management.
- [x] [Req 3, Req 12] Add `cmd/or3-intern/devices_cmd.go` and `cmd/or3-intern/devices_cmd_test.go` for pairing-request review, device listing, revocation, and rotation.
- [x] [Req 12] Update `cmd/or3-intern/main.go` command dispatch so approval and device management work without running the service listener.
- [x] [Req 9, Req 12] Ensure all CLI resolution paths stamp a stable actor string such as `cli` or `cli:<user>` into audit events.

# 6. Extend the existing service API instead of adding a new server

- [x] [Req 4, Req 12] Extend `cmd/or3-intern/service.go` with `/internal/v1/pairing/*`, `/internal/v1/devices/*`, and `/internal/v1/approvals/*` routes that call the broker directly.
- [x] [Req 3, Req 4, Req 10] Extend `cmd/or3-intern/service_auth.go` so current shared-secret bearer auth remains valid while paired-device tokens can authenticate operator/device routes with role checks.
- [x] [Req 4, Req 9] Add request metadata stamping so audit records distinguish shared-secret bootstrap/admin auth from paired-device auth.
- [x] [Req 3, Req 4, Req 10, Req 12] Add service tests in `cmd/or3-intern/service_test.go` and related auth tests for pairing request creation, code exchange, paired-device approval actions, revocation, rotation, and backward compatibility.

# 7. Tighten operational guidance and safety checks

- [x] [Req 2, Req 10, Req 12] Document the new config in `docs/configuration-reference.md` and `docs/security-and-hardening.md`, including the meaning of `deny`, `ask`, `allowlist`, and `trusted`.
- [x] [Req 12] Document CLI flows in `docs/cli-reference.md` and HTTP routes in `docs/api-reference.md`.
- [x] [Req 9, Req 10] Add operator guidance for approval expiration, revoked devices, and offline CLI operation.
- [x] [Req 13] Clearly document that web UI, chat approvals, secret-access approvals, outbound-message approvals, sandbox verification, and remote-node forwarding are compatible future phases, not hidden partial v1 behavior.

# 8. Post-review hardening follow-up

- [x] Make approval and pairing resolution single-winner so late deny/approve races cannot overwrite already resolved requests.
- [x] Require anonymous allowlist pairing attempts for existing devices to stay pending until an operator approves them.
- [x] Prevent device rotation workflows from reviving revoked devices outside of a fresh pairing exchange.
- [x] Stamp anonymous and authenticated service pairing flows with stable audit actor/auth metadata.
- [x] Add regression tests for malformed approval JSON, revoked-device rotation, and paired-identity lookup beyond the first device page.

# 9. Out of scope for the first implementation pass

- [ ] Do not add a browser UI or desktop pairing client.
- [ ] Do not gate every tool immediately; only `exec` and `run_skill_script` need full enforcement in the first pass.
- [ ] Do not add a separate approval daemon, REST framework, queue, or external database.
- [ ] Do not require `or3-sandbox` or remote nodes to ship in the same change; only preserve token and subject compatibility for them.
- [ ] Do not replace the current shared-secret service auth path until paired-device auth has landed and compatibility has been proven.
