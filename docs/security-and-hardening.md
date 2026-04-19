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
- tool calls are checked against capability tiers and bounded per-session quotas
- channel peers can be isolated by sender with `hardening.isolateChannelPeers=true`

## Phase 2 additions

Phase 2 adds tighter controls around autonomous execution and skill/script execution:

- skills declare permission metadata and tool allowlists
- script-capable skills default to quarantine until approved
- heartbeat, webhook, and file-watch turns carry a bounded `structured_event`
- privileged shell execution and `run_skill_script` can route through Bubblewrap
- `or3-intern doctor` audits common unsafe settings and supports `--strict`

## Phase 3 additions

Phase 3 adds stronger operational controls:

- encrypted secret references via `secret:<name>`
- HMAC-chained audit records with `or3-intern audit verify`
- named access profiles for channels and triggers
- outbound host restrictions through `security.network`

## Runtime profiles

Set `runtimeProfile` in `config.json` (or override with `OR3_RUNTIME_PROFILE`) to declare the intended execution posture:

| Profile | Intent |
| --- | --- |
| `local-dev` | Permissive defaults for local development; no additional security requirements enforced. |
| `single-user-hardened` | Personal server with tighter defaults; recommended for self-hosted single-user deployments. |
| `hosted-service` | Multi-user or public-facing service; secret-store, audit, and network policy are all required at startup. |
| `hosted-no-exec` | Hosted service with shell execution disabled; `enableExecShell` and `privilegedTools` are forbidden. |
| `hosted-remote-sandbox-only` | Hosted service where exec is only permitted inside a sandbox; startup fails if exec is enabled without sandbox. |

Hosted profiles (`hosted-*`) run strict validation at startup — `or3-intern serve` and `or3-intern service` will refuse to start if `security.secretStore`, `security.audit`, or `security.network` are absent or disabled.

Hosted profiles also bias the runtime toward explicit opt-in:

- executable skills stay quarantined by default even if `skills.policy.quarantineByDefault` is unset
- `hosted-no-exec` does not advertise local `exec` or `run_skill_script` capability to the skill inventory
- `hosted-remote-sandbox-only` only advertises local exec-capable tools when Bubblewrap sandboxing is enabled

`or3-intern doctor` warns when `runtimeProfile` is not set, and flags constraint violations for the active profile.

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
- `quotas`

### `security.secretStore`

- `enabled`
- `required`
- `keyFile`

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
- `allowedTools`
- `allowedHosts`
- `writablePaths`
- `allowSubagents`

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
- `exec` and `run_skill_script` enforce approval in the host-local execution path, not only in planning
- remote operator and service clients pair with the host using a six-digit one-time code exchanged through the existing service listener
- short-lived HMAC-signed approval tokens bind to the canonical subject hash and host ID
- paired device tokens are stored hashed and immediately invalidated on revocation or rotation
- all approval and pairing events append to the existing audit chain

See [Configuration reference — security.approvals](configuration-reference.md#securityapprovals) for the config options.

### Approval modes

Each domain (`exec`, `skillExecution`, `pairing`, etc.) can be set independently:

| Mode | Effect |
| --- | --- |
| `deny` | Execution is blocked unconditionally. |
| `ask` | Execution is blocked until an operator resolves the pending request. |
| `allowlist` | Execution is allowed if a matching rule exists; otherwise blocked pending approval. |
| `trusted` | Execution is allowed and audited without prompting. |

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

All approval and device management commands operate directly against the local SQLite database. The HTTP service listener does not need to be running. This means:

- `or3-intern approvals list/approve/deny` works without `or3-intern service`
- `or3-intern devices list/revoke/rotate` works without `or3-intern service`
- pairing requests created via the HTTP API can still be resolved offline via CLI

When the HTTP service is not running, the exchange step (`/internal/v1/pairing/exchange`) is unavailable to remote clients. Remote clients must wait until the service restarts.

### Startup validation

`or3-intern doctor` checks for common misconfigurations:

- `ask` or `allowlist` domains require a valid `keyFile`
- `hostId` should be set explicitly in production deployments
- service mode exposed beyond loopback should have approvals enabled for sensitive domains

Run `or3-intern doctor --strict` to fail on any warning rather than just printing it.

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
```

Run this before enabling external channels, webhook listeners, privileged tools, or service mode.

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Skills](skills.md)
- [MCP tool integrations](mcp-tool-integrations.md)
- [Internal service API reference](api-reference.md)
- [CLI reference](cli-reference.md)

## Related code

- `internal/security/`
- `internal/approval/`
- `cmd/or3-intern/doctor.go`
- `cmd/or3-intern/security_setup.go`
- `cmd/or3-intern/approvals_cmd.go`
- `cmd/or3-intern/devices_cmd.go`
