# Backend Secure Connections Audit

## Summary

Audit of 6 Go source files covering the secure connections HTTP handlers, relay WebSocket hub, and database store. The implementation follows sound cryptographic patterns (constant-time comparison, HMAC commitments, opaque relay). However, several issues were found: missing input validation on WebSocket identity parameters, a TOCTOU race in the relay forwarding layer, silent message loss under backpressure, and missing TTL bounds checks. No critical secret-leaking bugs were found; the pairing secret is never stored in plaintext.

## Findings

### Critical

_No critical findings. The pairing secret handling, constant-time comparison, and commitment scheme are correctly implemented._

### High

#### H1: Relay WebSocket accepts arbitrary identity from untrusted query parameter

**File:** `service_secure_relay.go:199-205`

The device WebSocket handler takes `device_id_hash` from a query parameter and registers the connection under that identity with no verification. A malicious client can connect as any device hash and receive frames intended for that device.

```go
deviceIDHash := strings.TrimSpace(r.URL.Query().Get("device_id_hash"))
// No validation that this client actually owns this identity
s.secureRelayHub.registerDevice(deviceIDHash, peer)
```

**Impact:** Impersonation of any device on the relay; interception of opaque frames destined for other devices.

**Fix:** Validate the device ID hash against the authenticated session's device identity (from the auth context or enrollment certificate), or require a proof-of-possession exchange during WebSocket handshake.

#### H2: Relay WebSocket host identity also spoofable via query parameter

**File:** `service_secure_relay.go:192-196`

Same issue as H1 for the host side. While the default falls back to the host's own identity (line 194), the `host_id_hash` query parameter override allows any authenticated client to register as an arbitrary host hash.

```go
hostIDHash := strings.TrimSpace(r.URL.Query().Get("host_id_hash"))
if hostIDHash == "" {
    hostIDHash = secureconn.HashBase64URL([]byte(store.Identity.HostID))
}
```

**Fix:** Remove the query parameter override, or validate that the provided hash matches the authenticated host's identity.

#### H3: Relay forward has TOCTOU race — peer used after RUnlock

**File:** `service_secure_relay.go:97-129`

The `forward` method looks up the target peer under `RLock`, releases the lock at line 119, then sends to the peer's channel at line 124. Between the unlock and the send, the peer can be unregistered and its `closed` channel closed. The `send` channel write may succeed on a peer that is being torn down.

```go
h.mu.RUnlock()           // line 119
if target == nil {
    return false
}
select {
case target.send <- env:  // line 124 — peer may be closing
    return true
```

**Impact:** Message sent to a closing peer; possible write to a closed channel (though buffered channel prevents panic). The read pump may have already closed `peer.closed`, so the message may never be processed.

**Fix:** Either hold the lock during the send (acceptable for short sends), or add a check on `target.closed` with a select:
```go
select {
case <-target.closed:
    return false
case target.send <- env:
    return true
default:
    return false
}
```

### Medium

#### M1: Relay send channel silently drops messages under backpressure

**File:** `service_secure_relay.go:221, 123-128`

The peer send channel has a buffer of 32. When full, messages are silently dropped (the `default` case in `select`). There is no notification to the sender or logging. Under sustained load, opaque frames are lost without any protocol-level feedback.

```go
send: make(chan secureRelayEnvelope, 32),
```

**Impact:** Silent data loss for active relay sessions under backpressure.

**Fix:** Return a "busy" or "dropped" indicator from `forward`, and propagate a `FRAME_DROPPED` envelope to the sender so the protocol layer can retry or throttle.

#### M2: No TTL validation on pairing intent creation

**File:** `service_secure_connections.go:216-229`

The `TTLSeconds` field is an `int` with no bounds check. A caller can pass `0`, a negative value, or an extremely large value (e.g., `math.MaxInt32` = ~68 years). The `CreatePairingIntent` code may create a rendezvous that expires immediately or persists indefinitely.

```go
TTL: time.Duration(body.TTLSeconds) * time.Second,
```

**Fix:** Validate `body.TTLSeconds` is within a reasonable range (e.g., 30–3600 seconds) before passing to `CreatePairingIntent`.

#### M3: Empty `rendezvous_id` passed to DB query

**File:** `service_secure_connections.go:564`

`handleRelayRendezvous` trims the `id` query parameter but does not check for emptiness. An empty string is passed to `GetRelayRendezvous`, which executes a DB query with `WHERE rendezvous_id=''`. This is a wasted query and may match unexpected rows if empty IDs exist.

```go
id := strings.TrimSpace(r.URL.Query().Get("id"))
rec, ok, err := store.DB.GetRelayRendezvous(r.Context(), id)
```

**Fix:** Return 400 if `id` is empty after trimming.

#### M4: `device_name` in pairing exchange not length-validated

**File:** `service_secure_connections.go:395-398`

The `device_name` field is trimmed but not length-limited. A client can submit an arbitrarily long string, which is stored in the device record and used to compute the device ID hash. This wastes storage and could cause hash collisions if names are truncated differently by different code paths.

**Fix:** Cap `device_name` at a reasonable length (e.g., 128 characters).

#### M5: Pairing exchange device ID may collide for same device name

**File:** `service_secure_connections.go:408`

The compatibility-mode device ID is derived from `HashBase64URL(rendezvousID, deviceName)`. Two different pairing exchanges with the same `device_name` produce different IDs (different rendezvous IDs), but the `UpsertSecureConnectionDevice` uses `ON CONFLICT(device_id) DO UPDATE`, meaning a second pairing with the same computed ID would silently overwrite the first device's credentials.

```go
deviceID := "secure-qr:" + secureconn.HashBase64URL([]byte(rendezvousID), []byte(deviceName))
```

