<!-- artifact_id: 7a9e9d3f-97da-45ef-a8df-90f8707420c7 -->

# Secure Connections Requirements

## Introduction

This plan defines the requirements for connecting OR3 mobile, web, and desktop clients to an Electron desktop host through an OR3 relay without trusting the relay with computer-control authority or command plaintext. OR3 App is effectively a remote control surface for a user's computer, so the connection design must treat relay compromise, account takeover attempts, stolen mobile devices, malicious browser code, replay attacks, and confused-user flows as primary risks.

The security objective is not to claim that the system is mathematically impossible to hack. The objective is stronger and more testable: compromising the OR3 relay SHALL NOT be sufficient to decrypt customer traffic, enroll devices, issue commands, approve actions, or take control of a paying customer's computer.

The UX objective is equally strict: mainstream pairing SHALL be understandable in under 500 ms and complete in no more than three user-visible steps. The normal path is scan a QR code, confirm on the desktop, and start using the phone.

The plan spans:

- Electron desktop host and local `or3-intern` runtime integration.
- iOS and Android mobile apps built with Capacitor.
- Web app access with lower-trust browser constraints.
- OR3 relay services for account auth, passkeys, rendezvous, routing, push wakeups, billing, abuse prevention, and metadata-minimized presence.
- Migration from the current six-digit pairing and bearer-token model to cryptographic QR pairing, device identities, host-signed enrollment, and end-to-end encrypted relay sessions.

## Definitions

- **Host:** The user's desktop computer running the Electron OR3 host and local OR3 runtime.
- **Device:** A phone, tablet, browser, or secondary desktop client requesting access to a host.
- **Relay:** OR3 cloud service that routes encrypted frames, brokers rendezvous, stores account metadata, and enforces abuse controls, but does not authorize computer control.
- **Enrollment:** The host-local act of approving a device and adding its public identity to the host trust list.
- **Pairing:** The short-lived physical ceremony, normally QR-based, that creates enough shared context to safely perform enrollment.
- **Session:** A short-lived encrypted transport association between an enrolled device and a host.
- **Step-up:** Recent user verification, normally passkey or platform biometric-backed unlock, required for sensitive actions.
- **Opaque frame:** Relay-carried bytes that are encrypted and authenticated end-to-end between device and host.

## Requirements

### 1. Hostile Relay Trust Model

**User Story:** As an OR3 customer, I want the relay to be unable to control or inspect my computer, so that a cloud breach does not become a desktop breach.

#### Acceptance Criteria

1. WHEN any command, file operation, terminal input, model instruction, approval response, memory payload, or runtime event crosses the relay THEN the payload SHALL be encrypted and authenticated end-to-end between enrolled endpoint keys.
2. WHEN the relay is compromised THEN the attacker SHALL NOT be able to derive session keys, decrypt historic traffic, decrypt live traffic, or forge valid command frames.
3. WHEN the relay attempts to inject, reorder, replay, duplicate, truncate, or modify frames THEN the receiving endpoint SHALL reject the affected frames or terminate the session with a structured security error.
4. WHEN the relay authenticates an account, subscription, or host presence THEN that authentication SHALL be treated as routing permission only, not command authorization.
5. IF relay storage, logs, or database backups are exposed THEN they SHALL contain no plaintext commands, no host private keys, no device private keys, no pairing secrets, no bearer tokens, and no material sufficient to enroll a new device.

### 2. Host as Final Authority

**User Story:** As a desktop owner, I want my local host to be the final authority for access, so that OR3 cloud services cannot silently add devices or widen privileges.

#### Acceptance Criteria

1. WHEN a new device requests enrollment THEN the host SHALL require local approval on the desktop before trusting that device.
2. WHEN enrollment succeeds THEN the host SHALL sign an enrollment record that binds device identity, host identity, role, capabilities, account binding, creation time, and revocation epoch.
3. WHEN a device presents a relay-issued token without a host-signed enrollment record THEN the host SHALL reject it for computer-control operations.
4. WHEN the relay claims that a device is approved THEN the host SHALL verify the claim against its local trust list and host-signed enrollment material.
5. IF local host trust state and relay account state disagree THEN the host-local trust state SHALL win for control decisions, while the UI shows a reconciliation warning.

