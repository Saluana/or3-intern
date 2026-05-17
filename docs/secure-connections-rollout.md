# Secure Connections Rollout

## Stage 0: Internal

- Enable capability discovery.
- Keep legacy six-digit pairing local-only.
- Validate QR pairing, enrollment certificates, session claims, revocation, and audit events.

## Stage 1: Beta

- Enable relay rendezvous for selected accounts.
- Require explicit desktop approval for every new device.
- Monitor pairing failures, join-rate limits, session expiry, and revocation outcomes.

## Stage 2: Production

- Make secure QR pairing the default path.
- Keep legacy pairing available only for local compatibility and documented recovery.
- Require release-gate evidence before broad rollout.

## Rollback

- Disable relay route creation.
- Keep host-local trust and revocation records intact.
- Fall back to local control while keeping remote legacy token authority disabled.
