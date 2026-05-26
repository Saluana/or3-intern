# Secure Connections External Review Package

Target review scope:

- requirements.md
- architecture.md
- design.md
- protocol-spec.md
- crypto-library-decisions.md
- attack walkthroughs in architecture/design
- Go package `internal/secureconn`
- additive DB schema in `internal/db`
- service endpoints under `/internal/v1/secure-connections/*`
- app helpers in `app/utils/or3/secure-connections.ts`

Review questions:

1. Does relay compromise remain insufficient to enroll a device, decrypt payloads, or forge host authority?
2. Are QR payload encoding, expiry, single-use rendezvous state, and secret commitments correctly specified?
3. Is enrollment certificate signing domain-separated and canonical enough for long-term compatibility?
4. Are host identity replacement and recovery semantics fail-closed?
5. Are relay metadata guardrails sufficient to prevent accidental plaintext/secret storage?
6. Which Noise implementation should be adopted for cross-language production handshakes?
7. What platform-native key storage work is required before iOS/Android can claim hardware-backed trust?

Scheduling status: ready to send once a reviewer is selected. This repo cannot actually book a third-party review by itself; the implementation package and questions are prepared for that step.
