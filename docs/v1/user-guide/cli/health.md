# Health

`health` is the normal readiness command for OR3 Intern.

```bash
or3-intern health
or3-intern health --fix
or3-intern health --json
```

Use it after setup, before pairing a device, or any time OR3 does not look ready. It checks the current config, local file/key prerequisites, provider and workspace posture, and runtime readiness without starting the full agent runtime.

## Common flows

Run the default readiness check:

```bash
or3-intern health
```

Apply safe automatic repairs:

```bash
or3-intern health --fix
```

Emit the structured report for scripts:

```bash
or3-intern health --json
```

## Supported flags

| Flag | Description |
| --- | --- |
| `--check` | Run the default readiness check explicitly |
| `--fix` | Apply safe automatic fixes where available |
| `--json` | Emit a structured JSON report |
| `--interactive` | Prompt for guided fixes when ambiguous repairs exist |
| `--probe` | Run bounded local runtime probes |
| `--advanced` | Use the stricter advanced diagnostic mode |
| `--area <name>` | Repeatable area filter |
| `--severity <level>` | Minimum severity filter: `info`, `warn`, `error`, or `block` |
| `--fixable-only` | Show only findings that have available fixes |

## `health`, `status`, and `doctor`

- `health` is the default readiness and repair command.
- `status` is a plain-language safety and access summary.
- `doctor` is the advanced diagnostic command for strict checks, filters, and deeper repair work.

Use `or3-intern doctor --strict` before exposing connected apps or service mode to a wider network.
