# Storage & Data Flow Secure Connections Audit

## Summary

This audit covers the secure connections DB store (`internal/db/secure_connection_store.go`), its schema definitions (`internal/db/db.go`), the HTTP service layer (`cmd/or3-intern/service_secure_connections.go`), the `secureconn` service/session/authorization layer, and the planning/design docs. The codebase uses parameterized SQL throughout (no SQL injection). Schema indexes are generally well-designed. However, there are several critical logic flaws in the pairing approval flow, missing cleanup/TTL for accumulated records, a race condition in CAS operations, and multiple issues already catalogued in `dumb-issues.md` that remain open.

## Findings

### Critical

**C1. Device enrollment persists even when CAS fails in `ApproveEnrollmentFromPairing`**
- File: `internal/secureconn/service.go:235-244`
- `ApproveEnrollmentFromPairing` calls `s.ApproveEnrollment(...)` (which upserts the device row) *before* calling `CompareAndSwapSecureConnectionPairingStatus`. If the CAS fails (e.g. a concurrent request already consumed the pairing session), the device record has already been written. The pairing session status is left unchanged, but the device is now enrolled without a valid pairing ceremony. This breaks the single-use pairing guarantee and allows duplicate enrollment from concurrent requests.
- Fix: Move the device enrollment *after* the CAS succeeds, or wrap both operations in a transaction. The CAS must succeed before any durable device state is written.

**C2. Pairing secret leaked in HTTP response from `handleSecureConnectionPairingIntent`**
- File: `cmd/or3-intern/service_secure_connections.go:250-255`
- The response includes `result.Payload` which contains the raw `PairingSecret` field. This exposes the high-entropy QR secret to renderer code, devtools, logs, crash telemetry, and any JS that calls the endpoint. The secret is only meant to exist in the QR image.
- Fix: Return only `encoded` (the QR string), `rendezvous_id`, and `expires_at`. Omit `payload` from the response, or return a redacted copy with `pairingSecret` stripped.

**C3. `handleSecureConnectionPairingExchange` does not verify the pairing secret against the relay rendezvous commitment**
- File: `cmd/or3-intern/service_secure_connections.go:362-398`
- The exchange handler reads the pairing session directly from `secure_connection_pairing_sessions`, verifies the secret commitment locally, and CAS-advances the status. But this means the relay rendezvous state (in `relay_rendezvous`) is never consumed or verified. A caller can skip the relay rendezvous entirely and directly hit the pairing session table. The relay rendezvous single-use enforcement is decorative.
- Fix: The exchange path should verify the relay rendezvous is in a consumed/joined state before allowing pairing session consumption, or the pairing session should only be reachable through the relay rendezvous state machine.

### High

**H1. Race window in `CompareAndSwapSecureConnectionPairingStatus`**
- File: `internal/db/secure_connection_store.go:227-242`
- The CAS pattern reads the current status in the handler, then issues `UPDATE ... WHERE status=?`. Between the read and the write, another request can transition the status. The CAS itself is correct (returns `rows==1`), but callers read the status separately and pass it in, creating a TOCTOU window. Two concurrent requests can both read `status=created`, both attempt CAS with `fromStatus=created`, and only one succeeds—but the caller of the failed one may have already performed side effects (see C1).
- Fix: Ensure all callers of CAS perform no side effects before the CAS succeeds. The CAS itself is safe; the problem is caller ordering.

**H2. No cleanup/TTL for pairing sessions, revoked devices, or expired sessions**
- File: `internal/db/secure_connection_store.go` (entire file)
- `ExpireSecureConnectionSessions` only changes status to `expired` but never deletes rows. There is no `DeleteExpiredPairingSessions` or `PurgeOldRevokedDevices` function. Over time, the database accumulates:
  - Expired pairing sessions with secret commitments
  - Revoked device records with public keys and enrollment certificates
  - Expired/revoked session records with route metadata
- Fix: Add a periodic cleanup that deletes pairing sessions past expiry+grace, deletes revoked devices older than a retention window, and purges expired sessions. The secret commitments in particular should not persist indefinitely.

**H3. `ValidateFrame` accepts nil replay window, weakening replay protection**
- File: `internal/secureconn/session.go:240-265`
- When `window` is nil, the frame is decoded and the DB sequence is updated, but no in-memory replay window tracking occurs. After a host restart, the in-memory window is lost, and only the high-water mark (`last_sequence_in`) protects against replay. Frames with sequence numbers below the high-water mark are rejected, but out-of-order frames within a gap window can be replayed.
- Fix: Either make the replay window mandatory or persist enough window state (e.g. a bitmap) to survive restarts. At minimum, document the degraded replay protection when window is nil.

