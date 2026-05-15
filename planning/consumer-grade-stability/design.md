# Consumer Grade Stability Design

## Overview

Use the existing setup/status/doctor/runtime spine and tighten it into a consumer-grade reliability loop:

```text
load config -> normalize -> evaluate readiness -> probe basics -> repair/quarantine/refuse -> start
```

The goal is not a new architecture. The goal is to make the current one impossible to leave in a confusing half-broken state.

## Affected areas

- `internal/config`: readiness state, migration/normalization, validation messages.
- `cmd/or3-intern/setup_cmd.go`: provider probes, draft vs ready setup, secret handling.
- `internal/doctor` and `cmd/or3-intern/status_cmd.go`: complete finding/fix surface.
- `cmd/or3-intern/startup_validation.go`: startup consumes doctor policy only.
- `internal/mcp` and service MCP routes: managed add-on lifecycle, reconnect, hot reload, catalog persistence.
- `cmd/or3-intern/service_auth.go` and `cmd/or3-intern/service.go`: replay guard and Unix socket transport.
- `internal/agent`, `internal/agentcli`, `internal/db`: resilient jobs, durable state, hermetic runner detection.

## Core model

### Config readiness

Add a small readiness model derived from config, doctor findings, and bounded probes:

- `ready`: safe to run the requested command.
- `needs-repair`: known repair path exists.
- `draft`: setup is saved but required basics are missing.
- `advanced-custom`: runnable, but outside standard presets.

Normal commands should show the user state and one next action, not raw config errors.

### Startup contract

Each command gets an explicit readiness mode:

- `chat`: provider, DB, workspace, and core runtime must work.
- `serve`: chat readiness plus enabled channels/triggers must pass.
- `service`: service auth, bind, profile, and transport checks must pass.

Startup should use doctor results directly. If a finding blocks startup, the error should include the top fixes and point to `status` or `doctor`.

### Repair flow

Every finding should choose one fix mode:

- `automatic`: safe local change, such as creating directories or generating keys.
- `guided`: user must choose, such as disable service vs generate secret.
- `manual`: needs external action, such as installing a dependency or fixing provider billing.

Setup and settings should run the same repair engine after save.

### Managed integrations

Treat channels, MCP servers, webhooks, external runners, and service transport as managed add-ons.

Each add-on stores or reports:
- configured state
- last test result
- last successful connection
- disabled-for-safety reason
- restart/hot-reload requirement

Broken optional add-ons should not break core chat. Public ingress and unsafe tool exposure still fail closed.

### Service hardening

Add an in-memory bounded replay guard for shared-secret bearer token nonces. Then reserve optional claims for method/path/body binding without breaking current callers.

Add POSIX Unix socket service transport using the existing HTTP handler stack. Keep auth on by default for consistent behavior.

Persist bounded service job metadata in SQLite: ID, kind, status, timestamps, last error, and final preview.

## Data changes

Prefer additive schema only:

- Optional config metadata for readiness/probe timestamps if needed.
- `service_jobs` for durable job summaries.
- Optional `mcp_tool_catalog` for discovered MCP tools and last-known status.

Do not persist unbounded logs or full streamed output.

## Testing strategy

- Fixture configs assert doctor findings and startup accept/reject results.
- Fake provider and fake runner binaries make tests independent of local tools.
- Misconfiguration tests prove broken configs repair, quarantine, or refuse with one next action.
- Race tests cover job registry, background workers, and runtime session paths.
- Fuzz smoke covers service request decoding, webhook parsing, structured task parsing, and host policy endpoint parsing.
