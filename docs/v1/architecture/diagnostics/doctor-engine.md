# Doctor Engine

The doctor engine runs checks across all config areas and produces a report of findings.

## Evaluate function

`Evaluate(cfg, opts)` is the main entry point. It runs findings from all engine modules in order:

1. Config validation findings
2. Provider findings
3. Filesystem findings
4. Hardening findings
5. Security findings
6. Approval findings
7. Webhook findings
8. Service findings
9. MCP findings
10. Network findings
11. Profile findings
12. Exec findings
13. Skill findings
14. Channel exposure findings
15. Channel ingress findings
16. Runtime profile findings
17. Probe findings (optional, only when `Probe=true`)

Source: `internal/doctor/engine.go:12-37`

## Severity levels

Findings have four severity levels, ranked from highest to lowest:

| Severity | Meaning | Startup behavior |
|----------|---------|-----------------|
| **block** | Critical issue, prevents operation | Blocks startup |
| **error** | Serious problem needing attention | Blocks startup in startup modes |
| **warn** | Potential issue to review | Does not block |
| **info** | Informational only | Does not block |

Source: `internal/doctor/report.go:9-16`

## Severity escalation

The `severityFor` function promotes advisory warnings to blocks when running in startup modes. The `severityForConfigureOrPostSave` function promotes findings to blocks in configure-post-save and all startup modes.

Source: `internal/doctor/engine.go:39-51`

## Finding structure

Each finding has:
- `ID` - unique identifier (e.g., "security.audit_disabled")
- `Area` - subsystem (e.g., "security", "approvals", "profiles")
- `Severity` - one of the four levels
- `Summary` - one-line description
- `Detail` - longer explanation
- `Evidence` - supporting details (optional)
- `FixMode` - how the finding can be fixed (none, automatic, interactive, manual)
- `FixHint` - human-readable fix suggestion
- `Metadata` - extra key-value context

Source: `internal/doctor/report.go:38-48` (Finding struct)

## Report sorting

Findings are sorted by severity (highest first), then by area name, then by ID, then by summary.

Source: `internal/doctor/report.go:105-123` (NewReport)
