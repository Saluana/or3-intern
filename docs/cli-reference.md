# CLI Reference

Install the supported local CLI without Go or a PATH edit:

```bash
npx @or3/connect@0.1.2 intern
```

From this checkout:

```bash
./scripts/install-cli.sh
or3-intern version
```

For a one-off run, replace `or3-intern` below with `go run ./cmd/or3-intern`.
See [Connect release status](connect-release-status.md) for exact immutable
versions.

## Commands

| Command | Purpose |
| --- | --- |
| `or3-intern setup` | Guided first-run setup with scenario and safety choices. |
| `or3-intern init` | Compatibility alias for `setup`. |
| `or3-intern connect --cloud-url <verified-endpoint>` | Advanced remote Connect for a verified staging or self-hosted endpoint. Managed Cloud Connect is withheld. |
| `or3-intern chat` | Interactive runner-backed chat. |
| `or3-intern health [--check|--fix|--json]` | Readiness checks and safe repairs. |
| `or3-intern status [--advanced]` | Safety, access, and problem summary. |
| `or3-intern settings [--section ...] [--export path|-]` | Task-based settings and config export. |
| `or3-intern configure [--section ...]` | Advanced section editor. |
| `or3-intern config-path` | Print the resolved config path. |
| `or3-intern serve` | Run enabled channels, triggers, heartbeat, cron, and workers. |
| `or3-intern service` | Run the authenticated internal HTTP API. |
| `or3-intern agent -m "..."` | Run and wait for a one-shot foreground runner turn. |
| `or3-intern doctor [--strict|--json|--fix]` | Advanced diagnostics and guided repair. |
| `or3-intern access <show|defaults|default|channel>` | Manage Reader, Operator, and Admin access profiles. |
| `or3-intern cron <add|list|show|remove|run|pause|resume>` | Manage scheduled runner jobs. |
| `or3-intern capabilities [--json]` | Inspect runner, terminal, ingress, approval, network, and auth posture. |
| `or3-intern embeddings <status|rebuild>` | Inspect or rebuild memory and document embeddings. |
| `or3-intern memory <search|add-note|pinned>` | Search, add, and pin long-term memory entries. |
| `or3-intern approvals ...` | Inspect and resolve approval requests and allowlists. |
| `or3-intern secrets <set|delete|list>` | Manage encrypted secret references. |
| `or3-intern audit [verify]` | Inspect or verify the append-only audit chain. |
| `or3-intern skills <list|info|check|search|install|update|remove>` | Manage skills. |
| `or3-intern devices <create|list|rotate|revoke>` | Issue and manage trusted device tokens. |
| `or3-intern pairing <request|list|approve-code|approve|deny|exchange>` | Manage approval-based device and channel pairing. |
| `or3-intern scope <link|list|resolve>` | Link session keys to shared history scopes. |
| `or3-intern migrate-jsonl <path> [session_key]` | Import a legacy JSONL session transcript. |
| `or3-intern migrate-openclaw [options] <agent-dir>` | Import local OpenClaw bootstrap files and memory. |
| `or3-intern version` | Print the binary version. |
| `or3-intern help [command]` | Show help. |

## Runner Jobs

Scheduled and background execution uses runner jobs. Cron payloads use `runner_run`; service job kinds use `runner:<runner_id>`.
