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

## Related code

- `internal/security/`
- `cmd/or3-intern/doctor.go`
- `cmd/or3-intern/security_setup.go`
