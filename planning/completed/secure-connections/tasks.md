<!-- artifact_id: c92a6f87-35e4-4fd6-b7af-8f51ba889682 -->

# Secure Connections Tasks

## Implementation Plan

All tasks start incomplete. The plan is split into phases so OR3 can ship safely: first specify and test the protocol, then build host/device trust, then add the relay path, then migrate current pairing, then harden and validate release behavior.

## 1. Finalize Protocol and Threat Model

- [x] 1.1 Write the formal Secure Connections v2 protocol specification.
    - Define QR payload canonical encoding, pairing handshake messages, runtime session prologue, secure frame schemas, error schemas, and version negotiation.
    - Requirements: 1, 4, 6, 9, 16, 20

- [x] 1.2 Choose implementation libraries for Go and TypeScript crypto.
    - Validate X25519, Ed25519, ChaCha20-Poly1305, BLAKE2s or SHA-256, HKDF, CBOR/protobuf canonical encoding, and secure random generation on Electron, iOS, Android, and web.
    - Requirements: 5, 6, 10, 11, 12, 16

- [x] 1.3 Produce cross-language test vectors before writing production flows.
    - Include QR payload encoding, rendezvous commitments, Noise handshake transcripts, enrollment signatures, frame encryption, replay rejection, and protocol downgrade rejection.
    - Requirements: 1, 4, 6, 16

- [x] 1.4 Update the OR3 threat model for hostile relay operation.
    - Document relay compromise, account takeover, stolen device, phishing, local network attacker, malicious browser origin, and host malware boundaries.
    - Requirements: 1, 2, 8, 9, 15, 19, 20

- [x] 1.5 Schedule external security review for the protocol and implementation plan.
    - Provide the requirements, design, architecture, test vectors, and attack walkthroughs as review material.
    - Requirements: 16, 19, 20

## 2. Add Host Identity and Trust Store

- [x] 2.1 Implement host identity initialization in `or3-intern`.
    - Generate host signing and Noise static keys on first launch, store them in the selected secure local store, expose public host identity to Electron, and prevent silent replacement.
    - Requirements: 2, 5, 6, 10

- [x] 2.2 Add local database schema for secure connection devices, sessions, and pairing sessions.
    - Create migrations for `secure_connection_devices`, `secure_connection_sessions`, and `secure_connection_pairing_sessions` with indexes for host ID, device ID, status, and expiry.
    - Requirements: 2, 5, 6, 13, 14

- [x] 2.3 Implement host trust-store service.
    - Add create, read, update, revoke, rotate, list, audit, and lookup-by-device-key operations.
    - Requirements: 2, 5, 7, 13, 19

- [x] 2.4 Add host-signed enrollment certificate generation and verification.
    - Use domain-separated canonical bytes, reject malformed certificates, and include enrollment epoch, role, capabilities, trust level, and optional expiry.
    - Requirements: 2, 4, 5, 7, 13, 16

- [x] 2.5 Add host identity change protections.
    - Detect unexpected key replacement, block remote sessions after unapproved host key changes, and add explicit recovery flows.
    - Requirements: 2, 6, 13, 19, 20

## 3. Build Relay Rendezvous and Opaque Routing

- [x] 3.1 Define relay metadata models.
    - Add host presence, route, rendezvous, endpoint hash, expiry, and abuse-control records without private keys, pairing secrets, or plaintext payloads.
    - Requirements: 1, 9, 15

- [x] 3.2 Implement relay host connection endpoint.
    - Allow desktop hosts to maintain outbound WSS connections, advertise presence, receive route requests, and reconnect with backoff.
    - Requirements: 9, 18

- [x] 3.3 Implement relay device connection endpoint.
    - Allow enrolled devices to request host routes, send opaque Noise frames, and receive opaque response frames.
    - Requirements: 1, 6, 9, 18

- [x] 3.4 Implement pairing rendezvous endpoints.
    - Create, join, consume, expire, and rate-limit rendezvous sessions using secret commitments rather than raw pairing secrets.
    - Requirements: 1, 3, 4, 9, 15

- [x] 3.5 Add relay abuse controls.
    - Enforce per-account and per-IP rate limits, QR join limits, frame-size limits, connection quotas, and safe disconnect behavior.
    - Requirements: 4, 9, 18, 19

