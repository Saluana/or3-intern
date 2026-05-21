# Frontend Secure Connections Audit

## Summary

Audited 5 source files and 4 test files covering the OR3 secure connections frontend. Found **20 issues**: 1 critical, 5 high, 7 medium, and 7 low severity. The most serious finding is that device identity private keys (Ed25519 and X25519) are stored as plaintext JSON in localStorage on web platforms, directly exfiltrable via XSS. A trust-level mapping bug also silently downgrades native devices to web-limited trust. Native secure storage uses `NativeBiometric` credentials as a generic key-value store without enforcing biometric gates. Several async error handling gaps and missing validations round out the findings.

## Findings

### Critical

#### C1. Device private keys stored as plaintext in localStorage

**File:** `secure-connections.ts:644` (save path), `secure-connections.ts:695-698` (key creation)
**Description:** `DeviceIdentityRecord` contains `deviceSigningPrivateKeyJwk` and `deviceNoisePrivateKeyJwk` — the raw JWK private keys. When native secure storage is unavailable (web, missing plugin), the entire identity record including these private keys is serialized to `localStorage` as plaintext JSON under key `or3-app:v1:secure-connections`. Any XSS, browser extension, or shared-computer scenario gives an attacker full access to the device signing and noise keys, allowing impersonation of the device and forgery of enrollment proposals.
**Suggested fix:** Never store private keys in localStorage. On web, derive keys from a user-provided passphrase using PBKDF2/Argon2, or use the Web Crypto API's non-extractable key generation (generate with `extractable: false` and only store wrapped forms). At minimum, encrypt the identity record before localStorage storage.

---

### High

#### H1. Trust level detection returns wrong value for native-secure mode

**File:** `secure-connections.ts:543`
**Description:** `detectSecureConnectionStorage()` returns `trustLevel: 'web-limited'` when `mode === 'native-secure'`. This means every device on native iOS/Android with the plugin available is classified as web-limited trust, defeating the trust model. Enrollment proposals will be downgraded, and capabilities will be restricted as if running in a browser.
**Suggested fix:** Change line 543 to return `trustLevel: 'native-software'` (or `'native-hardware'` if hardware backing can be detected).

#### H2. NativeBiometric used as plain key-value store, no biometric gate enforced

**File:** `nativeSecureStorage.ts:24-28`, `nativeSecureStorage.ts:77-83`, `nativeSecureStorage.ts:121-129`
**Description:** `NativeBiometric` (Capacitor plugin) is used to store/retrieve credentials. The code never calls `NativeBiometric.verifyIdentity()` or checks `isAvailable()`. The plugin's `setCredentials`/`getCredentials` may store data in the OS keychain, but without a biometric verification step, any app with foreground access can read the stored keys. This collapses the "native-secure" trust level.
**Suggested fix:** Call `NativeBiometric.verifyIdentity()` before reading sensitive credentials, or at minimum check `isAvailable()` and configure biometric-only access control on the keychain entries.

#### H3. No session expiry timer — expired sessions remain "connected" until next use

**File:** `useSecureConnectionSession.ts:17-21`
**Description:** The `connected` computed only re-evaluates when `claims` ref changes. There is no interval or timer to detect expiry in real-time. If a session expires while the user is idle, `connected` remains `true` until the next reactive trigger. Outbound frames could be sent with expired claims.
**Suggested fix:** Add a `setInterval` or `watchEffect` that checks expiry periodically (e.g., every 10s) and calls `clear()` when expired.

#### H4. Race condition in visibility-change pause/resume lifecycle

**File:** `useSecureConnectionLifecycle.ts:16-31`
**Description:** `pause()` and `resume()` are async but called via `void` from a synchronous event handler. Rapid tab switching can interleave pause/resume calls, leaving `isPaused` in an inconsistent state. There is no lock or guard to prevent concurrent execution.
**Suggested fix:** Add a mutex/flag to serialize pause/resume transitions, or debounce the visibility handler.

#### H5. Unhandled async errors in deep-link handler

**File:** `useSecureConnectionLifecycle.ts:37-39`
**Description:** `handleDeepLink` calls `throw new Error(...)` inside an event listener. This produces an unhandled rejection or silently swallowed exception depending on the runtime. It does not prevent the app from processing the URL — `event.preventDefault()` is called but the thrown error is wasted.
**Suggested fix:** Log the error and return early instead of throwing. Show a user-facing warning via toast/notification.

