# Skills Workflow

The current skills workflow is:

## 1. Search

```bash
or3-intern skills search web-scraper
```

## 2. Inspect with `info`

```bash
or3-intern skills info web-scraper
```

This is where you check metadata, permission state, and policy notes before installation.

## 3. Install

```bash
or3-intern skills install demo --version 1.0.0
```

## 4. Validate and filter for runnable skills

```bash
or3-intern skills check
or3-intern skills list --eligible
```

This matters because a skill can be discovered but not currently eligible to run due to policy, trust, or quarantine state.

## 5. Use the skill naturally

Once installed and eligible, ask the agent to do work the skill supports.

## 6. Update or remove managed installs

```bash
or3-intern skills update demo
or3-intern skills remove demo
```

Use `--force` on install/update only when you intentionally want to overwrite local modifications.