- [x] 3.6 Add relay no-plaintext guardrails.
    - Add code-level checks, logging filters, and tests that prevent storing or logging frame plaintext, pairing secrets, session keys, command names, tool args, or terminal contents.
    - Requirements: 1, 9, 15, 16

## 4. Implement QR Pairing v2 on Desktop Host

- [x] 4.1 Add desktop pairing intent API.
    - Let Electron request a short-lived QR pairing session with requested role, capabilities, account binding expectations, and expiration.
    - Requirements: 2, 3, 4, 7

- [x] 4.2 Generate QR payloads on the host.
    - Include relay origin, rendezvous ID, host identity, host display name, host Noise key, pairing secret, expiry, capabilities, and nonce.
    - Requirements: 3, 4, 6, 9

- [x] 4.3 Render the consumer pairing UI.
    - Show QR, short status text, refresh action, cancel action, and no setup commands in the main path.
    - Requirements: 3, 17

- [x] 4.4 Implement host-side Noise pairing session handling.
    - Join the relay rendezvous, validate prologue binding, decrypt enrollment proposal, enforce expiry, and close rendezvous after use.
    - Requirements: 1, 4, 6, 9, 16

- [x] 4.5 Build local approval prompt for new devices.
    - Display device name, platform, account, requested role, capability summary, and clear allow/deny actions.
    - Requirements: 2, 3, 4, 7, 17

- [x] 4.6 Persist approved enrollment records.
    - Store trust record, certificate, audit event, initial role, capabilities, trust level, and enrollment epoch.
    - Requirements: 2, 5, 7, 13

- [x] 4.7 Implement pairing rejection and timeout handling.
    - Reject safely, clear secrets from memory, mark rendezvous consumed or expired, and show simple user-facing state.
    - Requirements: 3, 4, 17, 18

## 5. Implement Device Identity and Pairing in Mobile Apps

- [x] 5.1 Add native device identity generation.
    - Generate device signing and Noise keys on first pairing attempt, store key material or key wraps in Keychain/Keystore, and attach local device display name.
    - Requirements: 5, 11

- [x] 5.2 Implement secure-storage capability detection.
    - Detect hardware-backed, software-backed, unavailable, restored, and wiped states; map each state to trust level and policy.
    - Requirements: 5, 11, 13

- [x] 5.3 Build QR scanner flow in the Capacitor app.
    - Parse `or3pair:v1` payloads, validate expiry and relay origin, then start the encrypted pairing session.
    - Requirements: 3, 4, 11, 17

- [x] 5.4 Implement mobile-side Noise pairing.
    - Join relay rendezvous without revealing pairing secret, validate host identity from QR, send encrypted enrollment proposal, and store returned certificate.
    - Requirements: 1, 4, 5, 6, 9

- [x] 5.5 Store host records and enrollment certificates securely.
    - Persist host ID, host signing key, host Noise key, enrollment certificate, role, capabilities, and trust level with safe backup behavior.
    - Requirements: 5, 6, 13

- [x] 5.6 Build mobile pairing success, waiting, rejection, and expired states.
    - Keep copy short, actionable, and tested for first-glance comprehension.
    - Requirements: 3, 17, 18

## 6. Implement Runtime E2EE Sessions

- [x] 6.1 Add runtime session connection manager on the host.
    - Accept relay route requests, perform Noise IK handshakes, validate enrolled device keys, and create short-lived session claims.
    - Requirements: 1, 2, 6, 7, 9

- [x] 6.2 Add runtime session connection manager in the app.
    - Discover host routes, run Noise IK with stored host identity, verify enrollment hash, and handle reconnects.
    - Requirements: 5, 6, 9, 18

- [x] 6.3 Implement secure frame encoding and replay protection.
    - Add sequence numbers, correlation IDs, session IDs, typed frame bodies, replay rejection, and safe error frames.
    - Requirements: 6, 7, 16, 18

- [x] 6.4 Implement rekey policy.
    - Rekey on duration, message count, byte count, app resume, host request, and protocol upgrade boundaries.
    - Requirements: 6, 18, 19

- [x] 6.5 Add session lifecycle cleanup.
    - Expire stale sessions, clean sequence windows, destroy in-memory keys, and audit abnormal termination.
    - Requirements: 6, 13, 18, 19

