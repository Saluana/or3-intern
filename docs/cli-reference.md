# CLI reference

Install the CLI once if you want to use the bare `or3-intern` command shown throughout this reference:

```bash
./scripts/install-cli.sh
or3-intern version
```

If you are running directly from a checkout without installing, replace `or3-intern` with `go run ./cmd/or3-intern`.

## Simple commands

| Command | Purpose |
| --- | --- |
| `or3-intern setup` | Guided first-run setup with scenario and safety choices |
| `or3-intern chat` | Interactive CLI session |
| `or3-intern status [--advanced]` | Shows a plain-language safety, access, and problems summary |
| `or3-intern settings [--section ...] [--export path|-]` | Opens the task-based settings home and supports focused edits or config export |
| `or3-intern connect-device [list|disconnect <device-id>|role <device-id>]` | Pairs a phone or other device using a short code and simple access levels |
| `or3-intern help` | Shows the simple root help; use `or3-intern --advanced --help` for the full operator surface |

## Advanced commands

| Command | Purpose |
| --- | --- |
| `or3-intern configure [--section ...]` | Interactive setup and reconfiguration wizard for provider, storage, workspace, web, channels, and service |
| `or3-intern init` | Guided first-run setup for config and provider settings |
| `or3-intern config-path` | Prints the resolved config.json path |
| `or3-intern serve` | Starts enabled channels, triggers, heartbeat, cron, and the shared worker runtime |
| `or3-intern service` | Starts the authenticated internal HTTP API |
| `or3-intern agent -m "..."` | Runs a one-shot foreground turn |
| `or3-intern version` | Prints the binary version |
| `or3-intern help [command]` | Shows root help or command-specific help |

Root help behavior:

- `or3-intern help` shows the short simple-command list.
- `or3-intern --advanced --help` shows the full command catalog.
- `or3-intern help <command>` shows detailed help for either simple or advanced commands.

### `or3-intern setup`

Guided setup using the product-facing mental model instead of raw config sections.

```
or3-intern setup
```

The flow asks for:

- provider
- workspace folder
- usage scenario
- safety mode

It then applies the corresponding runtime profile, approvals, audit, service, and hardening settings before saving the config.

If setup succeeds, it prints a short review covering files, commands, internet, devices, and activity log state, then asks whether to start chat next.

### `or3-intern status`

Shows a friendly status summary sourced from config plus doctor findings.

```
or3-intern status
or3-intern status --advanced
or3-intern status --fix approvals.key_path_missing
```

Default output focuses on:

- effective safety mode
- workspace/file boundaries
- command posture
- internet posture
- device/connectivity readiness
- activity log state
- a "What OR3 can access" dashboard for files, commands, internet, apps, devices, memory, and activity log
- problems that need attention

Use `--advanced` to also print the underlying finding IDs. Use `--fix <finding-id>` for one safe automatic repair when advanced output shows a `Fix now` command.

### `or3-intern settings`

```
or3-intern settings
or3-intern settings --section safety
or3-intern settings --section workspace
or3-intern settings --export config.json
```

This is the user-facing entry point for revisiting setup. The default view is task-based: AI Provider, Workspace Folder, Connected Devices, Safety Level, Channels, Tools, Memory, and Advanced.

Use `--section` to jump to one task area. Common sections are `provider`, `workspace`, `devices`, `safety`, `channels`, `tools`, `memory`, and `advanced`.

Use `--export` when you need the raw JSON config without making JSON editing the normal path.

### `or3-intern connect-device`

```
or3-intern connect-device
or3-intern connect-device list
or3-intern connect-device disconnect <device-id>
or3-intern connect-device role <device-id>
```

This flow:

- checks pairing/service prerequisites
- repairs missing safe defaults such as service secret or approval key when needed
- creates a pairing code
- lets the user choose an access level such as chat-only, workspace access, or admin

The `list` view shows connected devices with friendly role labels, last-used status, change-access guidance, and disconnect commands. `role` currently points users to disconnect/reconnect with a new access level so the role change remains explicit and safe.

Command boundary:

- `or3-intern serve` is the orchestration/runtime host for channels, triggers, workers, heartbeat, and cron.
- `or3-intern service` is the authenticated internal HTTP gateway and machine-facing control plane.

### `or3-intern configure`

Interactive setup and reconfiguration wizard. It loads the active config when present, shows a summary, and lets you change only the sections you care about.

Mode selection:

- Interactive TTY: launches the Bubble Tea setup UI.
- Non-interactive stdin/stdout: stays in the plain-text prompt flow.

Plain-text secret prompt behavior:

- Leave an existing secret blank to keep it.
- Enter a new value to replace it.
- Type `clear` to remove it.

Default keybindings in TUI mode:

- Arrow keys: move between sections, fields, and choices.
- `enter`: open a section or confirm the current choice.
- `space`: toggle boolean fields.
- `s`: save and apply changes.
- `q`: back out of the current screen or quit.

Examples:

```
or3-intern configure
or3-intern configure --section provider --section web
or3-intern configure --section channels
```

Available sections:

- `provider`
- `storage`
- `workspace`
- `web`
- `channels`
- `service`

Use `or3-intern init` if you only want the original lightweight first-run provider/storage wizard.

### `or3-intern init`

`init` is a first-run alias over `configure`. It uses the same TTY detection rules and the same Bubble Tea UI when interactive, but it preselects the original first-run sections: provider, storage, workspace, and web.

## Operator tools

| Command | Purpose |
| --- | --- |
| `or3-intern doctor [--strict|--json|--fix]` | Diagnoses readiness issues, emits machine-readable reports, and repairs safe local problems |
| `or3-intern capabilities [--channel name|--trigger name|--json]` | Shows the effective runtime posture, ingress policy, approvals, and access-profile limits |
| `or3-intern embeddings <status|rebuild> [memory|docs|all]` | Shows embedding compatibility status and rebuilds stored memory/doc embeddings after provider or model changes |
| `or3-intern secrets <set|delete|list>` | Manages encrypted secret references stored in SQLite |
| `or3-intern audit [verify]` | Inspects or verifies the append-only audit chain |
| `or3-intern approvals <list|show|approve|deny|cancel|expire|allowlist>` | Lists and resolves pending approval requests or approval allowlists |
| `or3-intern devices <list|requests|approve|deny|rotate|revoke>` | Lists paired devices and supports device rotation/revocation plus legacy pairing request actions |
| `or3-intern pairing <list|request|approve|deny|exchange>` | Runs the pairing workflow and can bind approvals to channel identities such as `slack:U123` |
| `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]` | Imports legacy session history |
| `or3-intern migrate-openclaw [--scope <scope-key>] <openclaw-agent-dir>` | Imports a local OpenClaw agent's soul, identity, static memory, user context, daily memory notes, and dreams |

### `or3-intern embeddings`

Use this command after switching `provider.apiBase` or `provider.embedModel`.

The authenticated HTTP parity lives under `or3-intern service` at `/internal/v1/embeddings/status` and `/internal/v1/embeddings/rebuild`.

```
or3-intern embeddings status
```

Prints the stored memory-vector dimensions, the stored embedding fingerprint, the current fingerprint, and whether they match.

```
or3-intern embeddings rebuild memory
or3-intern embeddings rebuild docs
or3-intern embeddings rebuild all
```

Rebuilds persisted embeddings in the current embedding space:

- `memory` re-embeds all long-term memory notes and rebuilds the vector index.
- `docs` re-syncs indexed files with the current embedding fingerprint.
- `all` runs both in sequence.

When chat is running, `/new` archives the current conversation before clearing live history. If you recently changed embedding providers or models, the runtime now repairs the active vector profile automatically for new archival writes, but you should still run an explicit rebuild so older memory/doc vectors are regenerated too.

### `or3-intern audit`

```
or3-intern audit
or3-intern audit verify
```

Verifies the append-only audit chain locally. The authenticated HTTP parity lives under `or3-intern service` at `/internal/v1/audit` and `/internal/v1/audit/verify`.

### `or3-intern migrate-openclaw`

Use this command when you have an OpenClaw agent workspace on disk and want to move the durable parts into the current `or3-intern` install.

```bash
or3-intern migrate-openclaw ~/.openclaw/agents/main
```

Imported data:

- `SOUL.md` → configured `soulFile`
- `IDENTITY.md` → configured `identityFile`
- `MEMORY.md` → configured `memoryFile`
- `USER.md` → appended into `memoryFile` under an import heading
- `memory/*.md` → imported into durable memory notes
- `DREAMS.md` and `memory/.dreams/*` → imported into durable memory notes as summary-style dream context

Daily memory files are chunked conservatively before embedding so the importer does not send oversized embedding requests. If the current embedding fingerprint does not match the stored memory-vector fingerprint, the command still imports the notes and falls back to FTS-only memory for those imported chunks until you run `or3-intern embeddings rebuild memory`.

