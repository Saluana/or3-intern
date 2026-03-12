# Tasks

## 1. Preset-driven initialization (Req 1, 2, 11)
- [ ] Update `cmd/or3-intern/init.go` to present `dev-local`, `private-service`, and `exposed-ingress` as the primary setup choices.
- [ ] Add preset helper functions in `cmd/or3-intern/init.go` that start from `initDefaults` and set concrete values for `security`, `hardening`, `service`, `triggers`, and network posture.
- [ ] Change the provider credential flow in `cmd/or3-intern/init.go` to prefer env vars first, then secret-store-backed storage, with plain `config.json` storage only behind an explicit unsafe/local-only confirmation.
- [ ] Generate a strong service secret automatically for `private-service` and `exposed-ingress`, while keeping loopback-only bind for `private-service`.
- [ ] Update `cmd/or3-intern/init_test.go` with preset coverage and secret-handling flow tests.
- [ ] Update operator docs in `docs/cli-reference.md`, `docs/security-and-hardening.md`, and `README.md` to describe the new preset-first setup flow.

## 2. Startup enforcement gate (Req 3, 7, 8, 9, 10, 11)
- [ ] Refactor `cmd/or3-intern/doctor.go` so findings carry stable codes/severities and can be evaluated in command-aware enforcement modes.
- [ ] Add a startup validation helper in `cmd/or3-intern` that consumes effective config and rejects blocking findings before starting service, webhook, channels, or remote MCP.
- [ ] Integrate the startup validation helper into `cmd/or3-intern/main.go` after `setupSecurity` resolves secrets and outbound endpoint policy.
- [ ] Enforce service startup rejection for missing/weak secrets and non-loopback binds in hardened/exposed modes.
- [ ] Enforce webhook startup rejection for missing secret or missing effective profile.
- [ ] Enforce public/open-access ingress rejection when profiles are disabled or channel/trigger mappings are incomplete.
- [ ] Enforce privileged-capability profile rejection when `allowedTools` is empty.
- [ ] Expand `cmd/or3-intern/doctor_test.go` to cover blocking vs advisory outcomes and new rule codes.

## 3. Service auth replay protection (Req 4, 5, 14)
- [ ] Extend `cmd/or3-intern/service_auth.go` with a bounded in-memory replay guard that tracks nonce reuse for the token validity window.
- [ ] Thread the replay guard through `serviceAuthMiddleware` without breaking the current service handler architecture.
- [ ] Optionally add config-backed knobs for token max age and replay-cache size in `internal/config/config.go`, keeping current behavior backward-compatible by default.
- [ ] Reserve token-claim/request-validation hooks in `cmd/or3-intern/service_auth.go` and `cmd/or3-intern/service_request.go` for later method/path/body binding.
- [ ] Add regression tests in `cmd/or3-intern/service_test.go` for replay rejection, expiry, future timestamp, malformed tokens, and bad signatures.

## 4. Safer local service transport (Req 6, 11)
- [ ] Extend `internal/config/config.go` and config loading tests with optional service transport settings for Unix domain socket mode.
- [ ] Update `cmd/or3-intern/service.go` to support serving the existing HTTP handler stack over Unix sockets on POSIX while preserving current TCP loopback behavior.
- [ ] Add startup validation in `cmd/or3-intern/doctor.go` / `main.go` for invalid socket configuration and stale-path handling.
- [ ] Add focused tests for Unix-socket service startup and request handling where supported in `cmd/or3-intern/service_test.go`.
- [ ] Document same-host transport guidance in `docs/security-and-hardening.md` and `docs/cli-reference.md`.

## 5. Mandatory ingress profiles (Req 7, 9, 10, 11, 14)
- [ ] Audit and reuse existing effective-profile resolution helpers in `cmd/or3-intern/doctor.go` for service, webhook, and all enabled channels.
- [ ] Add startup enforcement so non-CLI ingress must resolve to an effective access profile in hardened/exposed modes.
- [ ] Add explicit denial rules for public ingress profiles that expose `exec` or `run_skill_script` without narrow allowlists.
- [ ] Keep runtime tool guarding in `internal/agent/runtime.go` unchanged except where needed to stay consistent with the stricter startup rules.
- [ ] Add regression tests in `cmd/or3-intern/doctor_test.go` for missing mappings, default/profile drift, and privileged profiles without allowlists.

