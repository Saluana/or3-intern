# Security and hardening

## Overview

The README describes hardening in three layers. The defaults are meant to keep powerful tools and external exposure opt-in.

## Phase 1 defaults

Phase 1 establishes the baseline runtime posture:

- file tools stay workspace-rooted by default
- external channels stay closed unless explicitly enabled
- child processes receive a scrubbed environment allowlist
- `exec` prefers `program` plus `args`
- legacy shell execution is disabled unless `hardening.enableExecShell=true`
- service actions are checked against capability tiers and approval policy
- channel peers can be isolated by sender with `hardening.isolateChannelPeers=true`

## Phase 2 additions

Phase 2 adds tighter controls around autonomous execution and skill/script execution:

- skills declare permission metadata and tool allowlists
- script-capable skills default to quarantine until approved
- heartbeat, webhook, and file-watch turns carry a bounded `structured_event`
- privileged shell execution can route through Bubblewrap
- `or3-intern doctor` is the main readiness and repair command and supports `--strict`, `--json`, and `--fix`

## Phase 3 additions

Phase 3 adds stronger operational controls:

- encrypted secret references via `secret:<name>`
- HMAC-chained audit records with `or3-intern audit verify`
- named access profiles for channels and triggers
- outbound host restrictions through `security.network`

## Runtime profiles

Set `runtimeProfile` in `config.json` (or override with `OR3_RUNTIME_PROFILE`) to declare the intended execution posture:

| Profile                      | Intent                                                                                                          |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `local-dev`                  | Permissive defaults for local development; no additional security requirements enforced.                        |
| `single-user-hardened`       | Personal server with tighter defaults; recommended for self-hosted single-user deployments.                     |
| `hosted-service`             | Multi-user or public-facing service; secret-store, audit, and network policy are all required at startup.       |
| `hosted-no-exec`             | Hosted service with shell execution disabled; `enableExecShell` and `privilegedTools` are forbidden.            |
| `hosted-remote-sandbox-only` | Hosted service where exec is only permitted inside a sandbox; startup fails if exec is enabled without sandbox. |

Hosted profiles (`hosted-*`) run strict validation at startup — `or3-intern serve` and `or3-intern service` will refuse to start if `security.secretStore`, `security.audit`, or `security.network` are absent or disabled.

Hosted profiles also bias the runtime toward explicit opt-in:

- executable skills stay quarantined by default even if `skills.policy.quarantineByDefault` is unset
- `hosted-no-exec` does not advertise local exec-capable behavior to the skill inventory
- `hosted-remote-sandbox-only` only advertises local exec-capable tools when Bubblewrap sandboxing is enabled

`or3-intern doctor` warns when `runtimeProfile` is not set, flags constraint violations for the active profile, and groups blockers, warnings, and fixable findings into a single readiness report.

## Core config sections

### `runtimeProfile`

Named execution posture; selects startup validation rules. One of: `local-dev`, `single-user-hardened`, `hosted-service`, `hosted-no-exec`, `hosted-remote-sandbox-only`.

### `hardening`

- `guardedTools`
- `privilegedTools`
- `enableExecShell`
- `execAllowedPrograms`
- `childEnvAllowlist`
- `isolateChannelPeers`
- `sandbox`
- `quotas` (legacy persisted config; runner-first runtime does not enforce host tool quotas)

### `security.secretStore`

- `enabled`
- `required`
- `keyFile`

### `hardening.quotas`

Quota fields are retained for config compatibility with pre-runner-first installs. The runner-first runtime does not enforce host-local model tool quotas because tool execution is delegated to external runners and service/runner permission checks.

Use runner-specific limits, service capability ceilings, access profiles, and approvals for current runtime control.

### `security.audit`

- `enabled`
- `strict`
- `keyFile`
- `verifyOnStart`

### `security.profiles`

- `enabled`
- `default`
- `channels`
- `triggers`
- `profiles`

Each named profile can control:

- `maxCapability`
- `allowedHosts`
- `writablePaths`

### `security.network`

- `enabled`
- `defaultDeny`
- `allowedHosts`
- `allowLoopback`
- `allowPrivate`

## Operational guidance

The README's rollout guidance recommends:

