# CLI Reference

For normal local use, no installation is needed:

```bash
npx @or3/connect intern
```

This is the local-first path once the matching package and Intern release are
published. See [Connect release status](connect-release-status.md) for the
immutable version contract; the currently published package is older than the
source `intern` subcommand. From a checkout, use `go run ./cmd/or3-intern ...`
until the exact package/assets are released. Install the CLI only if you specifically want the bare
`or3-intern` command:

```bash
./scripts/install-cli.sh
or3-intern version
```

If running from a checkout, replace `or3-intern` with `go run ./cmd/or3-intern`.

## Commands

| Command | Purpose |
| --- | --- |
| `npx @or3/connect intern [command]` | Download and run OR3 Intern; with no command, opens guided local setup. |
| `or3-intern setup` | Guided first-run setup with scenario and safety choices. |
| `or3-intern connect --cloud-url <verified-endpoint>` | Advanced remote Connect for a verified staging or self-hosted endpoint. Managed Cloud Connect is withheld. |
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
