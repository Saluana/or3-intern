# CLI Reference

Install the CLI once if you want to use the bare `or3-intern` command:

```bash
./scripts/install-cli.sh
or3-intern version
```

If running from a checkout, replace `or3-intern` with `go run ./cmd/or3-intern`.

## Commands

| Command | Purpose |
| --- | --- |
| `or3-intern setup` | Guided first-run setup with scenario and safety choices. |
| `npx @or3/connect [status\|doctor\|disconnect\|uninstall]` | Connect this computer to OR3 Cloud without a VPN or pasted token. |
| `or3-intern chat` | Interactive runner-backed chat. |
| `or3-intern health [--check|--fix|--json]` | Readiness checks and safe repairs. |
| `or3-intern status [--advanced]` | Safety, access, and problem summary. |
| `or3-intern settings [--section ...] [--export path|-]` | Task-based settings and config export. |
| `or3-intern configure [--section ...]` | Advanced section editor. |
| `or3-intern config-path` | Print the resolved config path. |
| `or3-intern serve` | Run enabled channels, triggers, heartbeat, cron, and workers. |
| `or3-intern service` | Run the authenticated internal HTTP API. |
| `or3-intern agent -m "..."` | Enqueue a one-shot runner turn. |
| `or3-intern capabilities [--json]` | Inspect runtime posture, approvals, network, runner, and auth capabilities. |
| `or3-intern approvals ...` | Inspect and resolve approval requests and allowlists. |
| `or3-intern secrets <set|delete|list>` | Manage encrypted secret references. |
| `or3-intern skills <list|info|check|search|install|update|remove>` | Manage skills. |
| `or3-intern version` | Print the binary version. |
| `or3-intern help [command]` | Show help. |

## Runner Jobs

Scheduled and background execution uses runner jobs. Cron payloads use `runner_run`; service job kinds use `runner:<runner_id>`.
