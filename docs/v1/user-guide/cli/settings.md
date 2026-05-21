# Settings

`settings` is the canonical day-to-day configuration flow.

```bash
or3-intern settings
or3-intern settings --section safety
or3-intern settings --export -
```

## What it does

Instead of exposing raw config JSON first, `settings` presents task-oriented areas such as:

- AI Controls
- Workspace Folder
- Connected Devices
- Safety Level
- Connected Apps
- Tools
- Memory
- Advanced

Use it when you want to review or update your setup without dropping directly into the lower-level `configure` editor.

## Supported flags

| Flag | Description |
| --- | --- |
| `--section <name>` | Jump directly to `provider`, `workspace`, `devices`, `safety`, `channels`, `tools`, `memory`, or `advanced` |
| `--export <path|->` | Export the current config JSON to a file or stdout |
| `--advanced` | Show advanced settings actions on the home screen |

## Relationship to other commands

- `settings` — default configuration entrypoint
- `setup` — plain-language first-run flow
- `init` — compatibility alias for the original first-run wizard
- `configure` — advanced targeted editor
- `health --fix` — normal repair path for safe readiness fixes
- `doctor --fix` — advanced repair path with stricter diagnostics and filters

## Good uses

- review the current setup before enabling tools or connected apps
- jump to one task area with `--section`
- export the config without editing JSON by hand
