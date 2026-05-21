# Consumer Grade Stability Requirements

## Overview

Make `or3-intern` behave like consumer software: users can configure it incorrectly without ending up in a broken, confusing, or unsafe experience. Every startup and settings path should either repair itself, disable the broken feature safely, or refuse to proceed with one clear next action.

Scope:
- Config readiness and migration behavior.
- Setup, provider, model, and secret handling.
- Doctor/status/startup enforcement.
- Channels, MCP, service mode, devices, and external runners.
- Runtime resilience, degraded mode, and release gates.

## Requirements

1. Config must have an explicit readiness state.
   - Acceptance criteria:
     - Config evaluates to `ready`, `needs-repair`, `draft`, or `advanced-custom`.
     - `chat`, `serve`, and `service` refuse only with plain-language fixes.
     - Old or partial configs are normalized or loaded in repair mode instead of dead-ending.

2. Setup must prove the basics work before claiming success.
   - Acceptance criteria:
     - Provider endpoint, API key, chat model, and embedding model are validated with bounded probes.
     - Missing provider credentials produce a saved draft, not a "ready" setup.
     - API keys default to env or secret-store storage; plain config storage is explicit local-only.

3. Doctor and status must be the single repair surface.
   - Acceptance criteria:
     - Every finding has severity, user explanation, fix hint, and automatic/guided/manual fix mode.
     - Safe repairs can run from `status --fix`, `doctor --fix`, or setup completion.
     - Startup consumes the same doctor report and has no duplicate policy rules.

4. Integrations must fail closed and recover clearly.
   - Acceptance criteria:
     - Channels, MCP, webhooks, service, and external runners expose states: off, needs setup, connected, degraded, disabled for safety.
     - Invalid integration config is saved disabled or blocked before startup.
     - MCP supports bounded reconnect/backoff, hot reload, and persisted tool catalog metadata.

5. Service and device mode must be hard to expose unsafely.
   - Acceptance criteria:
     - Service bearer tokens reject nonce replay inside the validity window.
     - Same-host service can use Unix sockets on POSIX while keeping loopback TCP compatibility.
     - Non-CLI ingress requires effective access profiles in hardened modes.
     - Service job state is durably visible across restarts.

6. Runtime failures must degrade without corrupting user experience.
   - Acceptance criteria:
     - Optional broken subsystems do not crash core chat.
     - In-flight background jobs survive panics, terminal events, and restarts cleanly.
     - User-facing responses never expose raw stack traces, secrets, or internal-only IDs by default.

7. Stability must be enforced by tests and release gates.
   - Acceptance criteria:
     - `go test ./...` is hermetic and does not depend on local Codex/OpenCode/Gemini/Claude installs.
     - Fixture configs cover safe and unsafe local, private-service, exposed-ingress, MCP, and privileged-exec states.
     - CI runs tests, race checks for runtime/job paths, static analysis, security analysis, and fuzz smoke for parsers.

## Non-functional constraints

- Keep the current Go, SQLite, single-process architecture.
- Prefer repair, quarantine, or explicit refusal over permissive fallback.
- Keep all probes bounded by timeouts.
- Do not silently widen privileges to make a broken config run.
- Preserve advanced escape hatches, but do not make JSON editing the normal path.
