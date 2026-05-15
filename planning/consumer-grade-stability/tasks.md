# Consumer Grade Stability Tasks

## 1. Readiness contract

- [x] Add a config/runtime readiness model: `ready`, `needs-repair`, `draft`, `advanced-custom`. Requirements: 1.
- [x] Make `chat`, `serve`, and `service` each declare required readiness checks. Requirements: 1, 3.
- [x] Ensure invalid config loads in repair mode wherever possible. Requirements: 1.
- [x] Add fixture configs for ready, draft, repairable, unsafe, and advanced-custom states. Requirements: 1, 7.

## 2. Setup that proves basics

- [x] Add bounded provider probes for endpoint, API key, chat model, and embedding model. Requirements: 2.
- [x] Split setup completion into "ready to chat" vs "saved draft". Requirements: 2.
- [x] Change provider secret flow to prefer env vars, then secret store, then explicit local-only config storage. Requirements: 2.
- [x] After setup save, run doctor/repair summary and offer safe fixes immediately. Requirements: 2, 3.

## 3. Doctor and startup repair loop

- [x] Audit all doctor findings for summary, detail, fix hint, and fix mode. Requirements: 3.
- [x] Move remaining startup-only policy into doctor checks. Requirements: 3.
- [x] Expand automatic fixes for safe local repairs: dirs, keys, quotas, default binds, disabled broken ingress. Requirements: 3.
- [x] Expand guided fixes for provider, service secret, webhook secret, profiles, sandbox, and channel ingress. Requirements: 3.

## 4. Managed integration lifecycle

- [x] Add common integration state labels for channels, MCP, webhooks, service, and runners. Requirements: 4.
- [x] Save invalid optional integrations disabled or quarantined instead of runnable-broken. Requirements: 4.
- [x] Add MCP reconnect/backoff for remote transports. Requirements: 4.
- [x] Add MCP hot reload/add/remove without full process restart where safe. Requirements: 4.
- [x] Persist bounded MCP tool catalog/status metadata in SQLite. Requirements: 4.

## 5. Service and device hardening

- [x] Add bounded replay protection for service bearer token nonces. Requirements: 5.
- [x] Reserve optional token claims for method/path/body binding. Requirements: 5.
- [x] Add POSIX Unix socket service transport using existing HTTP handlers. Requirements: 5.
- [x] Require effective access profiles for non-CLI ingress in hardened modes. Requirements: 5.
- [x] Persist bounded service job summaries across restarts. Requirements: 5.

## 6. Runtime resilience

- [x] Verify background worker panic recovery and terminal job publish safety are covered by tests. Requirements: 6.
- [x] Add graceful degraded-mode behavior for optional subsystem failures. Requirements: 6.
- [x] Ensure user-facing errors are translated and redact secrets/internal details by default. Requirements: 6.
- [x] Add recovery guidance for provider outages, broken tools, missing runner auth, and DB/artifact problems. Requirements: 6.

## 7. Hermetic tests and release gates

- [x] Make runner discovery tests use fake binaries and controlled env only. Requirements: 7.
- [x] Add doctor/startup fixture tests for local, private-service, exposed-ingress, remote MCP, and privileged exec. Requirements: 7.
- [x] Add misconfiguration chaos tests proving repair, quarantine, or clear refusal. Requirements: 7.
- [x] Add CI gates for `go test ./...`, targeted `go test -race`, staticcheck, gosec, and fuzz smoke. Requirements: 7.
- [x] Document the release gate and "consumer-grade stability" definition of done. Requirements: 7.

## Out of scope

- [x] Do not add a new daemon, frontend, or service architecture solely for this work.
- [x] Do not require distributed state for replay protection.
- [x] Do not silently relax security to make startup pass.
- [x] Do not remove advanced config access; hide it from normal recovery paths.
