# Auth Sessions

Auth sessions authenticate users after passkey login. Sessions are issued by the auth service and validated on every authenticated request.

## Session creation

After a successful passkey login, `issueSession` creates a session record with:
- A random 32-hex-byte session token (returned to the client as raw string)
- The token stored as SHA-256 hash (never plaintext)
- Idle expiry (default from `sessionIdleTTLSeconds`)
- Absolute expiry (default from `sessionAbsoluteTTLSeconds`)
- User agent hash and remote address hash for binding
- The user's role (from paired device or default "admin")

Source: `internal/auth/service.go:570-599` (issueSession)

## Session validation

`ValidateSessionToken` checks, in order:
1. Session exists by token hash
2. Session not revoked
3. Session not expired (idle or absolute)
4. Associated user still exists

If valid, the session's idle expiry is extended and `LastSeenAt` is updated.

Source: `internal/auth/service.go:350-388` (ValidateSessionToken)

## Session revocation

`RevokeSessionToken` revokes a session by setting `RevokedAt` with a reason (e.g., "logout", "passkey-revoked"). When a passkey is revoked, all sessions from that credential are also revoked.

Source: `internal/auth/service.go:390-401` (RevokeSessionToken)
Source: `internal/auth/service.go:442-443` (RevokeAuthSessionsByCredential)

## Session claims

The `SessionClaims` struct returned by validation includes:
- `Session` - the full session record
- `User` - the authenticated user record
- `Role` - the effective role for this session

Source: `internal/auth/service.go:124-128` (SessionClaims)

## Login result

A successful login returns:
- `SessionToken` - the raw token for the client to store
- `Session` - the session record (without the token hash)
- `User` - the user record
- `CredentialID` - the passkey credential used

Source: `internal/auth/service.go:130-135` (LoginResult)