### 3. Three-Step Consumer Pairing

**User Story:** As a non-technical user, I want to connect my phone to my computer by scanning a QR code, so that I can start without understanding URLs, ports, tokens, or command lines.

#### Acceptance Criteria

1. WHEN the user starts pairing on the desktop THEN the host SHALL display a QR code containing short-lived high-entropy pairing material, host identity, rendezvous data, protocol version, and expiry.
2. WHEN the user scans the QR code on iOS or Android THEN the app SHALL start pairing without requiring the user to type a URL, code, IP address, or CLI command.
3. WHEN the device asks to connect THEN the desktop SHALL show a concise approval prompt naming the device and requested role.
4. WHEN the user approves on desktop THEN the phone SHALL show connected state without requiring another conceptual decision.
5. IF camera scanning fails THEN the fallback SHALL remain high-entropy and short-lived, such as a deep link or copyable pairing phrase, not a six-digit security code for remote enrollment.

### 4. Physical Pairing Security

**User Story:** As a security-conscious owner, I want pairing to prove physical access to the desktop screen, so that an internet attacker cannot pair from afar.

#### Acceptance Criteria

1. WHEN a pairing QR is generated THEN it SHALL include at least 256 bits of unpredictable secret material or an equivalent PAKE/Noise pre-shared secret.
2. WHEN a pairing QR expires THEN the relay and host SHALL reject all use of its rendezvous ID and pairing secret.
3. WHEN a pairing secret is used successfully THEN it SHALL become single-use and SHALL NOT authorize future sessions.
4. WHEN an attacker observes only relay traffic THEN they SHALL NOT learn enough to complete pairing.
5. WHEN an attacker observes only account credentials or relay admin panels THEN they SHALL NOT learn enough to complete pairing.

### 5. Device Identity and Secure Key Storage

**User Story:** As an enrolled-device user, I want my device identity protected by the operating system, so that copied app data is not enough to impersonate me.

#### Acceptance Criteria

1. WHEN a native iOS device enrolls THEN it SHALL generate or store long-lived device private keys in iOS Keychain or Secure Enclave-backed storage when available.
2. WHEN a native Android device enrolls THEN it SHALL generate or store long-lived device private keys in Android Keystore, preferring hardware-backed or StrongBox-backed keys when available and appropriate.
3. WHEN native secure key storage is unavailable THEN the app SHALL mark the device as lower assurance and apply shorter session lifetimes plus more frequent step-up.
4. WHEN a private key can be made non-exportable by the platform THEN OR3 SHALL use that property for device identity or unwrap keys rather than storing exportable raw private keys.
5. IF app storage is copied to another device THEN the copied data SHALL NOT be enough to establish a trusted session without the original device private key and host acceptance.

### 6. End-to-End Session Encryption

**User Story:** As a mobile user, I want every remote-control session encrypted directly to my computer, so that the relay only sees routing metadata.

#### Acceptance Criteria

1. WHEN an enrolled device connects to a host through the relay THEN the endpoints SHALL establish a mutually authenticated encrypted session using host and device keys.
2. WHEN the device knows the host public key from enrollment THEN the session SHALL authenticate the host before sensitive data leaves the device.
3. WHEN the host receives a session initiation THEN it SHALL authenticate the device against the host trust list before accepting command frames.
4. WHEN session keys are established THEN they SHALL provide forward secrecy through fresh ephemeral keys.
5. WHEN a session exceeds key lifetime, message count, or duration limits THEN endpoints SHALL rekey or establish a fresh session.

### 7. Command Authorization and Capability Enforcement

**User Story:** As a desktop owner, I want enrolled devices limited by role and capability, so that a stolen phone or web session cannot perform every possible action.

#### Acceptance Criteria