## 7. Integrate Authorization, Capabilities, and Approvals

- [x] 7.1 Map enrollment roles and capabilities into existing OR3 authorization checks.
    - Ensure viewer, operator, admin, and future custom profiles cannot bypass current approval broker and runtime profile rules.
    - Requirements: 2, 7

- [x] 7.2 Add secure connection claims to local runtime requests.
    - Include host ID, device ID, enrollment epoch, role, capabilities, trust level, session ID, step-up time, and relay route ID.
    - Requirements: 2, 7, 15

- [x] 7.3 Add sensitive action classification.
    - Classify terminal input, file writes/deletes, tool execution, secrets access, security changes, and profile escalation.
    - Requirements: 7, 8

- [x] 7.4 Enforce step-up requirements before sensitive execution.
    - Require recent passkey or platform verification where policy demands, then pass verified state into local authorization.
    - Requirements: 7, 8, 11, 12

- [x] 7.5 Add audit events for remote actions.
    - Log session start/end, device ID, route ID, action class, authorization outcome, approval outcome, and revocation state without secrets or plaintext payloads.
    - Requirements: 7, 13, 15, 19, 20

## 8. Integrate Passkeys and Account Binding

- [x] 8.1 Bind pairing to the expected OR3 account when available.
    - Include account binding proof in encrypted enrollment proposal and reject mismatched account flows in consumer mode.
    - Requirements: 2, 8, 9

- [x] 8.2 Preserve passkeys as account and step-up authority only.
    - Ensure no cloud passkey session can create host trust without QR pairing and desktop approval.
    - Requirements: 2, 4, 8

- [x] 8.3 Add passkey step-up APIs for secure sessions.
    - Support request, challenge, verify, and session-claim update paths with user verification required.
    - Requirements: 7, 8, 12

- [x] 8.4 Align iOS Associated Domains and Android Digital Asset Links with passkey and app-link flows.
    - Verify production domain files, beta/debug fingerprints, app identifiers, and fallback behavior.
    - Requirements: 8, 11, 16

## 9. Harden Electron Desktop Host

- [x] 9.1 Create Electron security baseline configuration.
    - Disable Node integration for renderer content, enable context isolation, enable sandboxing, block untrusted navigation, deny unexpected windows, and use a restrictive CSP.
    - Requirements: 10, 16

- [x] 9.2 Build a narrow typed IPC bridge.
    - Expose only pairing, secure session status, approval, and host management calls needed by the UI; validate sender frame and origin on every call.
    - Requirements: 10, 16

- [x] 9.3 Replace broad local HTTP assumptions for privileged host actions.
    - Prefer Unix domain socket or named pipe between Electron and `or3-intern`, with loopback TCP limited to development or explicit compatibility mode.
    - Requirements: 2, 10, 15

- [x] 9.4 Configure release fuses and packaged-content loading.
    - Prefer a custom app protocol for packaged UI, remove unnecessary Electron runtime features, and document release verification.
    - Requirements: 10, 16, 20

- [x] 9.5 Add desktop security regression checks.
    - Automate checks for CSP, IPC allowlist, navigation allowlist, new-window denial, no unsafe external URL opening, and production config drift.
    - Requirements: 10, 16

## 10. Harden Capacitor iOS and Android Apps

- [x] 10.1 Implement secure storage adapters for identity keys.
    - Use Keychain on iOS and Android Keystore on Android, with explicit fallback states and trust-level downgrade behavior.
    - Requirements: 5, 11

- [x] 10.2 Validate native app-link and passkey domain setup.
    - Add tests or scripts that verify `apple-app-site-association` and Digital Asset Links for every release channel.
    - Requirements: 8, 11, 16

- [x] 10.3 Prevent sensitive material in custom URL schemes.
    - Ensure QR secrets, enrollment certificates, and session data are never passed through insecure deep-link query strings.
    - Requirements: 4, 11, 15

- [x] 10.4 Add mobile lifecycle protections.
    - Pause sessions on background when required, rekey on resume, clear in-memory secrets, and handle biometric/device credential changes.
    - Requirements: 5, 6, 11, 18

- [x] 10.5 Add jailbreak/root/debug signal policy hooks where feasible.
    - Do not overpromise detection; use signals to lower trust level, require more step-up, or warn the user.
    - Requirements: 5, 11, 12, 17

