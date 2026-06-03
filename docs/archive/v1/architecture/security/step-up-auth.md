# Step-Up Authentication

Step-up authentication requires a recent passkey verification before performing sensitive actions. It prevents a stolen session token from being used for dangerous operations.

## When step-up is required

Step-up is required for:
- Adding new passkeys (after the first one)
- Revoking passkeys
- Other sensitive account operations

Source: `internal/auth/service.go:629-648` (requireRegistrationAuthorization)
Source: `internal/auth/service.go:425-426` (RevokePasskey step-up check)

## Step-up ceremony

Similar to login, but bound to an existing session:

1. `BeginStepUp` validates the session token, loads the user, and starts a WebAuthn login assertion for that specific user (not discoverable login). The ceremony is stored with a link to the current session ID.

2. `FinishStepUp` validates the session, checks the ceremony session ID matches, completes the assertion, and updates the session's `LastStepUpAt` timestamp.

Source: `internal/auth/service.go:282-348`

## Step-up expiry

The step-up is valid for `stepUpTTLSeconds` from `LastStepUpAt`. After this window, step-up must be performed again. The `hasRecentStepUp` method checks:

```
session.LastStepUpAt + (stepUpTTLSeconds * 1000) > now
```

Source: `internal/auth/service.go:650-655`

## Ceremony types

Three ceremony types exist:
- `registration` - for adding new passkeys
- `login` - for signing in
- `step-up` - for confirming identity before sensitive actions

Each ceremony ID is a random 16-hex-byte string. Ceremonies are consumed (one-time use) on completion.

Source: `internal/auth/service.go:31-34` (ceremony type constants)
