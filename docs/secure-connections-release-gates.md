# Secure Connections Release Gates

A production release must show evidence for each gate:

- Protocol vectors pass in Go and TypeScript.
- Relay stores no private keys, pairing secrets, plaintext frames, command text, tool args, terminal output, or file content.
- QR pairing is single-use and expires.
- Pairing approval rejects unsigned or tampered enrollment proposals, wrong pairing secrets, and replayed or expired pairing sessions, and pairing intent responses do not echo raw QR payloads.
- Enrollment certificates reject tampering, expiry, host mismatch, and account mismatch.
- Secure sessions use server-issued claims backed by a runtime Noise handshake and reject revoked devices, stale certificates, stale sessions, replayed frames, and missing step-up.
- Native app-link and passkey domain files validate for release identifiers.
- Browser enrollments are web-limited by default.
- Audit events contain device/session/action metadata without plaintext.
- Electron packaged builds load the app from ASAR only and do not expose a sensitive secure-connections renderer-to-main IPC bridge.
- Operator runbooks and customer-facing security copy are published.
