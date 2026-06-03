# Health Checks

Health checks report runtime service availability.

## Health report

The control plane's `GetHealth()` returns a `HealthReport`:

```json
{
  "status": "ok",
  "runtimeAvailable": true,
  "jobRegistryAvailable": true,
  "subagentManagerEnabled": true,
  "approvalBrokerAvailable": true,
  "processId": 12345,
  "startedAt": "2024-01-01T00:00:00Z"
}
```

Source: `internal/controlplane/controlplane.go:108-116` (HealthReport), `internal/controlplane/controlplane.go:198-212` (GetHealth)

## Status values

- **ok** - all core services available (runtime and job registry)
- **degraded** - runtime or job registry unavailable

Source: `internal/controlplane/controlplane.go:208-210`

## Checks performed

The health check verifies:
- `RuntimeAvailable` - agent runtime exists
- `JobRegistryAvailable` - job registry exists
- `SubagentManagerEnabled` - subagent manager exists
- `ApprovalBrokerAvailable` - approval broker exists

It does NOT check if these services are functioning correctly - only that they were initialized.

Source: `internal/controlplane/controlplane.go:198-212`

## Process info

The health report includes the OS process ID and the startup time (formatted as RFC 3339 nanoseconds). The startup time is captured when the controlplane package is initialized.

Source: `internal/controlplane/controlplane.go:39` (processStartedAt)

## Control plane error constants

The control plane defines named errors for unavailable services:
- `ErrApprovalBrokerUnavailable`
- `ErrJobRegistryUnavailable`
- `ErrJobNotFound`
- `ErrDatabaseUnavailable`
- `ErrProviderUnavailable`
- `ErrAuditUnavailable`

Source: `internal/controlplane/controlplane.go:25-31`
