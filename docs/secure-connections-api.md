# Secure Connections API

All secure-connections service routes require the local service role policy before they can be used.
Read-only discovery routes remain low-risk. Mutating secure-connections routes require an authenticated session, and enrollment, relay, and secure-session control routes require recent step-up. Shared-secret automation tokens are not sufficient for those mutating routes.

## Discovery

`GET /internal/v1/secure-connections/capabilities`

Returns supported protocol versions, QR pairing v2 support, relay rendezvous support, enrollment certificate support, secure frame support, and whether remote legacy pairing is allowed.

## Host Identity

`GET /internal/v1/secure-connections/host-identity`

Returns public host identity only: host ID, signing public key, Noise public key, fingerprint, and creation timestamp.

## Devices

`GET /internal/v1/secure-connections/devices`

Lists trusted secure-connection devices.

`POST /internal/v1/secure-connections/devices/{device_id}/revoke`

Revokes host-local trust and active sessions for the device.

`POST /internal/v1/secure-connections/devices/lookup-by-noise-key`

Finds a device by enrolled Noise public key without exposing private material.

## Pairing

`POST /internal/v1/secure-connections/pairing/intents`

Creates a single-use QR pairing intent, a host pairing-session record, and a relay rendezvous record. The response returns the encoded QR, `secret_commitment`, `rendezvous_id`, and `expires_at`. The raw QR payload is not echoed back in JSON.

`POST /internal/v1/secure-connections/pairing/approve`

Approves a pairing session using `rendezvous_id`, `pairing_secret`, and a device-signed enrollment proposal. Approval rejects unsigned or tampered proposals, wrong pairing secrets, expired or consumed pairing sessions, and mismatched account binding.

## Sessions

`POST /internal/v1/secure-connections/sessions`

Creates a short-lived secure session claim after enrolled-device lookup, certificate-hash validation, and a runtime `noise_handshake` that is bound to the relay route, relay origin, host identity, device ID hash, and enrollment certificate hash. The server derives step-up freshness from the authenticated session context; clients do not supply their own verified timestamp.

`POST /internal/v1/secure-connections/sessions/{session_id}/authorize`

Classifies and authorizes a requested action using active-session claims only. Revoked devices, stale sessions, stale enrollment epochs, and missing step-up are rejected before authorization is evaluated.

`POST /internal/v1/secure-connections/sessions/{session_id}/step-up`

Updates the verified step-up timestamp after passkey or platform verification derived from the current authenticated session.

`POST /internal/v1/secure-connections/sessions/expire`

Expires stale active sessions.

## Relay Rendezvous

`GET /internal/v1/secure-connections/relay/rendezvous?id=...`

Reads rendezvous metadata without returning QR secrets.

`POST /internal/v1/secure-connections/relay/rendezvous/{id}/join`

Marks a rendezvous joined while enforcing expiry and join-count limits.

`POST /internal/v1/secure-connections/relay/rendezvous/{id}/consume`

Consumes a rendezvous after successful pairing. Expired rendezvous IDs cannot be consumed.

`POST /internal/v1/secure-connections/relay/rendezvous/{id}/reject`

Rejects and closes a rendezvous after denial. Expired rendezvous IDs cannot be rejected as live approvals.

`POST /internal/v1/secure-connections/relay/rendezvous/expire`

Expires stale rendezvous records.
