# External Agent CLI Delegation — Tasks

## 1. Configuration and bootstrap

- [x] Add `AgentCLIConfig` to `internal/config/config.go` with safe defaults for enablement, disabled runners, concurrency, queue size, timeout limits, mode/isolation defaults, chunk/preview limits, and persisted-output cap. (Req: 4, 5, 10, 13)
- [x] Add env overrides for `OR3_AGENT_CLI_*` keys using the existing `ApplyEnvOverrides` style. (Req: 13)
- [x] Normalize and validate `AgentCLIConfig` in config finalization, clamping timeout/chunk/preview/concurrency values and defaulting mode/isolation safely. (Req: 9, 10, 13)
- [x] Add config tests for defaults, env overrides, disabled runners, timeout clamping, and `allowSandboxAuto=false` by default. (Req: 4, 5, 13)
- [ ] Wire an `AgentCLIManager` into service startup in `cmd/or3-intern/main.go` only when the feature is enabled, similar to `SubagentManager`. (Req: 8, 9, 13)

## 2. SQLite schema and store methods

- [x] Add additive migrations for `agent_cli_runs`, `agent_cli_events`, and their indexes in `internal/db/db.go`. (Req: 8, 10)
- [x] Add `AgentCLIRun`, `AgentCLIEvent`, status constants, and queue-full errors in `internal/db/store.go` near existing subagent types. (Req: 8, 9)
- [x] Implement enqueue, limited enqueue, get by run/job ID, queued/running lists, claim-next, abort queued, mark interrupted, append event, list events after sequence, and finalize methods. (Req: 8, 9, 10)
- [x] Ensure enqueue creates/updates the parent session with `EnsureSession` semantics but does not create a child chat session in v1. (Req: 8)
- [x] Add DB tests for table creation, lifecycle, concurrent claim, event ordering, `after_seq`, restart reconciliation, and legacy migration compatibility. (Req: 8, 9, 10)

## 3. Runner registry and detection

- [x] Create `internal/agentcli` with runner IDs, statuses, auth statuses, support flags, runner specs, registry, and detection helpers. (Req: 1, 6)
- [x] Implement `exec.LookPath` binary detection separately from version/help probing and auth probing. (Req: 1)
- [x] Add short-timeout detection commands for OpenCode, Codex, Claude, and Gemini. (Req: 1)
- [x] Add auth checks for OpenCode, Codex, and Claude; mark Gemini auth as unknown when help/version works. (Req: 1)
- [x] Include `or3-intern` as an always-available runner option in detection responses. (Req: 14)
- [x] Add fake-binary detection tests for missing, disabled, version failure, auth ready, auth missing, and Gemini `auth_unknown`. (Req: 1)

## 4. Typed request and policy validation

- [x] Add strict service request decoding for `POST /internal/v1/agent-runs` in `cmd/or3-intern/service_request.go`. (Req: 6, 12)
- [x] Support snake_case and current service compatibility aliases only where intentional; reject unknown fields. (Req: 6)
- [x] Validate required `parent_session_key`, `runner_id`, and `task`; trim task but preserve internal prompt characters. (Req: 2, 3, 6)
- [x] Validate `cwd` against configured workspace/file roots and reject paths outside allowed roots. (Req: 6, 12)
- [x] Implement mode/isolation validation so `sandbox_auto` requires `sandbox_dangerous` and enabled sandbox policy. (Req: 4, 5)
- [x] Add tests for unsupported runner, unsupported mode, host dangerous rejection, disabled sandbox auto rejection, invalid cwd, timeout bounds, and unknown JSON fields. (Req: 4, 5, 6, 12)

## 5. Runner adapters and argv builders

- [x] Implement OpenCode adapter with `run --format json`, optional `--model`, and dangerous flag only for sandbox auto. (Req: 2, 3, 4, 7)
- [x] Implement Codex adapter with `exec --json --color never`, safe sandbox/approval flags, optional model, conditional git check skipping, and no `--full-auto`. (Req: 2, 3, 4, 7)
- [x] Implement Claude adapter with `--bare -p`, `stream-json`, verbose partial messages, permission-mode mapping, optional model, and optional `max_turns`. (Req: 2, 3, 4, 6, 7)
- [x] Implement Gemini adapter with `--prompt`, `--output-format json`, approval-mode mapping, optional model, and no duplicate yolo flags. (Req: 2, 3, 4, 7)
- [x] Add adapter tests asserting exact argv arrays for default, review, safe edit, and sandbox auto modes. (Req: 2, 3, 4)
- [x] Add shell-metacharacter tests showing prompts such as `"; rm -rf /"`, backticks, and `$()` remain one argv element. (Req: 3)

## 6. Environment and sandbox handling

- [ ] Add an agent CLI child-env helper that reuses the `tools.BuildChildEnv` allowlist pattern while adding `NO_COLOR=1` and `TERM=dumb`. (Req: 11)
- [ ] Strip OR3 internal secrets such as `OR3_INTERNAL_TOKEN`, `OR3_PAIRING_SECRET`, and `OR3_NODE_SECRET` from child environments even if inherited. (Req: 11, 12)
- [ ] Keep `HOME`, `PATH`, and temp variables available by default so CLIs can find installed binaries and auth files. (Req: 1, 11)
- [ ] For sandbox isolation, reuse or wrap the existing Bubblewrap command builder only when it preserves argv boundaries and configured writable roots. (Req: 3, 5, 12)
- [ ] Add tests for env allowlist behavior, forced no-color/TERM settings, secret stripping, and sandbox-auto rejection when sandbox readiness is false. (Req: 5, 11, 12)

