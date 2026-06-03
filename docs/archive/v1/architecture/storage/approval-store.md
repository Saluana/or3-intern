# Approval Store

The approval store manages device pairing, approval requests, allowlists, and approval tokens. It handles the full lifecycle of security approvals for agent actions.

Source: `internal/db/approval_store.go`

## Data Types

### PairingRequestRecord (`approval_store.go:14-28`)
Device pairing requests with a pairing code hash, status tracking, and approval/denial timestamps.

### PairedDeviceRecord (`approval_store.go:30-41`)
Trusted devices with a token hash, role (`"operator"` or `"agent"`), status (`"active"`, `"revoked"`), and metadata JSON (which can include channel/identity info for external integrations).

### ApprovalRequestRecord (`approval_store.go:43-59`)
Agent action approval requests with subject hash, requester identity, execution host, policy mode, and resolution details.

### ApprovalAllowlistRecord (`approval_store.go:61-70`)
Domain-scoped allowlist entries with JSON scope and matcher configurations.

### ApprovalTokenRecord (`approval_store.go:72-80`)
Issued tokens tied to an approval request, with expiry and revocation.

## Pairing Operations

| Function | Purpose |
|----------|---------|
| `CreatePairingRequest()` | Inserts a new pairing request |
| `GetPairingRequest()` | Retrieves by ID |
| `ListPairingRequests()` | Lists by optional status filter |
| `UpdatePairingRequestStatus()` | Sets status, approver, timestamps |
| `ResolvePairingRequestStatus()` | Atomic status transition (from → to) |
| `CompareAndSwapPairingRequestStatus()` | Simple atomic status swap |
| `FindPairingRequestByCodeHash()` | Lookup by ID + code hash |
| `FindPairingRequestsByCodeHash()` | Multi-record lookup by code hash, status, and expiry |

## Device Operations

| Function | Purpose |
|----------|---------|
| `UpsertPairedDevice()` | Insert or update a device. If the token hash changes or the device is revoked, all auth sessions for that device are also revoked |
| `GetPairedDevice()` | Retrieves by device ID |
| `ListPairedDevices()` / `ListPairedDevicesPage()` | Paginated listing |
| `FindPairedDeviceByToken()` | Lookup by SHA256(token). Token hash is computed as `sha256.Sum256([]byte(rawToken))` |
| `TouchPairedDevice()` | Updates `last_seen_at` |
| `FindActivePairedDeviceByChannelIdentity()` | Finds active devices by channel + identity from metadata JSON |

## Approval Request Operations

| Function | Purpose |
|----------|---------|
| `CreateApprovalRequest()` | Creates a new request |
| `GetApprovalRequest()` | Retrieves by ID |
| `FindPendingApprovalRequest()` | Finds pending requests by type + hash + host (not expired) |
| `ListApprovalRequests()` / `ListApprovalRequestsFiltered()` | Lists by status and optional type |
| `ExpireApprovalRequests()` | Bulk-expires pending requests past their expiry |
| `ListExpiredPendingApprovalRequestIDs()` | Finds IDs of expired pending requests |
| `UpdateApprovalRequestResolution()` | Sets resolution fields |
| `ResolveApprovalRequest()` | Atomic status transition with resolution data |

## Allowlist Operations

| Function | Purpose |
|----------|---------|
| `CreateApprovalAllowlist()` | Creates an allowlist entry |
| `GetApprovalAllowlist()` | Retrieves by ID |
| `ListApprovalAllowlists()` | Lists by optional domain filter |
| `DisableApprovalAllowlist()` | Sets `disabled_at` |

## Token Operations

| Function | Purpose |
|----------|---------|
| `CreateApprovalToken()` | Issues a new token |
| `GetApprovalToken()` | Retrieves by ID |
| `RevokeApprovalToken()` | Sets `revoked_at` |
| `ConsumeApprovalToken()` | Atomic consumption (revoked_at=0 → now) |

## Key Design Patterns

- **Atomic status transitions** — `ResolveApprovalRequest()` and `ResolvePairingRequestStatus()` use `WHERE status=?` clauses to prevent double-processing.
- **Token-based auth** — `FindPairedDeviceByToken()` uses SHA256 hashing so raw tokens never appear in queries.
- **Cascading revocation** — When a device token rotates or the device is revoked, all auth sessions for that device are automatically revoked.
