# CLI reference

## Primary commands

| Command | Purpose |
| --- | --- |
| `or3-intern init` | Guided first-run setup for config and provider settings |
| `or3-intern chat` | Interactive CLI session |
| `or3-intern serve` | Starts enabled channels, triggers, heartbeat, cron, and the shared worker runtime |
| `or3-intern service` | Starts the authenticated internal HTTP API |
| `or3-intern agent -m "..."` | Runs a one-shot foreground turn |
| `or3-intern version` | Prints the binary version |

## Operational and admin commands

| Command | Purpose |
| --- | --- |
| `or3-intern doctor [--strict]` | Audits the current config for unsafe or inconsistent settings |
| `or3-intern capabilities [--channel name|--trigger name|--json]` | Shows the effective runtime posture, ingress policy, approvals, and access-profile limits |
| `or3-intern secrets <set|delete|list>` | Manages encrypted secret references stored in SQLite |
| `or3-intern audit [verify]` | Inspects or verifies the append-only audit chain |
| `or3-intern approvals <list|show|approve|deny|allowlist>` | Lists and resolves pending approval requests or approval allowlists |
| `or3-intern devices <list|requests|approve|deny|rotate|revoke>` | Lists paired devices and supports device rotation/revocation plus legacy pairing request actions |
| `or3-intern pairing <list|request|approve|deny|exchange>` | Runs the pairing workflow and can bind approvals to channel identities such as `slack:U123` |
| `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]` | Imports legacy session history |

## Skills commands

| Command | Purpose |
| --- | --- |
| `or3-intern skills list` | Lists discovered skills |
| `or3-intern skills list --eligible` | Lists only eligible skills |
| `or3-intern skills info <name>` | Shows metadata, permission state, and policy notes |
| `or3-intern skills check` | Validates available skills and reports policy state |
| `or3-intern skills search "query"` | Searches configured registries |
| `or3-intern skills install <slug>` | Installs a skill into the managed directory |
| `or3-intern skills update <name>` / `--all` | Updates managed installs |
| `or3-intern skills remove <name>` | Removes a managed install |

See [skills.md](skills.md) for how the loader, trust model, and quarantine policy work.

## Session scope commands

| Command | Purpose |
| --- | --- |
| `or3-intern scope link <session-key> <scope>` | Links a session to a named scope |
| `or3-intern scope list <scope>` | Lists session keys attached to a scope |
| `or3-intern scope resolve <session-key>` | Resolves the scope for a session |

Scopes let multiple session keys share one conversation history. See [memory-and-context.md](memory-and-context.md).

## Related references

- [Getting started](getting-started.md)
- [Configuration reference](configuration-reference.md)
- [Internal service API reference](api-reference.md)