## 11. Implement Web App Limited Trust Mode

- [x] 11.1 Add browser device identity generation.
    - Use WebCrypto non-extractable keys where possible and detect weaker fallback storage.
    - Requirements: 5, 12

- [x] 11.2 Add web enrollment restrictions.
    - Require explicit host approval, default to shorter certificate expiry, label browser devices clearly, and limit high-risk capabilities by default.
    - Requirements: 2, 7, 12, 17

- [x] 11.3 Add web passkey step-up for sensitive actions.
    - Require recent WebAuthn verification more frequently for browser devices than native devices.
    - Requirements: 8, 12

- [x] 11.4 Harden web origins and CSP.
    - Block cross-origin iframe enrollment, restrict scripts, validate origins, and add XSS regression coverage for pairing and secure-session pages.
    - Requirements: 10, 12, 16

## 12. Migrate Existing Pairing and Tokens

- [x] 12.1 Add capability discovery for secure connections v2.
    - Let app and host identify whether QR pairing v2, relay sessions, and enrollment certificates are supported.
    - Requirements: 14, 18

- [x] 12.2 Build upgrade flow for existing paired devices.
    - Prompt the user to upgrade from bearer-token pairing to device identity and host-signed enrollment with local confirmation.
    - Requirements: 3, 13, 14, 17

- [x] 12.3 Restrict six-digit pairing to legacy/local modes.
    - Disable six-digit pairing for relay-mediated remote enrollment by default and add clear admin/development overrides.
    - Requirements: 4, 14, 20

- [x] 12.4 Remove bearer token authority for remote computer control.
    - Keep compatibility shims only during migration and ensure token possession alone cannot execute commands over the relay.
    - Requirements: 1, 2, 6, 14

- [x] 12.5 Add migration audit and rollback plan.
    - Track upgraded devices, failed upgrades, revocations, and legacy-mode usage; define how to recover without restoring insecure defaults.
    - Requirements: 13, 14, 19, 20

## 13. Implement Revocation, Recovery, and Key Rotation

- [x] 13.1 Add device revocation from desktop host UI.
    - Show trusted devices, last seen, role, platform, trust level, and clear revoke action.
    - Requirements: 13, 17

- [x] 13.2 Add remote revocation request path through OR3 account.
    - Allow cloud account UI to stop relay routing immediately while making clear that host-local trust is final for control.
    - Requirements: 8, 9, 13, 19

- [x] 13.3 Implement revocation propagation and enforcement.
    - Reject revoked devices during session handshake, command authorization, and reconnect; invalidate existing sessions.
    - Requirements: 2, 6, 7, 13

- [x] 13.4 Add host key rotation procedure.
    - Support planned rotation, emergency rotation, device re-trust, audit events, and identity-change prompts.
    - Requirements: 2, 13, 19, 20

- [x] 13.5 Add account recovery guidance and flows.
    - Document what cloud recovery can restore and what requires local desktop access or re-pairing.
    - Requirements: 8, 13, 20

## 14. Add Observability Without Plaintext Leakage

- [x] 14.1 Define privacy-preserving telemetry events.
    - Track pairing success/failure classes, session establishment latency, disconnect reasons, revocation events, and UX drop-off without commands or content.
    - Requirements: 15, 17, 18, 19

- [x] 14.2 Add secure logging filters.
    - Redact keys, tokens, pairing secrets, certificates where appropriate, frame payloads, command text, tool args, terminal output, and file contents.
    - Requirements: 1, 9, 15, 16

- [x] 14.3 Add operational dashboards.
    - Monitor relay health, route latency, reconnect rates, QR expiration rates, handshake failures, abuse throttles, and incident signals.
    - Requirements: 9, 18, 19

- [x] 14.4 Add customer-visible transparency surfaces.
    - Show active sessions, trusted devices, recent remote activity, revocation status, and relay connectivity in plain language.
    - Requirements: 13, 15, 17, 20

## 15. Build Security Test Suites

- [x] 15.1 Build cryptographic protocol unit tests.
    - Cover test vectors, invalid public keys, malformed QR payloads, invalid signatures, wrong PSK, wrong prologue, downgrade attempts, rekey, and corrupted frames.
    - Requirements: 4, 6, 16

