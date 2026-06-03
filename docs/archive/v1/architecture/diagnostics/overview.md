# Diagnostics Overview

The diagnostics system (the "doctor") evaluates the OR3 Intern configuration and runtime environment for security and correctness issues.

## Components

1. **Doctor Engine** - evaluates config against rules and produces findings
2. **Findings** - structured reports about issues found
3. **Automatic Fixes** - safe fixes applied without user input
4. **Interactive Fixes** - guided fixes that require user choices
5. **Startup Validation** - blocks startup when critical issues exist
6. **Readiness Report** - summary of system readiness
7. **Health Checks** - runtime availability checks
8. **Audit Verification** - verifies audit chain integrity

## Source files

- `internal/doctor/engine.go` - main evaluation entry point
- `internal/doctor/engine_config.go` - config validation findings
- `internal/doctor/engine_security.go` - security audit findings
- `internal/doctor/engine_runtime.go` - runtime profile and probe findings
- `internal/doctor/engine_profiles.go` - access profile findings
- `internal/doctor/engine_approvals.go` - approval system findings
- `internal/doctor/engine_exec.go` - exec/sandbox findings
- `internal/doctor/engine_hardening.go` - hardening findings
- `internal/doctor/engine_network.go` - network policy findings
- `internal/doctor/engine_service.go` - service config findings
- `internal/doctor/engine_channels.go` - channel config findings
- `internal/doctor/engine_webhook.go` - webhook config findings
- `internal/doctor/engine_mcp.go` - MCP server findings
- `internal/doctor/engine_skills.go` - skill findings
- `internal/doctor/engine_provider.go` - provider findings
- `internal/doctor/engine_filesystem.go` - filesystem findings
- `internal/doctor/engine_predicates.go` - shared predicates
- `internal/doctor/fix.go` - automatic and interactive fixes
- `internal/doctor/render.go` - text and JSON rendering
- `internal/doctor/report.go` - report structure and filtering

## Evaluation modes

- **advisory** - standard evaluation
- **strict** - treats warnings as errors
- **startup-chat** - blocks startup on critical issues (chat mode)
- **startup-serve** - blocks startup on critical issues (serve mode)
- **startup-service** - blocks startup on critical issues (service mode)
- **configure-post-save** - validates after config save

Source: `internal/doctor/report.go:27-36` (Mode constants)
