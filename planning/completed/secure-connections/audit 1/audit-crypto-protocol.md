# Crypto & Protocol Secure Connections Audit

**Date:** 2026-05-18
**Scope:** `internal/secureconn/` package — Noise protocol, session management, certificates, frame encryption, authorization, privacy, identity, trust store.
**Reference:** protocol-spec.md, crypto-library-decisions.md, threat-model-hostile-relay.md

## Summary

The implementation establishes a solid foundation: deterministic CBOR encoding, Ed25519 enrollment certificates, X25519 key agreement, XChaCha20-Poly1305 AEAD, HMAC-based commitments, replay windows, and layered authorization. However, the Noise handshake is a custom construction rather than a vetted Noise IK pattern, session claims lack cryptographic binding, and several TOCTOU and validation gaps exist. The threat model correctly identifies hostile relay as in-scope, but the current code does not fully deliver on the "derive pairing secret from database" or "decrypt command/result frames" blocking claims without a proper Noise state machine.

## Findings

### Critical

#### C1. Custom Noise handshake — not a real Noise IK pattern
**File:** `noise.go:32-83`
**Description:** `HostAcceptNoiseIK` computes raw `X25519(ss)` and `X25519(es)`, hashes them into a transcript, and derives a session key via HMAC+HKDF. This is a custom protocol, not the Noise IK pattern. Noise IK requires: (1) initiator sends ephemeral, (2) responder replies, (3) specific `MixKey`/`MixHash` chaining operations, (4) encrypted static key transmission, (5) mutual authentication via transport payloads. The current code has no mutual authentication — the host computes the shared secret but the device never proves knowledge of its static private key. A passive observer who compromises the relay and obtains the host's static key could impersonate any device.
**Suggested fix:** Integrate a vetted Noise library (e.g., `flynn/noise` for Go) implementing the full IK pattern with proper chaining key evolution, encrypted statics, and payload authentication.

#### C2. Session claims not cryptographically bound to session key
**File:** `session.go:199-238`
**Description:** `LoadActiveSessionClaims` reconstructs claims entirely from database records. If the relay database is compromised (in-scope per threat model), an attacker can modify session records to change `Role`, `Capabilities`, `TrustLevel`, or `AccountID` for an active session. The session key derived from the Noise handshake is never used to verify claim integrity.
**Suggested fix:** Include a MAC (e.g., `HMAC-SHA-256(sessionKey, canonical_cbor(claims))`) in the session record or require claims to be presented as a signed token that the host verifies against the session key.

#### C3. TOCTOU in ApproveEnrollmentFromPairing — orphaned certs on CAS failure
**File:** `service.go:202-247`
**Description:** `ApproveEnrollmentFromPairing` creates the enrollment certificate and device record (lines 235-238) *before* the compare-and-swap on pairing session status (line 239). If two concurrent requests race, the first creates the cert/device and succeeds the CAS; the second creates a *duplicate* cert/device but fails the CAS. The duplicate device record persists in the database.
**Suggested fix:** Perform the CAS first (transition pairing session to a "processing" state), then create the cert/device. If cert creation fails, roll back the pairing status. Alternatively, make device upsert idempotent on `(deviceID, enrollmentEpoch)`.

### High

#### H1. No mutual authentication in handshake
**File:** `noise.go:65-72`
**Description:** The host computes `es = X25519(hostStaticPriv, deviceEphemeralPub)` and `ss = X25519(hostStaticPriv, deviceStaticPub)`, but the device never proves it holds the corresponding private key for `deviceStaticPub`. An attacker who obtains the host's static private key (e.g., from compromised secret store) can complete the handshake without knowing the device's private key.
**Suggested fix:** Add a device authentication payload encrypted under the handshake key that proves possession of the device static private key (standard in Noise IK pattern).

#### H2. Frame validation has no cryptographic binding to session
**File:** `frame.go:52-76`, `session.go:240-265`
**Description:** `ValidateSecureFrame` checks structural fields (version, kind, session ID, timestamps, replay) but does not verify any MAC or signature. `DecodeSecureFrame` decodes CBOR without cryptographic verification. The actual encryption/decryption via `SealNoiseTransport`/`OpenNoiseTransport` is separate. If a caller uses `DecodeSecureFrame` without also calling `OpenNoiseTransport`, frames are accepted without authentication.
**Suggested fix:** Either combine frame decoding and decryption into a single atomic operation, or add a mandatory `ValidateCryptoBinding` step that verifies the frame body's AEAD tag before any field validation.

#### H3. Account binding is not cryptographic
**File:** `service.go:304-326`
**Description:** `validateAccountBinding` checks if `proposal.AccountBinding["accountId"]` matches the expected account ID as a plain string comparison. There is no cryptographic proof (e.g., OAuth token, signed JWT, or passkey assertion) that the device actually owns the claimed account. Any device that knows the account ID can forge the binding.
**Suggested fix:** Require a signed token or passkey assertion from the account provider, verified against a pinned issuer key.