---

### Medium

#### M1. `resume()` swallows async rekey errors

**File:** `useSecureConnectionLifecycle.ts:20-26`
**Description:** If `options.rekey()` rejects, the error is unhandled (the `await` is in a fire-and-forget async arrow). The session will think it's resumed and active, but the rekey failed silently.
**Suggested fix:** Wrap rekey in try/catch, set an error state, and optionally call `pause()` to prevent operating with stale keys.

#### M2. `rotateDevice` does not update local token cache

**File:** `usePairing.ts:860-893`
**Description:** After a successful token rotation, the new token is returned but never written back to the local host cache. The app continues using the old (now invalidated) token until the next full reload.
**Suggested fix:** After rotation succeeds, call `cache.updateHost()` with the new token.

#### M3. V2 invite `issuedAtUnixMs` is never validated

**File:** `secure-connections.ts:307-358`
**Description:** `parsePairingInvite` validates `expiresAtUnixMs` but never checks that `issuedAtUnixMs <= nowUnixMs`. A crafted invite with `issuedAtUnixMs` far in the future could confuse time-based logic downstream (e.g., certificate TTL calculations).
**Suggested fix:** Add `if (invite.issuedAtUnixMs > nowUnixMs + 60_000) throw ...` after the expiry check.

#### M4. `rejectSensitiveDeepLink` is a shallow heuristic

**File:** `secure-connections.ts:774-793`
**Description:** Only query parameter *keys* are checked for sensitive substrings. Sensitive material in values (e.g., `?data=<secret>`) passes through. Also, the substring check is case-insensitive but only on keys, and false positives are possible (e.g., `?pagination_token=abc`). The hash fragment is never checked.
**Suggested fix:** Also scan values and the URL hash for high-entropy base64 strings or known secret prefixes. Document this as a best-effort heuristic.

#### M5. `concatBytes` defined twice

**File:** `secure-connections.ts:371-380` and `secure-connections.ts:1084-1094`
**Description:** Two identical `concatBytes` functions exist in the same file. This is dead-weight code that increases the attack surface and makes maintenance error-prone.
**Suggested fix:** Remove the duplicate at line 1084 and reuse the one at line 371.

#### M6. No validation of server-issued claims

**File:** `useSecureConnectionSession.ts:48`
**Description:** `claims.value = response.claims` stores server response directly without validating structure. If the server returns unexpected fields, missing fields, or wrong types, downstream consumers (rekey checks, frame building) will malfunction silently.
**Suggested fix:** Validate that `response.claims` contains all required fields (`session_id`, `issued_at_unix_ms`, `expires_at_unix_ms`, etc.) before storing.

#### M7. `writeHostTokensToNativeStorage` silently swallows write errors

**File:** `nativeSecureStorage.ts:77-83`
**Description:** The `.catch(() => undefined)` on `setCredentials` means the caller never knows if token persistence failed. The UI will show "connected" but the token is lost on next app launch.
**Suggested fix:** At minimum log the error. Consider throwing or returning a success boolean so callers can react.

---

### Low

#### L1. No upper bound on session TTL

**File:** `secure-connections.ts:898`
**Description:** `ttl_seconds: Math.max(60, ttlSeconds)` enforces a minimum of 60s but no maximum. A caller could request a 1-year TTL. The server should enforce this too, but the client should not send unreasonable values.
**Suggested fix:** Add `Math.min(Math.max(60, ttlSeconds), 24 * 60 * 60)` (capped at 24h).

#### L2. Sequence counter has no overflow protection

**File:** `useSecureConnectionSession.ts:58`
**Description:** `sequence.value += 1` can grow unbounded. While `Number.MAX_SAFE_INTEGER` is large, long-lived sessions sending many frames could theoretically hit it. `buildSecureFrame` checks `sequence <= 0` but not upper bounds.
**Suggested fix:** Add a rekey trigger when sequence exceeds a threshold (e.g., 2^32), or reset sequence on rekey.

#### L3. `detectPlatform` uses user-agent sniffing

**File:** `secure-connections.ts:1143-1149`
**Description:** UA string parsing is fragile. iPads now default to desktop UA strings and won't match `'ipad'`. This affects trust level assignment for enrollment proposals.
**Suggested fix:** Use `navigator.userAgentData?.platform` where available, with UA fallback. For iPad detection, check for `'Macintosh'` + `navigator.maxTouchPoints > 1`.

