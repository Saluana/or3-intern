# Skills

## Overview

`or3-intern` supports ClawHub/OpenClaw-compatible skill bundles with metadata, local overrides, trust policy, and quarantine controls.

## Skill locations and precedence

Skills are loaded from:

1. bundled: `builtin_skills/`
2. managed: `~/.or3-intern/skills`
3. workspace: `<workspace>/skills`

Precedence is:

`workspace > managed > bundled`

A legacy `<workspace>/workspace_skills` directory is still scanned below the new workspace root for migration safety.

## Skill metadata

Skills can ship a `skill.json` manifest for structured metadata such as summary, entrypoints, and timeouts.

Supported frontmatter keys called out in the README include:

- `name`
- `description`
- `homepage`
- `user-invocable`
- `disable-model-invocation`
- `command-dispatch`
- `command-tool`
- `command-arg-mode`

Supported metadata namespaces include:

- `metadata.openclaw`
- `metadata.clawdbot`
- `metadata.clawdis`

## Eligibility checks

The loader checks:

- OS compatibility
- required binaries
- any-of binaries
- required environment variables
- required config flags
- explicit per-skill disable flags

Ineligible skills remain inspectable through `read_skill` and `or3-intern skills info/check`.

## Per-skill configuration

The main config supports:

- `skills.enableExec`
- `skills.maxRunSeconds`
- `skills.managedDir`
- `skills.load.extraDirs`
- `skills.load.watch`
- `skills.load.watchDebounceMs`
- `skills.entries`
- `skills.policy`
- `skills.clawHub`

Per-skill config entries can include:

- `enabled`
- `apiKey`
- `env`
- `config`

## Trust model

The README's trust guidance is intentionally strict:

- treat third-party skills as untrusted input
- script-capable skills default to quarantine until explicitly approved
- origin metadata is persisted for managed installs
- install-time scanning flags obvious high-risk bundles
- `trustedOwners` and `trustedRegistries` can auto-approve known publishers
- `blockedOwners` hard-blocks known-bad publishers
- local edits after installation are treated as trust drift and re-quarantine the skill
- installer hints are informational only and are not auto-run

## Management commands

```bash
or3-intern skills list
or3-intern skills list --eligible
or3-intern skills info <name>
or3-intern skills check
or3-intern skills search "calendar"
or3-intern skills install <slug>
or3-intern skills update <name>
or3-intern skills update --all
or3-intern skills remove <name>
```

## User invocation

User-invocable skills can be triggered explicitly:

```text
/my-skill raw arguments here
```

For `command-dispatch: tool`, the runtime forwards the raw argument string directly to the selected tool. Otherwise the runtime starts a normal model turn seeded with the selected `SKILL.md`.

## Related documentation

- [CLI reference](cli-reference.md)
- [Security and hardening](security-and-hardening.md)
- [Configuration reference](configuration-reference.md)

## Related code

- `internal/skills/`
- `internal/clawhub/`
- `cmd/or3-intern/skills_cmd.go`
