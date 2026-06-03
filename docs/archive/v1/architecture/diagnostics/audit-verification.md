# Audit Verification

Audit verification checks the integrity of the tamper-evident audit chain.

## Verifying the audit chain

`VerifyAudit()` calls `audit.Verify()` which validates the entire HMAC chain of audit records. If any record has been inserted, deleted, or modified, verification fails.

The result includes:
- `Verified` - whether the chain is intact
- `EventCount` - total number of audit events

Source: `internal/controlplane/controlplane.go:518-531` (VerifyAudit)

## Audit status

`GetAuditStatus()` returns the current audit configuration and statistics:

- `Enabled`, `Strict`, `VerifyOnStart` - from config
- `Available` - whether the audit logger is operational (DB + key)
- `Status` - "ok", "disabled", or "unavailable"
- `EventCount` - total events recorded
- `LastEventID`, `LastEventType`, `LastActor`, `LastEventAt` - most recent event summary

Source: `internal/controlplane/controlplane.go:482-516` (GetAuditStatus)

## Availability check

The audit logger is considered available when:
- The audit logger is not nil
- It has a database connection
- It has a non-empty key

If the audit logger is unavailable but audit is configured as enabled, the status reports "unavailable".

Source: `internal/controlplane/controlplane.go:597-608` (auditLogger)

## Chain verification internals

The actual chain verification is performed by `DB.VerifyAuditChain` which walks the audit events table and verifies each record's HMAC links to its predecessor.

Source: `internal/security/store.go:174-179` (AuditLogger.Verify)

## Strict mode

When `Strict=true`, audit write failures cause the operation that triggered the audit to fail (fail closed). When strict is off, audit failures are silently logged and the operation continues.

Source: `internal/security/store.go:158-171` (Record)