- enabling the secret store before moving secrets to `secret:<name>` references
- verifying the audit chain before enforcing verify-on-start behavior
- starting with permissive network/profile rules, then tightening after observing real usage
- keeping service mode on loopback or private networking only

## Phase 4 additions — approval and pairing system

Phase 4 adds an explicit approval and pairing layer for runtime execution and device access:

- a single internal approval broker owns all approval state, allowlist rules, and audit events
- `exec` and runner permission requests enforce approval in the host-local execution path, not only in planning
- remote operator and service clients pair with the host using a six-digit one-time code exchanged through the existing service listener
- short-lived HMAC-signed approval tokens bind to the canonical subject hash and host ID
- paired device tokens are stored hashed and immediately invalidated on revocation or rotation
- all approval and pairing events append to the existing audit chain

See [Configuration reference — security.approvals](configuration-reference.md#securityapprovals) for the config options.

### Approval modes

Each domain (`exec`, `skillExecution`, `pairing`, etc.) can be set independently:

| Mode        | Effect                                                                              |
| ----------- | ----------------------------------------------------------------------------------- |
| `deny`      | Execution is blocked unconditionally.                                               |
| `ask`       | Execution is blocked until an operator resolves the pending request.                |
| `allowlist` | Execution is allowed if a matching rule exists; otherwise blocked pending approval. |
| `trusted`   | Execution is allowed and audited without prompting.                                 |

### Canonical subject binding

Approval tokens are not reusable across different execution contexts. A token is bound to:

- the exact executable path or skill identity
- the argument vector, working directory, and env binding hash
- the host ID
- a short expiry window

Changing any of these fields changes the subject hash and invalidates the token.

### Operator guidance

#### Approval expiration

Pending approval requests expire after `pendingTtlSeconds` (default: 3600 s). After expiry, the next execution attempt creates a fresh request. Operators can view pending requests with:

```bash
or3-intern approvals list
or3-intern approvals list pending
```

Expired requests are visible with `or3-intern approvals list expired`.

Approval tokens expire after `approvalTokenTtlSeconds` (default: 300 s). Once a token expires the tool must be re-evaluated — the pending request is reused if it has not also expired.

#### Revoked devices

Revoking a device immediately invalidates its bearer token for all future API requests. To revoke a paired device:

```bash
or3-intern devices list
or3-intern devices revoke <device-id>
```

Revocation is recorded in the audit chain with the acting operator identity. The device cannot be re-activated; a new pairing request must be created and approved.

Token rotation (`or3-intern devices rotate <device-id>`) replaces the current token with a new one without requiring a new pairing flow.

#### Offline CLI operation

Approval commands operate directly against the local SQLite database. The HTTP service listener does not need to be running. This means:

- `or3-intern approvals list/approve/deny` works without `or3-intern service`
- secure device enrollment requires the HTTP service to be running

### Startup validation

`or3-intern doctor` checks for common misconfigurations and local runtime readiness problems:

- `ask` or `allowlist` domains require a valid `keyFile`
- `hostId` should be set explicitly in production deployments
- service mode exposed beyond loopback should have approvals enabled for sensitive domains

Run `or3-intern doctor --strict` to fail on any warning, `or3-intern doctor --json` for CI-friendly output, and `or3-intern doctor --fix` to apply safe automatic repairs.

### Future phases

The following capabilities are designed to be compatible with the v1 approval and pairing system but are **not** implemented in the first pass:

- **Web UI approvals** — resolving requests from a browser dashboard
- **Chat approvals** — routing approval prompts through Telegram, Slack, Discord, or other channels
- **Secret-access approvals** — gating reads of named secrets through the approval broker
- **Outbound-message approvals** — gating channel sends through the approval broker
- **Sandbox verification** — `or3-sandbox` reusing approval tokens and subject hashes issued by this host
- **Remote-node forwarding** — multi-node environments sharing a signing key and approving from a central operator

These areas deliberately extend the existing schema, token format, and audit chain without breaking v1 behavior.

## Doctor command

Use:

```bash
or3-intern doctor
or3-intern doctor --strict
or3-intern doctor --fix
or3-intern doctor --fix --interactive
```

Run this before enabling external channels, webhook listeners, privileged tools, or service mode.

## App Doctor/Admin repair safety

The app repair flow uses the same service auth policy as the rest of OR3:

- read-only Doctor status, run, logs, Admin Brain availability, and metadata routes are operator-readable
- plan apply and rollback are sensitive routes and require a session plus recent step-up verification when policy requires it
- warning/danger plans show exact config diffs before apply
- the app no longer uses typed confirmation as the default approval path for warning/danger plans
- if neither passkey nor PIN-backed verification is available, the app directs the operator to set up passkeys/PIN before applying blocked changes
- remembered warning approval is bounded to 5 minutes and scoped by actor/device/action family/risk/scope through the approval context

Applied plans write audit entries, checkpoints, and rollback records. Restart-required plans reuse `/internal/v1/actions/restart-service`; the app suppresses expected reconnect noise while the service restarts and then resumes Doctor post-checks.

Manual fallbacks:

- **No AI/Admin Brain**: Basic Doctor still runs deterministic checks and displays recommended fixes.
- **Failed restart**: restart OR3 manually, then run the plan post-checks from the app.
- **Failed rollback**: use the rollback instructions stored with the plan and avoid repeated automatic rollback attempts.
- **Blocked danger change**: set up passkey/PIN verification or use the CLI/host console with an admin recovery path.

## External agent CLI delegation

The external agent CLI subsystem spawns child processes (OpenCode, Codex, Claude, Gemini) from the service. The following controls apply:

### Safe defaults

- The subsystem is **always active**. Runner orchestration is the only supported execution path — there is no `runners.enabled` config switch.
- The default mode is `safe_edit` — non-interactive edits with each CLI's built-in safety flags. No full-autonomy/yolo behaviour.
- `sandbox_auto` mode is rejected unless `runners.allowSandboxAuto: true` **and** the isolation is `sandbox_dangerous` (a true sandbox runtime, not the host filesystem).
- Host-machine runs are strictly limited to `review` (read-only) and `safe_edit` (workspace write only).

### Environment sanitization

- Child processes receive a **scrubbed environment allowlist**. The default list includes `PATH`, `HOME`, and `TMPDIR` so CLIs can find their auth/config directories, but forces `NO_COLOR=1` and `TERM=dumb`.
- The following are **explicitly stripped** even if they appear in the configured allowlist:

  | Blocked key | Reason |
  |-------------|--------|
  | `OR3_INTERNAL_TOKEN` | Internal service authentication |
  | `OR3_PAIRING_SECRET` | Device pairing secret |
  | `OR3_NODE_SECRET` | Node identity secret |
  | `OR3_SERVICE_SECRET` | Service shared secret |
  | `OR3_API_KEY` | Provider API key |
  | `OPENAI_API_KEY` | Provider API key |

- Child env is built via `tools.BuildChildEnv` with an overlay, never shell interpolation.
- `argv_preview` in API responses and events is a redacted copy of the command arguments; raw command lines are never returned.

### Process lifecycle

- Every run runs under `exec.CommandContext`, not a shell. Prompt text remains a single `argv` element regardless of metacharacters.
- On Unix, child processes are placed in their own process group (`Setpgid: true`). Cancellation sends `SIGTERM` to the group, then `SIGKILL` after a 2-second grace period.
- Run timeouts are bounded: request minimum 30 seconds, server-side maximum 7200 seconds, default 900 seconds.
- On service restart, queued runs resume and previously-running runs are marked `aborted` — child processes cannot be safely reattached.

### Output bounding

- Event chunks are capped at 16 KiB before publication or storage.
- Stdout and stderr previews retain at most 64 KiB each (ring buffer).
- Persisted full output is bounded by `runners.maxPersistedOutputBytes` (default 10 MiB).
- Truncation is explicit: `output_truncated` events record dropped byte counts.

## Doctor command

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Skills](skills.md)
- [MCP tool integrations](mcp-tool-integrations.md)
- [Internal service API reference](api-reference.md)
- [CLI reference](cli-reference.md)

## Related code

- `internal/security/`
- `internal/approval/`
- `internal/agentcli/` (runner registry, env sanitization, process manager)
- `cmd/or3-intern/doctor.go`
- `cmd/or3-intern/security_setup.go`
- `cmd/or3-intern/approvals_cmd.go`
- `cmd/or3-intern/devices_cmd.go`
