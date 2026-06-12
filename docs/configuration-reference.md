# Configuration Reference

`or3-intern` loads `config.json`, usually from `~/.or3-intern/config.json`. Environment overrides are applied after `.env` loading; already-exported shell variables win.

Use `or3-intern settings` for normal edits and `or3-intern configure` for section-focused advanced edits.

## Top-Level Sections

| Key | Purpose |
| --- | --- |
| `dbPath`, `artifactsDir`, `workspaceDir`, `allowedDir` | Storage and workspace boundaries. |
| `provider` | Embeddings, memory consolidation, doctor flows, and provider credentials. |
| `runners` | External runner selection, discovery, worker pool, timeouts, and isolation. |
| `runtime` | Runtime memory and compaction settings. |
| `context` | Runner prompt context packing. |
| `tools` | Web proxy, PATH additions, workspace read policy, and MCP servers. |
| `hardening` | Program allowlists, sandboxing, child environment controls, and isolation. |
| `skills` | Managed skill loading, trust policy, registry, and quarantine state. |
| `triggers`, `heartbeat`, `cron` | Automation inputs and scheduled runner jobs. |
| `service` | Internal authenticated HTTP API settings. |
| `channels` | Telegram, Slack, Discord, WhatsApp bridge, and Email configuration. |
| `security` | Secret store, audit, access profiles, approvals, auth, and network policy. |
| `session` | Session naming and shared-history scope behavior. |

## Minimal Shape

```json
{
  "provider": {},
  "runners": {},
  "runtime": {},
  "context": {},
  "tools": {},
  "hardening": {},
  "skills": {},
  "triggers": {},
  "heartbeat": {},
  "cron": {},
  "service": {},
  "channels": {},
  "security": {},
  "session": {}
}
```

## Runner Selection

Set the default runner with:

- `runners.default`
- `OR3_RUNNERS_DEFAULT`

Supported runners include OpenCode, Codex, Claude Code, and Gemini.

## API Shape

Service request payloads use snake_case. Unknown fields are rejected.

## Controls That Affect Runner Execution

- runner selection
- mode and isolation
- cwd/path policy
- approvals
- network policy
- writable paths
- auth and service access