## 6. Remote MCP network hardening (Req 8, 12, 14)
- [ ] Extend `cmd/or3-intern/doctor.go` to classify remote `sse` / `streamablehttp` MCP without deny-by-default host policy as blocking in hardened/exposed contexts.
- [ ] Reuse `validateConfiguredOutboundEndpoints` in `cmd/or3-intern/security_setup.go` as a required startup gate for enabled remote MCP servers.
- [ ] Add stricter checks for wildcard or overly broad `security.network.allowedHosts` when remote MCP is enabled.
- [ ] Update `internal/mcp/manager.go` tests to verify fail-closed behavior for invalid remote endpoint/network posture combinations.
- [ ] Update `docs/mcp-tool-integrations.md` and `docs/security-and-hardening.md` with the new mandatory host-policy requirements.

## 7. Privileged exec and skill execution hardening (Req 9, 10, 14)
- [ ] Refine `cmd/or3-intern/doctor.go` rules so privileged exec without Bubblewrap, empty `execAllowedPrograms`, or shell mode exposure become blocking in hardened/exposed startup.
- [ ] Decide whether strict startup should fail or auto-downgrade when Bubblewrap is missing; implement the chosen behavior consistently in `cmd/or3-intern/main.go` and docs.
- [ ] Add startup checks ensuring `skills.enableExec` requires `quarantineByDefault=true` and at least one trusted owner or registry in hardened modes.
- [ ] Add startup checks preventing public/open-access ingress from reaching `run_skill_script` unless explicitly allowed by a restrictive profile.
- [ ] Expand `cmd/or3-intern/doctor_test.go`, `cmd/or3-intern/security_setup_test.go`, and any relevant skill tests for these cases.

## 8. Durable service job history (Req 13)
- [ ] Add a SQLite migration and DB methods in `internal/db` for durable service job metadata storage.
- [ ] Extend `internal/agent/job_registry.go` or `cmd/or3-intern/service.go` to persist job lifecycle transitions without changing current streaming semantics.
- [ ] Store bounded job summaries only: status, timestamps, last error, truncated final output, and compact event summaries.
- [ ] Add SQLite-backed tests for service job persistence and recovery-friendly reads.
- [ ] Update service docs in `docs/api-reference.md` or `docs/cli-reference.md` if durable job inspection surfaces are added.

## 9. MCP completeness work (Req 12)
- [ ] Add bounded reconnect/backoff behavior for remote MCP sessions in `internal/mcp/manager.go`.
- [ ] Design and implement optional SQLite persistence for discovered remote tool catalog metadata in `internal/db` plus `internal/mcp`.
- [ ] Add a minimal hot reload/add/remove path for MCP servers that fits the current single-process manager lifecycle.
- [ ] Extend `internal/mcp/manager_test.go` with reconnect, persistence, and reload regression tests.
- [ ] Update `docs/mcp-tool-integrations.md` to reflect the new capabilities and any intentionally deferred gateway work.

## 10. Security regression and config fixture suite (Req 14, 15, 16)
- [ ] Add fixture configs under a testdata location for `safe-local`, `safe-private-service`, `unsafe-public-no-profiles`, `unsafe-remote-mcp-no-network`, and `unsafe-privileged-no-bwrap`.
- [ ] Add table-driven tests asserting both `doctor` findings and startup accept/reject behavior for each fixture.
- [ ] Add service auth regression tests that cover replay, expiry, future timestamps, and invalid signatures.
- [ ] Add endpoint/profile/network regression tests in `internal/security/network_test.go` and `cmd/or3-intern/doctor_test.go`.
- [ ] Add fuzz targets for service request decoding, webhook payload parsing, structured task parsing, and host policy endpoint parsing.

## 11. CI and release gates (Req 16)
- [ ] Add CI workflow steps for `go test ./...`, `go test -race ./...`, `staticcheck ./...`, and `gosec ./...`.
- [ ] Add a lightweight fuzz smoke stage for the security-sensitive fuzz targets.
- [ ] Define and enforce targeted coverage expectations for `cmd/or3-intern` auth/doctor paths and `internal/security`.
- [ ] Document the release gate expectations in repository docs or contributor guidance.

## 12. Out of scope for this plan
- [ ] Do not add a frontend, REST admin backend, or separate long-running gateway service solely for hardening.
- [ ] Do not introduce distributed nonce coordination or multi-process replay protection in the initial replay fix.
- [ ] Do not replace the existing service HTTP API format during the first auth-hardening pass.
- [ ] Do not require cross-host mTLS in the first same-host transport milestone unless a later phase explicitly prioritizes it.
