# CLI Overview

The OR3 Intern CLI is the main interface for local chat, setup, diagnostics, approvals, pairing, and the authenticated service runtime.

## First commands to learn

```bash
or3-intern help
or3-intern setup
or3-intern health
or3-intern pair --auto
or3-intern settings
```

- `setup` is the friendliest first-run flow.
- `health` checks whether OR3 is ready and can apply safe repairs with `--fix`.
- `pair --auto` starts the normal device pairing flow with readiness checks.
- `settings` is the canonical day-to-day configuration entrypoint.

## Command groups

The root help groups commands into three buckets:

| Group | Commands |
| --- | --- |
| Simple commands | `setup`, `chat`, `health`, `pair --auto`, `status`, `settings`, `connect-device`, `help` |
| Advanced commands | `configure`, `init`, `config-path`, `chat`, `serve`, `service`, `agent`, `version` |
| Operator tools | `doctor`, `capabilities`, `embeddings`, `secrets`, `audit`, `skills`, `approvals`, `devices`, `pairing`, `scope`, `migrate-jsonl`, `migrate-openclaw` |

## Root flags

These root flags are accepted before the command name:

- `--config <path>` — use a different `config.json`
- `--unsafe-dev` — bypass startup safety gates for local development
- `--advanced` — accepted for compatibility; root help is already complete
- `--help` or `-h` — show help

Many individual commands also define their own flags such as `--json`, `--fix`, or `--section`. Check the command-specific page before copying examples into scripts.

## Configuration path

By default OR3 Intern reads `~/.or3-intern/config.json`. Use `or3-intern config-path` to print the resolved location, especially if you launch with `--config`.

## Related guides

- See [settings](settings.md) for routine configuration changes.
- See [configure](configure.md) for targeted advanced edits.
- See [setup-init](setup-init.md) for first-run flows.
- See [health](health.md) for normal readiness checks and safe repairs.
