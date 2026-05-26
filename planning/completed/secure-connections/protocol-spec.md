# Secure Connections v2 Protocol Specification

Status: implementation baseline for phases 1-5.

## Encoding

All cryptographic payloads use deterministic CBOR. Endpoint-visible JSON may be returned by local service APIs for UI convenience, but signatures, QR payloads, hashes, and commitments are computed over canonical CBOR bytes.

Binary values are base64url without padding. Timestamps are Unix milliseconds. Version negotiation currently supports protocol version `1` only; peers must fail closed when `minProtocolVersion` / `maxProtocolVersion` does not include `1`.

## QR Pairing Payload

The desktop host emits:

```text
or3pair:v1:<base64url(cbor(PairingQRCodeV1))>
```

`PairingQRCodeV1` includes the relay origin, rendezvous ID, host ID, host signing public key, host Noise public key, 256-bit pairing secret, expiration, capabilities, and nonce.

The relay receives only:

- `rendezvousId`
- host/account routing hashes
- `expiresAtUnixMs`
- `HMAC-SHA-256(pairingSecret, "or3 relay rendezvous")`

The raw pairing secret must never be stored by relay code, logs, telemetry, or database records.

## Enrollment

The device sends an encrypted `DeviceEnrollmentProposalV1` during the pairing channel. The host shows the proposal locally and signs `HostEnrollmentCertificateV1` only after desktop approval.

Enrollment signatures are Ed25519 over:

```text
OR3-ENROLLMENT-CERTIFICATE-V1 || canonical_cbor(certificate_without_signature)
```

The certificate binds host ID, device ID, device public keys, role, capabilities, trust level, account ID, enrollment epoch, issue time, optional expiry, and the host signing public key.

## Sessions and Frames

Runtime sessions use the same transcript fields defined in `SessionPrologueV1`. Encrypted transport frame bodies carry `SecureFrameV1`:

- `sessionId`
- monotonic `sequence`
- `correlationId`
- `kind`
- `sentAtUnixMs`
- encrypted typed body

Receivers reject duplicate, stale, malformed, or wrong-session frames. Replay rejection is mandatory even when the relay delivers a byte stream in order, because relay compromise is in scope.

## Errors

Endpoint errors use structured safe codes such as `PAIRING_EXPIRED`, `HOST_IDENTITY_CHANGED`, `DEVICE_REVOKED`, `PROTOCOL_VERSION_UNSUPPORTED`, and `REPLAY_DETECTED`. User-facing text must avoid cryptographic internals and must not include keys, tokens, pairing secrets, command text, terminal output, file contents, or decrypted frame bodies.

## Implemented Baseline

The initial implementation adds:

- deterministic CBOR QR encoding and decoding in Go
- QR entropy and expiry enforcement
- rendezvous commitments
- host identity initialization backed by the local secret store
- host identity replacement detection
- host-signed enrollment certificate generation and verification
- replay-window validation for secure frames
- relay metadata tables and no-plaintext guardrails
- app-side QR parsing, device identity generation, secure-storage detection, and host enrollment storage helpers

Noise handshakes remain the next implementation boundary: the code now enforces the data model and trust primitives needed to integrate a vetted Noise state machine without changing the persisted protocol surface.
