# Auth Store

The auth store manages users, WebAuthn passkeys, authentication ceremonies, auth sessions, and recovery codes.

Source: `internal/db/auth_store.go`

## Data Types

### AuthUserRecord (`auth_store.go:15-21`)
A user identified by a string ID, with display name and optional disabled state.

### PasskeyCredentialRecord (`auth_store.go:23-44`)
A WebAuthn passkey credential. Key fields:

| Field | Description |
|-------|-------------|
| ID | Unique passkey identifier |
| UserID | Owning user |
| DeviceID | Associated paired device (optional) |
| CredentialID | WebAuthn credential ID (raw bytes) |
| PublicKey | WebAuthn public key (raw bytes) |
| SignCount | Signature counter |
| Transports | Authenticator transports (usb, nfc, ble, internal, hybrid) |
| AAGUID | Authenticator AAGUID (hex string) |
| AttestationType | `"none"`, `"direct"`, `"indirect"` |
| Flags | Authenticator flags byte (UP, UV, BE, BS) |
| BackupEligible | Whether the credential can be backed up |
| BackupState | Whether the credential IS backed up |
| UserVerifiedRequired | Whether UV was required during registration |
| CredentialJSON | Full WebAuthn credential JSON (alternative to discrete fields) |
| RevokedAt / RevokedReason | Revocation tracking |

### WebAuthnCeremonyRecord (`auth_store.go:46-63`)
Tracks in-progress WebAuthn ceremonies (registration or authentication). Stores challenge hash, session data JSON, and expiry.

### AuthSessionRecord (`auth_store.go:65-83`)
An authenticated session with:

| Field | Description |
|-------|-------------|
| TokenHash | Hashed session token (unique) |
| IdleExpiresAt | Session expires after inactivity |
| AbsoluteExpiresAt | Hard expiry regardless of activity |
| LastStepUpAt / LastStepUpCredentialID | Step-up authentication tracking |
| UserAgentHash / RemoteAddrHash | Hashed client fingerprint |

### AuthRecoveryCodeRecord (`auth_store.go:85-92`)
A hashed recovery code for account recovery.

## User Operations

| Function | Purpose |
|----------|---------|
| `UpsertAuthUser()` | Creates or updates a user |
| `GetAuthUser()` | Retrieves by ID |
| `ListAuthUsers()` | Lists users (default limit 50) |

## Passkey Operations

| Function | Purpose |
|----------|---------|
| `UpsertPasskeyCredential()` | Creates or updates a passkey |
| `GetPasskeyCredential()` | Retrieves by ID |
| `FindPasskeyCredentialByCredentialID()` | Lookup by WebAuthn credential ID bytes |
| `ListPasskeyCredentialsByUser()` | Lists by user, optionally including revoked |
| `RenamePasskeyCredential()` | Sets the nickname |
| `UpdatePasskeyCredentialUsage()` | Updates sign count, flags, and last used timestamp after authentication |
| `RevokePasskeyCredential()` | Revokes a passkey with reason |

The `ToWebAuthnCredential()` method on `PasskeyCredentialRecord` converts the stored record to a `libwebauthn.Credential`, preferring the `credential_json` field if present.

## Ceremony Operations

| Function | Purpose |
|----------|---------|
| `CreateWebAuthnCeremony()` | Starts a new ceremony |
| `GetWebAuthnCeremony()` | Retrieves by ID |
| `ConsumeWebAuthnCeremony()` | Atomic consumption (consumed_at=0 → now) |
| `MarkWebAuthnCeremonyFailure()` | Records failure with reason and increments failure count |
| `DeleteExpiredWebAuthnCeremonies()` | Bulk cleanup of expired ceremonies |

## Auth Session Operations

| Function | Purpose |
|----------|---------|
| `CreateAuthSession()` | Creates a new session |
| `GetAuthSession()` | Retrieves by ID |
| `ListAuthSessionsByUser()` | Lists sessions for a user |
| `FindAuthSessionByTokenHash()` | Lookup by token hash |
| `UpdateAuthSessionActivity()` | Updates `last_seen_at` and `idle_expires_at` |
| `UpdateAuthSessionStepUp()` | Records step-up authentication |
| `RevokeAuthSession()` | Revokes a single session |
| `RevokeAuthSessionsByDevice()` | Revokes all sessions for a device |
| `RevokeAuthSessionsByCredential()` | Revokes all sessions for a credential |
| `RevokeAuthSessionsByUser()` | Revokes all sessions for a user |
| `RevokeExpiredAuthSessions()` | Bulk-revokes sessions past idle or absolute expiry |

## Recovery Code Operations

| Function | Purpose |
|----------|---------|
| `CreateAuthRecoveryCode()` | Creates a recovery code |
| `GetAuthRecoveryCode()` | Retrieves by ID |
| `FindAuthRecoveryCodeByHash()` | Lookup by code hash |
| `ListAuthRecoveryCodesByUser()` | Lists codes for a user |
| `MarkAuthRecoveryCodeUsed()` | Records usage |
| `RevokeAuthRecoveryCode()` | Revokes a single code |
| `RevokeAuthRecoveryCodesByUser()` | Revokes all unused codes for a user |

## Key Design Patterns

- **Token hashing** — Session tokens and recovery codes are stored as hashes. Lookups use pre-computed hashes.
- **Atomic consumption** — Ceremony and session operations use `WHERE consumed_at=0` or `WHERE revoked_at=0` to prevent double-use.
- **Cascading revocation** — `UpsertPairedDevice()` in the approval store auto-revokes sessions when a device token rotates or the device is revoked.