### `or3-intern approvals`

Manage pending approval requests and allowlist rules. All sub-commands work directly against the local SQLite database — the HTTP service does not need to be running.

```
or3-intern approvals list [status]
```
Lists approval requests. Optionally filter by status: `pending`, `approved`, `denied`, `canceled`, `expired`. Up to 100 results are returned. The default output includes a short human summary of what OR3 wants to do.

```
or3-intern approvals show <id>
```
Shows one approval request with a friendly action summary and risk label, plus the raw subject details for advanced review.

```
or3-intern approvals approve <id> [--allowlist] [--note <text>]
```
Approves a pending request and issues a short-lived approval token. The token is printed once and can be passed to the next execution attempt via context.
- `--allowlist` also creates a persistent allowlist rule matching the same subject, so future identical executions are pre-approved without another prompt.
- `--note` attaches a free-text resolution note to the audit record.

```
or3-intern approvals deny <id> [--note <text>]
```
Denies a pending request and records the resolution in the audit chain. The blocked tool invocation returns an error to the agent.

```
or3-intern approvals cancel <id> [--note <text>]
```
Cancels a pending request without approving or denying the underlying action.

```
or3-intern approvals expire
```
Marks every currently expired pending request as `expired` and prints how many were updated.

```
or3-intern approvals allowlist list [domain]
```
Lists allowlist rules. Optionally filter by domain (`exec`, `skill_execution`).

```
or3-intern approvals allowlist add --domain <exec|skill_execution> [options]
```
Creates a new allowlist rule. Options:

| Flag | Description |
| --- | --- |
| `--domain` | Approval domain (`exec` or `skill_execution`). Default: `exec`. |
| `--host` | Host scope (default: current host ID). |
| `--tool` | Tool name scope. |
| `--profile` | Access profile scope. |
| `--agent` | Agent ID scope. |
| `--program` | (exec) Exact executable path to match. |
| `--cwd` | (exec) Working directory constraint. |
| `--skill` | (skill_execution) Skill ID to match. |
| `--version` | (skill_execution) Skill version constraint. |
| `--origin` | (skill_execution) Skill origin/registry constraint. |
| `--trust` | (skill_execution) Skill trust state constraint. |

```
or3-intern approvals allowlist remove <id>
```
Disables an allowlist rule by ID.

### `or3-intern devices`

Manage paired devices and pairing requests. All sub-commands work directly against the local SQLite database.

```
or3-intern devices list
```
Lists all paired devices with their status, role, and display name, followed by friendly role and status labels.

```
or3-intern devices requests [status]
```
Lists pairing requests. Optionally filter by status: `pending`, `approved`, `denied`, `exchanged`. The default output includes a human summary of the requested device access.

```
or3-intern devices approve <pairing-request-id>
```
Approves a pending pairing request. The remote client can then exchange the pairing code for a device token using the HTTP API.

```
or3-intern devices deny <pairing-request-id>
```
Denies a pending pairing request. The remote client receives an error on the next exchange attempt.

```
or3-intern devices revoke <device-id>
```
Revokes a paired device immediately. Any active bearer token for this device is invalidated on the next API request.

```
or3-intern devices rotate <device-id>
```
Rotates the device token and prints the new token once. The old token is invalidated. Use this to recover a potentially leaked token without re-pairing.

### `or3-intern pairing`

The pairing command group handles channel-identity pairing, which binds a channel user identity (e.g. `slack:U42`) to a paired device record.

```
or3-intern pairing list
```
Lists all pairing requests.

```
or3-intern pairing request --channel <channel> --identity <id> --name <name>
```
Creates a pairing request for a specific channel identity. Returns a request ID and one-time code.

```
or3-intern pairing approve <request-id>
```
Approves a pairing request from CLI so the remote client can exchange the code.

```
or3-intern pairing deny <request-id>
```
Denies a pairing request.

```
or3-intern pairing exchange <request-id> <code>
```

### `or3-intern scope`

```
or3-intern scope link <session-key> <scope-key>
or3-intern scope list <scope-key>
or3-intern scope resolve <session-key>
```

Links multiple physical session keys to one logical history scope and inspects those links. The authenticated HTTP parity lives under `or3-intern service` at `/internal/v1/scope/links`, `/internal/v1/scope/resolve`, and `/internal/v1/scope/sessions`.
Exchanges an approved pairing code for a device token (normally done by the remote client, but available here for local testing).

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