- [x] 15.2 Build malicious relay integration tests.
    - Simulate dropped frames, duplicated frames, reordered frames, host swap, route swap, injected bytes, replayed command frames, delayed revocation, and leaked relay database.
    - Requirements: 1, 6, 9, 15, 16

- [x] 15.3 Build pairing flow tests.
    - Cover single-use QR, expiration, approval, denial, two-device race, account mismatch, camera failure fallback, offline phone, and host identity change.
    - Requirements: 3, 4, 8, 17

- [x] 15.4 Build authorization integration tests.
    - Cover roles, capabilities, sensitive step-up, approval broker, runtime profiles, revoked devices, stale enrollment epoch, stale sessions, and audit output.
    - Requirements: 2, 7, 8, 13, 16

- [x] 15.5 Build Electron hardening tests.
    - Verify renderer isolation, sandbox, CSP, IPC validation, navigation blocking, window blocking, custom protocol behavior, and release fuse settings.
    - Requirements: 10, 16

- [x] 15.6 Build Capacitor platform tests.
    - Verify secure storage, key lifecycle, app links, passkey domain integration, biometric/device credential prompts, background/resume behavior, and reinstall behavior.
    - Requirements: 5, 8, 11, 16, 18

- [x] 15.7 Build web security tests.
    - Verify WebCrypto paths, storage fallback limits, CSP, origin validation, XSS protections, cross-origin iframe blocking, and passkey step-up.
    - Requirements: 12, 16

- [x] 15.8 Build performance and reliability tests.
    - Measure pairing time, handshake latency, reconnect latency, encryption throughput, host sleep/wake, mobile background/resume, relay restart, and degraded network behavior.
    - Requirements: 18

- [x] 15.9 Build incident and chaos tests.
    - Simulate relay compromise, relay outage, key rotation, leaked metadata, compromised cloud admin action, and emergency revocation.
    - Requirements: 1, 9, 13, 19

## 16. Validate UX and Accessibility

- [x] 16.1 Prototype the three-step consumer pairing flow.
    - Validate the desktop QR, mobile scan, desktop approve, and success states before production implementation is locked.
    - Requirements: 3, 17

- [x] 16.2 Run first-glance comprehension testing.
    - Test whether users understand the current state and next action within the 500 ms confusion budget.
    - Requirements: 3, 17

- [x] 16.3 Run recovery comprehension testing.
    - Test expired QR, rejected pairing, lost phone, revoked phone, host identity changed, and relay unavailable states.
    - Requirements: 13, 17, 19

- [x] 16.4 Add accessibility coverage.
    - Test screen readers, keyboard focus, large text, contrast, QR alternative path, reduced motion, timeouts, and clear error announcements.
    - Requirements: 3, 17, 20

- [x] 16.5 Remove technical language from mainstream UI.
    - Keep cryptographic and relay details in diagnostics/help, not in the primary consumer path.
    - Requirements: 3, 17, 20

## 17. Prepare Release, Documentation, and Incident Response

- [x] 17.1 Write operator runbooks.
    - Include relay outage, relay compromise, cloud credential compromise, key rotation, revocation support, and customer recovery.
    - Requirements: 19, 20

- [x] 17.2 Write customer-facing security explanation.
    - Explain that OR3 routes encrypted traffic but cannot see commands, and that local desktop approval controls computer access.
    - Requirements: 1, 2, 15, 20

- [x] 17.3 Add developer documentation for secure connection APIs.
    - Document relay endpoints, host APIs, app composables, secure storage adapters, test vectors, and migration rules.
    - Requirements: 14, 16, 20

- [x] 17.4 Create staged rollout plan.
    - Ship behind feature flags, enable internal testing, beta native apps, web limited mode, selected customers, then general availability.
    - Requirements: 14, 18, 19

- [x] 17.5 Define production release gates.
    - Require external review, passing malicious-relay tests, passing platform hardening tests, validated passkey/app-link domains, migration dry run, and incident tabletop completion.
    - Requirements: 16, 18, 19, 20

- [x] 17.6 Define legacy deprecation timeline.
    - Communicate when six-digit remote pairing and bearer-token remote control stop working outside explicit local/development modes.
    - Requirements: 14, 20
