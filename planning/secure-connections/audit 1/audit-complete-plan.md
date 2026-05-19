# Secure Connections Audit Complete Plan

This is the implementation plan for the findings in:

- [Backend audit](audit-backend.md)
- [Frontend audit](audit-frontend.md)
- [Crypto/protocol audit](audit-crypto-protocol.md)
- [Storage/dataflow audit](audit-storage-dataflow.md)

Do not implement every original finding literally. Some findings overlap, some are already fixed, and a few recommended fixes would make OR3 Intern or OR3 App harder to use without materially improving the current threat model. Implement the tasks below in severity order.

## P0 - Critical

### 1. Replace the custom runtime handshake with a real authenticated Noise flow

Source findings: [audit-crypto-protocol.md C1/H1/H2](audit-crypto-protocol.md)

Files:

- [noise.go](../../../internal/secureconn/noise.go)
- [session.go](../../../internal/secureconn/session.go)
- [frame.go](../../../internal/secureconn/frame.go)
- [secure-connections.ts](../../../../or3-app/app/utils/or3/secure-connections.ts)
- [protocol-spec.md](../protocol-spec.md)
- [crypto-library-decisions.md](../crypto-library-decisions.md)
- [threat-model-hostile-relay.md](../threat-model-hostile-relay.md)

Plan:

1. Put the current `HostAcceptNoiseIK` / `buildMobileNoiseHandshake` code behind a small handshake interface so tests can exercise old and new paths during migration.
2. Integrate a vetted Noise IK implementation for Go and a compatible TypeScript implementation for OR3 App. The audit's "use a vetted Noise library" recommendation is directionally correct, but incomplete unless both sides are implemented together.
3. Make device static-key proof part of the handshake, not just an input field checked against the enrolled device record.
4. Make frame open/decode/validate one atomic API. Callers should not be able to decode a `SecureFrameV1` as valid until its AEAD tag has been verified with the negotiated session key.
5. Add compatibility tests using shared vectors from [test-vectors.json](../test-vectors.json), plus failure tests for wrong device static key, wrong enrollment hash, wrong route/prologue, replay, and tampered ciphertext.

Acceptance criteria:

- A relay/database attacker cannot produce authenticated runtime frames without enrolled device key material.
- No production handler validates decoded frame metadata before cryptographic open succeeds.
- Existing enrolled devices either migrate cleanly or receive a clear re-pair requirement.

### 2. Bind session authorization claims to host-held integrity, not mutable DB rows alone

Source findings: [audit-crypto-protocol.md C2](audit-crypto-protocol.md), [audit-storage-dataflow.md H3](audit-storage-dataflow.md)

Files:

- [session.go](../../../internal/secureconn/session.go)
- [secure_connection_store.go](../../../internal/db/secure_connection_store.go)
- [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go)

Plan:

1. Add an integrity token/MAC for `SessionClaims` using host-held key material outside the ordinary session DB row. Do not store a MAC beside the mutable fields if the same database compromise can rewrite both without needing host-held material.
2. Prefer a host-signed or host-HMACed canonical claim blob stored with the session and verified in `LoadActiveSessionClaims`.
3. Include device ID, host ID, enrollment epoch, role, capabilities, trust level, account ID, route ID, issued time, and expiry in the protected claim payload.
4. Keep DB expiry/status checks, but treat claim-integrity failure as a hard session failure.

Acceptance criteria:

- Modifying role, capabilities, trust level, account ID, route ID, or expiry in the DB does not produce accepted active claims.
- Tests cover tampered DB claims and stale enrollment epochs.

### 3. Make pairing approval and exchange single-use before durable side effects

Source findings: [audit-storage-dataflow.md C1/C3/H1](audit-storage-dataflow.md), [audit-crypto-protocol.md C3](audit-crypto-protocol.md)

Files:

- [service.go](../../../internal/secureconn/service.go)
- [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go)
- [secure_connection_store.go](../../../internal/db/secure_connection_store.go)
- [relay_store.go](../../../internal/db/relay_store.go)
- [service_secure_connections_test.go](../../../cmd/or3-intern/service_secure_connections_test.go)
- [secureconn_test.go](../../../internal/secureconn/secureconn_test.go)

Plan:

1. Fix `ApproveEnrollmentFromPairing` so the pairing session transitions to a non-reusable processing/consumed state before certificate/device writes happen, or wrap the state change and device write in one transaction.
2. If a later certificate/device write fails, leave the pairing in a terminal failed state or roll back transactionally. Do not leave an active enrolled device after a failed CAS.
3. Wire compatibility pairing exchange through the relay rendezvous state machine. The exchange path must verify and consume the matching `relay_rendezvous` record, not only the `secure_connection_pairing_sessions` row.
4. Add concurrent approval/exchange tests where two requests race and only one can produce durable device/token state.

Acceptance criteria:

- A failed CAS cannot create or update an enrolled device.
- A pairing exchange cannot skip relay rendezvous consumption.
- Concurrent pair/approve attempts are deterministic and single-use.

### 4. Stop browser fallback from storing extractable device private keys in localStorage

Source findings: [audit-frontend.md C1](audit-frontend.md)

Files:

- [secure-connections.ts](../../../../or3-app/app/utils/or3/secure-connections.ts)
- [nativeSecureStorage.ts](../../../../or3-app/app/utils/auth/nativeSecureStorage.ts)
- [secure-connections.test.ts](../../../../or3-app/tests/unit/secure-connections.test.ts)

Plan:

1. Replace plaintext JWK private-key storage in `or3-app:v1:secure-connections` with non-extractable or wrapped key storage.
2. Preferred non-UX-breaking path: use IndexedDB-stored non-extractable `CryptoKey` objects for browser fallback where supported. Store public metadata in localStorage only if needed for bootstrapping.
3. If IndexedDB/non-extractable storage is unavailable, keep the device `web-limited`, show a clear re-pair/unavailable state, and avoid silently persisting raw private JWKs.
4. Add migration that detects old plaintext private JWK records, creates safer storage, and removes private key material from localStorage after successful migration.

Acceptance criteria:

- `localStorage.getItem('or3-app:v1:secure-connections')` no longer contains Ed25519 or X25519 private JWK material.
- Browser pairing still works without adding a mandatory password prompt on every app launch.

### 5. Authenticate relay WebSocket identities against the current host/device/session

Source findings: [audit-backend.md H1/H2](audit-backend.md)

Files:

- [service_secure_relay.go](../../../cmd/or3-intern/service_secure_relay.go)
- [service_secure_relay_test.go](../../../cmd/or3-intern/service_secure_relay_test.go)
- [secure-connections-api.md](../../../docs/v1/architecture/security/secure-connections/secure-connections-api.md)

Plan:

1. Remove the host-side `host_id_hash` query override unless it exactly matches `HashBase64URL(store.Identity.HostID)`.
2. For device WebSockets, require a short-lived route/session binding or proof that the authenticated device owns the requested `device_id_hash`.
3. Reject unknown, empty, malformed, or mismatched hashes before WebSocket upgrade.
4. Add handler-level tests, not only hub unit tests.

Acceptance criteria:

- An authenticated caller cannot register as an arbitrary host or device hash.
- The relay still supports the intended OR3 App connection flow without manual hash entry.

## P1 - High

### 6. Fix immediate backend validation gaps

Source findings: [audit-backend.md M2/M3/M4](audit-backend.md)

Files:

- [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go)

Plan:

1. Add explicit request validation for pairing-intent TTL before calling `CreatePairingIntent`; use the existing service bounds as the source of truth.
2. Reject empty `id` in `handleRelayRendezvous` with `400`.
3. Cap `device_name` in pairing exchange, for example 128 UTF-8 bytes or runes after trimming.

Acceptance criteria:

- Invalid TTL, empty rendezvous ID, and overlong device name are rejected before DB calls.

### 7. Fix role, action, timestamp, and replay defaults

Source findings: [audit-crypto-protocol.md H5/M2/M3/M4/L5](audit-crypto-protocol.md)

Files:

- [authorization.go](../../../internal/secureconn/authorization.go)
- [certificate.go](../../../internal/secureconn/certificate.go)
- [frame.go](../../../internal/secureconn/frame.go)
- [session.go](../../../internal/secureconn/session.go)

Plan:

1. Change `NormalizeRole("")` to return `""`; callers that want an operator default must set it explicitly.
2. Change `ClassifyAction` so unknown paths fail closed or map to a restrictive class instead of defaulting to chat/view.
3. Reduce future frame timestamp tolerance from 5 minutes to 30-60 seconds while keeping a larger past tolerance if needed.
4. Use `DefaultReplayWindowCap` consistently in `NewReplayWindow`.
5. Reduce `ValidateStepUpUpdate` future tolerance or reject future timestamps entirely.

Acceptance criteria:

- Missing roles do not become operator.
- Unknown mutating action classification cannot bypass step-up/capability policy.
- Tests cover empty roles, unknown paths, future frames, and replay-window default capacity.

### 8. Add secure-connection cleanup and retention jobs

Source findings: [audit-backend.md M7](audit-backend.md), [audit-storage-dataflow.md H2/M7](audit-storage-dataflow.md)

Files:

- [secure_connection_store.go](../../../internal/db/secure_connection_store.go)
- [relay_store.go](../../../internal/db/relay_store.go)
- [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go)
- [service_secure_relay.go](../../../cmd/or3-intern/service_secure_relay.go)

Plan:

1. Add purge methods for terminal pairing sessions, expired/revoked sessions, old revoked devices, expired relay rendezvous records, and expired relay routes.
2. Add in-memory relay route cleanup in `secureConnectionRelayHub`.
3. Keep retention configurable; default pairing/rendezvous retention should be short, for example 24 hours after terminal state.
4. Preserve enough audit/debug information without retaining commitments and route metadata indefinitely.

Acceptance criteria:

- Long-running OR3 Intern instances do not accumulate unbounded route/session/pairing records.
- Cleanup has tests and is safe to run repeatedly.

### 9. Harden secure-connection DB methods and schema

Source findings: [audit-storage-dataflow.md M1/M5/M6/L1/L2/L3/L4/L5/L6](audit-storage-dataflow.md)

Files:

- [db.go](../../../internal/db/db.go)
- [secure_connection_store.go](../../../internal/db/secure_connection_store.go)
- [secure_connection_store_test.go](../../../internal/db/secure_connection_store_test.go)

Plan:

1. Replace `secure_connection_devices_noise_key` with a composite `(host_id, device_noise_public_key)` index; make it unique if duplicate device noise keys should never be valid per host.
2. Replace the `fmt.Sprintf` CAS column selection with explicit queries or a strict allowlist.
3. Add status and expiry guards to `UpdateSecureConnectionSessionStepUp`.
4. Make raw revoke/list methods host-scoped or clearly split into host-scoped and admin/global methods.
5. Add timestamp comments or a named UTC-milliseconds type on secure-connection records.
6. Log or return JSON marshal errors instead of silently returning `{}` / `[]`.
7. Add tests for session lifecycle, pairing CAS races, revocation cascade, monotonic sequence updates, host identity replacement, and session-ID collision handling/documentation.

Acceptance criteria:

- Raw DB helpers cannot accidentally cross host boundaries.
- Store tests cover the main lifecycle and race-sensitive paths.

### 10. Fix high-impact OR3 App state and lifecycle bugs

Source findings: [audit-frontend.md H1/H3/H4/H5/M1/M2/M6/M7](audit-frontend.md)

Files:

- [secure-connections.ts](../../../../or3-app/app/utils/or3/secure-connections.ts)
- [useSecureConnectionSession.ts](../../../../or3-app/app/composables/useSecureConnectionSession.ts)
- [useSecureConnectionLifecycle.ts](../../../../or3-app/app/composables/useSecureConnectionLifecycle.ts)
- [usePairing.ts](../../../../or3-app/app/composables/usePairing.ts)
- [nativeSecureStorage.ts](../../../../or3-app/app/utils/auth/nativeSecureStorage.ts)

Plan:

1. Fix `detectSecureConnectionStorage()` so native-secure mode returns `native-software` unless hardware-backed storage is actually detected.
2. Add a session expiry timer that clears expired claims while the app is idle.
3. Serialize or debounce pause/resume transitions and handle `rekey()` errors explicitly.
4. Replace deep-link handler `throw` with a logged/user-visible rejection path.
5. Validate server-issued `SecureSessionClaims` shape before storing claims.
6. Update the local host token cache after `rotateDevice`.
7. Log native secure-storage write/delete failures instead of silently swallowing them.

