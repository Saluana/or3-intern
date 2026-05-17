# Secure Connections Test Plan

## Malicious Relay

- Drop opaque frames and assert retryable disconnect.
- Duplicate frames and assert replay rejection.
- Reorder frames and assert sequence-window rejection of stale messages.
- Swap route IDs and assert session ID/prologue mismatch rejection.
- Inject bytes and assert malformed frame errors without plaintext logging.
- Delay revocation and assert next authorization/session validation rejects the device.
- Leak relay DB and confirm it contains only hashes, commitments, status, and timing metadata.

## Web Security

- CSP includes `frame-ancestors 'none'`, `object-src 'none'`, and restricted `connect-src`.
- Pairing pages reject cross-origin iframe enrollment.
- Deep links reject secret, token, certificate, and session query keys.
- Browser devices are web-limited and get shorter certificate lifetime.
- Web step-up is required more frequently than native step-up.

## Performance And Reliability

- Pairing intent creation should remain under 250 ms locally.
- Session claim creation should remain under 250 ms locally.
- Frame encode/decode should handle 1,000 small frames in under 1 second on a laptop.
- Relay restart should not persist plaintext.
- Mobile resume forces rekey before sensitive actions.

## Incident And Chaos

- Relay compromise: leaked database contains no private material.
- Relay outage: local trust and revocation remain authoritative.
- Host key rotation: recovery-required state blocks remote sessions.
- Cloud admin compromise: cloud account cannot create host trust without QR plus desktop approval.
- Emergency revocation: active sessions for the device are invalidated.
