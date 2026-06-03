# Readiness Report

The readiness report tells you whether the system is ready to operate.

## Control plane endpoint

`GetReadiness()` runs the doctor engine in `ModeStartupService` and returns a `ReadinessReport`:

- `Ready` is true when `ErrorCount == 0 && BlockCount == 0`
- `Status` comes from the doctor summary
- `Findings` lists all issues found

Source: `internal/controlplane/controlplane.go:214-226`

## Report structure

```json
{
  "status": "ok",
  "ready": true,
  "summary": {
    "status": "ok",
    "infoCount": 2,
    "warnCount": 0,
    "errorCount": 0,
    "blockCount": 0,
    "fixableCount": 1
  },
  "findings": [...]
}
```

Source: `internal/controlplane/controlplane.go:118-123` (ReadinessReport)

## Status values

The summary status can be:
- **ok** - no issues
- **ready with warnings** - warnings exist but no errors
- **needs attention** - errors exist
- **not ready** - blockers exist

Source: `internal/doctor/report.go:235-244`

## Report filtering

Reports can be filtered by:
- Area (e.g., only "security" and "approvals")
- Minimum severity
- Fixable only

Source: `internal/doctor/report.go:71-75` (FilterOptions), `internal/doctor/report.go:145-178` (Filter)

## Top findings

`TopFindings` returns the first N findings (useful for displaying only the most critical issues).

Source: `internal/doctor/report.go:253-258`

## Blocking findings

Two helper methods check for critical findings:
- `HasBlockingFindings()` - any severity:block findings
- `HasStrictFailures()` - any findings at severity:warn or above

Source: `internal/doctor/report.go:180-196`