**H4. `UpsertSecureConnectionDevice` can overwrite `host_id` on conflict**
- File: `internal/db/secure_connection_store.go:105-108`
- The `ON CONFLICT(device_id) DO UPDATE` clause updates all fields except `device_id` and `created_at`, including `host_id`. If an attacker can call `UpsertSecureConnectionDevice` with an existing `device_id` but a different `host_id`, they can reassign a device to their host. The `ApproveEnrollment` path always sets `host_id` from the current identity, but the raw DB method has no guard.
- Fix: Either remove `host_id` from the UPDATE SET clause (it should be immutable after creation) or add a check that the existing `host_id` matches the new one.

**H5. Step-up timestamp is client-supplied and unauthenticated**
- File: `cmd/or3-intern/service_secure_connections.go:441-444`, `internal/secureconn/authorization.go:164-171`
- The session-start handler reads `identity.StepUpAt` from the auth context, and the step-up endpoint accepts a `verified_at` timestamp from the request body. `ValidateStepUpUpdate` only checks the timestamp is not zero and not too far in the future. There is no WebAuthn ceremony, no challenge binding, and no server-side verification. Any caller that can reach the endpoint can mint fresh step-up.
- Fix: Remove client-supplied step-up timestamps. Implement a begin/finish WebAuthn challenge flow bound to `session_id + device_id + route_id`. The server should only mint `StepUpAt` after verifying a valid credential assertion.

### Medium

**M1. `FindSecureConnectionDeviceByNoiseKey` index is suboptimal**
- File: `internal/db/db.go:413`, `internal/db/secure_connection_store.go:120-121`
- The index `secure_connection_devices_noise_key` covers only `device_noise_public_key`, but the query filters on `host_id=? AND device_noise_public_key=?`. Without `host_id` in the index, SQLite must scan all rows matching the noise key and then filter by host. This also means noise keys are not unique per host at the DB level.
- Fix: Replace with a composite index `ON secure_connection_devices(host_id, device_noise_public_key)` or add a unique constraint if noise keys should be host-scoped unique.

**M2. `mustJSONMap` silently swallows marshaling errors**
- File: `internal/db/approval_store.go:513-518`
- `mustJSONMap` discards the error from `json.Marshal`. If the metadata map contains unmarshalable values (e.g. `chan`, `func`, `math.Inf`), the stored JSON will be empty `{}` silently. This could mask bugs in metadata construction.
- Fix: At minimum, log the error. Better: return the error and let callers decide.

**M3. Foreign key from sessions to devices uses default FK action (RESTRICT)**
- File: `internal/db/db.go:428`
- `FOREIGN KEY(device_id) REFERENCES secure_connection_devices(device_id)` uses the default `ON DELETE` action (RESTRICT), which prevents deleting a device row if sessions reference it. Since `RevokeSecureConnectionDevice` sets status to `revoked` rather than deleting, this is not currently a problem, but if a hard-delete path is added later, it will fail silently or with confusing errors.
- Fix: Add `ON DELETE CASCADE` or `ON DELETE SET NULL` explicitly, or document that device rows are never hard-deleted.

**M4. No foreign key from pairing sessions to host identity**
- File: `internal/db/db.go:432-446`
- `secure_connection_pairing_sessions` references `host_id` but has no FK constraint to `secure_connection_host_identity`. Orphaned pairing sessions can exist if host identity is deleted or never created.
- Fix: Add FK constraint or document that host identity cleanup must also clean pairing sessions.

**M5. `CompareAndSwapSecureConnectionPairingStatus` uses `fmt.Sprintf` for column name**
- File: `internal/db/secure_connection_store.go:228-233`
- The `field` variable is derived from `toStatus` (hardcoded strings: `joined_at`, `consumed_at`). While not SQL-injectable today (the values are literals, not user input), using `fmt.Sprintf` for SQL column names is a fragile pattern. A future refactor that passes user-controlled values into `field` would create SQL injection.
- Fix: Validate `field` against an allowlist before interpolation, or use separate queries per transition.

**M6. `UpdateSecureConnectionSessionStepUp` does not check session status or expiry**
- File: `internal/db/secure_connection_store.go:192-195`
- The UPDATE sets `step_up_at` on any session matching the ID, regardless of status (`expired`, `revoked`) or expiry. This means step-up can be stamped on dead sessions.
- Fix: Add `AND status='active' AND (expires_at=0 OR expires_at>?)` to the WHERE clause.

**M7. No cleanup mechanism for expired pairing session data**
- File: `internal/db/secure_connection_store.go:213-220`
- Pairing sessions store `secret_commitment`, `account_id`, `relay_origin`, and metadata. These persist indefinitely even after `consumed` or `expired` status. The secret commitment is a hash, not reversible, but the other fields are metadata that should not outlive the pairing ceremony.
- Fix: Add a purge function that deletes pairing sessions in terminal states older than a configurable retention period (e.g. 24 hours).

