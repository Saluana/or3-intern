# Skills System

Skills are declarative, markdown-defined capabilities that extend OR3 Intern's tool set. The skills system is in `internal/skills/skills.go`.

## SKILL.md Format

Each skill is defined by a `SKILL.md` file (or `skill.md`) placed in a directory. The file has two parts:

### YAML Front Matter

Everything between `---` delimiters is parsed as YAML:

```yaml
---
name: my-skill
description: What this skill does
user-invocable: true
disable-model-invocation: false
command-dispatch: tool
command-tool: exec
command-arg-mode: raw
permissions:
  shell: false
  network: false
  write: false
  paths: []
  hosts: []
metadata:
  openclaw:
    always: false
    skillKey: my-skill
    primaryEnv: MY_SKILL_API_KEY
    os: [linux, darwin]
    requires:
      bins: [python3]
      anyBins: [jq, yq]
      env: [MY_SKILL_API_KEY]
      config: [my_skill.enabled]
    install:
      - id: brew-python
        kind: brew
        bins: [python3]
        formula: python3
---
```

### Markdown Body

Everything after the closing `---` is the skill body. It describes what the skill does and how to use it.

### skill.json Manifest

An optional `skill.json` in the skill directory declares:

```json
{
  "summary": "Short description",
  "entrypoints": [
    {
      "name": "run",
      "command": ["python3", "main.py"],
      "timeoutSeconds": 30,
      "acceptsStdin": false
    }
  ],
  "tools": ["exec", "files.read"],
  "permissions": {
    "shell": false,
    "network": true
  }
}
```

## Skill Sources

Skills come from five sources (`internal/skills/skills.go:33-46`):

| Source | Priority | Description |
|--------|----------|-------------|
| `workspace` | 40 | Workspace-local skills |
| `managed` | 30 | Installed from ClawHub |
| `global` | 25 | User-level shared skills |
| `bundled` | 20 | Shipped with the app |
| `extra` | 10 | Explicitly listed directories |

When the same skill name appears in multiple sources, the highest priority wins.

## Discovery

`ScanWithOptions` (`internal/skills/skills.go:237-308`) walks each root directory:

1. For each directory, check for `SKILL.md` or `skill.md`
2. Parse front matter and body
3. Load `skill.json` if present
4. Resolve dependencies (skills referenced in the markdown body)
5. Evaluate eligibility (binaries, env vars, config)
6. Apply approval policy
7. Sort by name, then by source priority

### Dependency Detection

Dependencies are found in the markdown body through two patterns (`internal/skills/skills.go:449-486`):

- `../<name>/SKILL.md` — relative references to other skill directories
- `` ` <name> ` `` — backtick references in lines mentioning "prerequisite" or "load skill"

## Eligibility

A skill is eligible only when all requirements are met (`internal/skills/skills.go:831-881`):

- No parse errors
- Not disabled in config
- OS matches (if declared)
- All required binaries are found on PATH
- Required environment variables are set
- Required config values are truthy
- Required tools are available in the runtime
- No unsupported features (e.g. nix plugins, missing tools)

## Approval Policy

The `ApprovalPolicy` (`internal/skills/skills.go:76-82`) controls which managed skills are trusted:

- `QuarantineByDefault` — if true, all managed skills need approval
- `ApprovedSkills` — explicit allowlist by name or key
- `TrustedOwners` — publishers whose skills are auto-approved
- `BlockedOwners` — publishers whose skills are always blocked
- `TrustedRegistries` — registries whose skills are auto-approved

The `applyApprovalPolicy` function (`internal/skills/skills.go:622-681`) evaluates each skill:

1. Blocked if parse errors exist
2. Blocked if publisher is in `BlockedOwners`
3. Blocked if scan status is `blocked` (high-severity findings)
4. Quarantined if locally modified
5. Quarantined if scan status is `quarantined`
6. Approved if in `ApprovedSkills` or from trusted publisher/registry
7. Quarantined if from managed source with no trusted origin
8. Approved if no permissions or entrypoints declared

## Runtime Metadata

Skills can declare runtime metadata under the `metadata.openclaw` key (`internal/skills/skills.go:806-829`). This is parsed into `SkillRuntimeMeta`:

- `always` — skill is always eligible regardless of other checks
- `skillKey` — alternate lookup key
- `primaryEnv` — environment variable for API key injection
- `emoji` — display emoji
- `homepage` — documentation URL
- `os` — supported operating systems
- `requires` — prerequisite checks
- `install` — installation guidance
- `nix` — optional nix plugin dependency

## Inventory API

The `Inventory` type (`internal/skills/skills.go:181-184`) provides lookup and summarization:

- `Get(name)` — finds a skill by name (case-insensitive)
- `Summary(max)` — human-readable skill list
- `ModelSummary(max)` — filtered list for AI context (only eligible, non-hidden skills)
- `RunEnv()` — merged environment from all eligible skills
- `RunEnvForSkill(name)` — environment for a specific skill
- `ResolveBundlePath(name, relPath)` — resolves a path within a skill's bundle
