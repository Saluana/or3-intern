# Configuration reference

`or3-intern` loads its primary configuration from `config.json`, usually under `~/.or3-intern/config.json` after `or3-intern init`.

## Top-level sections

| Key | Purpose |
| --- | --- |
| `dbPath`, `artifactsDir`, `workspaceDir`, `allowedDir` | Storage locations and workspace boundaries |
| `defaultSessionKey`, `session` | Session naming and cross-session identity/scope behavior |
| `identityFile`, `memoryFile` | Prompt bootstrap files |
| `provider` | Model API base, model names, embedding settings, keys, temperature, and timeouts |
| `tools` | Local tool behavior, proxying, timeouts, workspace restrictions, and MCP servers |
| `hardening` | Tool capability tiers, program allowlists, child environment controls, quotas, and sandboxing |
| `skills` | Managed skill loading, per-skill config, policy, and registry settings |
| `triggers` | Webhook and file-watch automation |
| `heartbeat` | Timer-driven autonomous turns |
| `cron` | Scheduled job storage and execution |
| `service` | Internal authenticated HTTP API settings |
| `channels` | Telegram, Slack, Discord, WhatsApp bridge, and Email configuration |
| `security` | Secret store, audit, access profiles, and outbound network policy |
| `runtimeProfile` | Named execution posture (`local-dev`, `hosted-service`, `hosted-no-exec`, etc.) |
| `docIndex` | Opt-in document indexing for prompt-time retrieval |
| `subagents` | Background job queueing and concurrency controls |

## Minimal shape

```json
{
  "provider": {},
  "tools": {},
  "hardening": {},
  "skills": {},
  "triggers": {},
  "heartbeat": {},
  "cron": {},
  "service": {},
  "channels": {},
  "security": {},
  "docIndex": {},
  "subagents": {},
  "session": {}
}
```

## Important sections

### `provider`

Controls the LLM and embedding provider settings:

- `apiBase`
- `apiKey`
- `model`
- `embedModel`
- `embedDimensions` — optional embedding-vector size override; `0` means use the provider/model default
- `temperature`
- `enableVision`
- `timeoutSeconds`

### `tools`

Controls local tool execution and optional MCP registration:

- `braveApiKey`
- `webProxy`
- `execTimeoutSeconds`
- `restrictToWorkspace`
- `pathAppend`
- `mcpServers`

See [mcp-tool-integrations.md](mcp-tool-integrations.md) for the MCP-specific settings.

### `hardening`

Core runtime safety controls:

- `guardedTools`
- `privilegedTools`
- `enableExecShell`
- `execAllowedPrograms`
- `childEnvAllowlist`
- `isolateChannelPeers`
- `sandbox`
- `quotas`

See [security-and-hardening.md](security-and-hardening.md) for rollout guidance.

### `skills`

Skill loading and trust policy:

- `enableExec`
- `maxRunSeconds`
- `managedDir`
- `load`
- `entries`
- `policy`
- `clawHub`

See [skills.md](skills.md).

### `triggers`, `heartbeat`, and `cron`

These sections control autonomous execution:

- `triggers.webhook`
- `triggers.fileWatch`
- `heartbeat`
- `cron`

See [triggers-and-automation.md](triggers-and-automation.md).

### `service`

Internal service mode settings:

- `enabled`
- `listen`
- `secret`

See [api-reference.md](api-reference.md).

### `channels`

Non-CLI integrations:

- `telegram`
- `slack`
- `discord`
- `whatsApp`
- `email`

Each external channel now supports `inboundPolicy` in addition to the legacy `openAccess` flag:

- `allowlist` — require the channel-specific allowlist field (`allowedChatIds`, `allowedUserIds`, `allowedFrom`, `allowedSenders`)
- `pairing` — require a matching paired identity from the approval broker/device store
- `deny` — enable outbound delivery while rejecting inbound traffic

When `inboundPolicy` is omitted, the runtime preserves the existing `openAccess` / allowlist behavior for backward compatibility.

See [channels.md](channels.md).

### `security`

Phase 3 security controls:

- `secretStore`
- `audit`
- `profiles`
- `network`

See [security-and-hardening.md](security-and-hardening.md).

### `security.approvals`

The approval and pairing system adds a small `approvals` block inside the `security` section. All fields have safe defaults; existing installs that omit this block continue to work unchanged.

```json
{
  "security": {
    "approvals": {
      "enabled": false,
      "hostId": "local",
      "keyFile": "",
      "pairingCodeTtlSeconds": 300,
      "pendingTtlSeconds": 3600,
      "approvalTokenTtlSeconds": 300,
      "localAutoPairLoopback": false,
      "pairing":        { "mode": "ask" },
      "exec":           { "mode": "trusted" },
      "skillExecution": { "mode": "trusted" },
      "secretAccess":   { "mode": "trusted" },
      "messageSend":    { "mode": "trusted" }
    }
  }
}
```