### Low

**L1. Tests do not cover the secure connection store beyond one smoke test**
- File: `internal/db/secure_connection_store_test.go:127-159`
- Only one test (`TestSecureConnectionDeviceLookupByNoiseKey`) exercises the secure connection store. There are no tests for session creation/expiry, pairing session CAS, revocation cascade, sequence tracking, host identity upsert, or concurrent access patterns.
- Fix: Add tests for: session lifecycle (create, touch, expire), pairing CAS race conditions, revocation cascading to sessions, sequence monotonicity enforcement, and host identity replacement detection.

**L2. `RevokeSecureConnectionDevice` does not verify the device belongs to the current host**
- File: `internal/db/secure_connection_store.go:165-168`
- The raw DB method revokes any device by ID. The `TrustStore.RevokeDevice` wrapper in `service.go` does not check host ownership either (unlike `GetDevice` which does). A caller with DB access could revoke devices belonging to other hosts.
- Fix: Add `AND host_id=?` to the revoke query, or have the service layer verify ownership before calling the DB method.

**L3. `ListSecureConnectionDevices` can return devices from any host if `hostID` is empty**
- File: `internal/db/secure_connection_store.go:129-163`
- If `hostID` is empty after trimming, no `host_id` filter is applied. The `TrustStore.ListDevices` wrapper always passes `s.Identity.HostID`, but the raw DB method is unprotected.
- Fix: Either require `hostID` in the raw DB method or document that it is the caller's responsibility.

**L4. Timestamps use mixed types (int64 milliseconds) without timezone documentation**
- File: `internal/db/secure_connection_store.go` (all record structs)
- All timestamps are `int64` Unix milliseconds. The schema stores them as `INTEGER`. There is no documentation that these are UTC milliseconds. Callers passing local time or seconds would produce silent data corruption.
- Fix: Add field comments (e.g. `// CreatedAt is UTC milliseconds since epoch`) or use a named type.

**L5. `mustJSONStringSlice` returns `"[]"` on marshal error**
- File: `internal/db/secure_connection_store.go:292-301`
- If `json.Marshal` fails, the function returns `"[]"` silently. This is unlikely for `[]string` but masks errors in the same way as `mustJSONMap`.
- Fix: Log the error.

**L6. `CreateSecureConnectionSession` does not check for duplicate session IDs**
- File: `internal/db/secure_connection_store.go:170-180`
- If `RandomBase64URL` produces a collision (astronomically unlikely but not impossible), the INSERT will fail with a UNIQUE constraint violation. The error is returned, but there is no retry logic.
- Fix: Document that session ID collision causes a retryable error, or add a retry loop.

## Task List

- [ ] **C1**: Reorder `ApproveEnrollmentFromPairing` to CAS first, then enroll device (or use a transaction)
- [ ] **C2**: Strip `pairingSecret` from `handleSecureConnectionPairingIntent` HTTP response
- [ ] **C3**: Wire pairing exchange through relay rendezvous state machine, not just pairing session table
- [ ] **H1**: Ensure callers of CAS perform zero side effects before CAS succeeds
- [ ] **H2**: Add periodic cleanup for expired pairing sessions, revoked devices, and expired sessions
- [ ] **H3**: Make replay window mandatory or persist window state to survive restarts
- [ ] **H4**: Remove `host_id` from `ON CONFLICT` UPDATE clause in `UpsertSecureConnectionDevice`
- [ ] **H5**: Replace client-supplied step-up timestamps with server-verified WebAuthn challenge flow
- [ ] **M1**: Add composite index `(host_id, device_noise_public_key)` on `secure_connection_devices`
- [ ] **M2**: Handle or log marshaling errors in `mustJSONMap`
- [ ] **M3**: Add explicit `ON DELETE` action to FK constraint on `secure_connection_sessions`
- [ ] **M4**: Add FK constraint from pairing sessions to host identity, or document cleanup responsibility
- [ ] **M5**: Validate `field` against allowlist in `CompareAndSwapSecureConnectionPairingStatus`
- [ ] **M6**: Add status/expiry check to `UpdateSecureConnectionSessionStepUp` WHERE clause
- [ ] **M7**: Add purge function for pairing sessions in terminal states past retention period
- [ ] **L1**: Add comprehensive tests for secure connection store (session lifecycle, CAS races, revocation cascade, sequence tracking)
- [ ] **L2**: Add host ownership check to `RevokeSecureConnectionDevice`
- [ ] **L3**: Require `hostID` in `ListSecureConnectionDevices` or guard the empty case
- [ ] **L4**: Document timestamp semantics (UTC milliseconds) on record struct fields
- [ ] **L5**: Log marshal errors in `mustJSONStringSlice`
- [ ] **L6**: Document or handle session ID collision in `CreateSecureConnectionSession`
