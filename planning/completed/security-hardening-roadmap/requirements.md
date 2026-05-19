# Requirements

## Overview

This plan raises or3-intern’s security-by-default posture, closes the most important service-mode auth gap, improves operational completeness for exposed or automated deployments, and increases confidence through explicit startup enforcement and regression coverage.

Scope is intentionally aligned to the current Go 1.22, CLI-first, single-process architecture. The plan assumes:

- the primary hardening surface is the existing CLI/runtime in `cmd/or3-intern`
- configuration continues to load from the current JSON + env override model in `internal/config`
- persistence remains SQLite-only and deterministic
- non-CLI ingress includes service mode, webhook triggers, channels, and remote MCP integrations
- new work should prefer extending `doctor`, `init`, `security_setup`, `service_auth`, and existing internal packages rather than introducing new daemons or frameworks

## Requirements

1. **Security presets become the primary initialization path**
   - The `init` flow must offer at least three named security presets: `dev-local`, `private-service`, and `exposed-ingress`.
   - Each preset must map to concrete existing config sections, including `security`, `hardening`, `service`, `triggers`, and `tools.mcpServers` defaults where relevant.
   - Acceptance criteria:
     - Running `init` presents the three presets before service-facing questions.
     - `dev-local` preserves today’s low-friction local behavior.
     - `private-service` enables secret store, audit, profiles, strong service secret generation, and loopback-only service binding by default.
     - `exposed-ingress` enables the same controls plus deny-by-default outbound network policy and stricter ingress/profile defaults.

2. **Bootstrap secret handling must avoid plain config by default**
   - `init` must prefer environment variables for provider credentials, then encrypted secret-store-backed references, and only allow plain config storage behind an explicit unsafe local-only decision.
   - Acceptance criteria:
     - The default `init` path does not prompt to write provider API keys directly into `config.json` as the primary option.
     - If the user selects secret-store-backed storage, the flow either stores the secret successfully or fails with a clear error when the secret store is required.
     - Plain config storage is labeled unsafe/local-only in CLI output and docs.

3. **Doctor strict mode must become an enforceable startup gate**
   - Startup paths for service-facing and externally reachable modes must reject configurations that `doctor --strict` classifies as blocking risks.
   - Acceptance criteria:
     - Service startup fails when the shared secret is missing/weak or the bind is non-loopback in strict exposed modes.
     - Webhook startup fails when a secret is missing or no effective access profile applies.
     - Privileged exec-capable startup fails when sandbox prerequisites are not met.
     - Remote HTTP MCP startup fails when deny-by-default network policy is not active.
     - Exposed-ingress startup fails when profiles are disabled or ingress mappings are incomplete.

4. **Service authentication must reject replayed bearer tokens**
   - Service-mode auth must reject nonce reuse within the token validity window.
   - Replay protection must fit the repo’s single-process runtime and remain bounded in memory.
   - Acceptance criteria:
     - A valid token is accepted once and rejected on reuse within the replay window.
     - Expired, future-dated, malformed, and bad-signature tokens continue to be rejected.
     - Replay tracking does not require a new external service.

5. **Service auth should support tighter request binding without breaking current callers abruptly**
   - The service auth design must leave room for optional binding to request method/path/body hash or a one-time signing mode.
   - Acceptance criteria:
     - The initial replay-safe implementation does not break existing service clients by default.
     - Follow-up hardening hooks are identified in config or token claims design without requiring a protocol rewrite in the same phase.

6. **A safer local transport path must exist for OR3 Net style same-host integration**
   - The system must define a repo-aligned path for non-TCP local integration, preferably Unix domain sockets on POSIX, while keeping current loopback service mode available.
   - Acceptance criteria:
     - The design specifies how same-host service access works without loopback TCP.
     - Service startup and auth behavior for the new local transport are defined without introducing a separate backend service.
     - Cross-host mTLS is explicitly deferred unless justified as a later phase.

7. **Access profiles must be mandatory for non-CLI ingress**
   - Service, webhook, enabled channels, and tool-capable triggers must resolve to an effective access profile whenever they are enabled in hardened modes.
   - Profiles that allow privileged capability must also specify an explicit allowed-tool list.
   - Acceptance criteria:
     - Doctor/startup detect missing default/channel/trigger mappings for enabled ingress.
     - Open-access channels without effective profiles are rejected in hardened startup modes.
     - Profiles with `maxCapability=privileged` and no `allowedTools` are rejected in strict enforcement paths.

8. **Remote MCP over HTTP must require deny-by-default outbound policy**
   - Enabling MCP servers with `sse` or `streamablehttp` transport must require active outbound host policy with `defaultDeny=true`.
   - Configured endpoints must be validated at startup and broad allowlists must be rejected in strict modes.
   - Acceptance criteria:
     - Remote MCP startup fails when `security.network.enabled` is false.
     - Remote MCP startup fails when `defaultDeny` is false.
     - Literal `*` and other overly broad allowlists are rejected in strict or exposed modes.
     - Endpoint validation runs before MCP connection attempts.