Acceptance criteria:

- Native builds are not silently downgraded to web-limited trust.
- Expired sessions disappear without waiting for the next user action.
- Token rotation does not leave the app using an invalid old token.

### 11. Reduce host identity secret blast radius

Source findings: [audit-crypto-protocol.md H4](audit-crypto-protocol.md)

Files:

- [identity.go](../../../internal/secureconn/identity.go)
- [secret-store.md](../../../docs/v1/architecture/security/secret-store.md)

Plan:

1. Store host public metadata separately from private signing/noise keys in the secret store.
2. Keep the existing encrypted `SecretManager` requirement; do not invent a parallel plaintext store.
3. Add a migration path from the current single JSON blob.
4. Avoid logging full decoded identity structs.

Acceptance criteria:

- A public identity read path never needs to deserialize private key material.
- Existing installations migrate without losing host identity.

## P2 - Medium / Low

### 12. Improve relay reliability and operator debugging

Source findings: [audit-backend.md H3/M1/L3/L4/L6](audit-backend.md)

Files:

- [service_secure_relay.go](../../../cmd/or3-intern/service_secure_relay.go)
- [service_secure_relay_test.go](../../../cmd/or3-intern/service_secure_relay_test.go)
- [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go)

Plan:

1. Check `target.closed` before sending relay messages and return a reason when forwarding fails.
2. Surface backpressure/drop feedback to the sender instead of silently dropping opaque frames.
3. Log WebSocket upgrade failures at debug level.
4. Return a filtered rendezvous response from `handleRelayRendezvous`; do not expose `SecretCommitment` unless a specific internal caller needs it.
5. Add WebSocket lifecycle and concurrent forwarding tests.

Acceptance criteria:

- Backpressure and closed-peer failures are visible to the sender/test logs.
- Rendezvous lookup returns only fields needed by clients/operators.

### 13. Tighten frontend parsing, scanner, and local persistence details

Source findings: [audit-frontend.md M3/M4/M5/L1/L2/L3/L4/L5/L6/L7](audit-frontend.md)

Files:

- [secure-connections.ts](../../../../or3-app/app/utils/or3/secure-connections.ts)
- [usePairing.ts](../../../../or3-app/app/composables/usePairing.ts)
- [secure-connections.test.ts](../../../../or3-app/tests/unit/secure-connections.test.ts)
- [pairing.test.ts](../../../../or3-app/tests/unit/pairing.test.ts)

Plan:

1. Validate V2 invite `issuedAtUnixMs` is not far in the future.
2. Improve `rejectSensitiveDeepLink` to scan hash fragments and high-risk value patterns while avoiding obvious false positives such as `pagination_token`.
3. Remove duplicate `concatBytes` / `concatByteArrays` implementation.
4. Cap client-requested session TTL before sending.
5. Add sequence overflow protection or force rekey before unsafe sequence values.
6. Improve platform detection for iPad desktop UA and `navigator.userAgentData`.
7. Add browser camera scan timeout and guard overlay cleanup.
8. Move pending pairing code from durable localStorage to sessionStorage or store only non-secret resume metadata.
9. Add tests for invite parsing, QR parsing, handshake payload construction, storage fallback, lifecycle, native storage mocks, and deep-link rejection.

Acceptance criteria:

- Parser and scanner edge cases have focused unit tests.
- Pending short-lived pairing code does not persist indefinitely in localStorage.

### 14. Improve error typing and security comments

Source findings: [audit-backend.md L1](audit-backend.md), [audit-crypto-protocol.md M5/L4/L7/L8](audit-crypto-protocol.md)

Files:

- [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go)
- [codec.go](../../../internal/secureconn/codec.go)
- [secureconn_test.go](../../../internal/secureconn/secureconn_test.go)

Plan:

1. Replace substring-based pairing error classification with typed `SecureConnectionError` values where practical.
2. Ensure client-facing errors use safe messages and internal details are logged server-side only.
3. Document CBOR decoder limits as security-relevant anti-DoS bounds.
4. Add tests for wrong device static key and concurrent frame/session validation.

Acceptance criteria:

- User-visible secure-connection errors do not depend on matching English substrings.
- Security-sensitive constants have enough comments to prevent accidental weakening.

## Not Included From The Original Audits

