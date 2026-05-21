# External Security Review Package

## Scope

Review Secure Connections v2 protocol, trust model, relay metadata handling, QR pairing, enrollment certificates, secure session claims, authorization, revocation, migration, and platform hardening.

## Materials

- `planning/secure-connections/requirements.md`
- `planning/secure-connections/design.md`
- `planning/secure-connections/architecture.md`
- `planning/secure-connections/protocol-spec.md`
- `planning/secure-connections/threat-model-hostile-relay.md`
- `planning/secure-connections/security-review-package.md`
- `docs/secure-connections-api.md`
- `docs/secure-connections-release-gates.md`

## Review Tracks

1. Cryptographic protocol and downgrade resistance.
2. Host trust store, enrollment certificates, and revocation behavior.
3. Hostile relay and no-plaintext storage guarantees.
4. Passkey/account binding boundaries.
5. Capacitor secure storage and app-link configuration.
6. Browser limited-trust behavior.

## Requested Deliverables

- Findings with severity, exploitability, affected requirements, and remediation guidance.
- Test-vector review notes.
- Release-gate pass/fail recommendation.

## Scheduling

Target the review after protocol vectors and malicious-relay tests are green. The package is ready for vendor intake; final scheduling requires selecting the external reviewer and calendar slot.