9. **Privileged exec must be impossible without sandbox prerequisites**
   - Privileged tool execution must not remain merely warned-about when sandboxing is absent or exec allowlists are empty.
   - Acceptance criteria:
     - Strict startup rejects privileged exec when Bubblewrap is unavailable or disabled.
     - Guarded/privileged exec startup rejects empty `execAllowedPrograms`.
     - Shell mode remains off by default and is only allowed behind explicit configuration.
     - Public ingress cannot reach `exec` or `run_skill_script` unless a profile explicitly allows it.

10. **Managed skill execution must default to a public-safe posture**
    - Public or open-access ingress must not reach `run_skill_script` unless explicitly and narrowly configured.
    - Managed skill execution must require quarantine-by-default and a non-empty trust policy when enabled.
    - Acceptance criteria:
      - Doctor/startup reject `skills.enableExec=true` with `quarantineByDefault=false` in hardened modes.
      - Doctor/startup reject managed skill execution with empty `trustedOwners` and `trustedRegistries` in hardened modes.
      - Public ingress profile mapping to `run_skill_script` is rejected unless explicitly enabled and documented.

11. **Expose a first-class hardened operating mode for service-facing deployments**
    - The repo must provide a coherent “exposed” operating mode that flips the existing config surface into a hardened baseline in one step.
    - Acceptance criteria:
      - The chosen preset or mode enables profiles, secret store, strict audit, startup verification, strong shared secret handling, and remote MCP/network hardening together.
      - Risky tool families remain disabled unless explicitly re-enabled.
      - Documentation describes the difference between local, private, and exposed deployment modes.

12. **MCP integration completeness must improve within the existing runtime model**
    - The current MCP manager should gain the next operational features already acknowledged as missing in docs: reconnect/backoff, persistent tool catalog, and hot reload/add/remove.
    - Acceptance criteria:
      - A reconnect/backoff strategy is defined for remote MCP transports.
      - Tool catalog persistence requirements identify SQLite tables/migrations when tool discovery state is stored.
      - Hot add/remove behavior is described without requiring a separate gateway service in the same phase.

13. **Service mode should persist job metadata for operational completeness**
    - Service jobs should have durable metadata in SQLite to support operational visibility across process restarts.
    - Acceptance criteria:
      - Job state transitions, timestamps, last error, and truncated final output are persisted.
      - The persistence design remains bounded and replay-safe.
      - Existing in-memory streaming/abort behavior remains compatible during rollout.

14. **Security-sensitive behavior must become a maintained regression contract**
    - Warnings and enforcement rules added for doctor/startup/auth/network/profile policy must be backed by targeted automated tests.
    - Acceptance criteria:
      - Table-driven regression tests cover doctor findings for the risky configurations called out in this plan.
      - Service auth tests cover expiry, future timestamps, bad signatures, malformed tokens, and replay rejection.
      - Startup enforcement tests cover accept/reject behavior for hardened and unsafe fixture configs.

15. **Security-sensitive parsing paths should have fuzz coverage**
    - Security-relevant decoders and parsers should gain fuzz targets where inputs can come from users, channels, or remote systems.
    - Acceptance criteria:
      - Fuzz targets are defined for service request decoding, structured task parsing, webhook payload parsing, and host policy endpoint parsing.
      - The fuzz scope remains bounded and suitable for Go’s built-in fuzzing workflow.

16. **Release confidence should include explicit CI quality gates**
    - CI/release validation should include correctness and security checks for the high-risk code paths in this repo.
    - Acceptance criteria:
      - The plan defines gates for `go test ./...`, race tests, static analysis, security analysis, and targeted coverage expectations for auth/doctor/security packages.
      - Fixture-based config validation is included in the proposed test strategy.

## Non-functional constraints

- Preserve deterministic, single-process behavior; avoid adding distributed state or external coordination services.
- Keep memory usage bounded, including nonce replay tracking, job history retention, tool output, and reconnect state.
- Maintain SQLite compatibility with the current migration model; schema changes must be additive and backward-compatible.
- Preserve current config loading and env override behavior; new settings must have safe defaults and sensible upgrade paths.
- Keep tool execution, file access, network access, and secret handling safe by default.
- Avoid widening ingress privileges during migration; stricter defaults must fail closed for exposed modes.
- Maintain session isolation and profile-based tool gating across service, webhook, channels, and subagents.
- Keep MCP/network validation cautious and bounded; endpoint checks must avoid unbounded retries or permissive wildcard behavior.