#### Top-level approval fields

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Activates the approval broker. When `false`, all approval checks pass through without enforcement. |
| `hostId` | string | `"local"` | Stable identifier for this host. Included in approval tokens and audit events so future sandboxes and remote nodes can verify provenance. |
| `keyFile` | string | `""` | Path to a 32-byte key file used to sign and verify approval tokens. Required when any domain uses `ask` or `allowlist` mode. |
| `pairingCodeTtlSeconds` | int | `300` | How long a pairing code remains valid before automatic expiry. |
| `pendingTtlSeconds` | int | `3600` | How long a pending approval request waits before expiring automatically. |
| `approvalTokenTtlSeconds` | int | `300` | How long an issued approval token remains valid after the operator resolves the request. |
| `localAutoPairLoopback` | bool | `false` | When `true`, automatically approves pairing requests from loopback addresses without requiring operator action. Intended for single-node local development only. |

#### Approval modes

Each domain under `security.approvals` accepts a `mode` field with one of these values:

| Mode | Behaviour |
| --- | --- |
| `deny` | All execution in this domain is blocked unconditionally. Suitable for `exec` on headless hosts that should never run subprocesses. |
| `ask` | Every execution attempt creates a pending approval request and is blocked until an operator resolves it. |
| `allowlist` | Execution is allowed if a matching allowlist rule exists; otherwise a pending request is created. Use this to pre-approve recurring safe patterns while still gating novel ones. |
| `trusted` | Execution is allowed without prompting an operator. An audit event is still recorded. This is the default for all domains. |

#### Domain fields

Configure each of these independently under `security.approvals`:

| Domain key | Controls |
| --- | --- |
| `pairing` | Whether new device pairing requests are auto-approved, gated, or denied. |
| `exec` | Shell and program execution via the `exec` tool. |
| `skillExecution` | Skill script execution via `run_skill_script`. |
| `secretAccess` | (Future) Gate on decrypting or reading a named secret. |
| `messageSend` | (Future) Gate on sending an outbound message through a channel. |

#### Upgrade notes

- Existing installs do not need any approval config. All domains default to `trusted` when `enabled` is `false` or absent.
- When you add `"enabled": true` for the first time, set every domain you want to gate explicitly; domains left unset keep `trusted` behavior.
- `keyFile` must exist before the service starts when any domain uses `ask` or `allowlist`. Run `or3-intern doctor` to detect missing keys.
- `hostId` should be stable and unique per host. Changing it invalidates all outstanding approval tokens issued under the old value.

### `runtimeProfile`

Selects the named execution posture enforced at startup. Valid values:

- `local-dev` — permissive; no additional security requirements.
- `single-user-hardened` — tighter defaults for self-hosted personal use.
- `hosted-service` — requires `security.secretStore`, `security.audit`, and `security.network` to be enabled.
- `hosted-no-exec` — like `hosted-service` but also forbids `hardening.enableExecShell` and `hardening.privilegedTools`.
- `hosted-remote-sandbox-only` — requires a sandbox when exec is enabled.

Override with the `OR3_RUNTIME_PROFILE` environment variable.

See [security-and-hardening.md](security-and-hardening.md) for startup validation details.

### `docIndex`

Opt-in file indexing and retrieval:

- `enabled`
- `roots`
- `maxFiles`
- `maxFileBytes`
- `maxChunks`
- `embedMaxBytes`
- `refreshSeconds`
- `retrieveLimit`

See [memory-and-context.md](memory-and-context.md).

## Environment overrides called out in the README

The codebase documents these direct environment overrides for service and channel setup:

- `OR3_SERVICE_ENABLED`
- `OR3_SERVICE_LISTEN`
- `OR3_SERVICE_SECRET`
- `OR3_TELEGRAM_TOKEN`
- `OR3_SLACK_APP_TOKEN`
- `OR3_SLACK_BOT_TOKEN`
- `OR3_DISCORD_TOKEN`
- `OR3_WHATSAPP_BRIDGE_URL`
- `OR3_WHATSAPP_BRIDGE_TOKEN`
- `OR3_EMAIL_IMAP_HOST`
- `OR3_EMAIL_IMAP_PORT`
- `OR3_EMAIL_IMAP_USERNAME`
- `OR3_EMAIL_IMAP_PASSWORD`
- `OR3_EMAIL_SMTP_HOST`
- `OR3_EMAIL_SMTP_PORT`
- `OR3_EMAIL_SMTP_USERNAME`
- `OR3_EMAIL_SMTP_PASSWORD`
- `OR3_EMAIL_FROM_ADDRESS`

## Related code

- `internal/config/config.go`
- `cmd/or3-intern/init.go`
