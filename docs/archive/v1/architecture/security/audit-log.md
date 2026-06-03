# Audit Logging

The audit logger records tamper-evident records of all security-relevant events.

## AuditLogger

The `AuditLogger` struct has:
- `DB` - database connection
- `Key` - HMAC key for chain integrity
- `Strict` - when true, audit failures cause operation failures (fail closed)

Source: `internal/security/store.go:151-155`

## Recording events

`Record` appends an event with:
- `eventType` - e.g., "approval.requested", "pairing.resolved", "device.revoked"
- `sessionKey` - linked session (optional, can be empty)
- `actor` - who performed the action
- `payload` - arbitrary structured data about the event

If the logger is unavailable (nil, no DB, no key) and strict mode is on, the operation fails. If not strict, the record is silently dropped.

Source: `internal/security/store.go:158-171`

## Chain verification

`Verify` validates the entire audit chain using the HMAC key. This checks that no records have been inserted, deleted, or modified since they were written. The verification is done through `DB.VerifyAuditChain`.

Source: `internal/security/store.go:174-179`

## Audit events from the approval broker

The approval broker generates these audit events:

- `approval.requested` - new approval request created
- `approval.resolved` - request approved or denied
- `approval.trusted` - action allowed by trusted mode
- `approval.blocked` - action blocked by deny mode or unavailable broker
- `approval.allowlist_match` - action allowed by allowlist match
- `approval.token_issued` - approval token created
- `approval.allowlist_changed` - allowlist added or removed
- `approval.expired` - expired requests cleaned up
- `approval.plan_sync_failed` - skill run plan sync failure
- `pairing.requested` - new pairing request
- `pairing.blocked` - pairing blocked by policy
- `pairing.resolved` - pairing approved or denied
- `pairing.exchanged` - code exchange complete
- `device.revoked` - device revoked
- `device.rotated` - device token rotated
- `exec.start`, `exec.complete`, `exec.fail` - exec lifecycle
- `exec.blocked` - exec blocked
- `skill_exec.blocked`, `skill_exec.fail`, `skill_exec.complete` - skill exec lifecycle

Source: `internal/approval/audit.go:51-75` (broker audit method) and various call sites in approval files

## Audit events from the auth service

The auth service generates:
- `auth.passkey.registration.begin/finish/failed`
- `auth.passkey.login.begin/finish/failed`
- `auth.stepup.begin/finish/failed`
- `auth.session.required/expired/revoked`
- `auth.passkey.revoked`

Source: `internal/auth/service.go:691-696` (auditEvent) and various call sites

## Audit status in control plane

The control plane exposes audit status via `GetAuditStatus`:
- Whether audit is enabled, strict, and verify-on-start
- Total event count
- Latest event summary (ID, type, actor, timestamp)

And `VerifyAudit` triggers a chain verification and returns the verified event count.

Source: `internal/controlplane/controlplane.go:482-531`
