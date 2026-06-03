# Device Pairing System

Device pairing lets external devices (phones, other computers) connect to OR3 Intern securely.

## How pairing works

1. A device requests pairing by calling `CreatePairingRequest`. This generates a random 6-digit code and a random 12-hex-digit device ID.

2. The request's status depends on the pairing policy mode (source: `internal/approval/pairing.go:34-62`):
   - `deny` - blocked immediately
   - `trusted` - auto-approved if the requester is authenticated, otherwise pending
   - `allowlist` - auto-approved for authenticated requesters whose device is already in the allowlist, blocked otherwise

3. The requesting device shows the 6-digit code to the user.

4. The operator approves the request by providing the code (via `ApprovePairingRequestByCode`). The code is stored as a SHA-256 hash, never in plaintext.

5. The device exchanges the code for a device token (via `ExchangePairingCode`). The broker:
   - Verifies the request is approved and not expired
   - Atomically swaps status from approved to exchanged (compare-and-swap)
   - Creates/rotates a device token (24 random hex bytes, stored as hash)
   - Returns the device record and raw token

Source: `internal/approval/pairing.go:142-171` (ExchangePairingCode)

## Device roles

Each paired device has a role (e.g., `admin`, `operator`, `service-client`). Role determines what the device can do. Token authentication checks allowed roles.

Source: `internal/approval/devices.go:14-30` (AuthenticateDeviceToken)

## Pairing codes

Codes are 6 random digits, hashed with SHA-256 before storage. They expire after `pairingCodeTTLSeconds`. The code hash is used to look up pending requests.

Source: `internal/approval/pairing.go:114-131` (ApprovePairingRequestByCode)

## Channel identity matching

The broker can check if a device is paired by matching channel-specific metadata (e.g., Telegram user ID, Slack user ID). This is used for inbound message authorization.

Source: `internal/approval/devices.go:72-114` (IsPairedChannelIdentity)