These findings should not become implementation tasks unless new evidence appears:

- [audit-storage-dataflow.md C2](audit-storage-dataflow.md): pairing intent leaking raw `pairingSecret` is already fixed. [service_secure_connections.go](../../../cmd/or3-intern/service_secure_connections.go) returns `qr`, `secret_commitment`, `rendezvous_id`, and `expires_at`, not `result.Payload`.
- [audit-storage-dataflow.md H4](audit-storage-dataflow.md): `UpsertSecureConnectionDevice` does not update `host_id` in the current `ON CONFLICT` clause.
- [audit-storage-dataflow.md H5](audit-storage-dataflow.md): the secure-session step-up endpoint does not accept a client-supplied `verified_at`; it reads verified step-up state from the authenticated session context.
- [audit-backend.md L2](audit-backend.md): `components()` currently initializes in-memory fields and does not return an error.
- [audit-crypto-protocol.md H6](audit-crypto-protocol.md): the signing-bytes helpers take structs by value in current code, so they do not mutate caller-owned structs.
- [audit-backend.md M5](audit-backend.md): compatibility device ID collision is cryptographically negligible once the CAS/rendezvous single-use fixes are in place.
- [audit-crypto-protocol.md L3](audit-crypto-protocol.md): broad sensitive-log redaction is acceptable because it fails closed.

## Deferred - UX-Sensitive Or Needs Product Design

Do not implement these as blunt security hardening in the first pass. They need a UX/product design that preserves OR3 App usability.

1. **Mandatory biometric prompts for every native credential read/write.**  
   Source: [audit-frontend.md H2](audit-frontend.md). Checking plugin availability and storage state belongs in P1, but prompting on every read/write would make app launch, reconnect, and background resume painful. Design a gated unlock policy first, likely prompt on sensitive actions or after idle timeout rather than every storage access.

2. **Passphrase-only browser key storage.**  
   Source: [audit-frontend.md C1](audit-frontend.md). Do not replace plaintext localStorage with a mandatory passphrase prompt unless IndexedDB non-extractable/wrapped keys are not viable. The preferred P0 approach avoids raw key exfiltration without making every browser user manage another password.

3. **Full account-binding proof with OAuth/passkey assertions in enrollment.**  
   Source: [audit-crypto-protocol.md H3](audit-crypto-protocol.md). This needs an account identity model across OR3 App and OR3 Intern. The immediate pairing controls are QR secret, host approval, signed proposal, and auth-session policy; add stronger account proof after the account UX is defined.

4. **Host identity key rotation flow.**  
   Source: [audit-crypto-protocol.md M7](audit-crypto-protocol.md), [secure-connections-key-rotation.md](../../../docs/v1/architecture/security/secure-connections/secure-connections-key-rotation.md). Rotation is important, but a rushed implementation can strand enrolled devices. Keep it as a separate recovery/migration project.

5. **Splitting `files` into read/write capabilities or requiring step-up for every web file write.**  
   Source: [audit-crypto-protocol.md M8](audit-crypto-protocol.md). This changes the app's day-to-day file workflow. Design a capability model that keeps normal file browsing/editing usable before tightening it.

6. **Gating capability discovery behind auth.**  
   Source: [audit-crypto-protocol.md L6](audit-crypto-protocol.md). Discovery is intentionally used for app bootstrap in [secure-connections-api.md](../../../docs/v1/architecture/security/secure-connections/secure-connections-api.md). Rate limiting is fine; auth-gating discovery should wait until the app has an alternate bootstrap path.

7. **Changing `HashBase64URL` separator format.**  
   Source: [audit-crypto-protocol.md L1](audit-crypto-protocol.md). Length-prefixing is cleaner, but changing hashes can break IDs/protocol compatibility. Defer until a protocol version bump.

8. **Strict key zeroing for Go session material.**  
   Source: [audit-crypto-protocol.md L2](audit-crypto-protocol.md). Useful defense-in-depth, but less urgent than removing architectural crypto gaps. Handle after the Noise/session-key design is settled.

9. **Secondary validation for `Origin: app://or3`.**  
   Source: [audit-backend.md L5](audit-backend.md). The origin header is not a local security boundary. Document the accepted risk for now; revisit with a local socket, app attestation, or signed route token design.