**Impact:** Low in practice (different rendezvous IDs produce different hashes), but the lack of uniqueness guarantee is fragile.

**Fix:** Include additional entropy (e.g., a random nonce) in the device ID computation, or use the rendezvous ID directly as a unique suffix.

#### M6: Step-up trusts identity context data without independent verification

**File:** `service_secure_connections.go:529-546`

The step-up endpoint reads `identity.StepUpAt` from the auth session context (which was populated by upstream middleware) and writes it directly to the session record. If the middleware is compromised or misconfigured, a forged `StepUpAt` timestamp grants elevated session privileges without an actual passkey verification.

**Impact:** Privilege escalation if auth context is tampered with.

**Fix:** Verify the step-up claim against the auth service's audit log or a signed assertion, not just the context value.

#### M7: Relay hub routes accumulate indefinitely — no expiry cleanup

**File:** `service_secure_relay.go:85-89`

Routes are registered in the in-memory hub but never cleaned up. Expired routes remain in the `routes` map. The `forward` method checks expiry at read time (line 100), but the map grows without bound.

```go
func (h *secureConnectionRelayHub) registerRoute(route secureRelayRoute) {
    h.routes[route.routeID] = route  // never deleted
}
```

**Impact:** Memory leak proportional to route creation rate.

**Fix:** Add a periodic cleanup goroutine or use a time-based eviction (e.g., `time.AfterFunc` to delete the route after expiry).

### Low

#### L1: `writeSecurePairingError` matches error text substrings — fragile classification

**File:** `service_secure_connections.go:282-291`

Error classification falls back to substring matching on `err.Error()` text ("expired", "consumed", "already", "not awaiting"). Changes to error message wording in dependencies will silently break the user-facing error mapping.

**Fix:** Use typed errors throughout (the `SecureConnectionError` path at line 272 is the correct pattern; extend it to cover all cases).

#### L2: `components()` called on every relay request without early return

**File:** `service_secure_relay.go:136, 209`

`s.components()` is called at the start of `handleSecureRelayRouteRequest` and `handleSecureRelayWebSocket` to lazy-initialize the hub. If initialization fails, the code continues and will panic on a nil hub. (Depends on `components()` implementation — if it panics on failure, this is fine; if it returns an error, the error is swallowed.)

**Fix:** Verify that `components()` panics or is idempotent. If it can fail, check the error.

#### L3: WebSocket upgrade error not logged

**File:** `service_secure_relay.go:217-219`

If `upgrader.Upgrade` fails, the error is silently discarded. This makes debugging WebSocket connection issues difficult.

```go
conn, err := upgrader.Upgrade(w, r, nil)
if err != nil {
    return  // error lost
}
```

**Fix:** Log the upgrade error at debug level.

#### L4: Rendezvous lookup endpoint exposes records to any authenticated operator

**File:** `service_secure_connections.go:559-575`

`handleRelayRendezvous` returns the full `RelayRendezvousRecord` (including `SecretCommitment`, `HostIDHash`, `AccountID`) to any caller with the operator role. The secret commitment is a hash, not the raw secret, so this is not a direct secret leak — but it exposes internal metadata unnecessarily.

**Fix:** Return a filtered subset of fields (status, expiry, rendezvous ID only).

#### L5: Relay origin check for `app://or3` has no secondary validation

**File:** `service_secure_relay.go:277-278`

The origin check for the `app` scheme only validates `host == "or3"`. There is no secondary check on the connection's TLS state or client certificate. Any process on the machine can set `Origin: app://or3`.

**Impact:** Low — this is by design for local Electron apps, but worth noting that the origin header alone is not a security boundary for local connections.

**Fix:** Document this as intentional. Consider adding a local socket or pipe check for higher assurance.

#### L6: Test coverage gaps

**File:** `service_secure_connections_test.go`, `service_secure_relay_test.go`

- No test for the pairing approve endpoint
- No test for session creation/authorization/step-up
- No test for WebSocket connection and message forwarding (only hub unit tests)
- No test for concurrent relay forwarding (race condition coverage)
- No test for the `handleRelayRendezvous` GET endpoint
- No test for empty/missing input validation on device ID hash

**Fix:** Add integration tests for the approve flow, WebSocket lifecycle, and concurrent forwarding scenarios.

## Task List

- [ ] **H1:** Validate device identity in relay WebSocket handler against authenticated session or enrollment certificate
- [ ] **H2:** Remove or validate `host_id_hash` query parameter override in host WebSocket handler
- [ ] **H3:** Add `closed` channel check in `forward()` before sending to target peer
- [ ] **M1:** Add backpressure feedback (return `FRAME_DROPPED` to sender) instead of silent drop
- [ ] **M2:** Add TTL bounds validation (e.g., 30s–3600s) for pairing intent creation
- [ ] **M3:** Reject empty `rendezvous_id` in `handleRelayRendezvous` with 400
- [ ] **M4:** Cap `device_name` length in pairing exchange handler
- [ ] **M5:** Include additional entropy in compatibility-mode device ID computation
- [ ] **M6:** Verify step-up claims against auth service audit log, not just context data
- [ ] **M7:** Add periodic cleanup or TTL-based eviction for in-memory relay routes
- [ ] **L1:** Extend typed error coverage to eliminate substring-based error classification
- [ ] **L2:** Verify `components()` error handling; add early return if initialization fails
- [ ] **L3:** Log WebSocket upgrade errors at debug level
- [ ] **L4:** Filter sensitive fields from rendezvous lookup response
- [ ] **L5:** Document `app://or3` origin check as intentional; consider secondary validation
- [ ] **L6:** Add test coverage for approve flow, WebSocket lifecycle, concurrent forwarding, and input validation
