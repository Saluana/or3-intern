# Status

`status` is the plain-language safety and access summary.

```bash
or3-intern status
or3-intern status --advanced
or3-intern status --fix all
```

## What it is for

Use `status` when you want a quick answer to questions like:

- what can OR3 access right now?
- what is blocked or waiting for setup?
- are there safe fixes I can apply immediately?

It is intended to be friendlier than `doctor` while still surfacing real problems.

## Supported flags

| Flag | Description |
| --- | --- |
| `--advanced` | Include internal finding IDs in the output |
| `--fix <number|all|finding-id>` | Apply a safe automatic repair |

## Typical output areas

Depending on your config, `status` can summarize provider posture, tool and file boundaries, service exposure, approvals, devices, and other runtime constraints.

## When to use `doctor` instead

If startup is failing, you need JSON diagnostics, or you want filtered/probe-based readiness checks, use `or3-intern doctor`.
