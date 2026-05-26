# Secure Connections Crypto Library Decisions

## Go

- Deterministic CBOR: `github.com/fxamacker/cbor/v2` with core deterministic encoding.
- Host enrollment signatures: standard library `crypto/ed25519`.
- Host transport static keys: `golang.org/x/crypto/curve25519`.
- Commitments and hashes: standard library HMAC-SHA-256 / SHA-256.
- Secret storage: existing OR3 `security.SecretManager`, AES-GCM under the configured local secret-store key.

## TypeScript / Capacitor App

- Hashing: existing `@noble/hashes`.
- Browser/native app identity baseline: WebCrypto-generated ECDSA/ECDH keys, stored through native secure storage when present; browser fallback is explicitly `web-limited`.
- QR parsing: app-side deterministic CBOR reader scoped to the v1 payload shape.

## Open Implementation Spikes

- Add a vetted Noise implementation for Go and TypeScript, or bind one behind small interfaces.
- Decide whether native iOS/Android should add custom non-exportable X25519/Ed25519 wrappers or use wrapped userland keys with platform keystore protection.
- Replace the current app-side WebCrypto compatibility identity with X25519/Ed25519 once the cross-platform library choice is finalized.

## Security Notes

No custom primitive replaces Ed25519 signatures, X25519 key agreement, or AEAD transport encryption. The current app identity path is marked lower assurance where platform storage cannot provide non-exportable key semantics.
