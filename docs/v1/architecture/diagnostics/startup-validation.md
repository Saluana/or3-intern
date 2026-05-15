# Startup Validation

The doctor engine validates the configuration at startup and can block operations when critical issues exist.

## Startup modes

Three startup modes trigger blocking behavior:

- `ModeStartupChat` - blocks chat CLI startup
- `ModeStartupServe` - blocks server startup
- `ModeStartupService` - blocks service startup

Source: `internal/doctor/report.go:30-34`

## Blocking findings

In startup modes, findings that would normally be `SeverityWarn` can be escalated to `SeverityBlock` via `severityFor`. This means they prevent startup.

Source: `internal/doctor/engine.go:39-43`

## Config validation at startup

The doctor checks two config validation issues:

1. **Config load errors** (`config.validation.load`) - when the config file failed to parse or validate normally. Severity escalates to block in startup and configure-post-save modes.

2. **Profile validation** (`runtime-profile.validation`) - when the runtime profile contradicts other settings. Handled by `config.ValidateProfile`.

3. **Snapshot validation** (`config.validation.snapshot`) - the in-memory config is written to a temp file and reloaded to verify round-trip validity.

Source: `internal/doctor/engine_config.go:10-48`

## Readiness check

The control plane's `GetReadiness` method uses the doctor with `ModeStartupService` and reports whether the system is ready (0 errors and 0 blocks).

Source: `internal/controlplane/controlplane.go:214-226`

## Probe checks

When `opts.Probe` is true, the doctor runs additional checks:
- SQLite database can be opened and pinged (read-only mode)
- Future probes can be added for other runtime dependencies

Source: `internal/doctor/engine_runtime.go:108-139` (probeFindings, probeSQLiteDatabase)