1. WHEN a command arrives at the host THEN the host SHALL verify device enrollment, session freshness, role, capability, and policy before executing it.
2. WHEN a command requests file writes, terminal input, tool execution, config changes, device management, or secret access THEN the host SHALL require a sensitive-action policy check and recent step-up where configured.
3. WHEN existing `or3-intern` approval broker rules apply THEN secure connection auth SHALL NOT bypass them.
4. WHEN device role is `viewer` THEN mutation, terminal, tool execution, and approval-granting actions SHALL be denied by default.
5. IF the relay labels a command as safe or approved THEN the host SHALL ignore that label unless it is independently signed and valid under host policy.

### 8. Passkeys for Account and Owner Presence

**User Story:** As an OR3 account owner, I want passkeys to protect cloud account access and sensitive local actions, so that phishing and password theft are not enough to reach my computers.

#### Acceptance Criteria

1. WHEN a user signs in to OR3 cloud services THEN passkey/WebAuthn authentication SHALL use the configured RP ID and exact allowed origins.
2. WHEN passkeys are used for local sensitive actions THEN they SHALL prove recent user verification, not replace host enrollment.
3. WHEN the relay validates a passkey THEN the result SHALL help account, billing, recovery, and routing decisions, but SHALL NOT alone authorize desktop control.
4. WHEN a passkey credential is revoked THEN related cloud sessions and local step-up grants SHALL be invalidated.
5. IF a passkey is unavailable on a platform THEN OR3 SHALL provide an explicit recovery or degraded mode rather than silently weakening sensitive-action policy.

### 9. Relay Responsibilities and Limits

**User Story:** As OR3 operator, I want the relay to provide reachability without holding root authority, so that cloud operations remain useful but contained.

#### Acceptance Criteria

1. WHEN hosts and devices are online THEN the relay SHALL route opaque frames over authenticated WSS/WebRTC-compatible transports without decrypting payloads.
2. WHEN a host is behind NAT or firewall THEN the host SHALL maintain outbound-only relay connectivity and SHALL NOT require inbound customer router configuration.
3. WHEN abuse, billing, or account checks fail THEN the relay MAY stop routing frames, but SHALL NOT forge host or device decisions.
4. WHEN the relay stores metadata THEN it SHALL minimize retention, separate identifiers from payloads, and avoid logging sensitive frame contents.
5. IF lawful, support, or admin tooling inspects a customer session THEN tooling SHALL expose only metadata allowed by policy and SHALL NOT provide payload decryption capability.

### 10. Electron Desktop Host Security

**User Story:** As a desktop user, I want the Electron host hardened against web-to-native escalation, so that a renderer bug cannot become full computer compromise.

#### Acceptance Criteria

1. WHEN Electron renders OR3 UI THEN it SHALL load packaged or trusted secure content with restrictive CSP and no arbitrary remote code execution.
2. WHEN a renderer communicates with privileged host APIs THEN Electron IPC SHALL validate sender origin/frame and expose only narrow methods through `contextBridge`.
3. WHEN remote or untrusted content is displayed THEN Node integration SHALL be disabled, context isolation SHALL be enabled, sandboxing SHALL be enabled, navigation SHALL be allowlisted, and new window creation SHALL be denied or constrained.
4. WHEN local privileged runtime access is needed THEN the host SHALL prefer Unix sockets, named pipes, or an equivalent local-only authenticated channel over exposed TCP.
5. IF Electron security checks fail in production builds THEN release gates SHALL fail.

### 11. Capacitor iOS and Android Security

**User Story:** As a mobile user, I want the native app to use platform security correctly, so that authentication, links, and storage behave safely on iOS and Android.

#### Acceptance Criteria

1. WHEN the iOS app uses passkeys or app links THEN OR3 SHALL configure Associated Domains and `webcredentials`/universal-link files under the canonical OR3 domain.
2. WHEN the Android app uses passkeys or app links THEN OR3 SHALL configure Digital Asset Links, app signing fingerprints, and `delegate_permission/common.get_login_creds` under the canonical OR3 domain.
3. WHEN the mobile app receives links THEN sensitive pairing or auth material SHALL use Universal Links/App Links rather than custom URL schemes.
4. WHEN native storage APIs fail or are unavailable THEN the app SHALL show a lower-security state and use stricter session policy.
5. IF the app detects a rooted, jailbroken, debug, tampered, or unverified environment THEN OR3 SHALL reduce trust, require stronger step-up, or block high-risk enrollment according to policy.

