# Device Management

## Device lifecycle

A paired device goes through these states:

- **Active** - device is paired and can authenticate
- **Revoked** - device access has been revoked

Source: status constants are defined in the approval package (e.g., `StatusActive`, `StatusRevoked`).

## Authenticating devices

When a device sends a request with a token, `AuthenticateDeviceToken` looks up the device by token hash and checks:
1. Token exists
2. Status is Active and not revoked
3. The device's role is in the allowed roles list (if specified)

On successful auth, the device's `LastSeenAt` timestamp is updated.

Source: `internal/approval/devices.go:14-30`

## Rotating tokens

Device tokens can be rotated without changing the device identity. `RotatePairedDeviceToken` generates a new 24-hex-byte random token while keeping the same device ID, role, display name, and metadata.

Source: `internal/approval/devices.go:61-70`

## Revoking devices

`RevokeDevice` marks a device as revoked, sets the revocation timestamp, and audits the event. After revocation, the device token can no longer authenticate.

Source: `internal/approval/devices.go:32-45`

## Listing devices

The control plane service exposes device listing through `ListDevices`, which calls the broker with a limit (default 100, max 200).

Source: `internal/controlplane/controlplane.go:314-320`

## Device metadata

Devices carry metadata as a `map[string]any`. This metadata is used for channel identity matching - the broker checks if metadata fields like `channel`, `identity`, `sender`, `user_id`, or `chat_id` match an inbound message's source.

Source: `internal/approval/devices.go:116-134` (pairedMetadataMatches)

## Audit events

Device operations produce audit events:
- `device.revoked` - when a device is revoked
- `device.rotated` - when a token is rotated
- `pairing.requested` - when pairing is requested
- `pairing.resolved` - when pairing is approved or denied
- `pairing.exchanged` - when the code exchange completes

Source: `internal/approval/devices.go:43`, `internal/approval/devices.go:57`, `internal/approval/pairing.go:67,110,138,169`