#### L4. Browser camera scanner never times out

**File:** `secure-connections.ts:1029-1081`
**Description:** `scanPairingQRCodeWithBrowserCamera` returns a promise that resolves only when a valid QR is scanned or Cancel is clicked. If the user walks away, the camera runs indefinitely, consuming battery and CPU.
**Suggested fix:** Add a timeout (e.g., 120s) that auto-cleans up and rejects with a "scan timed out" error.

#### L5. QR scan cleanup doesn't handle DOM removal edge case

**File:** `secure-connections.ts:1038`
**Description:** `overlay.remove()` is called in `cleanup()`, but if the component that mounted the overlay has already unmounted (SPA navigation), `overlay` may no longer be in the DOM. `Element.remove()` is a no-op in that case, so this is benign but worth noting.
**Suggested fix:** Guard with `overlay.isConnected && overlay.remove()` or wrap in try/catch.

#### L6. `persistPendingPairing` stores pairing code in localStorage

**File:** `usePairing.ts:229-244`
**Description:** The pending pairing object (including the 6-digit code) is stored in localStorage under `or3-app:v1:pending-pairing`. While the code is short-lived and only valid for the specific request, it's accessible to any JS on the same origin.
**Suggested fix:** Store only the request ID and host info; re-fetch the code from the user on resume, or use sessionStorage with a short TTL.

#### L7. Test coverage gaps

**Files:** All test files
**Description:**
- No tests for `parsePairingInvite` (V2 parsing, expiry, incomplete data)
- No tests for `parsePairingQRCode` (CBOR decoding, malformed data)
- No tests for `buildMobileNoiseHandshake` (key derivation)
- No tests for `loadSecureConnectionState` / `saveSecureConnectionState` (storage fallback logic)
- No tests for `useSecureConnectionLifecycle`
- No tests for `nativeSecureStorage` (plugin interaction mocking)
- No tests for `rejectSensitiveDeepLink` hash-fragment scenarios
**Suggested fix:** Add unit tests for the untested functions, prioritizing parsing, storage, and lifecycle.

---

## Task List

- [ ] **C1:** Refuse to store private keys in plaintext localStorage. Implement encryption-at-rest for web fallback (PBKDF2-wrapped keys or non-extractable Web Crypto keys).
- [ ] **H1:** Fix `detectSecureConnectionStorage` to return `trustLevel: 'native-software'` when `mode === 'native-secure'`.
- [ ] **H2:** Add `NativeBiometric.verifyIdentity()` call before reading/writing sensitive credentials, or configure biometric-only keychain access control.
- [ ] **H3:** Add an expiry timer in `useSecureConnectionSession` that proactively clears expired sessions.
- [ ] **H4:** Add a mutex or debounce to `useSecureConnectionLifecycle` pause/resume to prevent race conditions.
- [ ] **H5:** Replace `throw` with `console.warn` + user notification in `handleDeepLink`.
- [ ] **M1:** Add try/catch around `options.rekey()` in `resume()` with error state propagation.
- [ ] **M2:** Update local token cache after successful `rotateDevice`.
- [ ] **M3:** Add `issuedAtUnixMs` validation in `parsePairingInvite`.
- [ ] **M4:** Improve `rejectSensitiveDeepLink` to also scan values and hash fragments.
- [ ] **M5:** Remove duplicate `concatBytes` at line 1084 of `secure-connections.ts`.
- [ ] **M6:** Add structural validation of server-issued claims before storing.
- [ ] **M7:** Log errors from `writeHostTokensToNativeStorage` instead of silently swallowing.
- [ ] **L1:** Add max TTL cap in `buildSecureSessionStartPayload`.
- [ ] **L2:** Add sequence overflow protection or rekey trigger.
- [ ] **L3:** Improve `detectPlatform` to handle iPad desktop UA and use `userAgentData`.
- [ ] **L4:** Add timeout to browser QR scanner.
- [ ] **L5:** Guard `overlay.remove()` with connectivity check.
- [ ] **L6:** Avoid storing pairing code in localStorage; store only request ID.
- [ ] **L7:** Add missing unit tests for parsing, storage, lifecycle, and native storage.