### 12. Web App Access Constraints

**User Story:** As a web user, I want browser access when useful, while understanding that browsers are a lower-assurance device class for full computer control.

#### Acceptance Criteria

1. WHEN a browser enrolls as a device THEN the host SHALL label it as a web device and apply web-specific capability and session limits by default.
2. WHEN browser storage is used for identity material THEN the system SHALL prefer non-extractable WebCrypto keys where supported and SHALL avoid persistent raw secrets.
3. WHEN web code asks for sensitive actions THEN OR3 SHALL require recent passkey step-up and host policy checks.
4. WHEN the web app is embedded in an iframe THEN passkey and pairing flows SHALL be blocked unless exact embedding origins are explicitly allowed.
5. IF web origin integrity cannot be guaranteed THEN the host SHALL deny enrollment or restrict the device to low-risk viewing workflows.

### 13. Revocation, Lost Devices, and Recovery

**User Story:** As an OR3 owner, I want to revoke lost devices immediately and recover safely, so that losing a phone does not permanently expose or lock me out of my computer.

#### Acceptance Criteria

1. WHEN a device is revoked on the host THEN the host SHALL reject new sessions from that device immediately.
2. WHEN a device is revoked through the relay account UI THEN the relay SHALL stop routing to that device and the host SHALL apply the revocation after receiving a signed or host-confirmed revocation update.
3. WHEN a device is lost THEN recovery SHALL require local desktop control, an already trusted admin device, or a preconfigured recovery path.
4. WHEN all devices are revoked or lost THEN local desktop access SHALL remain the root recovery path.
5. IF a relay compromise publishes false revocations or false enrollments THEN the host SHALL require valid host-local authority before changing control state.

### 14. Migration from Current Pairing

**User Story:** As an existing OR3 user, I want a safe migration from six-digit pairing and bearer tokens, so that upgrades do not break access or preserve insecure defaults forever.

#### Acceptance Criteria

1. WHEN secure pairing v2 ships THEN the existing six-digit `/internal/v1/pairing/*` flow SHALL remain available only as legacy/local compatibility during a defined transition period.
2. WHEN an existing paired device reconnects THEN the host SHALL offer an upgrade path to cryptographic device identity and host-signed enrollment.
3. WHEN a legacy bearer token is used over the relay THEN the host SHALL reject it unless explicitly allowed by a temporary migration flag.
4. WHEN migration completes THEN long-lived bearer tokens SHALL be replaced by device-held private keys plus short-lived encrypted sessions.
5. IF migration fails THEN the app SHALL provide local desktop recovery instructions without exposing insecure remote pairing by default.

### 15. Privacy and Metadata Minimization

**User Story:** As a customer, I want OR3 to minimize what the cloud can learn, so that remote access does not create unnecessary surveillance data.

#### Acceptance Criteria

1. WHEN frames pass through the relay THEN payload contents SHALL remain opaque to OR3 cloud systems.
2. WHEN telemetry is collected THEN it SHALL avoid command text, filenames, terminal content, model prompts, file contents, and decrypted errors.
3. WHEN metadata is necessary for routing or abuse prevention THEN OR3 SHALL document what is stored, why, and for how long.
4. WHEN logs are emitted THEN they SHALL use safe reason codes, truncated identifiers, and correlation IDs rather than secrets or payloads.
5. IF customer support needs diagnostics THEN diagnostics SHALL be opt-in and redact encrypted payloads by design.

### 16. Security Testing and Verification

**User Story:** As an OR3 engineer, I want adversarial tests for the connection system, so that relay-hostile guarantees are maintained over time.

#### Acceptance Criteria

