# Secure Connections API

All secure-connections service routes require the local service role policy before they can be used.

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

## Sessions

`POST /internal/v1/secure-connections/sessions`

Creates a short-lived secure session claim after enrolled device and certificate-hash validation.

`POST /internal/v1/secure-connections/sessions/{session_id}/authorize`

Classifies and authorizes a requested action using role, capabilities, trust level, revocation state, and step-up freshness.

`POST /internal/v1/secure-connections/sessions/{session_id}/step-up`

Updates the verified step-up timestamp after passkey or platform verification.

`POST /internal/v1/secure-connections/sessions/expire`

Expires stale active sessions.

## Relay Rendezvous

`GET /internal/v1/secure-connections/relay/rendezvous?id=...`

Reads rendezvous metadata without returning QR secrets.

`POST /internal/v1/secure-connections/relay/rendezvous/{id}/join`

Marks a rendezvous joined while enforcing expiry and join-count limits.

`POST /internal/v1/secure-connections/relay/rendezvous/{id}/consume`

Consumes a rendezvous after successful pairing.

`POST /internal/v1/secure-connections/relay/rendezvous/{id}/reject`

Rejects and closes a rendezvous after denial.

`POST /internal/v1/secure-connections/relay/rendezvous/expire`

Expires stale rendezvous records.
