# Passkey / WebAuthn Authentication

OR3 Intern uses WebAuthn passkeys for authentication. Passkeys replace passwords with device-bound or synced credentials.

## WebAuthn setup

The auth service creates a WebAuthn instance with:
- RP (Relying Party) display name, ID, and allowed origins
- Cross-origin support if related origins are configured
- Resident key required (discoverable credentials)
- User verification required

Source: `internal/auth/service.go:137-159` (NewService)

## Registration ceremony

1. `BeginRegistration` starts a WebAuthn registration ceremony
   - Ensures the default user exists
   - If passkeys already exist, requires authorization (valid session + recent step-up)
   - Begins registration with resident key requirement
   - Stores ceremony data by ceremony ID

2. `FinishRegistration` completes the ceremony
   - Consumes the ceremony (one-time use)
   - Validates the user is authorized
   - Calls `webauthn.FinishRegistration`
   - Persists the credential

Source: `internal/auth/service.go:165-223`

## Login ceremony

1. `BeginLogin` uses discoverable login (no username needed)
   - The client sends a device ID
   - Begins a discoverable login assertion

2. `FinishLogin` completes the ceremony
   - Looks up the credential from the response
   - Resolves the user's role from paired device or fallback
   - Issues a session token

Source: `internal/auth/service.go:225-280`

## Credential persistence

Passkey credentials are stored with:
- Credential ID (hex-encoded)
- Public key
- Sign count
- AAGUID (authenticator model)
- Transport hints
- Backup eligibility and state flags
- Attestation type and format
- User verification required flag
- Optional nickname

If a credential with the same ID already exists, it is updated (upserted) preserving the original creation time, nickname, and device ID.

Source: `internal/auth/service.go:530-568` (persistCredential)

## Discovering users

For discoverable credentials, the `lookupDiscoverableUser` function finds the credential by raw ID, then loads the user from the stored user ID. If the user handle is present in the response, it's used instead.

Source: `internal/auth/service.go:601-617`

## Passkey revocation

Revoking a passkey requires:
1. A valid session token
2. A recent step-up verification (within `stepUpTTLSeconds`)
3. At least one other active recovery path (another passkey, active admin device, service secret, or paired token fallback)

Source: `internal/auth/service.go:417-447` (RevokePasskey)
Source: `internal/auth/service.go:657-675` (canRemovePasskey)

## Passkey listing and renaming

Passkeys can be listed per user and renamed (optional nickname).

Source: `internal/auth/service.go:403-415`