#### H4. Private keys serialized as JSON in secret store
**File:** `identity.go:57-63`
**Description:** `json.Marshal(identity)` serializes the full `HostIdentity` including `HostSigningPrivateKey` and `HostNoisePrivateKey` as base64url strings in JSON. If the secret store is compromised or logged, all private keys are exposed in a single blob.
**Suggested fix:** Store private keys separately from public identity metadata. Consider encrypting private keys with a passphrase or hardware-backed key before storage.

#### H5. Timestamp skew window allows 24-hour future frames
**File:** `frame.go:69`
**Description:** `skew > 24*60*60*1000` allows frames with `SentAtUnixMs` up to 24 hours in the future. An attacker with relay access could inject pre-generated frames with future timestamps that will remain valid for up to a day.
**Suggested fix:** Reduce the future tolerance to 30-60 seconds (enough for clock skew). The 24-hour upper bound should only apply to past frames.

#### H6. Signing bytes functions mutate their input
**File:** `certificate.go:101`, `certificate.go:179`
**Description:** `enrollmentSigningBytes` sets `cert.Signature = ""` on the passed struct (not a copy). Similarly, `EnrollmentProposalSigningBytes` sets `proposal.Signature = ""`. If the caller retains a reference to the original cert/proposal, its signature is silently zeroed, which could cause subsequent verification to fail or produce incorrect hashes.
**Suggested fix:** Copy the struct before mutating, or accept the struct by value (not pointer) and create a local copy.

### Medium

#### M1. Ephemeral public key not validated for small-subgroup attacks
**File:** `noise.go:58-61`
**Description:** The device ephemeral public key is decoded and length-checked but not validated as a valid X25519 point. While X25519 is designed to be safe against small-subgroup attacks (it clamps scalars and the base point has prime order), the code should still reject all-zeros public keys.
**Suggested fix:** Add a check: `if len(deviceEphemeralPub) == 32 && allZeros(deviceEphemeralPub) { return error }`.

#### M2. Path-based action classification can be bypassed
**File:** `authorization.go:56-83`
**Description:** `ClassifyAction` uses `strings.Contains` on URL paths and tool names. An attacker could craft a path like `/api/v1/data` that doesn't match any pattern, defaulting to `ActionView` with `CapabilityChat`. Combined with a GET request, this bypasses step-up requirements for what might be a sensitive operation.
**Suggested fix:** Use an allowlist-based router or explicit path-to-action mapping. Default to a restrictive class (e.g., `ActionSecurityConfig`) when no pattern matches, rather than `ActionView`.

#### M3. NormalizeRole defaults empty string to Operator
**File:** `certificate.go:226-227`
**Description:** `NormalizeRole("")` returns `RoleOperator`. This means any code path that passes an empty role string (e.g., missing field in CBOR) silently grants operator privileges.
**Suggested fix:** Return `""` for empty input and force callers to explicitly set a role. Or change the default to `RoleViewer`.

#### M4. Replay window capacity inconsistency
**File:** `frame.go:18-21` vs `session.go:18`
**Description:** `NewReplayWindow` defaults to capacity 128, but `DefaultReplayWindowCap` is 512. These should be consistent. If callers use `NewReplayWindow(0)`, they get 128 instead of 512.
**Suggested fix:** Use `DefaultReplayWindowCap` as the default in `NewReplayWindow`.

#### M5. Error wrapping may leak internal details
**File:** `session.go:94`, `session.go:105`, `session.go:113`, `service.go:216`
**Description:** Several errors use `fmt.Errorf("...: %w", err)` which wraps internal errors. If these propagate to clients, they could expose database schema details, internal paths, or crypto library error messages.
**Suggested fix:** Use `SecureConnectionError` with safe messages for all client-facing errors. Log internal details server-side only.

#### M6. UpdateDeviceTrust permanently strips metadata
**File:** `service.go:300`
**Description:** `rec.Metadata = RedactSecureConnectionLogValue(rec.Metadata).(map[string]any)` permanently overwrites the device record's metadata with redacted values before upsert. This destroys non-sensitive metadata (e.g., `"source": "secure-connections-v2"`) on every trust update.
**Suggested fix:** Only redact metadata in log/telemetry output, not in the persisted record.

#### M7. No host identity key rotation mechanism
**File:** `identity.go`, `service.go`
**Description:** There is no code to rotate host signing or noise keys. If a host key is compromised, there is no way to transparently re-key without breaking all enrolled devices.
**Suggested fix:** Implement a key rotation flow that re-signs enrollment certificates with the new key and updates the pinned host identity, requiring device re-approval.

#### M8. Web enrollment allows `files` capability but not `tools`
**File:** `privacy.go:54-61`
**Description:** `DefaultWebEnrollmentPolicy` allows `chat` and `files` for web, but `files` includes write access (line 78-80 in authorization.go maps non-GET methods to `CapabilityFiles`). A web-limited device with step-up can write arbitrary files.
**Suggested fix:** Consider splitting `files` into `files-read` and `files-write` for web enrollment, or require step-up for every file write operation (currently only required once per 2-minute window).

### Low