1. WHEN cryptographic protocol code changes THEN cross-language test vectors SHALL verify Go, TypeScript, Electron, iOS, Android, and web compatibility where applicable.
2. WHEN relay behavior changes THEN tests SHALL simulate malicious relay injection, replay, reorder, drop, duplication, stale routing, and database compromise.
3. WHEN pairing changes THEN tests SHALL cover QR entropy, TTL, single-use enforcement, local approval, failed approval, race conditions, camera fallback, and MITM attempts.
4. WHEN host authorization changes THEN tests SHALL cover roles, capabilities, step-up, revocation, audit, and existing approval broker compatibility.
5. IF a security regression is detected in CI THEN release SHALL be blocked until the regression is fixed or an explicit security exception is approved.

### 17. UX Testing and Confusion Budget

**User Story:** As a product owner, I want secure connection UX tested like a safety feature, so that ordinary users do not make dangerous mistakes because they are confused.

#### Acceptance Criteria

1. WHEN a first-time user sees pairing THEN they SHALL understand the next action within 500 ms in moderated usability tests.
2. WHEN pairing succeeds normally THEN the user-visible flow SHALL complete in no more than three steps: show QR, scan QR, approve on desktop.
3. WHEN pairing fails THEN the UI SHALL show one clear next action and SHALL NOT expose protocol details as the primary message.
4. WHEN a high-risk action needs approval or step-up THEN the prompt SHALL name the device, action category, and consequence in everyday language.
5. IF users repeatedly hesitate, backtrack, or misinterpret a security prompt THEN that prompt SHALL be treated as a product-security bug.

### 18. Performance and Reliability

**User Story:** As a mobile user, I want secure connections to feel instant and reliable, so that security does not make OR3 feel broken.

#### Acceptance Criteria

1. WHEN the host is online and already connected to the relay THEN an enrolled device SHALL establish a usable session quickly enough for interactive app use.
2. WHEN the network switches between Wi-Fi and cellular THEN the app SHALL reconnect without requiring re-pairing.
3. WHEN the relay restarts or deploys THEN active sessions SHALL fail closed and reconnect with fresh authenticated session keys.
4. WHEN the app is backgrounded and resumed THEN it SHALL reuse valid sessions only within policy and otherwise perform a clear reconnect or step-up.
5. IF the relay is unavailable THEN local access paths and clear offline state SHALL remain available where supported.

### 19. Incident Response and Key Rotation

**User Story:** As OR3 operator, I want a response plan for compromised relay, keys, or builds, so that containment does not depend on improvisation.

#### Acceptance Criteria

1. WHEN relay compromise is suspected THEN OR3 SHALL be able to rotate relay credentials and invalidate relay sessions without invalidating host-local enrollments unnecessarily.
2. WHEN a host identity key is rotated THEN enrolled devices SHALL require host-confirmed rekey or re-enrollment before trusting the new identity.
3. WHEN a device identity key is rotated THEN the host SHALL require an authenticated, enrolled-device rekey flow or local approval.
4. WHEN application signing keys or mobile association files change THEN release checklists SHALL verify iOS Associated Domains and Android Digital Asset Links before rollout.
5. IF a protocol version is deprecated THEN endpoints SHALL negotiate safe upgrades without downgrade attacks.

### 20. Documentation and Operator Transparency

**User Story:** As a customer or auditor, I want the security model documented clearly, so that I can understand what OR3 cloud can and cannot do.

#### Acceptance Criteria

1. WHEN secure connections ship THEN OR3 SHALL document the trust model, relay limitations, pairing flow, recovery model, and platform-specific security notes.
2. WHEN a user reviews connected devices THEN the UI SHALL show device type, trust level, last seen time, role, and revocation controls.
3. WHEN web access is lower assurance than native access THEN the UI SHALL say so without alarming or blaming the user.
4. WHEN a local approval is required THEN the docs SHALL explain why local approval protects against cloud and account compromise.
5. IF security-sensitive defaults are changed by the user THEN OR3 SHALL make the risk visible in settings and diagnostics.
