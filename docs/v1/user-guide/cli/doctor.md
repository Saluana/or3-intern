# Doctor

`doctor` is the readiness and repair command for OR3 Intern.

```bash
or3-intern doctor
or3-intern doctor --fix
or3-intern doctor --fix --interactive
```

## What it checks

`doctor` focuses on startup/readiness, local runtime prerequisites, ingress posture, and hardened execution requirements. Depending on flags and configuration, it can also run bounded probes.

Typical findings include:

- invalid or incomplete config
- missing directories or key files
- channel ingress posture problems
- service hardening or sandboxing issues
- unsafe or incompatible local runtime settings

## Supported flags

| Flag | Description |
| --- | --- |
| `--strict` | Exit non-zero when warnings are found |
| `--json` | Emit a structured JSON report |
| `--fix` | Apply safe automatic fixes where available |
| `--interactive` | Prompt for guided fixes when multiple valid repairs exist |
| `--probe` | Run bounded local runtime probes |
| `--area <name>` | Repeatable area filter |
| `--severity <level>` | Minimum severity filter: `info`, `warn`, `error`, or `block` |
| `--fixable-only` | Show only findings that have available fixes |

## `doctor` vs `status`

- `status` is the friendly summary for day-to-day use.
- `doctor` is the deeper readiness and repair tool.

Use `doctor` when the service will not start, a safety gate blocks startup, a channel or integration looks unhealthy, or you want a structured diagnostic report for debugging.