#### L1. HashBase64URL uses non-standard null-byte separator
**File:** `codec.go:71-78`
**Description:** `HashBase64URL` inserts a `0x00` byte between parts. While this prevents length-extension-like collisions between parts, it's not a standard domain-separation construction. If a part itself contains null bytes, ambiguity could arise.
**Suggested fix:** Use length-prefixed parts or a standard domain separation scheme (e.g., include part lengths in the hash).

#### L2. Session key material never zeroed from memory
**File:** `noise.go:74-82`, `session.go:163-178`
**Description:** The session key (`[]byte`) returned from `deriveNoiseSessionKey` and stored in `handshakeResult.SessionKey` is never explicitly zeroed after use. In Go, the garbage collector will eventually reclaim it, but the key may persist in memory longer than necessary.
**Suggested fix:** Add a `Close()` or `Destroy()` method that zeros key material. Use `defer` to ensure cleanup.

#### L3. Sensitive log key detection is overly broad
**File:** `privacy.go:129-137`
**Description:** `IsSensitiveLogKey` uses substring matching. Keys like `"certificate_chain_length"` or `"private_api_endpoint"` would be redacted even though they don't contain secrets. This is fail-safe (over-redaction) but could hinder debugging.
**Suggested fix:** Use exact key matching or a more specific list. Acceptable as-is since over-redaction is the safe direction.

#### L4. CBOR decoder limits not documented as security-relevant
**File:** `codec.go:24-34`
**Description:** `MaxArrayElements: 4096` and `MaxMapPairs: 4096` are anti-DoS limits, but this isn't documented. If these are increased without understanding the security implications, memory exhaustion attacks become possible.
**Suggested fix:** Add comments documenting these as security limits.

#### L5. ValidateStepUpUpdate accepts future timestamps
**File:** `authorization.go:168`
**Description:** `stepUpAt.After(now.Add(30*time.Second))` allows step-up timestamps up to 30 seconds in the future. A malicious client could set a step-up timestamp slightly ahead to extend their step-up window.
**Suggested fix:** Reject future step-up timestamps entirely, or reduce tolerance to 5 seconds.

#### L6. CapabilityDiscovery leaks feature flags to unauthenticated callers
**File:** `privacy.go:41-52`
**Description:** `CurrentCapabilityDiscovery()` returns protocol version, supported features, and capability list without any authentication. An attacker can use this to determine which attacks are worth attempting.
**Suggested fix:** Gate capability discovery behind at least a pairing session or rate limit.

#### L7. No test for Noise handshake with wrong device static key
**File:** `secureconn_test.go`
**Description:** Tests verify happy-path Noise handshake and transport, but there is no test that verifies the handshake fails when the device uses a different static key than what's enrolled.
**Suggested fix:** Add a test that attempts `HostAcceptNoiseIK` with a mismatched `DeviceNoisePublicKey` and asserts failure.

#### L8. No test for concurrent session validation (TOCTOU)
**File:** `secureconn_test.go`
**Description:** No test exercises concurrent frame validation on the same session to verify that the sequence tracking and replay window work correctly under race conditions.
**Suggested fix:** Add a test with goroutines submitting frames concurrently.

## Task List

- [ ] **C1:** Replace custom Noise handshake with vetted Noise IK library (e.g., `flynn/noise`)
- [ ] **C2:** Add cryptographic binding between session key and session claims (HMAC or signed token)
- [ ] **C3:** Fix TOCTOU in `ApproveEnrollmentFromPairing` — CAS before cert creation
- [ ] **H1:** Add device authentication payload to handshake proving static key possession
- [ ] **H2:** Combine frame decode and decryption into single atomic operation, or add mandatory crypto verification step
- [ ] **H3:** Replace string-based account binding with cryptographic proof (signed token/passkey assertion)
- [ ] **H4:** Store private keys separately from public identity; consider per-key encryption
- [ ] **H5:** Reduce future timestamp tolerance in frame validation from 24h to 30-60s
- [ ] **H6:** Fix signing bytes functions to not mutate input (copy struct first)
- [ ] **M1:** Add all-zeros check for ephemeral public keys
- [ ] **M2:** Use allowlist-based action classification; default to restrictive class
- [ ] **M3:** Change `NormalizeRole("")` to return `""` instead of `RoleOperator`
- [ ] **M4:** Unify replay window capacity to use `DefaultReplayWindowCap` (512)
- [ ] **M5:** Replace `fmt.Errorf %w` wrapping with `SecureConnectionError` for client-facing errors
- [ ] **M6:** Don't permanently redact metadata in `UpdateDeviceTrust` persisted records
- [ ] **M7:** Implement host identity key rotation flow
- [ ] **M8:** Split `files` capability for web enrollment (read vs write)
- [ ] **L1:** Use length-prefixed parts in `HashBase64URL` instead of null-byte separator
- [ ] **L2:** Add key material zeroing after session use
- [ ] **L3:** Refine sensitive log key detection (optional — current over-redaction is safe)
- [ ] **L4:** Document CBOR decoder limits as security-relevant
- [ ] **L5:** Reject future step-up timestamps or reduce tolerance to 5s
- [ ] **L6:** Gate capability discovery behind rate limiting or pairing session
- [ ] **L7:** Add test for handshake with wrong device static key
- [ ] **L8:** Add concurrent frame validation test
