# Skills

`skills` lists, inspects, validates, searches, installs, updates, and removes skills.

## Supported subcommands

| Command | Description |
| --- | --- |
| `list [--eligible]` | List discovered skills |
| `info <name>` | Show metadata, permission state, and policy notes |
| `check` | Validate available skills and report policy state |
| `search <query>` | Search configured skill registries |
| `install <slug> [--version v] [--force]` | Install a skill into the managed directory |
| `update <name>|--all [--version v] [--force]` | Update one or more managed skill installs |
| `remove <name>` | Remove a managed install |

## Examples

```bash
or3-intern skills search web-scraper
or3-intern skills info web-scraper
or3-intern skills install demo --version 1.0.0
or3-intern skills list --eligible
or3-intern skills check
```

## Important concepts

- `list` shows discovered skills, not only currently managed installs
- `--eligible` filters to skills that are currently allowed to run
- `info` and `check` surface policy, trust, quarantine, or permission-state details
- `install` and `update` can refuse local modifications unless `--force` is used

Use `search → info → install → check/list → ask the agent to use the skill` as the normal workflow.