## 7. Process manager and event streaming

- [ ] Implement `ProcessManager` with `exec.CommandContext`, validated cwd, sanitized env, stdout/stderr pipes, and no terminal PTY dependency. (Req: 2, 3, 9, 11)
- [ ] Add Unix process-group cancellation with SIGTERM then SIGKILL grace period in a platform-specific file. (Req: 9)
- [ ] Add Windows direct-kill implementation for v1 with clear TODO for Job Objects. (Req: 9)
- [ ] Stream stdout and stderr concurrently into bounded chunks and monotonic event sequences. (Req: 7, 9, 10)
- [ ] Maintain 64 KiB stdout/stderr preview ring buffers and final text preview. (Req: 10)
- [ ] Implement best-effort JSON/JSONL parsing per adapter while always publishing raw `output` events. (Req: 7)
- [ ] Emit `started`, `output`, `structured`, `output_truncated`, `completion`, and `error` events with timestamps and sequence numbers. (Req: 7, 10, 14)
- [ ] Add lifecycle tests for spawn failure, exit zero, nonzero exit, timeout, cancel, stdout/stderr streaming, long chunk splitting, parser failure, and monotonic seq. (Req: 7, 9, 10)

## 8. Agent CLI manager

- [ ] Implement `AgentCLIManager.Start`/`Stop` with worker goroutines, queued-job resume, and running-job restart reconciliation. (Req: 8, 9)
- [ ] Implement `Enqueue` to validate policy, detect selected runner readiness, persist run, register live job, publish `queued`, and signal workers. (Req: 1, 4, 5, 8)
- [ ] Implement worker claim/execute/finalize path that builds commands through adapters and records events in both SQLite and `JobRegistry`. (Req: 2, 3, 7, 8, 9, 10)
- [ ] Implement `Abort` for queued and running CLI jobs, using `JobRegistry.Cancel` when running and DB status update when queued. (Req: 9)
- [ ] Ensure manager errors are public-safe in API responses and detailed only in logs/audit where appropriate. (Req: 12)
- [ ] Add manager integration tests with temp fake CLIs on `PATH` for success, stderr failure, JSONL output, sleep timeout, and abort. (Req: 7, 8, 9)

## 9. Service app, control-plane, and HTTP APIs

- [ ] Add `ServiceApp` methods for runner detection, starting CLI runs, reading persisted CLI runs, subscribing/listing events, and abort delegation. (Req: 8, 12, 14)
- [ ] Add control-plane response builders for runner info, CLI run snapshots, CLI event lists, and normalized job snapshots. (Req: 8, 14)
- [ ] Register `GET /internal/v1/agent-runners` and `POST /internal/v1/agent-runs` in `newServiceMux`. (Req: 1, 6, 12, 14)
- [ ] Add `GET /internal/v1/agent-runs/:id` and, if needed for durable reconnect, `GET /internal/v1/agent-runs/:id/events?after_seq=N`. (Req: 8, 10, 14)
- [ ] Extend `/internal/v1/jobs/:job_id` fallback lookup to include persisted CLI runs after subagent snapshots. (Req: 8, 14)
- [ ] Extend `/internal/v1/jobs/:job_id/abort` to cancel external CLI jobs through the same process manager path. (Req: 9, 14)
- [ ] Ensure all new routes use existing service auth, mutation rate limits, body limits, browser boundary middleware, and audit logging. (Req: 12)
- [ ] Add service contract fixtures/tests for runner list, start run, stream run, read persisted snapshot, durable event reconnect, cancel, and error responses. (Req: 1, 6, 8, 9, 12, 14)

## 10. Job snapshot normalization and app contract notes

- [ ] Normalize CLI snapshots with `kind=agent_cli:<runner_id>`, `runner_id`, task/title preview, status, output preview, error preview, timestamps, and attempts. (Req: 8, 14)
- [ ] Include `argv_preview` only in `started` events and redact/omit sensitive values. (Req: 12, 14)
- [ ] Document app-side expectations for runner dropdown behavior, unavailable runner display, output/stderr panels, copy output/final result actions, raw events debug view, retry preservation, and cancel behavior. (Req: 14)
- [ ] Confirm `or3-intern` remains the default runner path in UI contracts and still submits to existing subagent/turn endpoints. (Req: 14)

## 11. Documentation and manual verification

- [ ] Update `docs/api-reference.md` with `/agent-runners`, `/agent-runs`, event payloads, mode/isolation policy, and cancellation semantics. (Req: 12, 14)
- [ ] Update `docs/security-and-hardening.md` with external CLI child-process risks, safe defaults, env stripping, and sandbox-auto restrictions. (Req: 4, 5, 11, 12)
- [ ] Update `docs/configuration-reference.md` with `agentCLI` config and `OR3_AGENT_CLI_*` env vars. (Req: 13)
- [ ] Add manual verification notes for real OpenCode, Codex, Claude, and Gemini commands before enabling structured parsers beyond best-effort. (Req: 7)
- [ ] Run focused Go tests for new packages, then `go test ./...`, then the existing `go build ./...` task. (Req: all)

## Out of scope for v1

- [ ] Do not implement pseudo-terminal parsing of external CLI approval prompts. (Req: 2, 5)
- [ ] Do not pass raw user-provided runner flags through the API. (Req: 3, 6)
- [ ] Do not enable host-machine yolo/dangerous mode by default. (Req: 4, 5)
- [ ] Do not build copied-workspace diff application unless the sandbox runtime already exists and is explicitly selected for this milestone. (Req: 5)
- [ ] Do not automatically write external CLI output into memory or chat history beyond normalized job snapshots. (Req: 8)
